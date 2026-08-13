import { randomUUID } from "node:crypto";
import { chmod, lstat, mkdir, readFile, readdir, realpath, rm, writeFile } from "node:fs/promises";
import { dirname, isAbsolute, join, relative, resolve } from "node:path";

const LEASE_DIRECTORY = /^lease-[0-9a-f-]{36}$/;
const MAXIMUM_LEASES = 128;
const MAXIMUM_OWNER_BYTES = 16 * 1024;

export function workspaceLeaseOwnerIsAlive(pid) {
  if (!Number.isInteger(pid) || pid <= 0) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return error?.code === "EPERM";
  }
}

function pathsOverlap(left, right) {
  const leftToRight = relative(left, right);
  const rightToLeft = relative(right, left);
  return leftToRight === "" || (!leftToRight.startsWith("..") && !isAbsolute(leftToRight))
    || (!rightToLeft.startsWith("..") && !isAbsolute(rightToLeft));
}

async function workspaceLeaseRoot(cwd) {
  const physicalCwd = await realpath(resolve(cwd));
  let current = physicalCwd;
  while (true) {
    try {
      const marker = await lstat(join(current, ".git"));
      if (!marker.isSymbolicLink() && (marker.isDirectory() || marker.isFile())) return current;
    } catch (error) {
      if (error?.code !== "ENOENT") throw error;
    }
    const parent = dirname(current);
    if (parent === current) return physicalCwd;
    current = parent;
  }
}

async function prepareRegistry(registryDir, uid) {
  await mkdir(registryDir, {recursive: true, mode: 0o700});
  const info = await lstat(registryDir);
  if (!info.isDirectory() || info.isSymbolicLink()) throw new Error(`Unsafe workspace lease registry: ${registryDir}`);
  if (typeof uid === "number" && info.uid !== uid) {
    throw new Error(`Workspace lease registry is owned by uid ${info.uid}, expected ${uid}`);
  }
  await chmod(registryDir, 0o700);
}

async function readOwner(leaseDir) {
  try {
    const path = join(leaseDir, "owner.json");
    const info = await lstat(path);
    if (!info.isFile() || info.isSymbolicLink() || info.size > MAXIMUM_OWNER_BYTES || info.mode & 0o077) return undefined;
    const value = JSON.parse(await readFile(path, "utf8"));
    if (value?.schemaVersion !== 1 || typeof value.leaseId !== "string" || value.leaseId.length > 64
      || !Number.isInteger(value.pid) || value.pid <= 0 || typeof value.taskId !== "string"
      || value.taskId.length < 1 || value.taskId.length > 128 || typeof value.cwd !== "string"
      || !isAbsolute(value.cwd) || value.cwd.length > 4096 || typeof value.acquiredAt !== "string") return undefined;
    return value;
  } catch {
    return undefined;
  }
}

async function readLockOwner(lockDir) {
  try {
    const path = join(lockDir, "owner.json");
    const info = await lstat(path);
    if (!info.isFile() || info.isSymbolicLink() || info.size > MAXIMUM_OWNER_BYTES || info.mode & 0o077) return undefined;
    const value = JSON.parse(await readFile(path, "utf8"));
    if (value?.schemaVersion !== 1 || typeof value.leaseId !== "string"
      || !Number.isInteger(value.pid) || value.pid <= 0) return undefined;
    return value;
  } catch {
    return undefined;
  }
}

async function acquireRegistryLock(registryDir, pid, ownerIsAlive) {
  const lockDir = join(registryDir, ".registry-lock");
  for (let attempt = 0; attempt < 100; attempt += 1) {
    try {
      await mkdir(lockDir, {mode: 0o700});
      const leaseId = randomUUID();
      try {
        await writeFile(join(lockDir, "owner.json"), `${JSON.stringify({schemaVersion: 1, pid, leaseId})}\n`, {mode: 0o600, flag: "wx"});
      } catch (error) {
        await rm(lockDir, {recursive: true, force: true});
        throw error;
      }
      return async () => {
        const owner = await readLockOwner(lockDir);
        if (owner?.leaseId === leaseId) await rm(lockDir, {recursive: true, force: true});
      };
    } catch (error) {
      if (error?.code !== "EEXIST") throw error;
      const owner = await readLockOwner(lockDir);
      const ageMs = await lstat(lockDir).then((info) => Date.now() - info.mtimeMs).catch(() => 0);
      if ((owner && !ownerIsAlive(owner.pid)) || (!owner && ageMs > 10_000)) {
        await rm(lockDir, {recursive: true, force: true});
        continue;
      }
      await new Promise((done) => setTimeout(done, 25));
    }
  }
  throw new Error("Timed out waiting for the workspace lease registry");
}

