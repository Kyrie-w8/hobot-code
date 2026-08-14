import { readFile, realpath, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const SEMVER = /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/;
const SHA256 = /^[0-9a-f]{64}$/;
const GIT_COMMIT = /^[0-9a-f]{40}$/;

export function isStrictSemVer(value) {
  return typeof value === "string" && SEMVER.test(value);
}

export const PI_LOCK_FIELDS = new Set([
  "PI_VERSION",
  "PI_COMMIT",
  "PI_LINUX_ARM64_SHA256",
  "PI_LINUX_ARM64_URL",
]);

export const TOOL_LOCK_FIELDS = new Set([
  "FD_VERSION",
  "FD_LINUX_ARM64_SHA256",
  "FD_LINUX_ARM64_BINARY_SHA256",
  "FD_MIT_SHA256",
  "FD_APACHE_SHA256",
  "FD_LINUX_ARM64_URL",
  "RIPGREP_VERSION",
  "RIPGREP_LINUX_ARM64_SHA256",
  "RIPGREP_LINUX_ARM64_BINARY_SHA256",
  "RIPGREP_MIT_SHA256",
  "RIPGREP_UNLICENSE_SHA256",
  "RIPGREP_LINUX_ARM64_URL",
]);

export function parseDataLock(content, label, expectedFields) {
  const values = {};
  const lines = content.split(/\r?\n/u);
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    if (!line || line.startsWith("#")) continue;
    const match = /^([A-Z][A-Z0-9_]*)=([0-9A-Za-z][0-9A-Za-z./:_+-]*)$/u.exec(line);
    if (!match) throw new Error(`${label}:${index + 1} must be a literal KEY=VALUE record`);
    const [, key, value] = match;
    if (!expectedFields.has(key)) throw new Error(`${label}:${index + 1} contains unknown key ${key}`);
    if (Object.hasOwn(values, key)) throw new Error(`${label}:${index + 1} duplicates ${key}`);
    values[key] = value;
  }
  for (const key of expectedFields) {
    if (!Object.hasOwn(values, key)) throw new Error(`${label} is missing ${key}`);
  }
  return values;
}

function requireSha256(value, label) {
  if (!SHA256.test(value)) throw new Error(`${label} must be a lowercase SHA256 digest`);
}

