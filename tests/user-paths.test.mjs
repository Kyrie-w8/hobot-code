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
  "HOBOT_CODE_MANAGED_PROVIDER_CONFIG",
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
  "managedProviderConfig",
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
  for (const name of ["settings.json", "models.json", "providers.json", "permissions.json", "memory.json", "goals.json", "hooks.json", "notifications.json", "lsp.json"]) {
    await copyFile(new URL(`../packaging/pi/${name}`, import.meta.url), join(defaults, name));
  }
  await copyFile(new URL("../packaging/pi/hobot.env.example", import.meta.url), join(defaults, "hobot.env.example"));
  await copyFile(new URL("../packaging/pi/tmux.conf", import.meta.url), join(runtime, "tmux.conf"));
  await writeFile(join(runtime, "hobot"), "#!/bin/sh\nprintf '%s\\n' \"$HOBOT_CODING_AGENT_DIR\" \"$HOBOT_CODING_AGENT_SESSION_DIR\" \"$HOBOT_CODE_MEMORY_DB\"\nfor hobot_arg in \"$@\"; do printf 'arg=<%s>\\n' \"$hobot_arg\"; done\n");
  await writeFile(join(runtime, "agentd"), `#!/bin/sh
if [ "\${1:-}" = tui ]; then
  shift
  [ "\${1:-}" = -- ] && shift
  exec "\${0%/*}/hobot" "$@"
fi
for hobot_arg in "$@"; do printf 'agentd=<%s>\\n' "$hobot_arg"; done
`);
  await Promise.all(["hobot", "agentd"].map((name) => chmod(join(runtime, name), 0o755)));
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
  -L)
    shift 2
    [ "\$1" = -f ] && shift 2
    exec "\$0" "\$@"
    ;;
  has-session)
    [ "\${FAKE_TMUX_SESSION_EXISTS:-0}" = 1 ]
    ;;
  list-sessions)
    printf 'hobot-code-main|1|1|2026-08-10 12:00:00\\nother-work|1|2|2026-08-09 10:00:00\\n'
    ;;
  new-session)
    if [ "\${FAKE_TMUX_REPORT_RUNTIME:-0}" = 1 ]; then
      printf 'tmux-runtime=<%s>\n' "\${TMUX_TMPDIR:-}"
    fi
    if [ "\${FAKE_TMUX_REPORT_CREDENTIALS:-0}" = 1 ]; then
      [ -z "\${ANTHROPIC_AUTH_TOKEN+x}" ] && printf 'tmux-anthropic=unset\n' || printf 'tmux-anthropic=set\n'
      [ -z "\${HOBOT_CODE_PROVIDER_KEY_ACME+x}" ] && printf 'tmux-managed=unset\n' || printf 'tmux-managed=set\n'
    fi
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
  assert.equal(paths.managedProviderConfig, "/home/rdk/.config/hobot-code/agent/providers.json");
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
    HOBOT_CODE_MANAGED_PROVIDER_CONFIG: "/managed/files/providers.json",
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
  assert.equal(paths.managedProviderConfig, overrides.HOBOT_CODE_MANAGED_PROVIDER_CONFIG);
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

