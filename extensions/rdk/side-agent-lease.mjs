import { randomUUID } from "node:crypto";
import { chmod, lstat, mkdir, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";

import { parseSideAgentLimit } from "./side-agent-session.mjs";

export function sideAgentOwnerIsAlive(pid) {
  if (!Number.isInteger(pid) || pid <= 0) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return error?.code === "EPERM";
  }
}

export async function acquireSideAgentLease({
  limitValue = process.env.HOBOT_CODE_MAX_SIDE_AGENTS,
  registryDir,
  pid = process.pid,
  uid = typeof process.getuid === "function" ? process.getuid() : "user",
  ownerIsAlive = sideAgentOwnerIsAlive,
} = {}) {
  const limit = parseSideAgentLimit(limitValue);
  const resolvedRegistryDir = registryDir ?? join(tmpdir(), `hobot-code-side-agents-${uid}`);
  await mkdir(resolvedRegistryDir, { recursive: true, mode: 0o700 });
  const registryStats = await lstat(resolvedRegistryDir);
  if (!registryStats.isDirectory() || registryStats.isSymbolicLink()) {
    throw new Error(`Unsafe side-agent registry: ${resolvedRegistryDir}`);
  }
  if (typeof uid === "number" && registryStats.uid !== uid) {
    throw new Error(`Side-agent registry is owned by uid ${registryStats.uid}, expected ${uid}`);
  }
  await chmod(resolvedRegistryDir, 0o700);

  const claimDir = join(resolvedRegistryDir, `claim-${pid}-${randomUUID()}`);
  await mkdir(claimDir, { mode: 0o700 });
  let released = false;
  const release = async () => {
    if (released) return;
    released = true;
    await rm(claimDir, { recursive: true, force: true });
  };

  try {
    const entries = await readdir(resolvedRegistryDir, { withFileTypes: true });
    await Promise.all(entries.map(async (entry) => {
      if (!entry.isDirectory()) return;
      const match = /^claim-(\d+)-/.exec(entry.name);
      if (!match || entry.name === basename(claimDir)) return;
      if (!ownerIsAlive(Number(match[1]))) {
        await rm(join(resolvedRegistryDir, entry.name), { recursive: true, force: true });
      }
    }));
    const activeCount = (await readdir(resolvedRegistryDir, { withFileTypes: true }))
      .filter((entry) => entry.isDirectory() && /^claim-\d+-/.test(entry.name))
      .length;
    if (activeCount > limit) {
      await release();
      throw new Error(
        `This board already has ${limit} side agents. Close one or raise HOBOT_CODE_MAX_SIDE_AGENTS (maximum 8).`,
      );
    }
    return { activeCount, limit, release };
  } catch (error) {
    await release();
    throw error;
  }
}
