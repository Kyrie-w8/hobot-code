import assert from "node:assert/strict";
import { mkdtemp, mkdir, readdir, realpath, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { acquireWorkspaceWriteLease } from "../extensions/rdk/workspace-write-lease.mjs";

test("workspace write leases serialize overlapping projects and release cleanly", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-workspace-lease-"));
  t.after(() => rm(root, {recursive: true, force: true}));
  const registryDir = join(root, "leases");
  const project = join(root, "project");
  const nested = join(project, "src");
  const sibling = join(root, "sibling");
  await Promise.all([mkdir(join(project, ".git"), {recursive: true}), mkdir(nested, {recursive: true}), mkdir(sibling)]);
  const options = {registryDir, uid: "test", ownerIsAlive: () => true};

  const first = await acquireWorkspaceWriteLease({...options, cwd: nested, taskId: "task-a", pid: 101});
	assert.equal(first.cwd, await realpath(project), "nested Git projects must lease the repository root");
  await assert.rejects(
    acquireWorkspaceWriteLease({...options, cwd: project, taskId: "task-b", pid: 102}),
    /task task-a \(PID 101\).*changing/,
  );
  const independent = await acquireWorkspaceWriteLease({...options, cwd: sibling, taskId: "task-c", pid: 103});
  await first.release();
  const replacement = await acquireWorkspaceWriteLease({...options, cwd: project, taskId: "task-b", pid: 102});
  await Promise.all([independent.release(), replacement.release()]);
});

test("workspace write leases reclaim crashed owners without letting stale handles delete replacements", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-workspace-stale-"));
  t.after(() => rm(root, {recursive: true, force: true}));
  const registryDir = join(root, "leases");
  const project = join(root, "project");
  await mkdir(project);

  const stale = await acquireWorkspaceWriteLease({registryDir, cwd: project, taskId: "stale", pid: 404, uid: "test", ownerIsAlive: () => true});
  const replacement = await acquireWorkspaceWriteLease({
    registryDir,
    cwd: project,
    taskId: "live",
    pid: 405,
    uid: "test",
    ownerIsAlive: (pid) => pid !== 404,
  });
  await stale.release();
  await assert.rejects(
    acquireWorkspaceWriteLease({registryDir, cwd: project, taskId: "blocked", pid: 406, uid: "test", ownerIsAlive: () => true}),
    /task live \(PID 405\)/,
  );
  await replacement.release();
});

test("workspace write leases fail closed on unsafe registry and owner metadata", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-workspace-unsafe-"));
  t.after(() => rm(root, {recursive: true, force: true}));
  const project = join(root, "project");
  const registryDir = join(root, "leases");
  await Promise.all([mkdir(project), mkdir(registryDir, {mode: 0o700})]);
  const first = await acquireWorkspaceWriteLease({registryDir, cwd: project, taskId: "first", pid: 501, uid: "test", ownerIsAlive: () => true});
	const entries = await readdir(registryDir);
  const claim = entries.find((entry) => entry.startsWith("lease-"));
  await writeFile(join(registryDir, claim, "owner.json"), "not-json\n", {mode: 0o600});
  await assert.rejects(
    acquireWorkspaceWriteLease({registryDir, cwd: project, taskId: "second", pid: 502, uid: "test", ownerIsAlive: () => true}),
	/owner metadata is unavailable/,
	);
  await first.release();
});
