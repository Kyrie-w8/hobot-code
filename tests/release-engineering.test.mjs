import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { access, chmod, copyFile, mkdir, mkdtemp, readFile, rm, stat, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { gzipSync } from "node:zlib";

import { parseDataLock, validateReleaseSource, writeBuildInfo } from "../scripts/release-metadata.mjs";
import {
  REQUIRED_EXECUTABLE_PATHS,
  REQUIRED_PACKAGE_DIRECTORIES,
  REQUIRED_PACKAGE_PATHS,
  validateRelativeImports,
  validateAgentdBinary,
  validatePackageMetadata,
  validatePackagedKnowledgeLayout,
  validateRequiredPackageLayout,
  validateSourceSyntax,
  verifyManifest,
} from "../scripts/validate-package.mjs";
import { createManifest } from "../scripts/write-release-manifest.mjs";

const execFileAsync = promisify(execFile);
const repository = resolve(dirname(fileURLToPath(import.meta.url)), "..");

function execFileWithInput(file, args, input, options = {}) {
  return new Promise((resolvePromise, rejectPromise) => {
    const child = execFile(file, args, { ...options, encoding: "utf8" }, (error, stdout, stderr) => {
      if (error) {
        error.stdout = stdout;
        error.stderr = stderr;
        rejectPromise(error);
        return;
      }
      resolvePromise({ stdout, stderr });
    });
    child.stdin.end(input);
  });
}

function configurationFingerprint(contents) {
  const digests = contents.map((content) => createHash("sha256").update(content).digest("hex"));
  return createHash("sha256").update(`${digests.join("\n")}\n`).digest("hex");
}

function tarArchiveWithEntry(name, type = "0", linkName = "") {
  const header = Buffer.alloc(512);
  const writeOctal = (value, offset, length) => {
    header.write(value.toString(8).padStart(length - 1, "0"), offset, length - 1, "ascii");
  };
  header.write(name, 0, 100, "utf8");
  writeOctal(type === "5" ? 0o755 : 0o644, 100, 8);
  writeOctal(0, 108, 8);
  writeOctal(0, 116, 8);
  writeOctal(0, 124, 12);
  writeOctal(0, 136, 12);
  header.fill(0x20, 148, 156);
  header.write(type, 156, 1, "ascii");
  header.write(linkName, 157, 100, "utf8");
  header.write("ustar\0", 257, 6, "ascii");
  header.write("00", 263, 2, "ascii");
  const checksum = header.reduce((sum, byte) => sum + byte, 0);
  header.write(checksum.toString(8).padStart(6, "0"), 148, 6, "ascii");
  header[154] = 0;
  header[155] = 0x20;
  return gzipSync(Buffer.concat([header, Buffer.alloc(1024)]), { level: 9 });
}

test("release locks are literal data and reject shell syntax or duplicate keys", () => {
  const expected = new Set(["VERSION", "URL"]);
  assert.deepEqual(
    parseDataLock("VERSION=1.2.3\nURL=https://example.test/file\n", "fixture.lock", expected),
    { VERSION: "1.2.3", URL: "https://example.test/file" },
  );
  assert.throws(
    () => parseDataLock("VERSION=$(touch_/tmp/pwn)\nURL=https://example.test/file\n", "fixture.lock", expected),
    /literal KEY=VALUE/,
  );
  assert.throws(
    () => parseDataLock("VERSION=1.2.3\nVERSION=1.2.4\nURL=https://example.test/file\n", "fixture.lock", expected),
    /duplicates VERSION/,
  );
});

test("release metadata is derived from the repository version and pinned inputs", async (t) => {
  const release = await validateReleaseSource(repository);
  const stage = await mkdtemp(join(tmpdir(), "hobot-build-info-"));
  t.after(() => rm(stage, { recursive: true, force: true }));
  await writeFile(join(stage, "agentd"), "agentd fixture\n");
  const info = await writeBuildInfo(repository, stage, {
    commit: "a".repeat(40),
    dirty: "0",
    builtAt: "2026-08-10T00:00:00Z",
  });
  assert.equal(info.version, release.version);
  assert.equal(info.commit, "a".repeat(40));
  assert.equal(info.pi.version, release.pi.PI_VERSION);
  assert.equal(JSON.parse(await readFile(join(stage, "BUILD_INFO.json"), "utf8")).dirty, false);

  await mkdir(join(stage, "runtime"));
  await writeFile(join(stage, "VERSION"), `${release.version}\n`);
  await writeFile(join(stage, "runtime/package.json"), `${JSON.stringify({ version: release.version })}\n`);
  await copyFile(join(repository, "CHANGELOG.md"), join(stage, "CHANGELOG.md"));
  await copyFile(join(repository, "CHANGELOG.md"), join(stage, "runtime/CHANGELOG.md"));
  await copyFile(join(repository, "pi-runtime/pi.lock"), join(stage, "PI_RUNTIME"));
  await copyFile(join(repository, "pi-runtime/tools.lock"), join(stage, "TOOLS_RUNTIME"));
  await validatePackageMetadata(stage);
  await writeFile(join(stage, "runtime/CHANGELOG.md"), "# Changelog\n\n## 99.0.0\n\n- Upstream entry.\n");
  await assert.rejects(() => validatePackageMetadata(stage), /must match the Hobot Code CHANGELOG/);
  await copyFile(join(repository, "CHANGELOG.md"), join(stage, "runtime/CHANGELOG.md"));
  info.tools.fd = "0.0.0";
  await writeFile(join(stage, "BUILD_INFO.json"), `${JSON.stringify(info)}\n`);
  await assert.rejects(() => validatePackageMetadata(stage), /tool provenance does not match/);
  info.tools.fd = release.tools.FD_VERSION;
  await writeFile(join(stage, "BUILD_INFO.json"), `${JSON.stringify(info)}\n`);
  await writeFile(join(stage, "agentd"), "mutated agentd fixture\n");
  await assert.rejects(() => validatePackageMetadata(stage), /agentdSha256 does not match/);
});

test("release CLIs execute through symbolic directory paths", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-release-cli-"));
  const linkedScripts = join(root, "linked-scripts");
  const packageRoot = join(root, "package");
  t.after(() => rm(root, { recursive: true, force: true }));
  await symlink(join(repository, "scripts"), linkedScripts);
  await mkdir(packageRoot);
  await writeFile(join(packageRoot, "payload.txt"), "verified\n");

  const metadata = await execFileAsync(process.execPath, [
    join(linkedScripts, "release-metadata.mjs"), "validate", repository,
  ]);
  assert.match(metadata.stdout, /Validated Hobot Code/);

  const manifest = await execFileAsync(process.execPath, [
    join(linkedScripts, "write-release-manifest.mjs"), packageRoot, "2026-08-10T00:00:00Z",
  ]);
  assert.match(manifest.stdout, /Wrote MANIFEST\.sha256/);
  await access(join(packageRoot, "MANIFEST.sha256"));

  const validation = await execFileAsync(process.execPath, [
    join(linkedScripts, "validate-package.mjs"), "--source", repository,
  ]);
  assert.match(validation.stdout, /Validated extension import closure/);
});

