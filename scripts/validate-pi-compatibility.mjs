import { createHash } from "node:crypto";
import { lstat, readFile, realpath } from "node:fs/promises";
import { dirname, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

import { parseDataLock, PI_LOCK_FIELDS } from "./release-metadata.mjs";

const API_VERSION = "hobot.pi-compatibility/v1";
const REQUIRED_CAPABILITIES = new Set([
  "interactive.tui",
  "rpc.lifecycle",
  "sessions.branching",
  "context.compaction",
  "extensions.lifecycle",
  "resources.discovery",
  "providers.models",
  "tools.parallel",
  "thinking.stream",
  "images.prompt",
]);
const REQUIRED_BOARDS = ["x5", "s100", "s600"];
const REQUIRED_PRODUCT_SCENARIOS = new Set(["readiness-diagnostics", "install-lifecycle"]);
const SOURCE_PATH = /^(?:tests\/[^/]+\.test\.mjs|agentd\/[^/]+_test\.go|studio\/frontend\/src\/[^/]+\.test\.mjs)$/u;
const UPSTREAM_PATH = /^(?:README\.md|docs\/[a-z0-9-]+\.md)$/u;
const ID = /^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$/u;
const MAX_FILE_BYTES = 4 * 1024 * 1024;

function object(value, label) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${label} must be an object`);
  return value;
}

function exactKeys(value, allowed, label) {
  for (const key of Object.keys(value)) {
    if (!allowed.has(key)) throw new Error(`${label} contains unknown field ${key}`);
  }
  for (const key of allowed) {
    if (!Object.hasOwn(value, key)) throw new Error(`${label} is missing ${key}`);
  }
}

function boundedText(value, label, maximum = 512) {
  if (typeof value !== "string" || value.length === 0 || value.length > maximum || value.trim() !== value || /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/u.test(value)) {
    throw new Error(`${label} must be bounded plain text`);
  }
  return value;
}

function uniqueStrings(values, label, { minimum = 1, maximum = 32, pattern } = {}) {
  if (!Array.isArray(values) || values.length < minimum || values.length > maximum) throw new Error(`${label} must contain ${minimum}-${maximum} items`);
  const result = values.map((value, index) => boundedText(value, `${label}[${index}]`, 1024));
  if (new Set(result).size !== result.length) throw new Error(`${label} contains duplicates`);
  if (pattern && result.some((value) => !pattern.test(value))) throw new Error(`${label} contains an invalid value`);
  return result;
}

async function boundedRegularFile(root, relativePath, label) {
  const path = resolve(root, relativePath);
  if (path === root || !path.startsWith(`${root}${sep}`)) throw new Error(`${label} escapes its root`);
  const info = await lstat(path);
  if (!info.isFile() || info.isSymbolicLink() || info.size <= 0 || info.size > MAX_FILE_BYTES) throw new Error(`${label} is not a bounded regular file`);
  return readFile(path, "utf8");
}

export function parsePiCompatibilityContract(content, label = "Pi compatibility contract") {
  let contract;
  try {
    contract = JSON.parse(content);
  } catch {
    throw new Error(`${label} is not valid JSON`);
  }
  object(contract, label);
  exactKeys(contract, new Set(["schemaVersion", "apiVersion", "pi", "policy", "capabilities", "boardAcceptance"]), label);
  if (contract.schemaVersion !== 1 || contract.apiVersion !== API_VERSION) throw new Error(`${label} has an unsupported schema or API version`);

  exactKeys(object(contract.pi, `${label}.pi`), new Set(["version", "commit"]), `${label}.pi`);
  boundedText(contract.pi.version, `${label}.pi.version`, 64);
  if (!/^[0-9a-f]{40}$/u.test(contract.pi.commit)) throw new Error(`${label}.pi.commit must be a lowercase Git commit`);

  exactKeys(object(contract.policy, `${label}.policy`), new Set([
    "sourceEvidenceRequired", "runtimeEvidenceRequired", "boardEvidenceRequiredForPublicRelease", "requiredBoards",
  ]), `${label}.policy`);
  for (const key of ["sourceEvidenceRequired", "runtimeEvidenceRequired", "boardEvidenceRequiredForPublicRelease"]) {
    if (contract.policy[key] !== true) throw new Error(`${label}.policy.${key} must remain enabled`);
  }
  const boards = uniqueStrings(contract.policy.requiredBoards, `${label}.policy.requiredBoards`, { pattern: ID });
  if (JSON.stringify(boards) !== JSON.stringify(REQUIRED_BOARDS)) throw new Error(`${label} must require x5, s100, and s600 in canonical order`);

  if (!Array.isArray(contract.capabilities) || contract.capabilities.length !== REQUIRED_CAPABILITIES.size) {
    throw new Error(`${label}.capabilities must define the complete required set`);
  }
  const capabilities = new Map();
  for (const [index, raw] of contract.capabilities.entries()) {
    const capability = object(raw, `${label}.capabilities[${index}]`);
    exactKeys(capability, new Set(["id", "description", "upstreamEvidence", "sourceEvidence"]), `${label}.capabilities[${index}]`);
    const id = boundedText(capability.id, `${label}.capabilities[${index}].id`, 64);
    if (!ID.test(id) || !REQUIRED_CAPABILITIES.has(id) || capabilities.has(id)) throw new Error(`${label} contains an unknown or duplicate capability ${id}`);
    boundedText(capability.description, `${label}.${id}.description`);
    if (!Array.isArray(capability.upstreamEvidence) || capability.upstreamEvidence.length === 0 || capability.upstreamEvidence.length > 8) {
      throw new Error(`${label}.${id}.upstreamEvidence must contain 1-8 assertions`);
    }
    for (const [evidenceIndex, rawEvidence] of capability.upstreamEvidence.entries()) {
      const evidence = object(rawEvidence, `${label}.${id}.upstreamEvidence[${evidenceIndex}]`);
      exactKeys(evidence, new Set(["path", "contains"]), `${label}.${id}.upstreamEvidence[${evidenceIndex}]`);
      if (!UPSTREAM_PATH.test(boundedText(evidence.path, `${label}.${id}.upstreamEvidence.path`, 128))) throw new Error(`${label}.${id} has an invalid upstream path`);
      uniqueStrings(evidence.contains, `${label}.${id}.upstreamEvidence.contains`, { maximum: 8 });
    }
    if (!Array.isArray(capability.sourceEvidence) || capability.sourceEvidence.length === 0 || capability.sourceEvidence.length > 8) {
      throw new Error(`${label}.${id}.sourceEvidence must contain 1-8 assertions`);
    }
    for (const [evidenceIndex, rawEvidence] of capability.sourceEvidence.entries()) {
      const evidence = object(rawEvidence, `${label}.${id}.sourceEvidence[${evidenceIndex}]`);
      exactKeys(evidence, new Set(["path", "contains"]), `${label}.${id}.sourceEvidence[${evidenceIndex}]`);
      if (!SOURCE_PATH.test(boundedText(evidence.path, `${label}.${id}.sourceEvidence.path`, 160))) throw new Error(`${label}.${id} has an invalid source-test path`);
      boundedText(evidence.contains, `${label}.${id}.sourceEvidence.contains`, 256);
    }
    capabilities.set(id, capability);
  }
  for (const id of REQUIRED_CAPABILITIES) {
    if (!capabilities.has(id)) throw new Error(`${label} is missing required capability ${id}`);
  }

  const acceptance = object(contract.boardAcceptance, `${label}.boardAcceptance`);
  exactKeys(acceptance, new Set(["reportSchema", "scenarios"]), `${label}.boardAcceptance`);
  if (acceptance.reportSchema !== "hobot.pi-board-compatibility/v1") throw new Error(`${label} has an unsupported board report schema`);
  if (!Array.isArray(acceptance.scenarios) || acceptance.scenarios.length === 0 || acceptance.scenarios.length > 16) throw new Error(`${label} must declare board scenarios`);
  const scenarioIDs = new Set();
  const covered = new Set();
  for (const [index, raw] of acceptance.scenarios.entries()) {
    const scenario = object(raw, `${label}.boardAcceptance.scenarios[${index}]`);
    exactKeys(scenario, new Set(["id", "description", "capabilities"]), `${label}.boardAcceptance.scenarios[${index}]`);
    const id = boundedText(scenario.id, `${label}.boardAcceptance.scenarios[${index}].id`, 64);
    if (!ID.test(id) || scenarioIDs.has(id)) throw new Error(`${label} contains an invalid or duplicate board scenario ${id}`);
    scenarioIDs.add(id);
    boundedText(scenario.description, `${label}.${id}.description`, 768);
    if (!Array.isArray(scenario.capabilities)) throw new Error(`${label}.${id}.capabilities must be an array`);
    if (REQUIRED_PRODUCT_SCENARIOS.has(id) && scenario.capabilities.length !== 0) {
      throw new Error(`${label}.${id} is a product scenario and must not claim a Pi capability`);
    }
    if (!REQUIRED_PRODUCT_SCENARIOS.has(id) && scenario.capabilities.length === 0) {
      throw new Error(`${label}.${id}.capabilities must not be empty`);
    }
    const scenarioCapabilities = scenario.capabilities.length === 0
      ? []
      : uniqueStrings(scenario.capabilities, `${label}.${id}.capabilities`, { pattern: ID });
    for (const capability of scenarioCapabilities) {
      if (!capabilities.has(capability)) throw new Error(`${label}.${id} references unknown capability ${capability}`);
      if (covered.has(capability)) throw new Error(`${label} assigns capability ${capability} to multiple board scenarios`);
      covered.add(capability);
    }
  }
  for (const id of REQUIRED_CAPABILITIES) {
    if (!covered.has(id)) throw new Error(`${label} has no board acceptance scenario for ${id}`);
  }
  for (const id of REQUIRED_PRODUCT_SCENARIOS) {
    if (!scenarioIDs.has(id)) throw new Error(`${label} is missing required product scenario ${id}`);
  }
  return contract;
}

async function validateAssertions(root, capabilities, field, label) {
  const cache = new Map();
  for (const capability of capabilities) {
    for (const evidence of capability[field]) {
      let content = cache.get(evidence.path);
      if (content === undefined) {
        content = await boundedRegularFile(root, evidence.path, `${label}/${evidence.path}`);
        cache.set(evidence.path, content);
      }
      const assertions = Array.isArray(evidence.contains) ? evidence.contains : [evidence.contains];
      for (const expected of assertions) {
        if (!content.includes(expected)) throw new Error(`${label}/${evidence.path} no longer proves ${capability.id}: missing ${JSON.stringify(expected)}`);
      }
    }
  }
}

export async function validatePiCompatibilitySource(rootDirectory) {
  const root = resolve(rootDirectory);
  const [contractContent, lockContent] = await Promise.all([
    boundedRegularFile(root, "pi-runtime/compatibility.json", "Pi compatibility contract"),
    boundedRegularFile(root, "pi-runtime/pi.lock", "Pi runtime lock"),
  ]);
  const contract = parsePiCompatibilityContract(contractContent, "pi-runtime/compatibility.json");
  const lock = parseDataLock(lockContent, "pi-runtime/pi.lock", PI_LOCK_FIELDS);
  if (contract.pi.version !== lock.PI_VERSION || contract.pi.commit !== lock.PI_COMMIT) throw new Error("Pi compatibility contract does not match pi-runtime/pi.lock");
  await validateAssertions(root, contract.capabilities, "sourceEvidence", "source");
  return { contract, digest: createHash("sha256").update(contractContent).digest("hex") };
}

export async function validatePackagedPiCompatibilityContract(rootDirectory) {
  const root = resolve(rootDirectory);
  const [contractContent, lockContent] = await Promise.all([
    boundedRegularFile(root, "PI_COMPATIBILITY.json", "packaged Pi compatibility contract"),
    boundedRegularFile(root, "PI_RUNTIME", "packaged Pi runtime lock"),
  ]);
  const contract = parsePiCompatibilityContract(contractContent, "PI_COMPATIBILITY.json");
  const lock = parseDataLock(lockContent, "PI_RUNTIME", PI_LOCK_FIELDS);
  if (contract.pi.version !== lock.PI_VERSION || contract.pi.commit !== lock.PI_COMMIT) throw new Error("packaged Pi compatibility contract does not match PI_RUNTIME");
  return { contract, digest: createHash("sha256").update(contractContent).digest("hex") };
}

export async function validatePackagedPiCompatibility(rootDirectory) {
  const root = resolve(rootDirectory);
  const result = await validatePackagedPiCompatibilityContract(root);
  await validateAssertions(resolve(root, "runtime"), result.contract.capabilities, "upstreamEvidence", "runtime");
  return result;
}

async function main() {
  const [mode, rootDirectory] = process.argv.slice(2);
  if ((mode !== "--source" && mode !== "--package") || !rootDirectory) throw new Error("Usage: validate-pi-compatibility.mjs --source <repository> | --package <package-root>");
  const result = mode === "--source"
    ? await validatePiCompatibilitySource(rootDirectory)
    : await validatePackagedPiCompatibility(rootDirectory);
  console.log(`Validated Pi ${result.contract.pi.version} compatibility contract ${result.digest.slice(0, 12)} across ${result.contract.capabilities.length} capabilities`);
}

if (process.argv[1] && await realpath(process.argv[1]) === await realpath(fileURLToPath(import.meta.url))) await main();
