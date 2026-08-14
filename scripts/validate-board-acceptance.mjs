#!/usr/bin/env node

import { createHash, randomBytes } from "node:crypto";
import { constants } from "node:fs";
import { lstat, mkdir, open, readdir, realpath, rename, unlink } from "node:fs/promises";
import { dirname, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

import { parseStrictJSON } from "../extensions/rdk/strict-json.mjs";
import { parsePiCompatibilityContract } from "./validate-pi-compatibility.mjs";

const REPORT_SCHEMA = "hobot.pi-board-compatibility/v1";
const MATRIX_SCHEMA = "hobot.pi-board-compatibility-matrix/v1";
const MAX_REPORT_BYTES = 1024 * 1024;
const MAX_REPORTS = 64;
const MATRIX_PRIVACY = "No board addresses, hostnames, prompts, commands, model output, paths, or credentials are retained.";
const ID = /^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$/u;
const SEMVER = /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/u;
const SHA256 = /^[0-9a-f]{64}$/u;
const TIMESTAMP = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/u;
const MODEL_EGRESS_CHECKS = new Map([
  ["anthropic-test", "anthropic-messages"],
  ["chat-test", "openai-completions"],
  ["responses-test", "openai-responses"],
]);
const NAMED_SCENARIO_CHECKS = new Map([
  ["rpc-background", new Set([
    "persistent-task", "tool-approval", "second-turn", "image-input",
    "reconnect-no-duplicate", "side-agent-multiturn", "side-agent-flat-parent", "main-agent-remains-active",
  ])],
  ["session-recovery", new Set([
    "context-compaction", "interrupted-session-recovery", "history-edit-branch", "fresh-client-connections",
  ])],
  ["extension-safety", new Set([
    "packaged-resource-discovery", "parallel-extension-tools", "permission-hook", "workspace-write-lease",
  ])],
  ["tui-basics", new Set([
    "ordinary-user-tui", "chinese-input", "thinking-stream", "editor-edit", "persistent-detach",
  ])],
  ["readiness-diagnostics", new Set([
    "read-only-inspection", "cli-json", "confirmation-required", "bounded-permission-repair",
    "privacy-no-support-file",
  ])],
  ["install-lifecycle", new Set([
    "isolated-root", "first-install", "ordinary-user-launcher", "upgrade-preserves-user-data",
    "failed-upgrade-restores-runtime", "rollback-restores-runtime", "uninstall-preserves-user-data",
  ])],
]);

function object(value, label) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${label} must be an object`);
  return value;
}

function exactKeys(value, allowed, label) {
  const keys = Object.keys(value);
  for (const key of keys) if (!allowed.has(key)) throw new Error(`${label} contains unknown field ${key}`);
  for (const key of allowed) if (!Object.hasOwn(value, key)) throw new Error(`${label} is missing ${key}`);
}

function text(value, label, maximum = 128, pattern = null) {
  if (typeof value !== "string" || value.length === 0 || value.length > maximum || value.trim() !== value || /[\u0000-\u001f\u007f]/u.test(value) || (pattern && !pattern.test(value))) {
    throw new Error(`${label} is invalid`);
  }
  return value;
}

function parseModelEgressChecks(checks, label) {
  if (!Array.isArray(checks) || checks.length !== MODEL_EGRESS_CHECKS.size) throw new Error(`${label} must contain all managed protocol checks`);
  const seen = new Set();
  return checks.map((raw, index) => {
    const check = object(raw, `${label}[${index}]`);
    exactKeys(check, new Set(["provider", "protocol", "status"]), `${label}[${index}]`);
    const provider = text(check.provider, `${label}[${index}].provider`, 64, ID);
    const protocol = text(check.protocol, `${label}[${index}].protocol`, 64, ID);
    if (seen.has(provider) || MODEL_EGRESS_CHECKS.get(provider) !== protocol) throw new Error(`${label} has an unexpected or duplicate provider check`);
    seen.add(provider);
    if (check.status !== "pass" && check.status !== "fail") throw new Error(`${label}[${index}].status is invalid`);
    return { provider, protocol, status: check.status };
  });
}

function parseGenericChecks(checks, label) {
  if (!Array.isArray(checks) || checks.length === 0 || checks.length > 64) throw new Error(`${label} must contain 1-64 checks`);
  const seen = new Set();
  return checks.map((raw, index) => {
    const check = object(raw, `${label}[${index}]`);
    exactKeys(check, new Set(["name", "status"]), `${label}[${index}]`);
    const name = text(check.name, `${label}[${index}].name`, 64, ID);
    if (seen.has(name)) throw new Error(`${label} contains duplicate check ${name}`);
    seen.add(name);
    if (check.status !== "pass" && check.status !== "fail") throw new Error(`${label}[${index}].status is invalid`);
    return { name, status: check.status };
  });
}

function parseNamedChecks(checks, expected, label) {
  const parsed = parseGenericChecks(checks, label);
  if (parsed.length !== expected.size || parsed.some((check) => !expected.has(check.name))) {
    throw new Error(`${label} does not match the declared scenario checks`);
  }
  return parsed;
}

export function parseAcceptanceReport(content, contract, label = "board acceptance report") {
  const report = object(parseStrictJSON(content, label), label);
  exactKeys(report, new Set(["schema", "scenario", "status", "capturedAt", "target", "build", "checks"]), label);
  if (report.schema !== REPORT_SCHEMA) throw new Error(`${label} has an unsupported schema`);
  const scenarios = new Set(contract.boardAcceptance.scenarios.map((scenario) => scenario.id));
  const scenario = text(report.scenario, `${label}.scenario`, 64, ID);
  if (!scenarios.has(scenario)) throw new Error(`${label} references unknown scenario ${scenario}`);
  if (report.status !== "pass" && report.status !== "fail") throw new Error(`${label}.status is invalid`);
  const capturedAt = text(report.capturedAt, `${label}.capturedAt`, 64, TIMESTAMP);
  if (!Number.isFinite(Date.parse(capturedAt))) throw new Error(`${label}.capturedAt is invalid`);

  const target = object(report.target, `${label}.target`);
  exactKeys(target, new Set(["architecture", "boardId", "rdkOsVersion"]), `${label}.target`);
  if (target.architecture !== "aarch64" && target.architecture !== "arm64") throw new Error(`${label}.target.architecture is invalid`);
  const boardId = text(target.boardId, `${label}.target.boardId`, 16, ID);
  if (!contract.policy.requiredBoards.includes(boardId)) throw new Error(`${label} references unexpected board ${boardId}`);
  const rdkOsVersion = text(target.rdkOsVersion, `${label}.target.rdkOsVersion`, 64, SEMVER);

  const build = object(report.build, `${label}.build`);
  exactKeys(build, new Set(["version", "agentdSha256", "manifestSha256", "piCompatibilitySha256"]), `${label}.build`);
  const normalizedBuild = {
    version: text(build.version, `${label}.build.version`, 64, SEMVER),
    agentdSha256: text(build.agentdSha256, `${label}.build.agentdSha256`, 64, SHA256),
    manifestSha256: text(build.manifestSha256, `${label}.build.manifestSha256`, 64, SHA256),
    piCompatibilitySha256: text(build.piCompatibilitySha256, `${label}.build.piCompatibilitySha256`, 64, SHA256),
  };
  const checks = scenario === "model-egress-runtime"
    ? parseModelEgressChecks(report.checks, `${label}.checks`)
    : NAMED_SCENARIO_CHECKS.has(scenario)
      ? parseNamedChecks(report.checks, NAMED_SCENARIO_CHECKS.get(scenario), `${label}.checks`)
      : parseGenericChecks(report.checks, `${label}.checks`);
  const expectedStatus = checks.some((check) => check.status === "fail") ? "fail" : "pass";
  if (report.status !== expectedStatus) throw new Error(`${label}.status does not match its checks`);
  return {
    schema: REPORT_SCHEMA, scenario, status: report.status, capturedAt,
    target: { architecture: target.architecture, boardId, rdkOsVersion }, build: normalizedBuild, checks,
  };
}

function buildKey(build) {
  return [build.version, build.agentdSha256, build.manifestSha256, build.piCompatibilitySha256].join("|");
}

export function buildAcceptanceMatrix(contract, reports, { expectedVersion, contractSha256, scenario = "" }) {
  if (!SEMVER.test(expectedVersion)) throw new Error("expected version must be strict SemVer");
  if (!SHA256.test(contractSha256)) throw new Error("contract SHA-256 is invalid");
  const declared = contract.boardAcceptance.scenarios.map((entry) => entry.id);
  if (scenario && !declared.includes(scenario)) throw new Error(`unknown selected scenario ${scenario}`);
  const selected = scenario ? [scenario] : declared;
  const requiredBoards = [...contract.policy.requiredBoards];
  const relevant = reports.filter((report) => selected.includes(report.scenario));
  if (relevant.length !== reports.length) throw new Error("reports contain a scenario outside the selected matrix");
  const byKey = new Map();
  for (const report of relevant) {
    const key = `${report.scenario}/${report.target.boardId}`;
    if (byKey.has(key)) throw new Error(`duplicate report for ${key}`);
    byKey.set(key, report);
  }
  const builds = new Set(relevant.map((report) => buildKey(report.build)));
  const build = builds.size === 1 ? relevant[0]?.build : undefined;
  const issues = [];
  if (relevant.length > 0 && builds.size !== 1) issues.push("mixed-builds");
  if (build && build.version !== expectedVersion) issues.push("unexpected-version");
  if (build && build.piCompatibilitySha256 !== contractSha256) issues.push("contract-mismatch");
  const scenarios = selected.map((scenarioId) => {
    const boards = requiredBoards.map((boardId) => {
      const report = byKey.get(`${scenarioId}/${boardId}`);
      if (!report) return { boardId, status: "missing" };
      return {
        boardId, status: report.status, capturedAt: report.capturedAt,
        architecture: report.target.architecture, rdkOsVersion: report.target.rdkOsVersion,
      };
    });
    const status = boards.some((board) => board.status === "fail") ? "fail" : boards.some((board) => board.status === "missing") ? "incomplete" : "pass";
    return { id: scenarioId, status, boards };
  });
  if (scenarios.some((entry) => entry.status === "fail")) issues.push("scenario-failed");
  if (scenarios.some((entry) => entry.status === "incomplete")) issues.push("missing-reports");
  const status = issues.some((issue) => issue !== "missing-reports") ? "fail" : issues.length > 0 ? "incomplete" : "pass";
  return {
    schema: MATRIX_SCHEMA, status, expectedVersion, contractSha256,
    selection: scenario || "all", requiredBoards, build: build ?? null,
    scenarios, issues: [...new Set(issues)].sort(),
    privacy: MATRIX_PRIVACY,
  };
}

export function parseAcceptanceMatrix(content, contract, label = "board acceptance matrix") {
  const matrix = object(parseStrictJSON(content, label), label);
  exactKeys(matrix, new Set([
    "schema", "status", "expectedVersion", "contractSha256", "selection", "requiredBoards",
    "build", "scenarios", "issues", "privacy",
  ]), label);
  if (matrix.schema !== MATRIX_SCHEMA) throw new Error(`${label} has an unsupported schema`);
  const expectedVersion = text(matrix.expectedVersion, `${label}.expectedVersion`, 64, SEMVER);
  const contractSha256 = text(matrix.contractSha256, `${label}.contractSha256`, 64, SHA256);
  const declaredScenarios = contract.boardAcceptance.scenarios.map((entry) => entry.id);
  const selection = matrix.selection === "all"
    ? "all"
    : text(matrix.selection, `${label}.selection`, 64, ID);
  if (selection !== "all" && !declaredScenarios.includes(selection)) throw new Error(`${label}.selection is unknown`);
  const selectedScenarios = selection === "all" ? declaredScenarios : [selection];

  if (!Array.isArray(matrix.requiredBoards) || matrix.requiredBoards.length !== contract.policy.requiredBoards.length) {
    throw new Error(`${label}.requiredBoards does not match the compatibility contract`);
  }
  const requiredBoards = matrix.requiredBoards.map((board, index) => text(board, `${label}.requiredBoards[${index}]`, 16, ID));
  if (requiredBoards.some((board, index) => board !== contract.policy.requiredBoards[index])) {
    throw new Error(`${label}.requiredBoards does not match the compatibility contract`);
  }

  let build = null;
  if (matrix.build !== null) {
    const rawBuild = object(matrix.build, `${label}.build`);
    exactKeys(rawBuild, new Set(["version", "agentdSha256", "manifestSha256", "piCompatibilitySha256"]), `${label}.build`);
    build = {
      version: text(rawBuild.version, `${label}.build.version`, 64, SEMVER),
      agentdSha256: text(rawBuild.agentdSha256, `${label}.build.agentdSha256`, 64, SHA256),
      manifestSha256: text(rawBuild.manifestSha256, `${label}.build.manifestSha256`, 64, SHA256),
      piCompatibilitySha256: text(rawBuild.piCompatibilitySha256, `${label}.build.piCompatibilitySha256`, 64, SHA256),
    };
  }

  if (!Array.isArray(matrix.scenarios) || matrix.scenarios.length !== selectedScenarios.length) {
    throw new Error(`${label}.scenarios does not match its selection`);
  }
  let presentBoards = 0;
  const scenarios = matrix.scenarios.map((rawScenario, scenarioIndex) => {
    const scenario = object(rawScenario, `${label}.scenarios[${scenarioIndex}]`);
    exactKeys(scenario, new Set(["id", "status", "boards"]), `${label}.scenarios[${scenarioIndex}]`);
    const id = text(scenario.id, `${label}.scenarios[${scenarioIndex}].id`, 64, ID);
    if (id !== selectedScenarios[scenarioIndex]) throw new Error(`${label}.scenarios is incomplete or out of order`);
    if (!Array.isArray(scenario.boards) || scenario.boards.length !== requiredBoards.length) {
      throw new Error(`${label}.scenarios[${scenarioIndex}].boards is incomplete`);
    }
    const boards = scenario.boards.map((rawBoard, boardIndex) => {
      const board = object(rawBoard, `${label}.scenarios[${scenarioIndex}].boards[${boardIndex}]`);
      const boardId = text(board.boardId, `${label}.scenarios[${scenarioIndex}].boards[${boardIndex}].boardId`, 16, ID);
      if (boardId !== requiredBoards[boardIndex]) throw new Error(`${label}.scenarios[${scenarioIndex}].boards is incomplete or out of order`);
      if (board.status === "missing") {
        exactKeys(board, new Set(["boardId", "status"]), `${label}.scenarios[${scenarioIndex}].boards[${boardIndex}]`);
        return { boardId, status: "missing" };
      }
      exactKeys(board, new Set(["boardId", "status", "capturedAt", "architecture", "rdkOsVersion"]), `${label}.scenarios[${scenarioIndex}].boards[${boardIndex}]`);
      if (board.status !== "pass" && board.status !== "fail") throw new Error(`${label}.scenarios[${scenarioIndex}].boards[${boardIndex}].status is invalid`);
      const capturedAt = text(board.capturedAt, `${label}.scenarios[${scenarioIndex}].boards[${boardIndex}].capturedAt`, 64, TIMESTAMP);
      if (!Number.isFinite(Date.parse(capturedAt))) throw new Error(`${label}.scenarios[${scenarioIndex}].boards[${boardIndex}].capturedAt is invalid`);
      if (board.architecture !== "aarch64" && board.architecture !== "arm64") throw new Error(`${label}.scenarios[${scenarioIndex}].boards[${boardIndex}].architecture is invalid`);
      const rdkOsVersion = text(board.rdkOsVersion, `${label}.scenarios[${scenarioIndex}].boards[${boardIndex}].rdkOsVersion`, 64, SEMVER);
      presentBoards += 1;
      return { boardId, status: board.status, capturedAt, architecture: board.architecture, rdkOsVersion };
    });
    const status = boards.some((board) => board.status === "fail")
      ? "fail"
      : boards.some((board) => board.status === "missing") ? "incomplete" : "pass";
    if (scenario.status !== status) throw new Error(`${label}.scenarios[${scenarioIndex}].status does not match its boards`);
    return { id, status, boards };
  });

  const allowedIssues = new Set(["contract-mismatch", "missing-reports", "mixed-builds", "scenario-failed", "unexpected-version"]);
  if (!Array.isArray(matrix.issues) || matrix.issues.some((issue) => typeof issue !== "string" || !allowedIssues.has(issue))) {
    throw new Error(`${label}.issues is invalid`);
  }
  const issues = [...matrix.issues];
  if (new Set(issues).size !== issues.length || issues.some((issue, index) => index > 0 && issues[index - 1] >= issue)) {
    throw new Error(`${label}.issues must be unique and sorted`);
  }
  const expectedIssues = [];
  if (presentBoards > 0 && build === null) expectedIssues.push("mixed-builds");
  if (build && build.version !== expectedVersion) expectedIssues.push("unexpected-version");
  if (build && build.piCompatibilitySha256 !== contractSha256) expectedIssues.push("contract-mismatch");
  if (scenarios.some((entry) => entry.status === "fail")) expectedIssues.push("scenario-failed");
  if (scenarios.some((entry) => entry.status === "incomplete")) expectedIssues.push("missing-reports");
  expectedIssues.sort();
  if (JSON.stringify(issues) !== JSON.stringify(expectedIssues)) throw new Error(`${label}.issues does not match its evidence`);
  const status = issues.some((issue) => issue !== "missing-reports") ? "fail" : issues.length > 0 ? "incomplete" : "pass";
  if (matrix.status !== status) throw new Error(`${label}.status does not match its evidence`);
  if (matrix.privacy !== MATRIX_PRIVACY) throw new Error(`${label}.privacy statement is invalid`);
  return {
    schema: MATRIX_SCHEMA, status, expectedVersion, contractSha256, selection,
    requiredBoards, build, scenarios, issues, privacy: MATRIX_PRIVACY,
  };
}

async function readPrivateFile(path, maximum, label) {
  const before = await lstat(path);
  if (!before.isFile() || before.isSymbolicLink() || before.size <= 0 || before.size > maximum || (before.mode & 0o077) !== 0) throw new Error(`${label} is not a private bounded regular file`);
  if (typeof process.getuid === "function" && before.uid !== process.getuid()) throw new Error(`${label} is not owned by the current user`);
  const file = await open(path, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0));
  try {
    const opened = await file.stat();
    if (opened.dev !== before.dev || opened.ino !== before.ino || opened.size !== before.size) throw new Error(`${label} changed while opening`);
    const content = await file.readFile();
    const after = await file.stat();
    if (content.length !== before.size || after.size !== before.size || after.mtimeMs !== before.mtimeMs) throw new Error(`${label} changed while reading`);
    return content.toString("utf8");
  } finally {
    await file.close();
  }
}

async function reportPaths(options) {
  if (options.reportsDirectory) {
    const directory = resolve(options.reportsDirectory);
    const info = await lstat(directory);
    if (!info.isDirectory() || info.isSymbolicLink() || (info.mode & 0o077) !== 0 || (typeof process.getuid === "function" && info.uid !== process.getuid())) {
      throw new Error("reports directory must be private and owned by the current user");
    }
    const names = (await readdir(directory)).filter((name) => name.endsWith(".json")).sort();
    if (names.length === 0 || names.length > MAX_REPORTS) throw new Error("reports directory must contain 1-64 JSON reports");
    return names.map((name) => resolve(directory, name));
  }
  return options.reports.map((path) => resolve(path));
}

async function readBoundedRegularFile(path, maximum, label) {
  const before = await lstat(path);
  if (!before.isFile() || before.isSymbolicLink() || before.size <= 0 || before.size > maximum) throw new Error(`${label} is not a bounded regular file`);
  const file = await open(path, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0));
  try {
    const opened = await file.stat();
    if (opened.dev !== before.dev || opened.ino !== before.ino || opened.size !== before.size) throw new Error(`${label} changed while opening`);
    const content = await file.readFile();
    const after = await file.stat();
    if (content.length !== before.size || after.size !== before.size || after.mtimeMs !== before.mtimeMs) throw new Error(`${label} changed while reading`);
    return content.toString("utf8");
  } finally {
    await file.close();
  }
}

async function atomicWrite(path, value) {
  const destination = resolve(path);
  const encoded = Buffer.from(`${JSON.stringify(value, null, 2)}\n`);
  if (encoded.length > MAX_REPORT_BYTES) throw new Error("matrix report exceeds the size limit");
  await mkdir(dirname(destination), { recursive: true, mode: 0o700 });
  const temporary = `${destination}.tmp.${process.pid}.${randomBytes(6).toString("hex")}`;
  const file = await open(temporary, "wx", 0o600);
  try {
    await file.writeFile(encoded);
    await file.sync();
    await file.close();
    await rename(temporary, destination);
  } catch (error) {
    await file.close().catch(() => {});
    await unlink(temporary).catch(() => {});
    throw error;
  }
}

export function parseArguments(args) {
  const options = { contract: "pi-runtime/compatibility.json", expectedVersion: "", reports: [], reportsDirectory: "", scenario: "", output: "" };
  for (let index = 0; index < args.length; index += 1) {
    const argument = args[index];
    const next = () => {
      const value = args[index + 1];
      if (!value) throw new Error(`${argument} requires a value`);
      index += 1;
      return value;
    };
    switch (argument) {
    case "--contract": options.contract = next(); break;
    case "--expected-version": options.expectedVersion = next(); break;
    case "--report": options.reports.push(next()); break;
    case "--reports": options.reportsDirectory = next(); break;
    case "--scenario": options.scenario = next(); break;
    case "--output": options.output = next(); break;
    case "--help": options.help = true; break;
    default: throw new Error(`unknown option: ${argument}`);
    }
  }
  if (options.help) return options;
  if (!SEMVER.test(options.expectedVersion)) throw new Error("--expected-version must be strict SemVer");
  if ((options.reports.length === 0) === !options.reportsDirectory) throw new Error("use either one or more --report options or one --reports directory");
  if (options.reports.length > MAX_REPORTS) throw new Error("at most 64 reports are allowed");
  if (options.reportsDirectory && options.output) {
    const reportRoot = resolve(options.reportsDirectory);
    const output = resolve(options.output);
    if (output === reportRoot || output.startsWith(`${reportRoot}${sep}`)) throw new Error("--output must be outside --reports directory");
  }
  if (options.output && options.reports.some((path) => resolve(path) === resolve(options.output))) {
    throw new Error("--output must not replace an input report");
  }
  return options;
}

function usage() {
  return `Hobot Code Pi board acceptance matrix verifier

Usage:
  node scripts/validate-board-acceptance.mjs --expected-version VERSION \\
    (--reports PRIVATE_DIR | --report FILE [--report FILE ...]) [options]

Options:
  --contract FILE      Pi compatibility contract (default pi-runtime/compatibility.json)
  --scenario ID        Validate one declared scenario; default requires the full matrix
  --output FILE        Write a private sanitized matrix report
`;
}

async function main() {
  const options = parseArguments(process.argv.slice(2));
  if (options.help) { process.stdout.write(usage()); return; }
  const contractPath = resolve(options.contract);
  const contractContent = await readBoundedRegularFile(contractPath, MAX_REPORT_BYTES, "compatibility contract");
  const contract = parsePiCompatibilityContract(contractContent, "Pi compatibility contract");
  const paths = await reportPaths(options);
  const reports = [];
  for (const path of paths) reports.push(parseAcceptanceReport(await readPrivateFile(path, MAX_REPORT_BYTES, `report ${reports.length + 1}`), contract, `report ${reports.length + 1}`));
  const matrix = buildAcceptanceMatrix(contract, reports, {
    expectedVersion: options.expectedVersion,
    contractSha256: createHash("sha256").update(contractContent).digest("hex"),
    scenario: options.scenario,
  });
  if (options.output) await atomicWrite(options.output, matrix);
  process.stdout.write(`${JSON.stringify({ status: matrix.status, selection: matrix.selection, scenarios: matrix.scenarios.map((entry) => ({ id: entry.id, status: entry.status })) }, null, 2)}\n`);
  if (matrix.status !== "pass") process.exitCode = 1;
}

if (process.argv[1] && await realpath(process.argv[1]) === await realpath(fileURLToPath(import.meta.url))) {
  await main().catch((error) => {
    process.stderr.write(`Board acceptance check failed: ${error instanceof Error ? error.message : "unknown error"}\n`);
    process.exitCode = 2;
  });
}
