import assert from "node:assert/strict";
import test from "node:test";

import { parseArguments, parseBuildInfo, validateReleaseEvidence } from "../scripts/validate-release-candidate.mjs";

const hashes = {
  archive: "a".repeat(64),
  matrix: "b".repeat(64),
  manifest: "c".repeat(64),
  compatibility: "d".repeat(64),
  agentd: "e".repeat(64),
};
const commit = "f".repeat(40);
const builtAt = "2026-08-14T08:00:00.000Z";
const capturedAt = "2026-08-14T08:30:00.000Z";
const now = new Date("2026-08-14T09:00:00.000Z");

function buildInfo(overrides = {}) {
  return {
    schemaVersion: 3,
    version: "0.26.0",
    commit,
    dirty: false,
    builtAt,
    target: "linux-arm64",
    agentdSha256: hashes.agentd,
    pi: {
      version: "0.84.1",
      commit: "1".repeat(40),
      archiveSha256: "2".repeat(64),
      compatibilitySha256: hashes.compatibility,
    },
    tools: { fd: "10.4.2", ripgrep: "15.2.0" },
    ...overrides,
  };
}

function matrix() {
  return {
    schema: "hobot.pi-board-compatibility-matrix/v1",
    status: "pass",
    expectedVersion: "0.26.0",
    contractSha256: hashes.compatibility,
    selection: "all",
    requiredBoards: ["x5", "s100", "s600"],
    build: {
      version: "0.26.0",
      agentdSha256: hashes.agentd,
      manifestSha256: hashes.manifest,
      piCompatibilitySha256: hashes.compatibility,
    },
    scenarios: [
      "tui-basics", "rpc-background", "model-egress-runtime", "session-recovery", "extension-safety",
      "readiness-diagnostics", "install-lifecycle",
    ].map((id) => ({
      id,
      status: "pass",
      boards: ["x5", "s100", "s600"].map((boardId) => ({ boardId, status: "pass", capturedAt })),
    })),
    issues: [],
  };
}

function input() {
  return {
    version: "0.26.0",
    expectedCommit: commit,
    archiveName: "hobot-code-0.26.0-linux-arm64.tar.gz",
    matrixName: "hobot-code-0.26.0-board-acceptance.json",
    archiveSha256: hashes.archive,
    matrixSha256: hashes.matrix,
    manifestSha256: hashes.manifest,
    piCompatibilitySha256: hashes.compatibility,
    agentdSha256: hashes.agentd,
    buildInfo: buildInfo(),
    matrix: matrix(),
  };
}

test("BUILD_INFO parsing rejects undeclared metadata and normalizes a clean build", () => {
  const parsed = parseBuildInfo(JSON.stringify(buildInfo()));
  assert.equal(parsed.commit, commit);
  assert.equal(parsed.dirty, false);
  assert.throws(() => parseBuildInfo(JSON.stringify({ ...buildInfo(), branch: "main" })), /unknown field branch/);
  assert.throws(() => parseBuildInfo('{"schemaVersion":3,"schemaVersion":2}'), /duplicate key/);
  assert.throws(() => parseArguments([
    "--package-root", "/package", "--archive", "/archive", "--matrix", "/matrix",
    "--expected-version", "0.26.0..rc", "--expected-commit", commit, "--output", "/evidence",
  ]), /expected-version is invalid/);
});

test("release evidence deterministically binds the tag, package, archive, and full matrix", () => {
  const first = validateReleaseEvidence(input(), { now });
  const second = validateReleaseEvidence(input(), { now: new Date("2026-08-14T10:00:00.000Z") });
  assert.deepEqual(first, second);
  assert.equal(first.schema, "hobot.release-evidence/v1");
  assert.equal(first.artifact.sha256, hashes.archive);
  assert.equal(first.boardAcceptance.matrixName, "hobot-code-0.26.0-board-acceptance.json");
  assert.equal(first.package.manifestSha256, hashes.manifest);
  assert.equal(first.boardAcceptance.scenarios.length, 7);
});

test("release evidence rejects dirty, mismatched, and incomplete candidates", () => {
  const dirty = input();
  dirty.buildInfo.dirty = true;
  assert.throws(() => validateReleaseEvidence(dirty, { now }), /clean package build/);

  const wrongCommit = input();
  wrongCommit.buildInfo.commit = "0".repeat(40);
  assert.throws(() => validateReleaseEvidence(wrongCommit, { now }), /build identity/);

  const wrongManifest = input();
  wrongManifest.matrix.build.manifestSha256 = "0".repeat(64);
  assert.throws(() => validateReleaseEvidence(wrongManifest, { now }), /manifestSha256 does not match/);

  const wrongMatrixName = input();
  wrongMatrixName.matrixName = "matrix.json";
  assert.throws(() => validateReleaseEvidence(wrongMatrixName, { now }), /matrix name must be/);

  const partial = input();
  partial.matrix.status = "incomplete";
  partial.matrix.issues = ["missing-reports"];
  assert.throws(() => validateReleaseEvidence(partial, { now }), /complete three-board acceptance matrix/);
});

test("release evidence enforces build-relative, future, and freshness windows", () => {
  const predatesBuild = input();
  predatesBuild.matrix.scenarios[0].boards[0].capturedAt = "2026-08-14T07:00:00.000Z";
  assert.throws(() => validateReleaseEvidence(predatesBuild, { now }), /predates the candidate build/);

  const future = input();
  future.matrix.scenarios[0].boards[0].capturedAt = "2026-08-14T09:06:00.000Z";
  assert.throws(() => validateReleaseEvidence(future, { now }), /timestamp is in the future/);

  const stale = input();
  stale.buildInfo.builtAt = "2026-08-01T08:00:00.000Z";
  for (const scenario of stale.matrix.scenarios) {
    for (const board of scenario.boards) board.capturedAt = "2026-08-01T08:30:00.000Z";
  }
  assert.throws(() => validateReleaseEvidence(stale, { now }), /evidence is stale/);
});