test("launcher refuses broad system and home roots before changing permissions", async () => {
  const fixture = await createLauncherFixture("hobot-broad-root-");
  try {
    for (const [name, value] of [["HOBOT_CODE_CONFIG_DIR", "/"], ["HOBOT_CODE_STATE_DIR", fixture.home], ["HOBOT_CODING_AGENT_DIR", "/etc"]]) {
      await assert.rejects(() => execFileAsync(fixture.launcher, ["doctor"], {
        env: {HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin", [name]: value},
      }), /must identify a scoped private directory/);
    }
  } finally {
    await rm(fixture.root, {recursive: true, force: true});
  }
});

test("RDK extension delegates runtime paths to the shared fail-closed resolver", async () => {
  const source = await readFile(new URL("../extensions/rdk/index.ts", import.meta.url), "utf8");
  assert.match(
    source,
    /export default function rdkExtension\([^)]*\)\s*\{\s*(?:\/\/[^\n]*\n\s*)?resolveUserPaths\(\);/,
  );
  for (const field of EXTENSION_PATH_FIELDS) {
	if (field !== "managedProviderConfig") assert.match(source, new RegExp(`resolveUserPaths\\(\\)\\.${field}\\b`), field);
  }
	const managedProviderSource = await readFile(new URL("../extensions/rdk/managed-providers.mjs", import.meta.url), "utf8");
	assert.match(managedProviderSource, /resolveUserPaths\(env\)\.managedProviderConfig\b/u);
  assert.doesNotMatch(
    source,
    /process\.env\.(?:XDG_CONFIG_HOME|XDG_STATE_HOME|HOBOT_CODE_CONFIG_DIR|HOBOT_CODING_AGENT_DIR|HOBOT_CODE_STATE_DIR|HOBOT_CODING_AGENT_SESSION_DIR|HOBOT_CODE_PERMISSION_POLICY|HOBOT_CODE_MEMORY_CONFIG|HOBOT_CODE_MEMORY_DB|HOBOT_CODE_GOAL_CONFIG|HOBOT_CODE_GOAL_DB|HOBOT_CODE_HOOK_CONFIG|HOBOT_CODE_HOOK_AUDIT|HOBOT_CODE_NOTIFICATION_CONFIG|HOBOT_CODE_LSP_CONFIG|HOBOT_CODE_RDK_KNOWLEDGE_DIR|HOBOT_CODE_RDK_EXPERT_PROMPT)\b/,
  );
});

test("runtime model probes retain the D-Robotics provider without the full RDK prompt overlay", async () => {
  const source = await readFile(new URL("../extensions/rdk/index.ts", import.meta.url), "utf8");
  assert.match(source, /const runtimeProbeMode = process\.env\.HOBOT_CODE_RUNTIME_PROBE === "1"/u);
  assert.match(source, /if \(ephemeralSideAgentMode \|\| runtimeProbeMode\) return undefined/u);
  assert.match(source, /pi\.registerProvider\("drobotics"/u);
});

test("RDK model probes retain the expert prompt and expose only diagnostic tools", async () => {
  const source = await readFile(new URL("../extensions/rdk/index.ts", import.meta.url), "utf8");
  assert.match(source, /const rdkProbeMode = process\.env\.HOBOT_CODE_RDK_PROBE === "1"/u);
  assert.match(source, /if \(rdkProbeMode\) pi\.setActiveTools\(\["system_snapshot", "rdk_docs_search"\]\)/u);
  assert.match(source, /if \(!rdkProbeMode\) await captureWorkspaceTurnFingerprint\(ctx\.cwd\)/u);
  assert.doesNotMatch(source, /if \(sideAgentMode \|\| runtimeProbeMode \|\| rdkProbeMode\) return undefined/u);
  assert.match(source, /if \(!sideAgentMode && !rdkProbeMode\) \{/u);
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
	assert.equal(settings.retry.maxRetries, 5);
  assert.doesNotMatch(launcher, /\/etc\/hobot-code|\/var\/lib\/hobot-code/);
  assert.match(launcher, /XDG_CONFIG_HOME/);
  assert.match(launcher, /XDG_STATE_HOME/);
});

test("launcher initializes an isolated user but blocks the first conversation without a model", async () => {
  const fixture = await createLauncherFixture("hobot-user-layout-");
  try {
    await assert.rejects(() => execFileAsync(fixture.launcher, [], {
      env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" },
    }), /needs a model provider before the first conversation/);
    const { stdout } = await execFileAsync(fixture.launcher, ["tui"], {
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

test("launcher routes the read-only doctor before model setup", async () => {
  const fixture = await createLauncherFixture("hobot-doctor-route-");
  try {
    const { stdout } = await execFileAsync(fixture.launcher, ["doctor", "--json"], {
      env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" },
    });
    assert.equal(stdout.trim(), "agentd=<doctor>\nagentd=<--json>");
  } finally {
    await rm(fixture.root, { recursive: true, force: true });
  }
});

test("launcher routes explicit TUI sandbox selection through agentd", async () => {
  const fixture = await createLauncherFixture("hobot-tui-route-");
  try {
    const { stdout } = await execFileAsync(fixture.launcher, ["tui", "--sandbox", "review", "--", "--resume"], {
      env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" },
    });
    assert.match(stdout, /arg=<--sandbox>\narg=<review>\narg=<-->\narg=<--resume>\n?$/);
  } finally {
    await rm(fixture.root, { recursive: true, force: true });
  }
});

test("launcher routes managed provider commands through agentd", async () => {
  const fixture = await createLauncherFixture("hobot-provider-route-");
  try {
    const { stdout } = await execFileAsync(fixture.launcher, ["provider", "list", "--json"], {
      env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" },
    });
    assert.equal(stdout.trim(), "agentd=<provider>\nagentd=<list>\nagentd=<--json>");
    const rotated = await execFileAsync(fixture.launcher, ["provider", "rotate", "acme", "--token-stdin"], {
      env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" },
    });
    assert.equal(rotated.stdout.trim(), "agentd=<provider>\nagentd=<rotate>\nagentd=<acme>\nagentd=<--token-stdin>");
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

test("persistent tmux server never inherits model credentials", async () => {
  const fixture = await createLauncherFixture("hobot-persistent-credentials-");
  try {
    const fakeBin = await installFakeTmux(fixture.root);
    const configRoot = join(fixture.home, ".config/hobot-code");
    await mkdir(configRoot, { recursive: true });
    await writeFile(join(configRoot, "hobot.env"), [
      "ANTHROPIC_AUTH_TOKEN=drobotics-secret",
      "HOBOT_CODE_PROVIDER_KEY_ACME=managed-secret",
      "",
    ].join("\n"), { mode: 0o600 });
    const { stdout } = await execFileAsync(fixture.launcher, ["persistent", "start", "secure"], {
      cwd: fixture.root,
      env: {
        HOME: fixture.home,
        PATH: `${fakeBin}:${process.env.PATH ?? "/usr/bin:/bin"}`,
        FAKE_TMUX_REPORT_CREDENTIALS: "1",
        FAKE_TMUX_REPORT_RUNTIME: "1",
      },
    });
    assert.match(stdout, /^tmux-anthropic=unset$/m);
    assert.match(stdout, /^tmux-managed=unset$/m);
    assert.match(stdout, new RegExp(`^tmux-runtime=<${join(fixture.home, ".local/state/hobot-code/tmux")}>$`, "m"));
    assert.doesNotMatch(stdout, /drobotics-secret|managed-secret/);
  } finally {
    await rm(fixture.root, { recursive: true, force: true });
  }
});

test("launcher routes daemon, deployment, task, bridge, diagnosis, extensions, and model commands after loading the user environment", async () => {
  const fixture = await createLauncherFixture("hobot-agentd-route-");
  try {
    const environment = { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" };
    const daemon = await execFileAsync(fixture.launcher, ["daemon", "status"], { env: environment });
    assert.equal(daemon.stdout.trim(), "agentd=<daemon>\nagentd=<status>");
    const deployment = await execFileAsync(fixture.launcher, ["deploy", "inspect", "--cwd", "/root/models"], { env: environment });
    assert.equal(deployment.stdout.trim(), "agentd=<deploy>\nagentd=<inspect>\nagentd=<--cwd>\nagentd=</root/models>");
    const task = await execFileAsync(fixture.launcher, ["task", "list"], { env: environment });
    assert.equal(task.stdout.trim(), "agentd=<task>\nagentd=<list>");
    const bridge = await execFileAsync(fixture.launcher, ["bridge", "--stdio"], { env: environment });
    assert.equal(bridge.stdout.trim(), "agentd=<bridge>\nagentd=<--stdio>");
    const diagnose = await execFileAsync(fixture.launcher, ["diagnose", "--json"], { env: environment });
    assert.equal(diagnose.stdout.trim(), "agentd=<diagnose>\nagentd=<--json>");
    const extensions = await execFileAsync(fixture.launcher, ["extensions", "--json"], { env: environment });
    assert.equal(extensions.stdout.trim(), "agentd=<extensions>\nagentd=<--json>");
    const model = await execFileAsync(fixture.launcher, ["model", "check", "drobotics/kimi-k3"], { env: environment });
    assert.equal(model.stdout.trim(), "agentd=<model>\nagentd=<check>\nagentd=<drobotics/kimi-k3>");
    const modelStatus = await execFileAsync(fixture.launcher, ["model", "status", "drobotics/kimi-k3"], { env: environment });
    assert.equal(modelStatus.stdout.trim(), "agentd=<model>\nagentd=<status>\nagentd=<drobotics/kimi-k3>");
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

test("persistent tmux forwards application clipboard sequences", async () => {
  const config = await readFile(new URL("../packaging/pi/tmux.conf", import.meta.url), "utf8");
  assert.match(config, /^set -g set-clipboard on$/m);
  assert.match(config, /terminal-features[^\n]*xterm\*:clipboard/);
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
