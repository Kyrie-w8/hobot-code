import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  buildAcceptanceMatrix,
  parseAcceptanceMatrix,
  parseAcceptanceReport,
  parseArguments,
} from "../scripts/validate-board-acceptance.mjs";
import { parsePiCompatibilityContract } from "../scripts/validate-pi-compatibility.mjs";

const repository = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const contractContent = await readFile(join(repository, "pi-runtime/compatibility.json"), "utf8");
const contract = parsePiCompatibilityContract(contractContent);
const contractSha256 = createHash("sha256").update(contractContent).digest("hex");
const build = {
  version: "0.26.0", agentdSha256: "a".repeat(64), manifestSha256: "b".repeat(64), piCompatibilitySha256: contractSha256,
};

function report(boardId, overrides = {}) {
  return {
    schema: "hobot.pi-board-compatibility/v1", scenario: "model-egress-runtime", status: "pass",
    capturedAt: "2026-08-14T05:10:30.241295Z",
    target: { architecture: "aarch64", boardId, rdkOsVersion: boardId === "x5" ? "3.4.1" : boardId === "s100" ? "4.0.5-Beta" : "5.1.0" },
    build, checks: [
      { provider: "anthropic-test", protocol: "anthropic-messages", status: "pass" },
      { provider: "chat-test", protocol: "openai-completions", status: "pass" },
      { provider: "responses-test", protocol: "openai-responses", status: "pass" },
    ],
    ...overrides,
  };
}

test("model egress reports are strict, bounded, and scenario-specific", () => {
  const parsed = parseAcceptanceReport(JSON.stringify(report("s600")), contract);
  assert.equal(parsed.target.boardId, "s600");
  assert.throws(() => parseAcceptanceReport(JSON.stringify({...report("s600"), hostname: "private"}), contract), /unknown field hostname/);
  const wrong = report("s600");
  wrong.checks[0].protocol = "openai-responses";
  assert.throws(() => parseAcceptanceReport(JSON.stringify(wrong), contract), /unexpected or duplicate/);
  assert.throws(() => parseAcceptanceReport('{"schema":"x","schema":"y"}', contract), /duplicate key/);
});

test("RPC and session reports require their exact scenario check sets", () => {
  const session = report("s600", {
    scenario: "session-recovery",
    checks: ["context-compaction", "interrupted-session-recovery", "history-edit-branch", "fresh-client-connections"]
      .map((name) => ({ name, status: "pass" })),
  });
  assert.equal(parseAcceptanceReport(JSON.stringify(session), contract).checks.length, 4);
  session.checks[3].name = "unrelated-check";
  assert.throws(() => parseAcceptanceReport(JSON.stringify(session), contract), /declared scenario checks/);

  const rpc = report("s600", {
    scenario: "rpc-background",
    checks: [
      "persistent-task", "tool-approval", "second-turn", "image-input",
      "reconnect-no-duplicate", "side-agent-multiturn", "side-agent-flat-parent", "main-agent-remains-active",
    ].map((name) => ({ name, status: "pass" })),
  });
  assert.equal(parseAcceptanceReport(JSON.stringify(rpc), contract).checks.length, 8);
  rpc.checks.pop();
  assert.throws(() => parseAcceptanceReport(JSON.stringify(rpc), contract), /declared scenario checks/);
});

test("extension safety reports require the complete declared check set", () => {
  const extension = report("s600", {
    scenario: "extension-safety",
    checks: ["packaged-resource-discovery", "parallel-extension-tools", "permission-hook", "workspace-write-lease"]
      .map((name) => ({ name, status: "pass" })),
  });
  assert.equal(parseAcceptanceReport(JSON.stringify(extension), contract).checks.length, 4);
  extension.checks[0].name = "resource-count";
  assert.throws(() => parseAcceptanceReport(JSON.stringify(extension), contract), /declared scenario checks/);
});

test("TUI reports require the complete ordinary-user interaction check set", () => {
  const tui = report("s600", {
    scenario: "tui-basics",
    checks: ["ordinary-user-tui", "chinese-input", "thinking-stream", "editor-edit", "persistent-detach"]
      .map((name) => ({ name, status: "pass" })),
  });
  assert.equal(parseAcceptanceReport(JSON.stringify(tui), contract).checks.length, 5);
  tui.checks.pop();
  assert.throws(() => parseAcceptanceReport(JSON.stringify(tui), contract), /declared scenario checks/);
});

test("readiness reports require inspection, confirmation, bounded repair, and privacy evidence", () => {
  const readiness = report("s600", {
    scenario: "readiness-diagnostics",
    checks: [
      "read-only-inspection", "cli-json", "confirmation-required", "bounded-permission-repair",
      "privacy-no-support-file",
    ].map((name) => ({name, status: "pass"})),
  });
  assert.equal(parseAcceptanceReport(JSON.stringify(readiness), contract).checks.length, 5);
  readiness.checks[4].name = "support-file-created";
  assert.throws(() => parseAcceptanceReport(JSON.stringify(readiness), contract), /declared scenario checks/);
});