test("release manifest detects post-build mutation", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-manifest-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(join(root, "nested"));
  await writeFile(join(root, "nested/file.txt"), "original\n");
  await createManifest(root, "2026-08-10T00:00:00Z");
  await verifyManifest(root);
  await writeFile(join(root, "nested/file.txt"), "changed\n");
  await assert.rejects(() => verifyManifest(root), /checksum mismatch/);
  await writeFile(join(root, "nested/file.txt"), "original\n");
  await symlink("nested/file.txt", join(root, "linked.txt"));
  await assert.rejects(() => verifyManifest(root), /unsupported filesystem entry/);
  await assert.rejects(() => createManifest(root), /must not contain symlinks/);
});

test("release layout covers installer inputs and linked documentation", async (t) => {
  const installerInputs = [
    "agentd",
    "runtime/hobot",
    "extensions/rdk/index.ts",
    "skills/rdk-board/SKILL.md",
    "knowledge/manifest.json",
    "prompts/rdk-expert.md",
    "config/settings.json",
    "config/models.json",
    "config/permissions.json",
    "config/memory.json",
    "config/goals.json",
    "config/hooks.json",
    "config/notifications.json",
    "config/lsp.json",
    "config/hobot.env.example",
    "config/tmux.conf",
    "licenses/hobot-code-MIT.txt",
    "licenses/pi-mono-MIT.txt",
    "licenses/fd-MIT.txt",
    "licenses/fd-APACHE-2.0.txt",
    "licenses/ripgrep-MIT.txt",
    "licenses/ripgrep-UNLICENSE.txt",
    "managed-bin/fd",
    "managed-bin/rg",
    "hobot-launcher",
    "install.sh",
    "rollback.sh",
    "release.sh",
    "uninstall.sh",
  ];
  for (const name of installerInputs) assert.ok(REQUIRED_PACKAGE_PATHS.includes(name), `missing package contract: ${name}`);
  for (const name of ["extensions", "skills", "knowledge", "prompts", "licenses"]) {
    assert.ok(REQUIRED_PACKAGE_DIRECTORIES.includes(name), `missing package directory contract: ${name}`);
  }
  for (const name of [
    "agentd-protocol.md",
    "architecture.md",
    "board-reliability.md",
    "cache-efficiency.md",
    "compatibility.md",
    "configuration.md",
    "model-capabilities.md",
    "releasing.md",
    "user-directory-layout.md",
  ]) {
    assert.ok(REQUIRED_PACKAGE_PATHS.includes(`docs/${name}`), `missing packaged documentation: ${name}`);
  }

  const root = await mkdtemp(join(tmpdir(), "hobot-package-layout-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  for (const name of REQUIRED_PACKAGE_DIRECTORIES) await mkdir(join(root, name), { recursive: true });
  for (const name of REQUIRED_PACKAGE_PATHS) {
    const path = join(root, name);
    await mkdir(dirname(path), { recursive: true });
    await writeFile(path, "fixture\n");
  }
  await mkdir(join(root, "knowledge/common"), { recursive: true });
  await writeFile(join(root, "knowledge/common/fixture.md"), "# Fixture\n\nPackaged knowledge fixture.\n");
  await writeFile(join(root, "knowledge/manifest.json"), `${JSON.stringify({
    schemaVersion: 1,
    knowledgeVersion: "2026.08.3",
    updatedAt: "2026-08-10",
    documents: [{ id: "fixture", file: "common/fixture.md" }],
  })}\n`);
  for (const name of REQUIRED_EXECUTABLE_PATHS) await chmod(join(root, name), 0o755);
  await validateRequiredPackageLayout(root);
  await rm(join(root, "knowledge/common/fixture.md"));
  await assert.rejects(() => validatePackagedKnowledgeLayout(root), /missing knowledge\/common\/fixture\.md/);
  await writeFile(join(root, "knowledge/common/fixture.md"), "# Fixture\n\nPackaged knowledge fixture.\n");
  await rm(join(root, "config/memory.json"));
  await assert.rejects(() => validateRequiredPackageLayout(root), /missing config\/memory\.json/);
  await writeFile(join(root, "config/memory.json"), "fixture\n");
  await rm(join(root, "docs/architecture.md"));
  await assert.rejects(() => validateRequiredPackageLayout(root), /missing docs\/architecture\.md/);
  await writeFile(join(root, "docs/architecture.md"), "fixture\n");
  await chmod(join(root, "install.sh"), 0o644);
  await assert.rejects(() => validateRequiredPackageLayout(root), /not executable: install\.sh/);
});

test("upstream archive validation rejects traversal, links, and special entries", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-archive-validation-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const validator = join(repository, "scripts/validate-tar-archive.sh");
  const valid = join(root, "valid.tar.gz");
  await writeFile(valid, tarArchiveWithEntry("pi/file"));
  await execFileAsync(validator, [valid, "pi", "fixture archive"]);

  const invalidEntries = [
    ["absolute", "/tmp/escape", "0", ""],
    ["traversal", "pi/../escape", "0", ""],
    ["symlink", "pi/link", "2", "../../escape"],
    ["hardlink", "pi/hardlink", "1", "pi/file"],
    ["fifo", "pi/pipe", "6", ""],
  ];
  for (const [label, name, type, linkName] of invalidEntries) {
    const archive = join(root, `${label}.tar.gz`);
    await writeFile(archive, tarArchiveWithEntry(name, type, linkName));
    await assert.rejects(
      () => execFileAsync(validator, [archive, "pi", `${label} archive`]),
      /outside pi|non-canonical path|unsupported archive entry type|Error exit delayed/,
    );
  }
});

test("release validation accepts only Linux ARM64 agentd binaries", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-agentd-elf-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const header = Buffer.alloc(64);
  header.set([0x7f, 0x45, 0x4c, 0x46, 2, 1]);
  header.writeUInt16LE(183, 18);
  const marker = Buffer.from("HOBOT_CODE_AGENTD_VERSION=0.22.2;");
  await writeFile(join(root, "agentd"), Buffer.concat([header, marker]));
  await validateAgentdBinary(root, "0.22.2");
  await assert.rejects(() => validateAgentdBinary(root, "0.22.1"), /release marker does not match/);
  header.writeUInt16LE(62, 18);
  await writeFile(join(root, "agentd"), header);
  await assert.rejects(() => validateAgentdBinary(root), /Linux ARM64 ELF/);
});

test("extension validation catches missing relative imports and TypeScript syntax errors", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-imports-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(join(root, "extensions"));
  const source = join(root, "extensions/index.ts");
  await writeFile(source, 'import "./missing.mjs";\n');
  await assert.rejects(() => validateRelativeImports(root), /missing\.mjs/);
  await writeFile(join(root, "extensions/missing.mjs"), "export const value = 1;\n");
  await validateRelativeImports(root);
  await writeFile(source, 'import { type Value } from "./types.ts";\nconst value: Value = { ok: true };\n');
  await writeFile(join(root, "extensions/types.ts"), "export interface Value { ok: boolean }\n");
  await validateSourceSyntax(root);
  await writeFile(source, "export const broken = ;\n");
  await assert.rejects(() => validateSourceSyntax(root), /syntax validation failed/);
});