function requirePinnedGithubUrl(value, expectedPath, label) {
  const url = new URL(value);
  if (url.protocol !== "https:" || url.hostname !== "github.com" || url.pathname !== expectedPath || url.search || url.hash) {
    throw new Error(`${label} must be ${`https://github.com${expectedPath}`}`);
  }
}

export async function validateReleaseSource(rootDirectory) {
  const root = resolve(rootDirectory);
  const version = (await readFile(resolve(root, "VERSION"), "utf8")).trim();
  if (!isStrictSemVer(version)) throw new Error(`VERSION is not strict SemVer: ${version}`);

  const packageJson = JSON.parse(await readFile(resolve(root, "pi-runtime/package.json"), "utf8"));
  if (packageJson.version !== version) {
    throw new Error(`VERSION ${version} does not match pi-runtime/package.json ${packageJson.version}`);
  }
  const extensionCatalog = JSON.parse(await readFile(resolve(root, "extensions/catalog.json"), "utf8"));
  if (extensionCatalog.schemaVersion !== 1 || extensionCatalog.apiVersion !== "hobot.extensions/v1") {
    throw new Error("extensions/catalog.json has an unsupported schema or API version");
  }
  if (extensionCatalog.productVersion !== version) {
    throw new Error(`VERSION ${version} does not match extensions/catalog.json ${extensionCatalog.productVersion ?? "missing"}`);
  }
  if (!Array.isArray(extensionCatalog.entries) || extensionCatalog.entries.length === 0) {
    throw new Error("extensions/catalog.json has no extension entries");
  }

  const desktopConfig = JSON.parse(await readFile(resolve(root, "studio/wails.json"), "utf8"));
  if (desktopConfig.name !== "Hobot Code" || desktopConfig.outputfilename !== "HobotCode") {
    throw new Error("studio/wails.json must use the Hobot Code product and executable names");
  }
  if (desktopConfig.info?.productVersion !== version) {
    throw new Error(`VERSION ${version} does not match studio/wails.json ${desktopConfig.info?.productVersion ?? "missing"}`);
  }

  const changelog = await readFile(resolve(root, "CHANGELOG.md"), "utf8");
  const firstRelease = /^## ([^\s]+)$/mu.exec(changelog)?.[1];
  if (firstRelease !== version) throw new Error(`CHANGELOG first release ${firstRelease ?? "missing"} does not match ${version}`);

  const pi = parseDataLock(
    await readFile(resolve(root, "pi-runtime/pi.lock"), "utf8"),
    "pi-runtime/pi.lock",
    PI_LOCK_FIELDS,
  );
  if (!isStrictSemVer(pi.PI_VERSION)) throw new Error(`PI_VERSION is not strict SemVer: ${pi.PI_VERSION}`);
  if (!GIT_COMMIT.test(pi.PI_COMMIT)) throw new Error("PI_COMMIT must be a lowercase 40-character Git commit");
  requireSha256(pi.PI_LINUX_ARM64_SHA256, "PI_LINUX_ARM64_SHA256");
  requirePinnedGithubUrl(
    pi.PI_LINUX_ARM64_URL,
    `/earendil-works/pi/releases/download/v${pi.PI_VERSION}/pi-linux-arm64.tar.gz`,
    "PI_LINUX_ARM64_URL",
  );
  const piCompatibilityContent = await readFile(resolve(root, "pi-runtime/compatibility.json"), "utf8");
  const piCompatibility = JSON.parse(piCompatibilityContent);
  if (piCompatibility.schemaVersion !== 1 || piCompatibility.apiVersion !== "hobot.pi-compatibility/v1") {
    throw new Error("pi-runtime/compatibility.json has an unsupported schema or API version");
  }
  if (piCompatibility.pi?.version !== pi.PI_VERSION || piCompatibility.pi?.commit !== pi.PI_COMMIT) {
    throw new Error("pi-runtime/compatibility.json does not match pi-runtime/pi.lock");
  }
  const piCompatibilitySHA256 = createHash("sha256").update(piCompatibilityContent).digest("hex");

  const tools = parseDataLock(
    await readFile(resolve(root, "pi-runtime/tools.lock"), "utf8"),
    "pi-runtime/tools.lock",
    TOOL_LOCK_FIELDS,
  );
  if (!isStrictSemVer(tools.FD_VERSION)) throw new Error(`FD_VERSION is not strict SemVer: ${tools.FD_VERSION}`);
  if (!isStrictSemVer(tools.RIPGREP_VERSION)) {
    throw new Error(`RIPGREP_VERSION is not strict SemVer: ${tools.RIPGREP_VERSION}`);
  }
  for (const key of TOOL_LOCK_FIELDS) {
    if (key.endsWith("_SHA256")) requireSha256(tools[key], key);
  }
  requirePinnedGithubUrl(
    tools.FD_LINUX_ARM64_URL,
    `/sharkdp/fd/releases/download/v${tools.FD_VERSION}/fd-v${tools.FD_VERSION}-aarch64-unknown-linux-gnu.tar.gz`,
    "FD_LINUX_ARM64_URL",
  );
  requirePinnedGithubUrl(
    tools.RIPGREP_LINUX_ARM64_URL,
    `/BurntSushi/ripgrep/releases/download/${tools.RIPGREP_VERSION}/ripgrep-${tools.RIPGREP_VERSION}-aarch64-unknown-linux-gnu.tar.gz`,
    "RIPGREP_LINUX_ARM64_URL",
  );

  return { root, version, packageJson, extensionCatalog, desktopConfig, pi, piCompatibilitySHA256, tools };
}

export async function writeBuildInfo(rootDirectory, stageDirectory, options) {
  const release = await validateReleaseSource(rootDirectory);
  if (!GIT_COMMIT.test(options.commit)) throw new Error(`Invalid Hobot Code commit: ${options.commit}`);
  if (options.dirty !== "0" && options.dirty !== "1") throw new Error("dirty must be 0 or 1");
  const builtAt = new Date(options.builtAt);
  if (Number.isNaN(builtAt.valueOf())) throw new Error(`Invalid build timestamp: ${options.builtAt}`);
  const payload = {
    schemaVersion: 3,
    version: release.version,
    commit: options.commit,
    dirty: options.dirty === "1",
    builtAt: builtAt.toISOString(),
    target: "linux-arm64",
    agentdSha256: createHash("sha256").update(await readFile(resolve(stageDirectory, "agentd"))).digest("hex"),
    pi: {
      version: release.pi.PI_VERSION,
      commit: release.pi.PI_COMMIT,
      archiveSha256: release.pi.PI_LINUX_ARM64_SHA256,
      compatibilitySha256: release.piCompatibilitySHA256,
    },
    tools: {
      fd: release.tools.FD_VERSION,
      ripgrep: release.tools.RIPGREP_VERSION,
    },
  };
  await writeFile(resolve(stageDirectory, "BUILD_INFO.json"), `${JSON.stringify(payload, null, 2)}\n`, { mode: 0o644 });
  return payload;
}

async function main() {
  const [command, rootDirectory, stageDirectory, commit, dirty, builtAt] = process.argv.slice(2);
  if (command === "validate" && rootDirectory && !stageDirectory) {
    const release = await validateReleaseSource(rootDirectory);
    console.log(`Validated Hobot Code ${release.version}, Pi ${release.pi.PI_VERSION}, fd ${release.tools.FD_VERSION}, and ripgrep ${release.tools.RIPGREP_VERSION}`);
    return;
  }
  if (command === "write" && rootDirectory && stageDirectory && commit && dirty && builtAt) {
    await writeBuildInfo(rootDirectory, stageDirectory, { commit, dirty, builtAt });
    return;
  }
  throw new Error("Usage: release-metadata.mjs validate <root> | write <root> <stage> <commit> <dirty:0|1> <builtAt>");
}

if (process.argv[1] && await realpath(process.argv[1]) === await realpath(fileURLToPath(import.meta.url))) await main();
