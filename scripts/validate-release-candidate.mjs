#!/usr/bin/env node

import { createHash, randomBytes } from "node:crypto";
import { constants } from "node:fs";
import { link, lstat, mkdir, open, realpath, unlink } from "node:fs/promises";
import { basename, dirname, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

import { parseStrictJSON } from "../extensions/rdk/strict-json.mjs";
import { parseAcceptanceMatrix } from "./validate-board-acceptance.mjs";
import { validatePackage } from "./validate-package.mjs";
import { parsePiCompatibilityContract } from "./validate-pi-compatibility.mjs";

const EVIDENCE_SCHEMA = "hobot.release-evidence/v1";
const MAX_METADATA_BYTES = 2 * 1024 * 1024;
const MAX_ARCHIVE_BYTES = 4 * 1024 * 1024 * 1024;
const DEFAULT_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000;
const DEFAULT_CLOCK_SKEW_MS = 5 * 60 * 1000;
const SEMVER = /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/u;
const SHA256 = /^[0-9a-f]{64}$/u;
const GIT_COMMIT = /^[0-9a-f]{40}$/u;
const TIMESTAMP = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/u;

function object(value, label) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${label} must be an object`);
  return value;
}

function exactKeys(value, allowed, label) {
  for (const key of Object.keys(value)) if (!allowed.has(key)) throw new Error(`${label} contains unknown field ${key}`);
  for (const key of allowed) if (!Object.hasOwn(value, key)) throw new Error(`${label} is missing ${key}`);
}

function text(value, label, maximum, pattern) {
  if (typeof value !== "string" || value.length === 0 || value.length > maximum || value.trim() !== value || !pattern.test(value)) {
    throw new Error(`${label} is invalid`);
  }
  return value;
}

function timestamp(value, label) {
  const normalized = text(value, label, 64, TIMESTAMP);
  const parsed = Date.parse(normalized);
  if (!Number.isFinite(parsed)) throw new Error(`${label} is invalid`);
  return { value: normalized, milliseconds: parsed };
}

export function parseBuildInfo(content, label = "BUILD_INFO.json") {
  const info = object(parseStrictJSON(content, label), label);
  exactKeys(info, new Set([
    "schemaVersion", "version", "commit", "dirty", "builtAt", "target", "agentdSha256", "pi", "tools",
  ]), label);
  if (info.schemaVersion !== 3) throw new Error(`${label}.schemaVersion is unsupported`);
  const version = text(info.version, `${label}.version`, 64, SEMVER);
  const commit = text(info.commit, `${label}.commit`, 40, GIT_COMMIT);
  if (typeof info.dirty !== "boolean") throw new Error(`${label}.dirty must be boolean`);
  const builtAt = timestamp(info.builtAt, `${label}.builtAt`).value;
  if (info.target !== "linux-arm64") throw new Error(`${label}.target must be linux-arm64`);
  const agentdSha256 = text(info.agentdSha256, `${label}.agentdSha256`, 64, SHA256);

  const pi = object(info.pi, `${label}.pi`);
  exactKeys(pi, new Set(["version", "commit", "archiveSha256", "compatibilitySha256"]), `${label}.pi`);
  const normalizedPi = {
    version: text(pi.version, `${label}.pi.version`, 64, SEMVER),
    commit: text(pi.commit, `${label}.pi.commit`, 40, GIT_COMMIT),
    archiveSha256: text(pi.archiveSha256, `${label}.pi.archiveSha256`, 64, SHA256),
    compatibilitySha256: text(pi.compatibilitySha256, `${label}.pi.compatibilitySha256`, 64, SHA256),
  };

  const tools = object(info.tools, `${label}.tools`);
  exactKeys(tools, new Set(["fd", "ripgrep"]), `${label}.tools`);
  const normalizedTools = {
    fd: text(tools.fd, `${label}.tools.fd`, 64, SEMVER),
    ripgrep: text(tools.ripgrep, `${label}.tools.ripgrep`, 64, SEMVER),
  };
  return {
    schemaVersion: 3, version, commit, dirty: info.dirty, builtAt,
    target: "linux-arm64", agentdSha256, pi: normalizedPi, tools: normalizedTools,
  };
}

function requireDigest(value, label) {
  return text(value, label, 64, SHA256);
}

function requireDate(value, label) {
  const date = value instanceof Date ? value : new Date(value);
  if (!Number.isFinite(date.valueOf())) throw new Error(`${label} is invalid`);
  return date;
}

export function validateReleaseEvidence(input, options = {}) {
  const version = text(input.version, "expected version", 64, SEMVER);
  const expectedCommit = text(input.expectedCommit, "expected commit", 40, GIT_COMMIT);
  const archiveName = text(input.archiveName, "archive name", 160, /^[0-9A-Za-z][0-9A-Za-z._-]+$/u);
  const expectedArchiveName = `hobot-code-${version}-linux-arm64.tar.gz`;
  if (archiveName !== expectedArchiveName) throw new Error(`archive name must be ${expectedArchiveName}`);
  const matrixName = text(input.matrixName, "matrix name", 160, /^[0-9A-Za-z][0-9A-Za-z._-]+$/u);
  const expectedMatrixName = `hobot-code-${version}-board-acceptance.json`;
  if (matrixName !== expectedMatrixName) throw new Error(`matrix name must be ${expectedMatrixName}`);
  const archiveSha256 = requireDigest(input.archiveSha256, "archive SHA-256");
  const matrixSha256 = requireDigest(input.matrixSha256, "matrix SHA-256");
  const manifestSha256 = requireDigest(input.manifestSha256, "manifest SHA-256");
  const piCompatibilitySha256 = requireDigest(input.piCompatibilitySha256, "Pi compatibility SHA-256");
  const agentdSha256 = requireDigest(input.agentdSha256, "agentd SHA-256");
  const buildInfo = input.buildInfo;
  const matrix = input.matrix;
  const now = requireDate(options.now ?? new Date(), "current time");
  const maxAgeMs = options.maxAgeMs ?? DEFAULT_MAX_AGE_MS;
  const clockSkewMs = options.clockSkewMs ?? DEFAULT_CLOCK_SKEW_MS;
  if (!Number.isFinite(maxAgeMs) || maxAgeMs <= 0 || !Number.isFinite(clockSkewMs) || clockSkewMs < 0) {
    throw new Error("release evidence time limits are invalid");
  }

  if (buildInfo.version !== version || buildInfo.commit !== expectedCommit || buildInfo.target !== "linux-arm64") {
    throw new Error("package build identity does not match the requested release tag");
  }
  if (buildInfo.dirty) throw new Error("public releases require a clean package build");
  if (buildInfo.agentdSha256 !== agentdSha256) throw new Error("BUILD_INFO.json agentd SHA-256 does not match the package");
  if (buildInfo.pi.compatibilitySha256 !== piCompatibilitySha256) {
    throw new Error("BUILD_INFO.json Pi compatibility SHA-256 does not match the package");
  }

  if (matrix.status !== "pass" || matrix.selection !== "all" || matrix.issues.length !== 0 || matrix.build === null) {
    throw new Error("the complete three-board acceptance matrix must pass without issues");
  }
  if (matrix.expectedVersion !== version || matrix.contractSha256 !== piCompatibilitySha256) {
    throw new Error("board acceptance version or Pi compatibility contract does not match the package");
  }
  const expectedMatrixBuild = { version, agentdSha256, manifestSha256, piCompatibilitySha256 };
  for (const [key, value] of Object.entries(expectedMatrixBuild)) {
    if (matrix.build[key] !== value) throw new Error(`board acceptance build ${key} does not match the package`);
  }

  const builtAt = timestamp(buildInfo.builtAt, "BUILD_INFO.json.builtAt");
  if (builtAt.milliseconds > now.valueOf() + clockSkewMs) throw new Error("package build timestamp is in the future");
  const captured = matrix.scenarios.flatMap((scenario) => scenario.boards.map((board) => {
    if (board.status !== "pass") throw new Error("the complete three-board acceptance matrix contains a non-passing board");
    return timestamp(board.capturedAt, `board acceptance ${scenario.id}/${board.boardId}`).milliseconds;
  }));
  if (captured.length === 0) throw new Error("board acceptance matrix contains no board evidence");
  for (const capturedAt of captured) {
    if (capturedAt < builtAt.milliseconds - clockSkewMs) throw new Error("board acceptance predates the candidate build");
    if (capturedAt > now.valueOf() + clockSkewMs) throw new Error("board acceptance timestamp is in the future");
    if (capturedAt < now.valueOf() - maxAgeMs) throw new Error("board acceptance evidence is stale");
  }
  const oldestEvidenceAt = new Date(Math.min(...captured)).toISOString();
  const newestEvidenceAt = new Date(Math.max(...captured)).toISOString();

  return {
    schema: EVIDENCE_SCHEMA,
    version,
    sourceCommit: expectedCommit,
    artifact: { name: archiveName, sha256: archiveSha256 },
    package: {
      builtAt: buildInfo.builtAt,
      agentdSha256,
      manifestSha256,
      piCompatibilitySha256,
    },
    boardAcceptance: {
      matrixName,
      matrixSha256,
      requiredBoards: [...matrix.requiredBoards],
      scenarios: matrix.scenarios.map((scenario) => scenario.id),
      oldestEvidenceAt,
      newestEvidenceAt,
      status: "pass",
    },
  };
}

async function readRegularFile(path, maximum, label) {
  const before = await lstat(path);
  if (!before.isFile() || before.isSymbolicLink() || before.size <= 0 || before.size > maximum) {
    throw new Error(`${label} is not a bounded regular file`);
  }
  const file = await open(path, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0));
  try {
    const opened = await file.stat();
    if (opened.dev !== before.dev || opened.ino !== before.ino || opened.size !== before.size) throw new Error(`${label} changed while opening`);
    const content = await file.readFile();
    const after = await file.stat();
    if (content.length !== before.size || after.size !== before.size || after.mtimeMs !== before.mtimeMs) throw new Error(`${label} changed while reading`);
    return content;
  } finally {
    await file.close();
  }
}

async function hashRegularFile(path, maximum, label) {
  const before = await lstat(path);
  if (!before.isFile() || before.isSymbolicLink() || before.size <= 0 || before.size > maximum) {
    throw new Error(`${label} is not a bounded regular file`);
  }
  const file = await open(path, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0));
  const hash = createHash("sha256");
  const buffer = Buffer.allocUnsafe(1024 * 1024);
  let position = 0;
  try {
    const opened = await file.stat();
    if (opened.dev !== before.dev || opened.ino !== before.ino || opened.size !== before.size) throw new Error(`${label} changed while opening`);
    while (position < before.size) {
      const { bytesRead } = await file.read(buffer, 0, Math.min(buffer.length, before.size - position), position);
      if (bytesRead === 0) throw new Error(`${label} changed while hashing`);
      hash.update(buffer.subarray(0, bytesRead));
      position += bytesRead;
    }
    const after = await file.stat();
    if (after.size !== before.size || after.mtimeMs !== before.mtimeMs) throw new Error(`${label} changed while hashing`);
    return hash.digest("hex");
  } finally {
    await file.close();
  }
}

async function writeEvidence(path, evidence) {
  const destination = resolve(path);
  const encoded = Buffer.from(`${JSON.stringify(evidence, null, 2)}\n`);
  if (encoded.length > MAX_METADATA_BYTES) throw new Error("release evidence exceeds the size limit");
  await mkdir(dirname(destination), { recursive: true, mode: 0o700 });
  const temporary = `${destination}.tmp.${process.pid}.${randomBytes(6).toString("hex")}`;
  const file = await open(temporary, "wx", 0o644);
  try {
    await file.writeFile(encoded);
    await file.sync();
    await file.close();
    await link(temporary, destination);
    await unlink(temporary);
  } catch (error) {
    await file.close().catch(() => {});
    await unlink(temporary).catch(() => {});
    throw error;
  }
}

export function parseArguments(args) {
  const options = { packageRoot: "", archive: "", matrix: "", expectedVersion: "", expectedCommit: "", output: "" };
  for (let index = 0; index < args.length; index += 1) {
    const argument = args[index];
    const next = () => {
      const value = args[index + 1];
      if (!value) throw new Error(`${argument} requires a value`);
      index += 1;
      return value;
    };
    switch (argument) {
    case "--package-root": options.packageRoot = next(); break;
    case "--archive": options.archive = next(); break;
    case "--matrix": options.matrix = next(); break;
    case "--expected-version": options.expectedVersion = next(); break;
    case "--expected-commit": options.expectedCommit = next(); break;
    case "--output": options.output = next(); break;
    case "--help": options.help = true; break;
    default: throw new Error(`unknown option: ${argument}`);
    }
  }
  if (options.help) return options;
  for (const key of ["packageRoot", "archive", "matrix", "expectedVersion", "expectedCommit", "output"]) {
    if (!options[key]) throw new Error(`--${key.replace(/[A-Z]/g, (letter) => `-${letter.toLowerCase()}`)} is required`);
  }
  text(options.expectedVersion, "--expected-version", 64, SEMVER);
  text(options.expectedCommit, "--expected-commit", 40, GIT_COMMIT);
  return options;
}

function usage() {
  return `Hobot Code public release candidate verifier

Usage:
  node scripts/validate-release-candidate.mjs \\
    --package-root DIR --archive FILE --matrix FILE \\
    --expected-version VERSION --expected-commit COMMIT --output FILE
`;
}

async function main() {
  const options = parseArguments(process.argv.slice(2));
  if (options.help) { process.stdout.write(usage()); return; }
  const packageRoot = resolve(options.packageRoot);
  const archivePath = resolve(options.archive);
  const matrixPath = resolve(options.matrix);
  const outputPath = resolve(options.output);
  if (outputPath === packageRoot || outputPath.startsWith(`${packageRoot}${sep}`)) {
    throw new Error("release evidence output must be outside the candidate package");
  }
  if (outputPath === archivePath || outputPath === matrixPath) {
    throw new Error("release evidence output must not replace an input file");
  }
  const rootInfo = await lstat(packageRoot);
  if (!rootInfo.isDirectory() || rootInfo.isSymbolicLink()) throw new Error("package root must be a real directory");
  await validatePackage(packageRoot);

  const [buildInfoContent, contractContent, matrixContent, archiveSha256, manifestSha256, agentdSha256] = await Promise.all([
    readRegularFile(resolve(packageRoot, "BUILD_INFO.json"), MAX_METADATA_BYTES, "BUILD_INFO.json"),
    readRegularFile(resolve(packageRoot, "PI_COMPATIBILITY.json"), MAX_METADATA_BYTES, "PI_COMPATIBILITY.json"),
    readRegularFile(matrixPath, MAX_METADATA_BYTES, "board acceptance matrix"),
    hashRegularFile(archivePath, MAX_ARCHIVE_BYTES, "release archive"),
    hashRegularFile(resolve(packageRoot, "MANIFEST.sha256"), MAX_METADATA_BYTES, "MANIFEST.sha256"),
    hashRegularFile(resolve(packageRoot, "agentd"), MAX_ARCHIVE_BYTES, "agentd"),
  ]);
  const contract = parsePiCompatibilityContract(contractContent.toString("utf8"), "PI_COMPATIBILITY.json");
  const buildInfo = parseBuildInfo(buildInfoContent.toString("utf8"));
  const matrix = parseAcceptanceMatrix(matrixContent.toString("utf8"), contract);
  const evidence = validateReleaseEvidence({
    version: options.expectedVersion,
    expectedCommit: options.expectedCommit,
    archiveName: basename(archivePath),
    matrixName: basename(matrixPath),
    archiveSha256,
    matrixSha256: createHash("sha256").update(matrixContent).digest("hex"),
    manifestSha256,
    piCompatibilitySha256: createHash("sha256").update(contractContent).digest("hex"),
    agentdSha256,
    buildInfo,
    matrix,
  });
  await writeEvidence(outputPath, evidence);
  process.stdout.write(`${JSON.stringify(evidence, null, 2)}\n`);
}

if (process.argv[1] && await realpath(process.argv[1]) === await realpath(fileURLToPath(import.meta.url))) {
  await main().catch((error) => {
    process.stderr.write(`Release candidate check failed: ${error instanceof Error ? error.message : "unknown error"}\n`);
    process.exitCode = 1;
  });
}