async function launcherFixture(t) {
  const root = await mkdtemp(join(tmpdir(), "hobot-launcher-release-"));
  t.after(() => rm(root, { recursive: true, force: true }));
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
  await writeFile(join(runtime, "hobot"), "#!/bin/sh\nprintf 'umask=%s\\nliteral=%s\\nfingerprint=%s\\nargs=%s\\n' \"$(umask)\" \"${LITERAL_VALUE:-}\" \"${HOBOT_CODE_CONFIG_FINGERPRINT:-}\" \"$*\"\n");
  await writeFile(join(runtime, "agentd"), `#!/bin/sh
if [ "\${1:-}" = tui ]; then
  shift
  [ "\${1:-}" = -- ] && shift
  exec "\${0%/*}/hobot" "$@"
fi
for arg in "$@"; do printf 'agentd=<%s>\\n' "$arg"; done
`);
  await writeFile(join(runtime, "release.sh"), "#!/bin/sh\nprintf 'release:%s\\n' \"$*\"\n");
  await writeFile(join(runtime, "uninstall.sh"), "#!/bin/sh\nprintf 'uninstall:%s\\n' \"$*\"\n");
  await Promise.all(["hobot", "agentd", "release.sh", "uninstall.sh"].map((name) => chmod(join(runtime, name), 0o755)));
  const launcherSource = await readFile(new URL("../packaging/pi/hobot-launcher", import.meta.url), "utf8");
  const launcher = join(root, "hobot-launcher");
  await writeFile(launcher, launcherSource.replaceAll("/usr/local/lib/hobot-code", runtime));
  await chmod(launcher, 0o755);
  return { root, runtime, home, launcher };
}

