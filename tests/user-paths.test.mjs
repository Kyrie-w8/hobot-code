import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { access, chmod, copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { promisify } from "node:util";

import { resolveUserPaths } from "../extensions/rdk/user-paths.mjs";

const execFileAsync = promisify(execFile);
const PATH_OVERRIDE_NAMES = [
  "XDG_CONFIG_HOME",
  "XDG_STATE_HOME",
  "HOBOT_CODE_CONFIG_DIR",
  "HOBOT_CODING_AGENT_DIR",
  "HOBOT_CODE_STATE_DIR",
  "HOBOT_CODING_AGENT_SESSION_DIR",
  "HOBOT_CODE_PERMISSION_POLICY",
  "HOBOT_CODE_MEMORY_CONFIG",
  "HOBOT_CODE_MEMORY_DB",
  "HOBOT_CODE_GOAL_CONFIG",
  "HOBOT_CODE_GOAL_DB",
  "HOBOT_CODE_HOOK_CONFIG",
  "HOBOT_CODE_HOOK_AUDIT",
  "HOBOT_CODE_NOTIFICATION_CONFIG",
  "HOBOT_CODE_LSP_CONFIG",
  "HOBOT_CODE_RDK_KNOWLEDGE_DIR",
  "HOBOT_CODE_RDK_EXPERT_PROMPT",
];
const EXTENSION_PATH_FIELDS = [
  "stateRoot",
  "permissionPolicy",
  "memoryConfig",
  "memoryDatabase",
  "goalConfig",
  "goalDatabase",
  "hookConfig",
  "hookAudit",
  "notificationConfig",
  "lspConfig",
  "rdkKnowledgeDir",
  "rdkExpertPrompt",
];

async function createLauncherFixture(prefix) {
  const root = await mkdtemp(join(tmpdir(), prefix));
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
  await writeFile(join(runtime, "hobot"), "#!/bin/sh\nprintf '%s\\n' \"$HOBOT_CODING_AGENT_DIR\" \"$HOBOT_CODING_AGENT_SESSION_DIR\" \"$HOBOT_CODE_MEMORY_DB\"\nfor hobot_arg in \"$@\"; do printf 'arg=<%s>\\n' \"$hobot_arg\"; done\n");
  await chmod(join(runtime, "hobot"), 0o755);
  const source = await readFile(new URL("../packaging/pi/hobot-launcher", import.meta.url), "utf8");
  const launcher = join(root, "hobot-launcher");
  await writeFile(launcher, source.replaceAll("/usr/local/lib/hobot-code", runtime));
  await chmod(launcher, 0o755);
  return { root, home, launcher };
}

async function installFakeTmux(root) {
  const binDir = join(root, "fake-bin");
  const tmux = join(binDir, "tmux");
  await mkdir(binDir, { recursive: true });
  await writeFile(tmux, `#!/bin/sh
case "\$1" in
  has-session)
    [ "\${FAKE_TMUX_SESSION_EXISTS:-0}" = 1 ]
    ;;
  list-sessions)
    printf 'hobot-code-main|1|1|2026-08-10 12:00:00\\nother-work|1|2|2026-08-09 10:00:00\\n'
    ;;
  new-session)
    if [ "\${FAKE_TMUX_RUN_COMMAND:-0}" = 1 ]; then
      for fake_tmux_arg in "\$@"; do fake_tmux_command=\$fake_tmux_arg; done
      exec /bin/sh -c "\$fake_tmux_command"
    fi
    printf '<%s>\\n' "\$@"
    ;;
  attach-session|switch-client|kill-session)
    printf '<%s>\\n' "\$@"
    ;;
  *) exit 2 ;;
esac
`);
  await chmod(tmux, 0o755);
  return binDir;
}

test("Hobot Code defaults config and mutable state to the current user", () => {
  const paths = resolveUserPaths({}, "/home/rdk");
  assert.equal(paths.configRoot, "/home/rdk/.config/hobot-code");
  assert.equal(paths.agentDir, "/home/rdk/.config/hobot-code/agent");
  assert.equal(paths.stateRoot, "/home/rdk/.local/state/hobot-code");
  assert.equal(paths.sessionDir, "/home/rdk/.local/state/hobot-code/sessions");
  assert.equal(paths.permissionPolicy, "/home/rdk/.config/hobot-code/agent/permissions.json");
  assert.equal(paths.memoryConfig, "/home/rdk/.config/hobot-code/agent/memory.json");
  assert.equal(paths.memoryDatabase, "/home/rdk/.local/state/hobot-code/memory/memory.db");
  assert.equal(paths.goalConfig, "/home/rdk/.config/hobot-code/agent/goals.json");
  assert.equal(paths.goalDatabase, "/home/rdk/.local/state/hobot-code/goals/goals.db");
  assert.equal(paths.hookConfig, "/home/rdk/.config/hobot-code/agent/hooks.json");
  assert.equal(paths.hookAudit, "/home/rdk/.local/state/hobot-code/audit/hooks.jsonl");
  assert.equal(paths.notificationConfig, "/home/rdk/.config/hobot-code/agent/notifications.json");
  assert.equal(paths.lspConfig, "/home/rdk/.config/hobot-code/agent/lsp.json");
  assert.equal(paths.rdkKnowledgeDir, "/usr/local/lib/hobot-code/knowledge");
  assert.equal(paths.rdkExpertPrompt, "/usr/local/lib/hobot-code/prompts/rdk-expert.md");
});

test("XDG and explicit path overrides remain supported", () => {
  const xdgPaths = resolveUserPaths({
    XDG_CONFIG_HOME: "/cfg",
    XDG_STATE_HOME: "/state",
  }, "/home/rdk");
  assert.equal(xdgPaths.configRoot, "/cfg/hobot-code");
  assert.equal(xdgPaths.stateRoot, "/state/hobot-code");

  const overrides = {
    XDG_CONFIG_HOME: "/cfg",
    XDG_STATE_HOME: "/state",
    HOBOT_CODE_CONFIG_DIR: "/managed/config",
    HOBOT_CODING_AGENT_DIR: "/managed/agent",
    HOBOT_CODE_STATE_DIR: "/managed/state",
    HOBOT_CODING_AGENT_SESSION_DIR: "/managed/sessions",
    HOBOT_CODE_PERMISSION_POLICY: "/managed/files/permissions.json",
    HOBOT_CODE_MEMORY_CONFIG: "/managed/files/memory.json",
    HOBOT_CODE_MEMORY_DB: "/managed/files/memory.db",
    HOBOT_CODE_GOAL_CONFIG: "/managed/files/goals.json",
    HOBOT_CODE_GOAL_DB: "/managed/files/goals.db",
    HOBOT_CODE_HOOK_CONFIG: "/managed/files/hooks.json",
    HOBOT_CODE_HOOK_AUDIT: "/managed/files/hooks.jsonl",
    HOBOT_CODE_NOTIFICATION_CONFIG: "/managed/files/notifications.json",
    HOBOT_CODE_LSP_CONFIG: "/managed/files/lsp.json",
    HOBOT_CODE_RDK_KNOWLEDGE_DIR: "/managed/knowledge",
    HOBOT_CODE_RDK_EXPERT_PROMPT: "/managed/prompts/rdk-expert.md",
  };
  const paths = resolveUserPaths(overrides, "/home/rdk");
  assert.equal(paths.configRoot, overrides.HOBOT_CODE_CONFIG_DIR);
  assert.equal(paths.agentDir, "/managed/agent");
  assert.equal(paths.stateRoot, "/managed/state");
  assert.equal(paths.sessionDir, overrides.HOBOT_CODING_AGENT_SESSION_DIR);
  assert.equal(paths.permissionPolicy, overrides.HOBOT_CODE_PERMISSION_POLICY);
  assert.equal(paths.memoryConfig, overrides.HOBOT_CODE_MEMORY_CONFIG);
  assert.equal(paths.memoryDatabase, overrides.HOBOT_CODE_MEMORY_DB);
  assert.equal(paths.goalConfig, overrides.HOBOT_CODE_GOAL_CONFIG);
  assert.equal(paths.goalDatabase, overrides.HOBOT_CODE_GOAL_DB);
  assert.equal(paths.hookConfig, overrides.HOBOT_CODE_HOOK_CONFIG);
  assert.equal(paths.hookAudit, overrides.HOBOT_CODE_HOOK_AUDIT);
  assert.equal(paths.notificationConfig, overrides.HOBOT_CODE_NOTIFICATION_CONFIG);
  assert.equal(paths.lspConfig, overrides.HOBOT_CODE_LSP_CONFIG);
  assert.equal(paths.rdkKnowledgeDir, overrides.HOBOT_CODE_RDK_KNOWLEDGE_DIR);
  assert.equal(paths.rdkExpertPrompt, overrides.HOBOT_CODE_RDK_EXPERT_PROMPT);
});

test("all relative runtime path overrides are rejected instead of resolved from cwd", () => {
  assert.throws(() => resolveUserPaths({}, "relative-home"), /HOME must be an absolute path/);
  for (const name of PATH_OVERRIDE_NAMES) {
    assert.throws(
      () => resolveUserPaths({ [name]: "relative/path" }, "/home/rdk"),
      (error) => error instanceof Error && error.message.includes(`${name} must be an absolute path`),
      name,
    );
  }
});

test("RDK extension delegates runtime paths to the shared fail-closed resolver", async () => {
  const source = await readFile(new URL("../extensions/rdk/index.ts", import.meta.url), "utf8");
  assert.match(
    source,
    /export default function rdkExtension\([^)]*\)\s*\{\s*(?:\/\/[^\n]*\n\s*)?resolveUserPaths\(\);/,
  );
  for (const field of EXTENSION_PATH_FIELDS) {
    assert.match(source, new RegExp(`resolveUserPaths\\(\\)\\.${field}\\b`), field);
  }
  assert.doesNotMatch(
    source,
    /process\.env\.(?:XDG_CONFIG_HOME|XDG_STATE_HOME|HOBOT_CODE_CONFIG_DIR|HOBOT_CODING_AGENT_DIR|HOBOT_CODE_STATE_DIR|HOBOT_CODING_AGENT_SESSION_DIR|HOBOT_CODE_PERMISSION_POLICY|HOBOT_CODE_MEMORY_CONFIG|HOBOT_CODE_MEMORY_DB|HOBOT_CODE_GOAL_CONFIG|HOBOT_CODE_GOAL_DB|HOBOT_CODE_HOOK_CONFIG|HOBOT_CODE_HOOK_AUDIT|HOBOT_CODE_NOTIFICATION_CONFIG|HOBOT_CODE_LSP_CONFIG|HOBOT_CODE_RDK_KNOWLEDGE_DIR|HOBOT_CODE_RDK_EXPERT_PROMPT)\b/,
  );
});

test("RDK prompt rendering uses the shared Unicode sanitizer", async () => {
  const source = await readFile(new URL("../extensions/rdk/index.ts", import.meta.url), "utf8");
  assert.match(source, /import \{ toWellFormedText \} from "\.\/text-safety\.mjs";/);
  assert.match(source, /prompt\.replaceAll\(token, \(\) => toWellFormedText\(value\)\)/);
  assert.doesNotMatch(source, /\bsanitizeText\b/);
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
  const fixture = await createLauncherFixture("hobot-user-layout-");
  try {
    const { stdout } = await execFileAsync(fixture.launcher, [], {
      env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" },
    });
    const paths = stdout.trim().split("\n");
    assert.deepEqual(paths, [
      join(fixture.home, ".config/hobot-code/agent"),
      join(fixture.home, ".local/state/hobot-code/sessions"),
      join(fixture.home, ".local/state/hobot-code/memory/memory.db"),
    ]);
    assert.equal(JSON.parse(await readFile(join(paths[0], "settings.json"), "utf8")).defaultProvider, "drobotics");
  } finally {
    await rm(fixture.root, { recursive: true, force: true });
  }
});

test("launcher persistent sessions preserve arguments and shell safety", async () => {
  const fixture = await createLauncherFixture("hobot-persistent-start-");
  try {
    const fakeBin = await installFakeTmux(fixture.root);
    const marker = join(fixture.root, "must-not-run");
    const unsafeArgument = `'; touch ${marker}; #`;
    const { stdout } = await execFileAsync(
      fixture.launcher,
      ["persistent", "start", "review", "--", "--resume", unsafeArgument],
      {
        cwd: fixture.root,
        env: {
          HOME: fixture.home,
          PATH: `${fakeBin}:${process.env.PATH ?? "/usr/bin:/bin"}`,
          FAKE_TMUX_RUN_COMMAND: "1",
        },
      },
    );
    assert.ok(stdout.includes(join(fixture.home, ".config/hobot-code/agent")));
    assert.match(stdout, /arg=<--resume>/);
    assert.ok(stdout.includes(`arg=<${unsafeArgument}>`));
    await assert.rejects(() => access(marker));
  } finally {
    await rm(fixture.root, { recursive: true, force: true });
  }
});

test("launcher persistent management is scoped to Hobot Code sessions", async () => {
  const fixture = await createLauncherFixture("hobot-persistent-manage-");
  try {
    const fakeBin = await installFakeTmux(fixture.root);
    const env = { HOME: fixture.home, PATH: `${fakeBin}:${process.env.PATH ?? "/usr/bin:/bin"}` };
    const started = await execFileAsync(fixture.launcher, ["persistent"], { cwd: fixture.root, env });
    assert.match(started.stdout, /<new-session>\n<-s>\n<hobot-code-main>/);

    const listed = await execFileAsync(fixture.launcher, ["persistent", "list"], { env });
    assert.match(listed.stdout, /^NAME\s+ATTACHED\s+WINDOWS\s+CREATED/m);
    assert.match(listed.stdout, /^main\s+1\s+1\s+2026-08-10/m);
    assert.doesNotMatch(listed.stdout, /other-work/);

    const stopped = await execFileAsync(fixture.launcher, ["persistent", "stop", "review"], {
      env: { ...env, FAKE_TMUX_SESSION_EXISTS: "1" },
    });
    assert.match(stopped.stdout, /<kill-session>\n<-t>\n<=hobot-code-review>/);
    assert.match(stopped.stdout, /Stopped persistent Hobot Code session: review/);

    await assert.rejects(
      () => execFileAsync(fixture.launcher, ["persistent", "start", "../other"], { env }),
      /Persistent session names must start with a letter or digit/,
    );
  } finally {
    await rm(fixture.root, { recursive: true, force: true });
  }
});

test("launcher rejects every relative runtime path override", async () => {
  const fixture = await createLauncherFixture("hobot-relative-layout-");
  try {
    for (const name of PATH_OVERRIDE_NAMES) {
      await assert.rejects(
        () => execFileAsync(fixture.launcher, [], {
          env: {
            HOME: fixture.home,
            PATH: process.env.PATH ?? "/usr/bin:/bin",
            [name]: "relative/path",
          },
        }),
        (error) => error instanceof Error && String(error.stderr).includes(`${name} must be an absolute path`),
        name,
      );
    }

    const envFile = join(fixture.home, ".config/hobot-code/hobot.env");
    await writeFile(envFile, "HOBOT_CODE_MEMORY_DB=relative/from-env-file\n");
    await chmod(envFile, 0o600);
    await assert.rejects(
      () => execFileAsync(fixture.launcher, [], {
        env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" },
      }),
      (error) => error instanceof Error
        && String(error.stderr).includes("HOBOT_CODE_MEMORY_DB must be an absolute path"),
    );
  } finally {
    await rm(fixture.root, { recursive: true, force: true });
  }
});
