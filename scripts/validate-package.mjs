import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { lstat, readFile, readdir } from "node:fs/promises";
import { dirname, extname, relative, resolve, sep } from "node:path";
import { pathToFileURL } from "node:url";

import { parseDataLock, PI_LOCK_FIELDS, TOOL_LOCK_FIELDS } from "./release-metadata.mjs";

const SOURCE_EXTENSIONS = new Set([".js", ".mjs", ".cjs", ".ts", ".mts", ".cts"]);
const STRIP_TYPES_PROGRAM = [
  'import { readFileSync } from "node:fs";',
  'import { stripTypeScriptTypes } from "node:module";',
  'const source = readFileSync(0, "utf8");',
  'process.stdout.write(stripTypeScriptTypes(source, { mode: "strip" }));',
].join("\n");
const MAX_SOURCE_BYTES = 16 * 1024 * 1024;
export const REQUIRED_PACKAGE_DIRECTORIES = [
  "config",
  "docs",
  "extensions",
  "knowledge",
  "licenses",
  "managed-bin",
  "prompts",
  "runtime",
  "skills",
];

export const REQUIRED_PACKAGE_PATHS = [
  "BUILD_INFO.json",
  "CHANGELOG.md",
  "CONTRIBUTING.md",
  "LICENSE",
  "MANIFEST.sha256",
  "README.md",
  "SECURITY.md",
  "VERSION",
  "PI_RUNTIME",
  "TOOLS_RUNTIME",
  "docs/architecture.md",
  "docs/configuration.md",
  "docs/prime-agent-crush-review.md",
  "docs/user-directory-layout.md",
  "runtime/README.md",
  "runtime/CHANGELOG.md",
  "runtime/docs/index.md",
  "runtime/hobot",
  "runtime/package.json",
  "extensions/rdk/index.ts",
  "skills/rdk-board/SKILL.md",
  "skills/system-info/SKILL.md",
  "skills/workspace-coding/SKILL.md",
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
];

export const REQUIRED_EXECUTABLE_PATHS = [
  "runtime/hobot",
  "managed-bin/fd",
  "managed-bin/rg",
  "hobot-launcher",
  "install.sh",
  "rollback.sh",
];

async function walkFiles(directory) {
  const files = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) files.push(...await walkFiles(path));
    else if (entry.isFile()) files.push(path);
    else throw new Error(`validation tree contains an unsupported filesystem entry: ${path}`);
  }
  return files;
}

export async function validatePackagedKnowledgeLayout(rootDirectory) {
  const root = resolve(rootDirectory);
  const knowledgeRoot = resolve(root, "knowledge");
  const manifest = JSON.parse(await readFile(resolve(knowledgeRoot, "manifest.json"), "utf8"));
  if (!Array.isArray(manifest.documents) || manifest.documents.length === 0) {
    throw new Error("packaged knowledge manifest has no documents");
  }

  const expected = new Set();
  for (const document of manifest.documents) {
    if (!document.file || expected.has(document.file)) {
      throw new Error(`packaged knowledge manifest has duplicate or empty file: ${document.file}`);
    }
    expected.add(document.file);
    const path = resolve(knowledgeRoot, document.file);
    if (!path.startsWith(`${knowledgeRoot}${sep}`)) {
      throw new Error(`packaged knowledge path escapes root: ${document.file}`);
    }
    let stats;
    try {
      stats = await lstat(path);
    } catch (error) {
      if (error?.code === "ENOENT") throw new Error(`release package is missing knowledge/${document.file}`);
      throw error;
    }
    if (!stats.isFile()) throw new Error(`packaged knowledge entry is not a regular file: ${document.file}`);
  }

  const actual = (await walkFiles(knowledgeRoot))
    .filter((path) => extname(path) === ".md")
    .map((path) => relative(knowledgeRoot, path).split(sep).join("/"));
  const unlisted = actual.filter((file) => !expected.has(file));
  if (unlisted.length > 0) throw new Error(`packaged knowledge is missing manifest entries:\n${unlisted.join("\n")}`);
}