test("a selected three-board scenario passes only for one identical build", () => {
  const reports = ["x5", "s100", "s600"].map((boardId) => parseAcceptanceReport(JSON.stringify(report(boardId)), contract));
  const matrix = buildAcceptanceMatrix(contract, reports, { expectedVersion: "0.26.0", contractSha256, scenario: "model-egress-runtime" });
  assert.equal(matrix.status, "pass");
  assert.equal(matrix.scenarios[0].status, "pass");
  assert.equal(matrix.build.manifestSha256, build.manifestSha256);

  const mixed = structuredClone(reports);
  mixed[2].build.manifestSha256 = "c".repeat(64);
  assert.equal(buildAcceptanceMatrix(contract, mixed, { expectedVersion: "0.26.0", contractSha256, scenario: "model-egress-runtime" }).status, "fail");
});

test("the full public matrix stays incomplete when only model egress evidence exists", () => {
  const reports = ["x5", "s100", "s600"].map((boardId) => parseAcceptanceReport(JSON.stringify(report(boardId)), contract));
  const matrix = buildAcceptanceMatrix(contract, reports, { expectedVersion: "0.26.0", contractSha256 });
  assert.equal(matrix.status, "incomplete");
  assert.deepEqual(matrix.issues, ["missing-reports"]);
  assert.equal(matrix.scenarios.find((entry) => entry.id === "model-egress-runtime").status, "pass");
  assert.equal(
    matrix.scenarios.filter((entry) => entry.status === "incomplete").length,
    contract.boardAcceptance.scenarios.length - 1,
  );
});

test("install lifecycle reports require the complete transactional check set", () => {
  const lifecycle = report("s600", {
    scenario: "install-lifecycle",
    checks: [
      "isolated-root", "first-install", "ordinary-user-launcher", "upgrade-preserves-user-data",
      "failed-upgrade-restores-runtime", "rollback-restores-runtime", "uninstall-preserves-user-data",
    ].map((name) => ({name, status: "pass"})),
  });
  assert.equal(parseAcceptanceReport(JSON.stringify(lifecycle), contract).checks.length, 7);
  lifecycle.checks.pop();
  assert.throws(() => parseAcceptanceReport(JSON.stringify(lifecycle), contract), /declared scenario checks/);
});

function completeMatrix() {
  const reports = contract.boardAcceptance.scenarios.flatMap((scenario) =>
    contract.policy.requiredBoards.map((boardId) => ({
      schema: "hobot.pi-board-compatibility/v1",
      scenario: scenario.id,
      status: "pass",
      capturedAt: "2026-08-14T05:10:30.241Z",
      target: {
        architecture: "aarch64",
        boardId,
        rdkOsVersion: boardId === "x5" ? "3.4.1" : boardId === "s100" ? "4.0.5-Beta" : "5.1.0",
      },
      build,
      checks: [],
    })),
  );
  return buildAcceptanceMatrix(contract, reports, { expectedVersion: "0.26.0", contractSha256 });
}

test("a complete aggregate matrix is parsed and normalized strictly", () => {
  const matrix = completeMatrix();
  assert.equal(matrix.status, "pass");
  assert.equal(parseAcceptanceMatrix(JSON.stringify(matrix), contract).scenarios.length, contract.boardAcceptance.scenarios.length);
});

test("aggregate matrix parsing rejects added, reordered, and forged evidence", () => {
  const extra = completeMatrix();
  extra.hostname = "private-board";
  assert.throws(() => parseAcceptanceMatrix(JSON.stringify(extra), contract), /unknown field hostname/);

  const wrongScenarioStatus = completeMatrix();
  wrongScenarioStatus.scenarios[0].status = "fail";
  assert.throws(() => parseAcceptanceMatrix(JSON.stringify(wrongScenarioStatus), contract), /status does not match its boards/);

  const reorderedBoards = completeMatrix();
  reorderedBoards.scenarios[0].boards.reverse();
  assert.throws(() => parseAcceptanceMatrix(JSON.stringify(reorderedBoards), contract), /incomplete or out of order/);

  const forgedIssues = completeMatrix();
  forgedIssues.issues = ["missing-reports"];
  forgedIssues.status = "incomplete";
  assert.throws(() => parseAcceptanceMatrix(JSON.stringify(forgedIssues), contract), /issues does not match its evidence/);
});

test("CLI requires one private report source and keeps matrix output outside a report directory", () => {
  assert.throws(() => parseArguments(["--expected-version", "0.26.0"]), /either one or more/);
  assert.throws(() => parseArguments(["--expected-version", "0.26.0..rc", "--report", "x5.json"]), /strict SemVer/);
  assert.throws(() => parseArguments(["--expected-version", "0.26.0", "--reports", "reports", "--output", "reports/matrix.json"]), /outside/);
  assert.throws(() => parseArguments(["--expected-version", "0.26.0", "--report", "x5.json", "--output", "x5.json"]), /must not replace/);
  const options = parseArguments(["--expected-version", "0.26.0", "--report", "x5.json", "--scenario", "model-egress-runtime"]);
  assert.equal(options.reports.length, 1);
});
