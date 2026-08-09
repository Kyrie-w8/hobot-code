import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { chmod, copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { promisify } from "node:util";

import { resolveUserPaths } from "../extensions/rdk/user-paths.mjs";

const execFileAsync = promisify(execFile);

test("Hobot Code defaults config and mutable state to the current user", () => {
  const paths = resolveUserPaths({}, "/home/rdk");
  assert.equal(paths.configRoot, "/home/rdk/.config/hobot-code");
  assert.equal(paths.agentDir, "/home/rdk/.config/hobot-code/agent");
  assert.equal(paths.stateRoot, "/home/rdk/.local/state/hobot-code");
  assert.equal(paths.memoryDatabase, "/home/rdk/.local/state/hobot-code/memory/memory.db");
  assert.equal(paths.goalDatabase, "/home/rdk/.local/state/hobot-code/goals/goals.db");
});

test("XDG and explicit path overrides remain supported", () => {
  const paths = resolveUserPaths({
    XDG_CONFIG_HOME: "/cfg",
    XDG_STATE_HOME: "/state",
    HOBOT_CODING_AGENT_DIR: "/managed/agent",
    HOBOT_CODE_STATE_DIR: "/managed/state",
  }, "/home/rdk");
  assert.equal(paths.configRoot, "/cfg/hobot-code");
  assert.equal(paths.agentDir, "/managed/agent");
  assert.equal(paths.stateRoot, "/managed/state");
});

test("packaged settings and launcher do not default to system config or state", async () => {
  const settings = JSON.parse(await readFile(new URL("../packaging/pi/settings.json", import.meta.url), "utf8"));
  const launcher = await readFile(new URL("../packaging/pi/hobot-launcher", import.meta.url), "utf8");
  assert.equal(settings.sessionDir, undefined);
  assert.doesNotMatch(launcher, /\/etc\/hobot-code|\/var\/lib\/hobot-code/);
  assert.match(launcher, /XDG_CONFIG_HOME/);
  assert.match(launcher, /XDG_STATE_HOME/);
});

test("launcher initializes an isolated user without system configuration", async () => {
  const root = await mkdtemp(join(tmpdir(), "hobot-user-layout-"));
  try {
    const runtime = join(root, "runtime");
    const defaults = join(runtime, "default-config");
    const home = join(root, "home");
    await mkdir(join(runtime, "bin"), { recursive: true });
    await mkdir(defaults, { recursive: true });
    await mkdir(home, { recursive: true });
    for (const name of ["settings.json", "models.json", "permissions.json", "memory.json", "goals.json", "hooks.json", "notifications.json", "lsp.json"]) {
      await copyFile(new URL(`../packaging/pi/${name}`, import.meta.url), join(defaults, name));
    }
    await copyFile(new URL("../packaging/pi/hobot.env.example", import.meta.url), join(defaults, "hobot.env.example"));
    await writeFile(join(runtime, "hobot"), "#!/bin/sh\nprintf '%s\\n' \"$HOBOT_CODING_AGENT_DIR\" \"$HOBOT_CODING_AGENT_SESSION_DIR\" \"$HOBOT_CODE_MEMORY_DB\"\n");
    await chmod(join(runtime, "hobot"), 0o755);
    const source = await readFile(new URL("../packaging/pi/hobot-launcher", import.meta.url), "utf8");
    const launcher = join(root, "hobot-launcher");
    await writeFile(launcher, source.replaceAll("/usr/local/lib/hobot-code", runtime));
    await chmod(launcher, 0o755);

    const { stdout } = await execFileAsync(launcher, [], {
      env: { HOME: home, PATH: process.env.PATH ?? "/usr/bin:/bin" },
    });
    const paths = stdout.trim().split("\n");
    assert.deepEqual(paths, [
      join(home, ".config/hobot-code/agent"),
      join(home, ".local/state/hobot-code/sessions"),
      join(home, ".local/state/hobot-code/memory/memory.db"),
    ]);
    assert.equal(JSON.parse(await readFile(join(paths[0], "settings.json"), "utf8")).defaultProvider, "drobotics");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