async function resolveImport(importer, specifier) {
  const cleanSpecifier = specifier.split(/[?#]/u, 1)[0];
  const candidate = resolve(dirname(importer), cleanSpecifier);
  const candidates = extname(candidate)
    ? [candidate]
    : [candidate, ...[".ts", ".mts", ".js", ".mjs", ".json"].map((suffix) => `${candidate}${suffix}`), resolve(candidate, "index.ts"), resolve(candidate, "index.mjs")];
  for (const path of candidates) {
    try {
      if ((await lstat(path)).isFile()) return path;
    } catch (error) {
      if (error?.code !== "ENOENT" && error?.code !== "ENOTDIR") throw error;
    }
  }
  return undefined;
}

export async function validateRelativeImports(rootDirectory) {
  const root = resolve(rootDirectory);
  const extensionRoot = resolve(root, "extensions");
  const failures = [];
  for (const importer of await walkFiles(extensionRoot)) {
    if (!SOURCE_EXTENSIONS.has(extname(importer))) continue;
    const content = await readFile(importer, "utf8");
    const pattern = /(?:\bfrom\s*|\bimport\s*\(\s*|\bimport\s*)["'](\.{1,2}\/[^"']+)["']/gu;
    for (const match of content.matchAll(pattern)) {
      const resolved = await resolveImport(importer, match[1]);
      if (!resolved) failures.push(`${relative(root, importer)} -> ${match[1]}`);
      else if (resolved !== root && !resolved.startsWith(`${root}${sep}`)) {
        failures.push(`${relative(root, importer)} escapes package root via ${match[1]}`);
      }
    }
  }
  if (failures.length > 0) throw new Error(`relative import validation failed:\n${failures.join("\n")}`);
}

export async function validateSourceSyntax(rootDirectory) {
  const root = resolve(rootDirectory);
  const extensionRoot = resolve(root, "extensions");
  for (const sourcePath of await walkFiles(extensionRoot)) {
    const extension = extname(sourcePath);
    if (!SOURCE_EXTENSIONS.has(extension)) continue;
    try {
      if (extension.includes("ts")) {
        const source = await readFile(sourcePath);
        const inputType = extension === ".cts" ? "commonjs" : "module";
        const stripped = execFileSync(
          process.execPath,
          ["--no-warnings", "--input-type=module", "-e", STRIP_TYPES_PROGRAM],
          { input: source, maxBuffer: MAX_SOURCE_BYTES, stdio: ["pipe", "pipe", "pipe"] },
        );
        execFileSync(
          process.execPath,
          [`--input-type=${inputType}`, "--check", "-"],
          { input: stripped, maxBuffer: MAX_SOURCE_BYTES, stdio: ["pipe", "pipe", "pipe"] },
        );
      } else {
        execFileSync(process.execPath, ["--check", sourcePath], {
          maxBuffer: MAX_SOURCE_BYTES,
          stdio: ["ignore", "pipe", "pipe"],
        });
      }
    } catch (error) {
      const detail = String(error?.stderr || error?.message || error).trim();
      throw new Error(`syntax validation failed for ${relative(root, sourcePath)}${detail ? `:\n${detail}` : ""}`);
    }
  }
}

export async function verifyManifest(rootDirectory) {
  const root = resolve(rootDirectory);
  const content = await readFile(resolve(root, "MANIFEST.sha256"), "utf8");
  const seen = new Set();
  for (const [index, line] of content.trimEnd().split("\n").entries()) {
    const match = /^([0-9a-f]{64})  ([^\r\n]+)$/u.exec(line);
    if (!match) throw new Error(`MANIFEST.sha256:${index + 1} is malformed`);
    const [, expected, name] = match;
    if (seen.has(name)) throw new Error(`MANIFEST.sha256 duplicates ${name}`);
    seen.add(name);
    const path = resolve(root, name);
    if (path === root || !path.startsWith(`${root}${sep}`)) throw new Error(`manifest path escapes package root: ${name}`);
    const stats = await lstat(path);
    if (!stats.isFile()) throw new Error(`manifest entry is not a regular file: ${name}`);
    const actual = createHash("sha256").update(await readFile(path)).digest("hex");
    if (actual !== expected) throw new Error(`manifest checksum mismatch: ${name}`);
  }
  const actualFiles = (await walkFiles(root))
    .map((path) => relative(root, path).split(sep).join("/"))
    .filter((name) => name !== "MANIFEST.sha256");
  const missing = actualFiles.filter((name) => !seen.has(name));
  if (missing.length > 0) throw new Error(`manifest omits release files:\n${missing.join("\n")}`);
}

export async function validateRequiredPackageLayout(rootDirectory) {
  const root = resolve(rootDirectory);
  for (const name of REQUIRED_PACKAGE_DIRECTORIES) {
    const path = resolve(root, name);
    let stats;
    try {
      stats = await lstat(path);
    } catch (error) {
      if (error?.code === "ENOENT") throw new Error(`release package is missing directory ${name}`);
      throw error;
    }
    if (!stats.isDirectory()) throw new Error(`release package entry is not a directory: ${name}`);
  }
  for (const name of REQUIRED_PACKAGE_PATHS) {
    const path = resolve(root, name);
    let stats;
    try {
      stats = await lstat(path);
    } catch (error) {
      if (error?.code === "ENOENT") throw new Error(`release package is missing ${name}`);
      throw error;
    }
    if (!stats.isFile()) throw new Error(`release package entry is not a regular file: ${name}`);
  }
  for (const name of REQUIRED_EXECUTABLE_PATHS) {
    const stats = await lstat(resolve(root, name));
    if ((stats.mode & 0o111) === 0) throw new Error(`release package entry is not executable: ${name}`);
  }
  await validatePackagedKnowledgeLayout(root);
}

export async function validatePackageMetadata(rootDirectory) {
  const root = resolve(rootDirectory);
  const [versionContent, buildInfoContent, runtimePackageContent, changelog, runtimeChangelog] = await Promise.all([
    readFile(resolve(root, "VERSION"), "utf8"),
    readFile(resolve(root, "BUILD_INFO.json"), "utf8"),
    readFile(resolve(root, "runtime/package.json"), "utf8"),
    readFile(resolve(root, "CHANGELOG.md"), "utf8"),
    readFile(resolve(root, "runtime/CHANGELOG.md"), "utf8"),
  ]);
  const version = versionContent.trim();
  const buildInfo = JSON.parse(buildInfoContent);
  const runtimePackage = JSON.parse(runtimePackageContent);
  const pi = parseDataLock(await readFile(resolve(root, "PI_RUNTIME"), "utf8"), "PI_RUNTIME", PI_LOCK_FIELDS);
  const tools = parseDataLock(
    await readFile(resolve(root, "TOOLS_RUNTIME"), "utf8"),
    "TOOLS_RUNTIME",
    TOOL_LOCK_FIELDS,
  );

  if (buildInfo.schemaVersion !== 1) throw new Error("BUILD_INFO.json has an unsupported schemaVersion");
  if (!/^[0-9a-f]{40}$/u.test(buildInfo.commit)) throw new Error("BUILD_INFO.json has an invalid commit");
  if (typeof buildInfo.dirty !== "boolean") throw new Error("BUILD_INFO.json dirty must be boolean");
  if (buildInfo.target !== "linux-arm64") throw new Error(`BUILD_INFO.json has an invalid target: ${buildInfo.target}`);
  const builtAt = new Date(buildInfo.builtAt);
  if (Number.isNaN(builtAt.valueOf()) || builtAt.toISOString() !== buildInfo.builtAt) {
    throw new Error("BUILD_INFO.json has an invalid builtAt timestamp");
  }
  if (buildInfo.version !== version || runtimePackage.version !== version) {
    throw new Error(`release version mismatch: VERSION=${version}, build=${buildInfo.version}, runtime=${runtimePackage.version}`);
  }
  if (runtimeChangelog !== changelog) {
    throw new Error("runtime/CHANGELOG.md must match the Hobot Code CHANGELOG.md");
  }
  const firstRelease = /^## ([^\s]+)$/mu.exec(runtimeChangelog)?.[1];
  if (firstRelease !== version) {
    throw new Error(`runtime changelog first release ${firstRelease ?? "missing"} does not match ${version}`);
  }
  if (
    buildInfo.pi?.version !== pi.PI_VERSION ||
    buildInfo.pi?.commit !== pi.PI_COMMIT ||
    buildInfo.pi?.archiveSha256 !== pi.PI_LINUX_ARM64_SHA256
  ) {
    throw new Error("BUILD_INFO.json Pi provenance does not match PI_RUNTIME");
  }
  if (buildInfo.tools?.fd !== tools.FD_VERSION || buildInfo.tools?.ripgrep !== tools.RIPGREP_VERSION) {
    throw new Error("BUILD_INFO.json tool provenance does not match TOOLS_RUNTIME");
  }
}

export async function validatePackage(rootDirectory, { sourceOnly = false } = {}) {
  const root = resolve(rootDirectory);
  if (!sourceOnly) await validateRequiredPackageLayout(root);
  await validateSourceSyntax(root);
  await validateRelativeImports(root);
  if (sourceOnly) return;
  await validatePackageMetadata(root);
  await verifyManifest(root);
}

async function main() {
  const [mode, rootDirectory] = process.argv.slice(2);
  if ((mode !== "--source" && mode !== "--package") || !rootDirectory) {
    throw new Error("Usage: validate-package.mjs --source <repository> | --package <package-root>");
  }
  await validatePackage(rootDirectory, { sourceOnly: mode === "--source" });
  console.log(mode === "--source" ? "Validated extension import closure" : "Validated release package contents and manifest");
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) await main();
