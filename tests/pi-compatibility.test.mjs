import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  parsePiCompatibilityContract,
  validatePackagedPiCompatibility,
  validatePiCompatibilitySource,
} from "../scripts/validate-pi-compatibility.mjs";

const repository = resolve(dirname(fileURLToPath(import.meta.url)), "..");

test("Pi compatibility source gate binds every required capability to the runtime lock and a regression test", async () => {
  const result = await validatePiCompatibilitySource(repository);
  assert.equal(result.contract.apiVersion, "hobot.pi-compatibility/v1");
  assert.equal(result.contract.capabilities.length, 10);
  assert.match(result.digest, /^[0-9a-f]{64}$/u);
});

test("Pi compatibility contract rejects schema drift and incomplete board coverage", async () => {
  const original = JSON.parse(await readFile(join(repository, "pi-runtime/compatibility.json"), "utf8"));
  assert.throws(
    () => parsePiCompatibilityContract(JSON.stringify({...original, unexpected: true})),
    /unknown field unexpected/,
  );

  const missing = structuredClone(original);
  missing.capabilities.pop();
  assert.throws(() => parsePiCompatibilityContract(JSON.stringify(missing)), /complete required set/);

  const duplicate = structuredClone(original);
  duplicate.boardAcceptance.scenarios[1].capabilities.push("interactive.tui");
  assert.throws(() => parsePiCompatibilityContract(JSON.stringify(duplicate)), /multiple board scenarios/);

  const missingLifecycle = structuredClone(original);
  missingLifecycle.boardAcceptance.scenarios = missingLifecycle.boardAcceptance.scenarios.filter((scenario) => scenario.id !== "install-lifecycle");
  assert.throws(() => parsePiCompatibilityContract(JSON.stringify(missingLifecycle)), /missing required product scenario/);

  const missingReadiness = structuredClone(original);
  missingReadiness.boardAcceptance.scenarios = missingReadiness.boardAcceptance.scenarios.filter((scenario) => scenario.id !== "readiness-diagnostics");
  assert.throws(() => parsePiCompatibilityContract(JSON.stringify(missingReadiness)), /missing required product scenario/);

  const falseCapability = structuredClone(original);
  falseCapability.boardAcceptance.scenarios.find((scenario) => scenario.id === "readiness-diagnostics").capabilities.push("interactive.tui");
  assert.throws(() => parsePiCompatibilityContract(JSON.stringify(falseCapability)), /must not claim a Pi capability/);
});

test("packaged Pi compatibility gate verifies the actual pinned runtime surface", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "hobot-pi-compatibility-"));
  t.after(() => rm(root, {recursive: true, force: true}));
  const contractContent = await readFile(join(repository, "pi-runtime/compatibility.json"), "utf8");
  const contract = JSON.parse(contractContent);
  await writeFile(join(root, "PI_COMPATIBILITY.json"), contractContent);
  await writeFile(join(root, "PI_RUNTIME"), await readFile(join(repository, "pi-runtime/pi.lock"), "utf8"));

  const assertions = new Map();
  for (const capability of contract.capabilities) {
    for (const evidence of capability.upstreamEvidence) {
      const values = assertions.get(evidence.path) ?? [];
      values.push(...evidence.contains);
      assertions.set(evidence.path, values);
    }
  }
  for (const [path, values] of assertions) {
    const target = join(root, "runtime", path);
    await mkdir(dirname(target), {recursive: true});
    await writeFile(target, `${[...new Set(values)].join("\n")}\n`);
  }

  const result = await validatePackagedPiCompatibility(root);
  assert.equal(result.contract.pi.version, "0.84.1");
  await writeFile(join(root, "runtime/docs/rpc.md"), "# RPC without the required protocol surface\n");
  await assert.rejects(() => validatePackagedPiCompatibility(root), /no longer proves/);
});
