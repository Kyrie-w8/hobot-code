import { chmod, lstat, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { basename, join } from "node:path";
import { randomUUID } from "node:crypto";

const BPU_COMMAND = /(?:^|[;&|()\s])(?:\S*\/)?(?:hrt_model_exec|hrt_model_inference|hb_perf|horizon_tc_ui)\b|\/dev\/bpu(?:_core\d+)?\b/i;
const MEDIA_COMMAND = /(?:^|[;&|()\s])(?:\S*\/)?(?:hbn_(?:vflow|vnode|vin|vot)|vp_(?:wrap|display)|vflow|vnode)\b/i;
const CAMERA_DEVICE = /\/dev\/video\d+\b/g;
const RESOURCE_NAME = /^(?:bpu|media-pipeline|camera-video\d+)$/;

export function hardwareResourcesForTool(toolName, input) {
  if (toolName !== "bash" || !input || typeof input.command !== "string") return [];
  const command = input.command;
  const resources = new Set();
  if (BPU_COMMAND.test(command)) resources.add("bpu");
  if (MEDIA_COMMAND.test(command)) resources.add("media-pipeline");
  for (const device of command.match(CAMERA_DEVICE) ?? []) {
    resources.add(`camera-${basename(device)}`);
  }
  return [...resources].sort();
}

export function hardwareLeaseOwnerIsAlive(pid) {
  if (!Number.isInteger(pid) || pid <= 0) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return error?.code === "EPERM";
  }
}

function validateResource(resource) {
  if (!RESOURCE_NAME.test(resource)) throw new Error(`Invalid hardware resource name: ${resource}`);
  return resource;
}

async function prepareRegistry(registryDir, uid) {
  await mkdir(registryDir, { recursive: true, mode: 0o700 });
  const info = await lstat(registryDir);
  if (!info.isDirectory() || info.isSymbolicLink()) throw new Error(`Unsafe hardware lease registry: ${registryDir}`);
  if (typeof uid === "number" && info.uid !== uid) {
    throw new Error(`Hardware lease registry is owned by uid ${info.uid}, expected ${uid}`);
  }
  await chmod(registryDir, 0o700);
}

async function readOwner(leaseDir) {
  try {
    const value = JSON.parse(await readFile(join(leaseDir, "owner.json"), "utf8"));
    if (!Number.isInteger(value.pid) || value.pid <= 0 || typeof value.taskId !== "string") return undefined;
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
        await writeFile(join(lockDir, "owner.json"), `${JSON.stringify({pid, taskId: "registry-lock", leaseId})}\n`, {mode: 0o600, flag: "wx"});
      } catch (error) {
        await rm(lockDir, {recursive: true, force: true});
        throw error;
      }
      return async () => {
        const owner = await readOwner(lockDir);
        if (owner?.leaseId === leaseId) await rm(lockDir, {recursive: true, force: true});
      };
    } catch (error) {
      if (error?.code !== "EEXIST") throw error;
      const owner = await readOwner(lockDir);
      const ageMs = await lstat(lockDir).then((info) => Date.now() - info.mtimeMs).catch(() => 0);
      if ((owner && !ownerIsAlive(owner.pid)) || (!owner && ageMs > 10_000)) {
        await rm(lockDir, {recursive: true, force: true});
        continue;
      }
      await new Promise((resolve) => setTimeout(resolve, 25));
    }
  }
  throw new Error("Timed out waiting for the hardware lease registry");
}

function busyMessage(resource, owner) {
  if (!owner) return `Hardware resource ${resource} is busy and its owner metadata is unavailable.`;
  const since = typeof owner.acquiredAt === "string" ? ` since ${owner.acquiredAt}` : "";
  return `Hardware resource ${resource} is busy: task ${owner.taskId || "unknown"} (PID ${owner.pid})${since}. Wait for that tool to finish or stop its task.`;
}

export async function acquireHardwareResourceLease({
  resources,
  registryDir,
  taskId,
  cwd,
  toolCallId,
  pid = process.pid,
  uid = typeof process.getuid === "function" ? process.getuid() : "user",
  ownerIsAlive = hardwareLeaseOwnerIsAlive,
} = {}) {
  const requested = [...new Set((resources ?? []).map(validateResource))].sort();
  if (requested.length === 0) return { resources: [], release: async () => undefined };
  if (!registryDir) throw new Error("Hardware lease registry path is required");
  await prepareRegistry(registryDir, uid);

  const unlock = await acquireRegistryLock(registryDir, pid, ownerIsAlive);

  const acquired = [];
  let releasePromise;
  const release = () => {
    if (releasePromise) return releasePromise;
    releasePromise = (async () => {
      const releaseUnlock = await acquireRegistryLock(registryDir, pid, ownerIsAlive);
      try {
        await Promise.all(acquired.map(async ({leaseDir, leaseId}) => {
          const owner = await readOwner(leaseDir);
          if (owner?.leaseId === leaseId) await rm(leaseDir, {recursive: true, force: true});
        }));
      } finally {
        await releaseUnlock();
      }
    })();
    return releasePromise;
  };
  try {
    for (const resource of requested) {
      const leaseDir = join(registryDir, resource);
      let claimed = false;
      for (let attempt = 0; attempt < 2 && !claimed; attempt += 1) {
        try {
          await mkdir(leaseDir, { mode: 0o700 });
          const leaseId = randomUUID();
          const owner = {
            schemaVersion: 1,
            leaseId,
            resource,
            taskId: String(taskId || `process-${pid}`).slice(0, 128),
            pid,
            cwd: String(cwd || "").slice(0, 4096),
            toolCallId: String(toolCallId || "").slice(0, 128),
            acquiredAt: new Date().toISOString(),
          };
          try {
            await writeFile(join(leaseDir, "owner.json"), `${JSON.stringify(owner)}\n`, { mode: 0o600, flag: "wx" });
          } catch (error) {
            await rm(leaseDir, {recursive: true, force: true});
            throw error;
          }
          acquired.push({leaseDir, leaseId});
          claimed = true;
        } catch (error) {
          if (error?.code !== "EEXIST") throw error;
          const owner = await readOwner(leaseDir);
          if (attempt === 0 && (!owner || !ownerIsAlive(owner.pid))) {
            await rm(leaseDir, { recursive: true, force: true });
            continue;
          }
          throw new Error(busyMessage(resource, owner));
        }
      }
    }
    await unlock();
    return { resources: requested, release };
  } catch (error) {
    await unlock();
    await release();
    throw error;
  }
}