test("launcher routes product lifecycle commands without taking Pi extension updates", async (t) => {
  const fixture = await launcherFixture(t);
  const environment = { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" };
  const update = await execFileAsync(fixture.launcher, ["update", "--check"], { env: environment });
  assert.equal(update.stdout.trim(), "release:update --check");
  const uninstall = await execFileAsync(fixture.launcher, ["uninstall", "--yes"], { env: environment });
  assert.equal(uninstall.stdout.trim(), "uninstall:--yes");
  const extensions = await execFileAsync(fixture.launcher, ["update", "--extensions"], { env: environment });
  assert.match(extensions.stdout, /^args=update --extensions$/m);
});

test("launcher blocks runtime starts while install or rollback owns the transaction lock", async (t) => {
  const fixture = await launcherFixture(t);
  const lock = join(fixture.root, "hobot-code.install.lock");
  await mkdir(lock);
  const environment = {...process.env, HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin", HOBOT_CODE_TESTING: "1", HOBOT_CODE_TEST_INSTALL_LOCK: lock};
  await assert.rejects(() => execFileAsync(fixture.launcher, [], {env: environment}), /install or rollback is in progress/);
  await assert.rejects(() => execFileAsync(fixture.launcher, ["bridge", "--stdio"], {env: environment}), /install or rollback is in progress/);
  const update = await execFileAsync(fixture.launcher, ["update", "--check"], {env: environment});
  assert.equal(update.stdout.trim(), "release:update --check");
});

async function releaseFixture(t, responses) {
  const root = await mkdtemp(join(tmpdir(), "hobot-public-release-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  for (const [name, body] of responses) {
    const path = join(root, name.replace(/^\/releases\//u, ""));
    await mkdir(dirname(path), { recursive: true });
    await writeFile(path, body);
  }
  return `file://${root}`;
}

async function updateRuntimeFixture(t, version = "9.8.7") {
  const root = await mkdtemp(join(tmpdir(), "hobot-update-runtime-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const installRoot = join(root, "installed");
  const runtimeRoot = join(installRoot, "usr/local/lib/hobot-code");
  const launcher = join(installRoot, "usr/local/bin/hobot");
  const processRoot = join(root, "proc");
  const processDirectory = join(processRoot, "123");
  const log = join(root, "launcher.log");
  const stagingLog = join(root, "staging.log");
  await mkdir(runtimeRoot, { recursive: true });
  await mkdir(dirname(launcher), { recursive: true });
  await mkdir(processDirectory, { recursive: true });
  await writeFile(join(runtimeRoot, "VERSION"), "1.0.0\n");
  await writeFile(join(runtimeRoot, "agentd"), "agentd fixture\n");
  await writeFile(join(runtimeRoot, "hobot"), "runtime fixture\n");
  await writeFile(launcher, `#!/bin/sh
set -eu
case "\${1:-}" in
  daemon)
    case "\${2:-}" in
      status) printf '{"activeTasks":%s}\\n' "\${HOBOT_TEST_ACTIVE_TASKS:-0}" ;;
      stop) printf 'stop\\n' >> "$HOBOT_TEST_UPDATE_LOG"; rm -f "$HOBOT_TEST_PROC_EXE" ;;
      start) printf 'start\\n' >> "$HOBOT_TEST_UPDATE_LOG"; ln -s "$HOBOT_TEST_AGENTD" "$HOBOT_TEST_PROC_EXE" ;;
      *) exit 2 ;;
    esac
    ;;
  *) exit 2 ;;
esac
`);
  await chmod(launcher, 0o755);
  await symlink(join(runtimeRoot, "agentd"), join(processDirectory, "exe"));
  await writeFile(join(processDirectory, "cmdline"), Buffer.from(`${join(runtimeRoot, "agentd")}\0serve\0`));

  const packageParent = join(root, "package");
  const packageName = `hobot-code-${version}-linux-arm64`;
  const packageRoot = join(packageParent, packageName);
  await mkdir(packageRoot, { recursive: true });
  await writeFile(join(packageRoot, "install.sh"), `#!/bin/sh
set -eu
if [ "\${HOBOT_TEST_INSTALL_FAIL:-0}" = 1 ]; then
  printf 'simulated install failure\\n' >&2
  exit 9
fi
	printf '%s\\n' "$0" > "$HOBOT_TEST_STAGE_LOG"
	printf '%s\\n' '${version}' > "$HOBOT_CODE_TEST_INSTALL_ROOT/usr/local/lib/hobot-code/VERSION"
`);
  await chmod(join(packageRoot, "install.sh"), 0o755);
  const archiveName = `${packageName}.tar.gz`;
  const archive = join(root, archiveName);
  await execFileAsync("tar", ["-czf", archive, "-C", packageParent, packageName]);
  const archiveContent = await readFile(archive);
  const archiveDigest = createHash("sha256").update(archiveContent).digest("hex");
  const releaseBase = await releaseFixture(t, new Map([
    [`/releases/download/v${version}/${archiveName}`, archiveContent],
    [`/releases/download/v${version}/${archiveName}.sha256`, `${archiveDigest}  ${archiveName}\n`],
  ]));

  const fakeBin = join(root, "bin");
  await mkdir(fakeBin);
  await writeFile(join(fakeBin, "sudo"), "#!/bin/sh\nexec \"$@\"\n");
  await chmod(join(fakeBin, "sudo"), 0o755);
  return {
    root, installRoot, runtimeRoot, launcher, processRoot, processDirectory, log, stagingLog, releaseBase, version,
    env: {
      ...process.env,
      PATH: `${fakeBin}:${process.env.PATH ?? "/usr/bin:/bin"}`,
      HOBOT_CODE_ALLOW_UNSUPPORTED: "1",
      HOBOT_CODE_RELEASE_BASE_URL: releaseBase,
      HOBOT_CODE_TESTING: "1",
      HOBOT_CODE_TEST_INSTALL_ROOT: installRoot,
      HOBOT_CODE_TEST_PROC_ROOT: processRoot,
      HOBOT_TEST_UPDATE_LOG: log,
      HOBOT_TEST_STAGE_LOG: stagingLog,
      HOBOT_TEST_PROC_EXE: join(processDirectory, "exe"),
      HOBOT_TEST_AGENTD: join(runtimeRoot, "agentd"),
    },
  };
}

test("release installer resolves the latest stable version without downloading the archive", async (t) => {
  const base = await releaseFixture(t, new Map([
    ["/releases/latest/download/hobot-code-version.txt", "9.8.7\n"],
  ]));
  const script = join(repository, "scripts/hobot-release.sh");
  const { stdout } = await execFileAsync("/bin/sh", [script, "update", "--check"], {
    env: {
      ...process.env,
      HOBOT_CODE_ALLOW_UNSUPPORTED: "1",
      HOBOT_CODE_RELEASE_BASE_URL: base,
      HOBOT_CODE_TESTING: "1",
    },
  });
  assert.match(stdout, /Hobot Code 9\.8\.7 is available/);
});

test("release update checks fail quickly without leaking curl diagnostics", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-update-network-"));
  t.after(() => rm(root, {recursive: true, force: true}));
  const fakeBin = join(root, "bin");
  const curlArguments = join(root, "curl-arguments.txt");
  await mkdir(fakeBin);
  await writeFile(join(fakeBin, "curl"), `#!/bin/sh
printf '%s\\n' "$@" > "$HOBOT_TEST_CURL_ARGUMENTS"
printf '%s\\n' 'curl: noisy transport failure' >&2
exit 28
`);
  await chmod(join(fakeBin, "curl"), 0o755);
  let error;
  try {
    await execFileAsync("/bin/sh", [join(repository, "scripts/hobot-release.sh"), "update", "--check"], {
      env: {
        ...process.env,
        PATH: `${fakeBin}:${process.env.PATH ?? "/usr/bin:/bin"}`,
        HOBOT_CODE_ALLOW_UNSUPPORTED: "1",
        HOBOT_CODE_RELEASE_BASE_URL: "https://example.invalid/releases",
        HOBOT_TEST_CURL_ARGUMENTS: curlArguments,
      },
    });
    assert.fail("update check should fail when curl fails");
  } catch (caught) {
    error = caught;
  }
  assert.match(error.stderr, /Unable to check for Hobot Code updates within 10 seconds/);
  assert.match(error.stderr, /installed version was not changed/);
  assert.doesNotMatch(error.stderr, /noisy transport failure/);
  const argumentsText = await readFile(curlArguments, "utf8");
  assert.match(argumentsText, /--connect-timeout\n5\n/);
  assert.match(argumentsText, /--max-time\n10\n/);
  assert.doesNotMatch(argumentsText, /--retry\n/);
});

test("release installer never treats stale latest metadata as an update", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-stale-release-"));
  t.after(() => rm(root, {recursive: true, force: true}));
  const installRoot = join(root, "installed");
  await mkdir(join(installRoot, "usr/local/lib/hobot-code"), {recursive: true});
  await mkdir(join(root, "empty-proc"));
  await writeFile(join(installRoot, "usr/local/lib/hobot-code/VERSION"), "0.25.0\n");
  const base = await releaseFixture(t, new Map([
    ["/releases/latest/download/hobot-code-version.txt", "0.21.0\n"],
  ]));
  const environment = {
    ...process.env,
    HOBOT_CODE_ALLOW_UNSUPPORTED: "1",
    HOBOT_CODE_RELEASE_BASE_URL: base,
    HOBOT_CODE_TESTING: "1",
    HOBOT_CODE_TEST_INSTALL_ROOT: installRoot,
    HOBOT_CODE_TEST_PROC_ROOT: join(root, "empty-proc"),
  };
  const {stdout} = await execFileAsync("/bin/sh", [join(repository, "scripts/hobot-release.sh"), "update", "--check"], {env: environment});
  assert.match(stdout, /metadata reports 0\.21\.0, older than installed version 0\.25\.0/);
  assert.doesNotMatch(stdout, /is available/);
  await assert.rejects(
    () => execFileAsync("/bin/sh", [join(repository, "scripts/hobot-release.sh"), "update"], {env: environment}),
    /Refusing to downgrade Hobot Code from 0\.25\.0 to 0\.21\.0/,
  );
});

test("release installer requires explicit consent for an intentional downgrade", async (t) => {
  const fixture = await updateRuntimeFixture(t, "0.9.0");
  await assert.rejects(
    () => execFileAsync("/bin/sh", [join(repository, "scripts/hobot-release.sh"), "update", "--version", fixture.version], {env: fixture.env}),
    /add --allow-downgrade/,
  );
  await execFileAsync("/bin/sh", [
    join(repository, "scripts/hobot-release.sh"), "update", "--version", fixture.version, "--allow-downgrade",
  ], {env: fixture.env});
  assert.equal(await readFile(join(fixture.runtimeRoot, "VERSION"), "utf8"), `${fixture.version}\n`);
  await assert.rejects(
    () => execFileAsync("/bin/sh", [join(repository, "scripts/hobot-release.sh"), "update", "--allow-downgrade"], {env: fixture.env}),
    /requires an explicit --version/,
  );
});

test("release comparison follows SemVer precedence and ignores build metadata", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-semver-release-"));
  t.after(() => rm(root, {recursive: true, force: true}));
  const installRoot = join(root, "installed");
  const runtime = join(installRoot, "usr/local/lib/hobot-code");
  const emptyProc = join(root, "empty-proc");
  await mkdir(runtime, {recursive: true});
  await mkdir(emptyProc);
  const environment = {...process.env, HOBOT_CODE_ALLOW_UNSUPPORTED: "1", HOBOT_CODE_TESTING: "1", HOBOT_CODE_TEST_INSTALL_ROOT: installRoot, HOBOT_CODE_TEST_PROC_ROOT: emptyProc};
  const cases = [
    ["1.0.0", "1.0.0+build.7", /is current/],
    ["1.0.0-rc.2", "1.0.0-rc.10", /is available/],
    ["1.0.0", "1.0.0-rc.10", /older than installed/],
    ["1.0.0-rc.10", "1.0.0", /is available/],
  ];
  for (const [installed, candidate, expected] of cases) {
    await writeFile(join(runtime, "VERSION"), `${installed}\n`);
    const {stdout} = await execFileAsync("/bin/sh", [join(repository, "scripts/hobot-release.sh"), "update", "--version", candidate, "--check"], {env: environment});
    assert.match(stdout, expected, `${installed} -> ${candidate}`);
  }
});

test("latest metadata resolves to an immutable versioned payload in private staging", async (t) => {
  const fixture = await updateRuntimeFixture(t, "9.8.7");
  const latestDirectory = join(fileURLToPath(fixture.releaseBase), "latest/download");
  await mkdir(latestDirectory, {recursive: true});
  await writeFile(join(latestDirectory, "hobot-code-version.txt"), `${fixture.version}\n`);
  await execFileAsync("/bin/sh", [join(repository, "scripts/hobot-release.sh"), "update"], {env: fixture.env});
  assert.equal(await readFile(join(fixture.runtimeRoot, "VERSION"), "utf8"), `${fixture.version}\n`);
  assert.match(await readFile(fixture.stagingLog, "utf8"), /hobot-code\.package\./);
});

test("release installer falls back to a private user cache when TMPDIR is unavailable", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-update-cache-"));
  t.after(() => rm(root, {recursive: true, force: true}));
  const cache = join(root, "cache");
  const base = await releaseFixture(t, new Map([
    ["/releases/latest/download/hobot-code-version.txt", "9.8.7\n"],
  ]));
  const {stdout} = await execFileAsync("/bin/sh", [join(repository, "scripts/hobot-release.sh"), "update", "--check"], {
    env: {
      ...process.env,
      HOME: root,
      TMPDIR: join(root, "missing-temporary-directory"),
      XDG_CACHE_HOME: cache,
      HOBOT_CODE_ALLOW_UNSUPPORTED: "1",
      HOBOT_CODE_RELEASE_BASE_URL: base,
      HOBOT_CODE_TESTING: "1",
      HOBOT_CODE_TEST_INSTALL_ROOT: join(root, "no-installation"),
      HOBOT_CODE_TEST_PROC_ROOT: join(root, "empty-proc"),
    },
  });
  assert.match(stdout, /Hobot Code 9\.8\.7 is available/);
  assert.equal((await stat(join(cache, "hobot-code"))).mode & 0o777, 0o700);
});

test("release installer rejects an active runtime before downloading the archive", async (t) => {
  const fixture = await updateRuntimeFixture(t);
  await rm(join(fixture.processDirectory, "exe"));
  await symlink(join(fixture.runtimeRoot, "hobot"), join(fixture.processDirectory, "exe"));
  await assert.rejects(
    () => execFileAsync("/bin/sh", [join(repository, "scripts/hobot-release.sh"), "update", "--version", fixture.version], {env: fixture.env}),
    (error) => {
      assert.match(error.stderr, /foreground, persistent, automation, or Studio bridge session/);
      assert.doesNotMatch(error.stderr, /runtime fixture|command line/);
      return true;
    },
  );
  await assert.rejects(() => access(fixture.log));
  assert.equal(await readFile(join(fixture.runtimeRoot, "VERSION"), "utf8"), "1.0.0\n");
});

test("release installer treats Studio bridges as active clients, not extra daemons", async (t) => {
  const fixture = await updateRuntimeFixture(t);
  const bridgeDirectory = join(fixture.processRoot, "456");
  await mkdir(bridgeDirectory);
  await symlink(join(fixture.runtimeRoot, "agentd"), join(bridgeDirectory, "exe"));
  await writeFile(join(bridgeDirectory, "cmdline"), Buffer.from(`${join(fixture.runtimeRoot, "agentd")}\0bridge\0--stdio\0`));
  await assert.rejects(
    () => execFileAsync("/bin/sh", [join(repository, "scripts/hobot-release.sh"), "update", "--version", fixture.version], {env: fixture.env}),
    (error) => {
      assert.match(error.stderr, /Studio bridge session/);
      assert.doesNotMatch(error.stderr, /Multiple Hobot Code background services/);
      return true;
    },
  );
  await assert.rejects(() => access(fixture.log));
});

test("release installer preserves active board-side tasks", async (t) => {
  const fixture = await updateRuntimeFixture(t);
  const workerDirectory = join(fixture.processRoot, "456");
  await mkdir(workerDirectory);
  await symlink(join(fixture.runtimeRoot, "hobot"), join(workerDirectory, "exe"));
  await assert.rejects(
    () => execFileAsync("/bin/sh", [join(repository, "scripts/hobot-release.sh"), "update", "--version", fixture.version], {
      env: {...fixture.env, HOBOT_TEST_ACTIVE_TASKS: "2", HOBOT_CODE_RELEASE_BASE_URL: "file:///release-must-not-be-read"},
    }),
    (error) => {
      assert.match(error.stderr, /2 active board-side task/);
      return true;
    },
  );
  await assert.rejects(() => access(fixture.log));
  await access(join(fixture.processDirectory, "exe"));
});

test("release installer rolls an idle daemon through stop, install, and restart", async (t) => {
  const fixture = await updateRuntimeFixture(t);
  const {stdout} = await execFileAsync("/bin/sh", [
    join(repository, "scripts/hobot-release.sh"), "update", "--version", fixture.version,
  ], {env: fixture.env});
  assert.equal(await readFile(fixture.log, "utf8"), "stop\nstart\n");
  assert.equal(await readFile(join(fixture.runtimeRoot, "VERSION"), "utf8"), `${fixture.version}\n`);
  assert.match(stdout, /restarted its background service/);
  await access(join(fixture.processDirectory, "exe"));
});

test("release installer restores an idle daemon after installation failure", async (t) => {
  const fixture = await updateRuntimeFixture(t);
  await assert.rejects(
    () => execFileAsync("/bin/sh", [join(repository, "scripts/hobot-release.sh"), "update", "--version", fixture.version], {
      env: {...fixture.env, HOBOT_TEST_INSTALL_FAIL: "1"},
    }),
    (error) => {
      assert.match(error.stderr, /simulated install failure/);
      assert.match(error.stderr, /Restored the Hobot Code background service/);
      return true;
    },
  );
  assert.equal(await readFile(fixture.log, "utf8"), "stop\nstart\n");
  assert.equal(await readFile(join(fixture.runtimeRoot, "VERSION"), "utf8"), "1.0.0\n");
  await access(join(fixture.processDirectory, "exe"));
});

test("release installer rejects non-strict versions, repositories, and checksum records", async (t) => {
  const script = join(repository, "scripts/hobot-release.sh");
  const commonEnvironment = { ...process.env, HOBOT_CODE_ALLOW_UNSUPPORTED: "1" };
  await assert.rejects(
    () => execFileAsync("/bin/sh", [script, "update", "--version", "01.2.3", "--check"], { env: commonEnvironment }),
    /not valid SemVer/,
  );
  await assert.rejects(
    () => execFileAsync("/bin/sh", [script, "update", "--version", "1.2.3", "--check"], {
      env: { ...commonEnvironment, HOBOT_CODE_REPOSITORY: "owner/nested/repository" },
    }),
    /exactly one slash/,
  );

  const version = "9.8.7";
  const archiveName = `hobot-code-${version}-linux-arm64.tar.gz`;
  const base = await releaseFixture(t, new Map([
    [`/releases/download/v${version}/${archiveName}`, "not-an-archive"],
    [`/releases/download/v${version}/${archiveName}.sha256`, `${"0".repeat(64)}  another-file\n`],
  ]));
  await assert.rejects(
    () => execFileAsync("/bin/sh", [script, "install", "--version", version], {
      env: {
        ...commonEnvironment,
        HOBOT_CODE_RELEASE_BASE_URL: base,
        HOBOT_CODE_TESTING: "1",
      },
    }),
    /exactly one SHA256 record/,
  );

  const escapedVersion = "9.8.6";
  const escapedArchiveName = `hobot-code-${escapedVersion}-linux-arm64.tar.gz`;
  const escapedArchive = tarArchiveWithEntry("outside/file");
  const escapedDigest = createHash("sha256").update(escapedArchive).digest("hex");
  const escapedBase = await releaseFixture(t, new Map([
    [`/releases/download/v${escapedVersion}/${escapedArchiveName}`, escapedArchive],
    [`/releases/download/v${escapedVersion}/${escapedArchiveName}.sha256`, `${escapedDigest}  ${escapedArchiveName}\n`],
  ]));
  await assert.rejects(
    () => execFileAsync("/bin/sh", [script, "install", "--version", escapedVersion], {
      env: {
        ...commonEnvironment,
        HOBOT_CODE_RELEASE_BASE_URL: escapedBase,
        HOBOT_CODE_TESTING: "1",
      },
    }),
    /outside hobot-code-9\.8\.6-linux-arm64/,
  );
});

test("launcher treats environment values literally and restores the caller umask", async (t) => {
  const fixture = await launcherFixture(t);
  const configRoot = join(fixture.home, ".config/hobot-code");
  const marker = join(fixture.root, "must-not-exist");
  await mkdir(configRoot, { recursive: true });
  await writeFile(join(configRoot, "hobot.env"), `LITERAL_VALUE=$(printf unsafe > ${marker})\n`);
  await chmod(join(configRoot, "hobot.env"), 0o600);
  const { stdout } = await execFileAsync("/bin/sh", ["-c", 'umask 0022; exec "$1"', "sh", fixture.launcher], {
    env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" },
  });
  assert.match(stdout, /^umask=0022$/m);
  assert.match(stdout, /literal=\$\(printf unsafe/);
  const agentRoot = join(configRoot, "agent");
  const contents = await Promise.all(["hobot.env", "agent/settings.json", "agent/models.json"].map((name) => readFile(join(configRoot, name))));
  assert.match(stdout, new RegExp(`^fingerprint=${configurationFingerprint(contents)}$`, "m"));
  const originalFingerprint = stdout.match(/^fingerprint=([0-9a-f]{64})$/m)?.[1];
  await writeFile(join(agentRoot, "models.json"), '{"providers":{"changed":{}}}\n');
  const changed = await execFileAsync(fixture.launcher, [], { env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" } });
  assert.notEqual(changed.stdout.match(/^fingerprint=([0-9a-f]{64})$/m)?.[1], originalFingerprint);
  await assert.rejects(() => access(marker));
});

test("launcher setup writes a private model configuration without exposing the token", async (t) => {
  const fixture = await launcherFixture(t);
  const config = join(fixture.home, ".config/hobot-code/hobot.env");
  const token = "sk-private-setup-token";
  const environment = { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" };
  const configured = await execFileWithInput(fixture.launcher, [
    "setup", "--token-stdin", "--model", "glm-5.2", "--base-url", "https://ai-api.d-robotics.cc",
  ], `${token}\n`, { env: environment });
  assert.doesNotMatch(configured.stdout, new RegExp(token));
  assert.doesNotMatch(configured.stderr, new RegExp(token));
  assert.match(configured.stdout, /Model: drobotics\/glm-5\.2/);
  assert.match(configured.stdout, /Verify the route when ready/);

  const saved = await readFile(config, "utf8");
  assert.match(saved, /^ANTHROPIC_AUTH_TOKEN=sk-private-setup-token$/m);
  assert.match(saved, /^ANTHROPIC_MODEL=glm-5\.2$/m);
  assert.match(saved, /^ANTHROPIC_BASE_URL=https:\/\/ai-api\.d-robotics\.cc$/m);
  assert.equal((saved.match(/^ANTHROPIC_AUTH_TOKEN=/gm) ?? []).length, 1);
  assert.equal((saved.match(/^ANTHROPIC_MODEL=/gm) ?? []).length, 1);
  assert.equal((saved.match(/^ANTHROPIC_BASE_URL=/gm) ?? []).length, 1);
  assert.equal((await stat(config)).mode & 0o777, 0o600);

  const launched = await execFileAsync(fixture.launcher, [], { env: environment });
  const configRoot = join(fixture.home, ".config/hobot-code");
  const fingerprint = configurationFingerprint(await Promise.all(["hobot.env", "agent/settings.json", "agent/models.json"].map((name) => readFile(join(configRoot, name)))));
  assert.match(launched.stdout, new RegExp(`^fingerprint=${fingerprint}$`, "m"));
});

test("launcher setup normalizes legacy model defaults and bounds token input", async (t) => {
  const fixture = await launcherFixture(t);
  const environment = { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" };
  const configRoot = join(fixture.home, ".config/hobot-code");
  const config = join(configRoot, "hobot.env");
  await mkdir(configRoot, { recursive: true });
  await writeFile(config, "ANTHROPIC_BASE_URL=https://ai-api.d-robotics.cc\nANTHROPIC_AUTH_TOKEN=old-token\nANTHROPIC_MODEL=drobotics/glm-5.2\n");
  await chmod(config, 0o600);
  await execFileWithInput(fixture.launcher, ["setup", "--token-stdin"], "new-token\n", { env: environment });
  assert.match(await readFile(config, "utf8"), /^ANTHROPIC_MODEL=glm-5\.2$/m);

  await writeFile(config, "ANTHROPIC_BASE_URL=https://ai-api.d-robotics.cc\nANTHROPIC_AUTH_TOKEN=old-token\nANTHROPIC_MODEL=deepseek-v4-flash\n");
  await chmod(config, 0o600);
  await execFileWithInput(fixture.launcher, ["setup", "--token-stdin"], "new-flash-token\n", { env: environment });
  assert.match(await readFile(config, "utf8"), /^ANTHROPIC_MODEL=deepseek\/deepseek-v4-flash$/m);

  await writeFile(config, "ANTHROPIC_BASE_URL=https://ai-api.d-robotics.cc\nANTHROPIC_AUTH_TOKEN=old-token\nANTHROPIC_MODEL=legacy-unknown\n");
  await chmod(config, 0o600);
  await execFileWithInput(fixture.launcher, ["setup", "--token-stdin"], "newer-token\n", { env: environment });
  assert.match(await readFile(config, "utf8"), /^ANTHROPIC_MODEL=kimi-k3$/m);

  const beforeOversized = await readFile(config, "utf8");
  await assert.rejects(
    () => execFileWithInput(fixture.launcher, ["setup", "--token-stdin"], `${"x".repeat(8193)}\n`, { env: environment }),
    /8192-byte limit/,
  );
  assert.equal(await readFile(config, "utf8"), beforeOversized);
});

test("launcher setup rejects unsafe values and follows no credential symlink", async (t) => {
  const fixture = await launcherFixture(t);
  const environment = { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" };
  await assert.rejects(
    () => execFileWithInput(fixture.launcher, ["setup", "--token-stdin", "--model", "other-model"], "token\n", { env: environment }),
    /Unsupported D-Robotics model/,
  );
  await assert.rejects(
    () => execFileWithInput(fixture.launcher, ["setup", "--token-stdin", "--base-url", "http://gateway.example"], "token\n", { env: environment }),
    /must use HTTPS/,
  );
  await assert.rejects(
    () => execFileWithInput(fixture.launcher, ["setup", "--token-stdin"], "\n", { env: environment }),
    /token cannot be empty/,
  );

  const config = join(fixture.home, ".config/hobot-code/hobot.env");
  const beforeRelativeState = await readFile(config, "utf8");
  await writeFile(config, `${beforeRelativeState}HOBOT_CODE_STATE_DIR=relative-state\n`);
  await chmod(config, 0o600);
  await assert.rejects(
    () => execFileWithInput(fixture.launcher, ["setup", "--token-stdin"], "token\n", { env: environment }),
    /HOBOT_CODE_STATE_DIR must be an absolute path/,
  );
  assert.equal(await readFile(config, "utf8"), `${beforeRelativeState}HOBOT_CODE_STATE_DIR=relative-state\n`);
  await writeFile(config, beforeRelativeState);
  await chmod(config, 0o600);

  const outside = join(fixture.root, "outside.env");
  await writeFile(outside, "do-not-replace\n");
  await rm(config);
  await symlink(outside, config);
  await assert.rejects(
    () => execFileWithInput(fixture.launcher, ["setup", "--token-stdin"], "token\n", { env: environment }),
    /symbolic link/,
  );
  assert.equal(await readFile(outside, "utf8"), "do-not-replace\n");
});

test("launcher setup does not restart a running daemon and can check with a test double", async (t) => {
  const fixture = await launcherFixture(t);
  const environment = { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" };
  const pidRoot = join(fixture.home, ".local/state/hobot-code/agentd");
  await mkdir(pidRoot, { recursive: true });
  await writeFile(join(pidRoot, "agentd.pid"), `${process.pid}\n`);
  const configured = await execFileWithInput(fixture.launcher, ["setup", "--token-stdin", "--check"], "first-token\n", { env: environment });
  assert.match(configured.stderr, /Run: hobot daemon restart/);
  assert.match(configured.stderr, /then run: hobot model check/);
  assert.doesNotMatch(configured.stdout, /agentd=</);

  await rm(join(pidRoot, "agentd.pid"));
  const checked = await execFileWithInput(fixture.launcher, ["setup", "--token-stdin", "--model", "qwen3.8-max", "--check"], "second-token\n", { env: environment });
  assert.equal(checked.stdout.trim().split("\n").slice(-3).join("\n"), "agentd=<model>\nagentd=<check>\nagentd=<drobotics/qwen3.8-max>");
  assert.doesNotMatch(checked.stdout, /second-token/);
});

test("launcher rejects process-injection variables and managed symlinks", async (t) => {
  const fixture = await launcherFixture(t);
  const configRoot = join(fixture.home, ".config/hobot-code");
  await mkdir(configRoot, { recursive: true });
  await writeFile(join(configRoot, "hobot.env"), "NODE_OPTIONS=--require=/tmp/inject.js\n");
  await chmod(join(configRoot, "hobot.env"), 0o600);
  await assert.rejects(
    () => execFileAsync(fixture.launcher, [], { env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" } }),
    /process-injection variable NODE_OPTIONS/,
  );
  await writeFile(join(configRoot, "hobot.env"), "ANTHROPIC_AUTH_TOKEN=token\"\n");
  await assert.rejects(
    () => execFileAsync(fixture.launcher, [], { env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" } }),
    /unmatched quote/,
  );
  await writeFile(join(configRoot, "hobot.env"), "HOBOT_CODE_CONFIG_DIR=/tmp/other-hobot\n");
  await assert.rejects(
    () => execFileAsync(fixture.launcher, [], { env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" } }),
    /must set launcher path variable HOBOT_CODE_CONFIG_DIR before invoking hobot/,
  );
  await writeFile(join(configRoot, "hobot.env"), "ANTHROPIC_AUTH_TOKEN=token\n");
  const outside = join(fixture.root, "outside-agent");
  await mkdir(outside);
  await rm(join(configRoot, "agent"), { recursive: true, force: true });
  await symlink(outside, join(configRoot, "agent"));
  await assert.rejects(
    () => execFileAsync(fixture.launcher, [], { env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" } }),
    /symbolic link/,
  );
});

test("launcher rejects credential files exposed to group or other users", async (t) => {
  const fixture = await launcherFixture(t);
  const configRoot = join(fixture.home, ".config/hobot-code");
  await mkdir(configRoot, { recursive: true });
  await writeFile(join(configRoot, "hobot.env"), "ANTHROPIC_AUTH_TOKEN=token\n");
  await chmod(join(configRoot, "hobot.env"), 0o644);
  await assert.rejects(
    () => execFileAsync(fixture.launcher, [], { env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" } }),
    /must not grant group or other access/,
  );
});

test("launcher reports an unreadable credential file", { skip: process.getuid?.() === 0 }, async (t) => {
  const fixture = await launcherFixture(t);
  const configRoot = join(fixture.home, ".config/hobot-code");
  await mkdir(configRoot, { recursive: true });
  await writeFile(join(configRoot, "hobot.env"), "ANTHROPIC_AUTH_TOKEN=token\n");
  await chmod(join(configRoot, "hobot.env"), 0o000);
  await assert.rejects(
    () => execFileAsync(fixture.launcher, [], { env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" } }),
    /not readable by the current user/,
  );
});

test("launcher rejects non-regular managed configuration files", async (t) => {
  const fixture = await launcherFixture(t);
  const configRoot = join(fixture.home, ".config/hobot-code");
  await mkdir(join(configRoot, "agent/settings.json"), { recursive: true });
  await writeFile(join(configRoot, "hobot.env"), "ANTHROPIC_AUTH_TOKEN=token\n");
  await chmod(join(configRoot, "hobot.env"), 0o600);
  await assert.rejects(
    () => execFileAsync(fixture.launcher, [], { env: { HOME: fixture.home, PATH: process.env.PATH ?? "/usr/bin:/bin" } }),
    /configuration must be a regular file/,
  );
});

test("release scripts preserve transaction and provenance invariants", async () => {
  const [makefile, packager, installer, rollback, launcher, releaseInstaller, uninstaller, workflow, studioPackager] = await Promise.all([
    readFile(join(repository, "Makefile"), "utf8"),
    readFile(join(repository, "scripts/package-pi.sh"), "utf8"),
    readFile(join(repository, "scripts/install-pi.sh"), "utf8"),
    readFile(join(repository, "scripts/rollback-pi.sh"), "utf8"),
    readFile(join(repository, "packaging/pi/hobot-launcher"), "utf8"),
    readFile(join(repository, "scripts/hobot-release.sh"), "utf8"),
    readFile(join(repository, "scripts/uninstall-pi.sh"), "utf8"),
    readFile(join(repository, ".github/workflows/release.yml"), "utf8"),
    readFile(join(repository, "scripts/package-studio-macos.sh"), "utf8"),
  ]);
  assert.doesNotMatch(makefile, /package-pi\.sh\s+\$\(VERSION\)/);
  assert.doesNotMatch(packager, /^\.\s+.*\.lock/m);
  assert.match(packager, /^umask 022$/m);
  assert.match(packager, /HOBOT_CODE_ALLOW_DIRTY_BUILD/);
  assert.match(packager, /release-metadata\.mjs" write/);
  assert.match(packager, /HOBOT_CODE_AGENTD_BINARY/);
  assert.match(packager, /stage_dir\/agentd/);
  assert.match(packager, /output_part=.*\.part\.\$\$/);
  assert.match(packager, /\.package-pi\.lock/);
  assert.match(packager, /package_download_partial=.*package_download_destination\.part\.\$\$/);
  assert.match(packager, /mv -f "\$package_download_partial" "\$package_download_destination"/);
  assert.doesNotMatch(packager, /^\s*(?:destination|partial|expected|actual|label|url)=/m);
  assert.match(packager, /tar --no-recursion/);
  assert.match(packager, /gzip -n -9 -c/);
  assert.match(packager, /CONTRIBUTING\.md/);
  assert.match(packager, /SECURITY\.md/);
  assert.match(packager, /LICENSE/);
  assert.match(packager, /stage_dir\/runtime\/CHANGELOG\.md/);
  assert.match(installer, /MANIFEST\.sha256/);
  assert.match(installer, /must not contain symbolic links/);
  assert.match(installer, /Install home must not traverse symbolic links/);
  assert.match(installer, /install_home_owner/);
  assert.match(installer, /refuse_tree_symlinks "\$config_root"/);
  assert.match(installer, /refuse_tree_symlinks \/usr\/local\/lib\/hobot-code/);
  assert.match(installer, /Expected a managed command file or an absent path/);
  assert.match(installer, /Installed command validation failed/);
  assert.match(installer, /TOOLS_RUNTIME/);
  assert.match(installer, /HOBOT_CODE_INSTALL_CHANNEL/);
  assert.match(installer, /HOBOT_CODE_BACKUP_KEEP:-3/);
  assert.match(installer, /HOBOT_CODE_BACKUP_MAX_MIB:-768/);
  assert.match(installer, /prune_install_backups "\$backup_dir"/);
  assert.match(installer, /candidate" = "\$protected_backup/);
  assert.match(installer, /could not prune old Hobot Code backup/);
  assert.match(installer, /package_dir\/agentd/);
  assert.match(installer, /new_runtime\/agentd/);
  assert.match(installer, /package_dir\/docs\/\." "\$new_runtime\/docs/);
  assert.match(installer, /ANTHROPIC_MODEL=deepseek\/deepseek-v4-flash/);
  assert.match(installer, /drobotics\/deepseek\/deepseek-v4-flash/);
  assert.match(installer, /Installed component version mismatch/);
  assert.match(installer, /Configure your model first: hobot setup/);
  assert.match(installer, /ANTHROPIC_AUTH_TOKEN=/);
  assert.match(installer, /for process_path in \/proc\/\[0-9\]\*;/);
  assert.equal((installer.match(/active_pids=\$\(active_hobot_pids\)/g) ?? []).length, 2);
  assert.doesNotMatch(installer, /pgrep -f/);
  assert.doesNotMatch(installer, /chown\s+-R|find\s+"\$config_root"/);
  assert.doesNotMatch(installer, /\/usr\/local\/bin\/hobot --version/);
  assert.match(rollback, /LAST_BACKUP/);
  assert.match(rollback, /\.hobot-restored/);
  assert.match(rollback, /Backup has already been restored/);
  assert.match(rollback, /last_restored_write_started/);
  assert.match(rollback, /hobot-rollback-command/);
  assert.match(rollback, /Backup must not contain symbolic links/);
  assert.match(rollback, /Expected a managed command file or an absent path/);
  assert.match(rollback, /Restored launcher validation failed/);
  assert.match(rollback, /rm -f \/usr\/local\/sbin\/hobot-rollback/);
  assert.match(rollback, /chmod 0755 "\$staged_runtime"/);
  assert.match(rollback, /runtime_device=.*stat -c %d/);
  assert.match(rollback, /check_available_space "\$runtime_required_kib" \/usr\/local\/lib 'rollback'/);
  assert.match(rollback, /readlink "\$process_path\/exe"/);
  assert.doesNotMatch(rollback, /pgrep -f/);
  assert.doesNotMatch(launcher, /^\s*\.\s+.*hobot\.env/m);
  assert.match(launcher, /umask "\$original_umask"/);
  assert.match(launcher, /release\.sh update/);
  assert.match(launcher, /hobot-code\.install\.lock/);
  assert.match(launcher, /uninstall\.sh/);
  assert.match(launcher, /HOBOT_CODE_CONFIG_FINGERPRINT/);
  assert.match(launcher, /hobot setup --token-stdin/);
  assert.match(launcher, /stty -echo <\/dev\/tty/);
  assert.match(launcher, /mktemp "\$hobot_config_root\/\.hobot\.env\.setup\.XXXXXX"/);
  assert.match(launcher, /mv -f "\$setup_temp" "\$hobot_config_root\/hobot\.env"/);
  assert.match(releaseInstaller, /curl --proto '=https' --tlsv1\.2 -fsSL/);
  assert.doesNotMatch(releaseInstaller, /\bwget\b/);
  assert.match(releaseInstaller, /--max-filesize/);
  assert.match(releaseInstaller, /checksum_target/);
  assert.match(releaseInstaller, /unsupported entry type/);
  assert.match(releaseInstaller, /foreground, persistent, automation, or Studio bridge session/);
  assert.match(releaseInstaller, /daemon_stopped_for_update/);
  assert.match(releaseInstaller, /release_cache_home/);
  assert.match(uninstaller, /--purge/);
  assert.match(uninstaller, /Stop active Hobot Code processes/);
  assert.match(uninstaller, /readlink "\$process_path\/exe"/);
  assert.doesNotMatch(uninstaller, /pgrep -f/);
  assert.match(workflow, /attest-build-provenance@v2/);
  assert.match(workflow, /test "\$\{GITHUB_REF_NAME\}" = "v\$\{version\}"/);
  assert.match(workflow, /environment: production/);
  assert.match(workflow, /MACOS_CERTIFICATE_BASE64/);
  assert.match(workflow, /APPLE_NOTARY_KEY_BASE64/);
  assert.match(workflow, /HOBOT_CODE_REQUIRE_SIGNED_RELEASE: "1"/);
  assert.match(workflow, /--draft/);
  assert.match(workflow, /gh release download "v\$version"/);
  assert.match(workflow, /diff -u expected-assets\.txt actual-assets\.txt/);
  assert.match(workflow, /verify_checksum "hobot-code-\$version-linux-arm64\.tar\.gz\.sha256"/);
  assert.match(workflow, /verify_checksum "hobot-code-\$version-macos-arm64\.dmg\.sha256"/);
  assert.match(workflow, /gh release edit "v\$version" --draft=false/);
  assert.match(studioPackager, /codesign --force --deep --options runtime --timestamp/);
  assert.match(studioPackager, /notarytool submit/);
  assert.match(studioPackager, /stapler validate/);
  assert.match(studioPackager, /spctl --assess/);
});

test("packager rejects command-line version overrides before doing release work", async () => {
  await assert.rejects(
    () => execFileAsync("/bin/sh", [join(repository, "scripts/package-pi.sh"), "9.9.9"]),
    /command-line versions are not accepted/,
  );
});