function busyMessage(owner) {
  if (!owner) return "This workspace is being changed by another Agent, but its owner metadata is unavailable.";
  const since = Number.isFinite(Date.parse(owner.acquiredAt)) ? ` since ${owner.acquiredAt}` : "";
  return `Workspace writes are busy: task ${owner.taskId} (PID ${owner.pid})${since} is changing ${owner.cwd}. Wait for that Agent turn to finish or stop its task.`;
}

export async function acquireWorkspaceWriteLease({
  registryDir,
  cwd,
  taskId,
  pid = process.pid,
  uid = typeof process.getuid === "function" ? process.getuid() : "user",
  ownerIsAlive = workspaceLeaseOwnerIsAlive,
} = {}) {
  if (!registryDir) throw new Error("Workspace lease registry path is required");
  if (!cwd) throw new Error("Workspace lease path is required");
  const physicalCwd = await workspaceLeaseRoot(cwd);
  await prepareRegistry(registryDir, uid);
  const unlock = await acquireRegistryLock(registryDir, pid, ownerIsAlive);
  let claimDir;
  let leaseId;
  try {
    const entries = await readdir(registryDir, {withFileTypes: true});
    if (entries.length > MAXIMUM_LEASES + 8) throw new Error("Workspace lease registry has too many entries; run Hobot Code diagnostics.");
    let liveLeases = 0;
    for (const entry of entries) {
      if (!entry.isDirectory() || !LEASE_DIRECTORY.test(entry.name)) continue;
      const leaseDir = join(registryDir, entry.name);
      const owner = await readOwner(leaseDir);
      if (!owner) {
		const ageMs = await lstat(leaseDir).then((info) => Date.now() - info.mtimeMs).catch(() => 0);
		if (ageMs <= 10_000) throw new Error(busyMessage(undefined));
		await rm(leaseDir, {recursive: true, force: true});
		continue;
	  }
	  if (!ownerIsAlive(owner.pid)) {
        await rm(leaseDir, {recursive: true, force: true});
        continue;
      }
      liveLeases += 1;
      if (pathsOverlap(physicalCwd, owner.cwd)) throw new Error(busyMessage(owner));
    }
    if (liveLeases >= MAXIMUM_LEASES) throw new Error("Workspace write capacity is full; stop an inactive task and retry.");

    leaseId = randomUUID();
    claimDir = join(registryDir, `lease-${leaseId}`);
    await mkdir(claimDir, {mode: 0o700});
    const owner = {
      schemaVersion: 1,
      leaseId,
      taskId: String(taskId || `process-${pid}`).slice(0, 128),
      pid,
      cwd: physicalCwd,
      acquiredAt: new Date().toISOString(),
    };
    try {
      await writeFile(join(claimDir, "owner.json"), `${JSON.stringify(owner)}\n`, {mode: 0o600, flag: "wx"});
    } catch (error) {
      await rm(claimDir, {recursive: true, force: true});
      throw error;
    }
  } finally {
    await unlock();
  }

  let releasePromise;
  const release = () => {
    if (releasePromise) return releasePromise;
    releasePromise = (async () => {
      const releaseUnlock = await acquireRegistryLock(registryDir, pid, ownerIsAlive);
      try {
        const owner = await readOwner(claimDir);
        if (owner?.leaseId === leaseId) await rm(claimDir, {recursive: true, force: true});
      } finally {
        await releaseUnlock();
      }
    })();
    return releasePromise;
  };
  return {cwd: physicalCwd, release};
}
