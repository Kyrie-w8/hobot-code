import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { access, chmod, copyFile, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
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
  for (const name of ["agentd-protocol.md", "architecture.md", "configuration.md", "prime-agent-crush-review.md", "releasing.md", "user-directory-layout.md"]) {
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
  await writeFile(join(root, "agentd"), header);
  await validateAgentdBinary(root);
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
  await writeFile(join(runtime, "hobot"), "#!/bin/sh\nprintf 'umask=%s\\nliteral=%s\\nargs=%s\\n' \"$(umask)\" \"${LITERAL_VALUE:-}\" \"$*\"\n");
  await writeFile(join(runtime, "release.sh"), "#!/bin/sh\nprintf 'release:%s\\n' \"$*\"\n");
  await writeFile(join(runtime, "uninstall.sh"), "#!/bin/sh\nprintf 'uninstall:%s\\n' \"$*\"\n");
  await Promise.all(["hobot", "release.sh", "uninstall.sh"].map((name) => chmod(join(runtime, name), 0o755)));
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
  await assert.rejects(() => access(marker));
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
  assert.doesNotMatch(launcher, /^\s*\.\s+.*hobot\.env/m);
  assert.match(launcher, /umask "\$original_umask"/);
  assert.match(launcher, /release\.sh update/);
  assert.match(launcher, /uninstall\.sh/);
  assert.match(releaseInstaller, /curl --proto '=https' --tlsv1\.2 -fsSL/);
  assert.doesNotMatch(releaseInstaller, /\bwget\b/);
  assert.match(releaseInstaller, /--max-filesize/);
  assert.match(releaseInstaller, /checksum_target/);
  assert.match(releaseInstaller, /unsupported entry type/);
  assert.match(uninstaller, /--purge/);
  assert.match(uninstaller, /Stop active Hobot Code processes/);
  assert.match(workflow, /attest-build-provenance@v2/);
  assert.match(workflow, /test "\$\{GITHUB_REF_NAME\}" = "v\$\{version\}"/);
  assert.match(workflow, /environment: production/);
  assert.match(workflow, /MACOS_CERTIFICATE_BASE64/);
  assert.match(workflow, /APPLE_NOTARY_KEY_BASE64/);
  assert.match(workflow, /HOBOT_CODE_REQUIRE_SIGNED_RELEASE: "1"/);
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
