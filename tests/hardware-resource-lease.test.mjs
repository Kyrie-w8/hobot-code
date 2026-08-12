import assert from "node:assert/strict";
import { mkdtemp, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  acquireHardwareResourceLease,
  hardwareResourcesForTool,
} from "../extensions/rdk/hardware-resource-lease.mjs";

test("hardware resource detection is explicit and device-specific", () => {
  assert.deepEqual(hardwareResourcesForTool("bash", {command: "hrt_model_exec model.hbm"}), ["bpu"]);
  assert.deepEqual(hardwareResourcesForTool("bash", {command: "v4l2-ctl -d /dev/video2 --stream-mmap"}), ["camera-video2"]);
  assert.deepEqual(hardwareResourcesForTool("bash", {command: "hbn_vflow camera.json /dev/video0"}), ["camera-video0", "media-pipeline"]);
  assert.deepEqual(hardwareResourcesForTool("bash", {command: "python3 train.py && ffmpeg -i input.mp4 output.mp4"}), []);
  assert.deepEqual(hardwareResourcesForTool("read", {path: "/dev/video0"}), []);
});

test("hardware leases reject conflicts and release capacity", async (t) => {
  const registryDir = await mkdtemp(join(tmpdir(), "hobot-hardware-lease-"));
  t.after(() => rm(registryDir, {recursive: true, force: true}));
  const options = {registryDir, uid: "test", ownerIsAlive: () => true, cwd: "/work"};
  const first = await acquireHardwareResourceLease({...options, resources: ["bpu"], taskId: "task-a", toolCallId: "tool-a", pid: 101});
  await assert.rejects(
    acquireHardwareResourceLease({...options, resources: ["bpu"], taskId: "task-b", toolCallId: "tool-b", pid: 102}),
    /resource bpu is busy: task task-a \(PID 101\)/,
  );
  const camera = await acquireHardwareResourceLease({...options, resources: ["camera-video0"], taskId: "task-b", toolCallId: "tool-c", pid: 102});
  await first.release();
  const replacement = await acquireHardwareResourceLease({...options, resources: ["bpu"], taskId: "task-b", toolCallId: "tool-d", pid: 102});
  await Promise.all([camera.release(), replacement.release()]);
});

test("multi-resource failure rolls back partial claims and stale owners are reclaimed", async (t) => {
  const registryDir = await mkdtemp(join(tmpdir(), "hobot-hardware-rollback-"));
  t.after(() => rm(registryDir, {recursive: true, force: true}));
  const ownerIsAlive = (pid) => pid !== 404;
  const camera = await acquireHardwareResourceLease({registryDir, resources: ["camera-video0"], taskId: "camera", pid: 201, uid: "test", ownerIsAlive: () => true});
  await assert.rejects(
    acquireHardwareResourceLease({registryDir, resources: ["bpu", "camera-video0"], taskId: "combo", pid: 202, uid: "test", ownerIsAlive: () => true}),
    /camera-video0 is busy/,
  );
  assert.equal((await readdir(registryDir)).includes("bpu"), false, "partial BPU lease was not rolled back");
  await camera.release();

  const stale = await acquireHardwareResourceLease({registryDir, resources: ["bpu"], taskId: "stale", pid: 404, uid: "test", ownerIsAlive: () => true});
  // Simulate a crashed process by leaving its directory and acquiring with a liveness check that marks it dead.
  const replacement = await acquireHardwareResourceLease({registryDir, resources: ["bpu"], taskId: "live", pid: 203, uid: "test", ownerIsAlive});
  await stale.release();
  assert.equal((await readdir(registryDir)).includes("bpu"), true, "stale owner released the replacement lease");
  await stale.release();
  await replacement.release();
});
