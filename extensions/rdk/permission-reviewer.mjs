import { createHash } from "node:crypto";

// This reviewer deliberately consumes structured facts produced by the board
// safety checks. It never receives the Agent's prose or treats tool arguments
// as instructions. A positive result is only a single-call decision.
export const AUTO_REVIEW_MODE = "auto-review";
export const REVIEWER_VERSION = 1;
export const REVIEWER_DENIAL_LIMIT = 3;
export const REVIEWER_WINDOW_LIMIT = 10;
export const REVIEWER_WINDOW_MS = 10 * 60_000;

const ELIGIBLE_TOOLS = new Set(["bash", "write", "edit"]);
const HUMAN_ONLY_TOOLS = new Set([
  "network", "quality_gate", "memory_save", "goal_complete", "goal_progress", "openexplorer_build_host", "openexplorer_remote_run",
]);
const SHARED_NETWORK_LOCAL_EXECUTABLES = new Set([
  "basename", "cat", "cmp", "comm", "cut", "date", "df", "diff", "dirname", "dmesg", "du", "echo", "false", "file", "find", "free", "head", "hrt_model_exec", "id", "iostat", "journalctl", "lscpu", "lsblk", "lsusb", "ls", "nvidia-smi", "pgrep", "pidstat", "ps", "pwd", "printf", "readlink", "realpath", "rg", "grep", "sed", "sort", "stat", "tail", "test", "top", "touch", "tree", "true", "type", "uname", "uniq", "uptime", "vmstat", "wc", "which", "whoami",
]);
const SENSITIVE_PATH = /(?:^|\/)(?:\.ssh|\.gnupg|\.aws|\.kube|\.netrc|\.npmrc|\.pypirc|authorized_keys)(?:\/|$)|(?:^|\/)(?:\.bashrc|\.zshrc|\.profile|\.gitconfig)$/i;

function stableJson(value) {
  if (Array.isArray(value)) return `[${value.map(stableJson).join(",")}]`;
  if (value && typeof value === "object") return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${stableJson(value[key])}`).join(",")}}`;
  return JSON.stringify(value) ?? "null";
}

export function actionFingerprint(tool, input) {
  return createHash("sha256").update(`${String(tool).toLowerCase()}\0${stableJson(input ?? {})}`).digest("hex");
}

export function createPermissionReviewer({ now = () => Date.now() } = {}) {
  const denials = [];
  const exactRetries = new Set();
  let consecutiveDenials = 0;

  const trim = () => {
    const cutoff = now() - REVIEWER_WINDOW_MS;
    while (denials.length && denials[0].at < cutoff) denials.shift();
    if (denials.length === 0) consecutiveDenials = 0;
  };
  const recordDenial = (fingerprint) => {
    trim();
    denials.push({ fingerprint, at: now() });
    consecutiveDenials += 1;
  };
  const resetDenialRun = () => {
    consecutiveDenials = 0;
  };
  const circuitOpen = () => {
    trim();
    return consecutiveDenials >= REVIEWER_DENIAL_LIMIT || denials.length >= REVIEWER_WINDOW_LIMIT;
  };

  return {
    requestExactRetry(fingerprint) {
      if (!fingerprint || !denials.some((entry) => entry.fingerprint === fingerprint) || exactRetries.has(fingerprint)) return false;
      exactRetries.add(fingerprint);
      return true;
    },
    recordDenial,
    recordNonDenial: resetDenialRun,
    review(request) {
      const tool = String(request?.tool ?? "");
      const input = request?.input && typeof request.input === "object" ? request.input : {};
      const fingerprint = actionFingerprint(tool, input);
      const facts = request?.facts && typeof request.facts === "object" ? request.facts : {};
      const destructive = Array.isArray(facts.destructiveReasons) ? facts.destructiveReasons : [];
      const ambiguous = Array.isArray(facts.ambiguousReasons) ? facts.ambiguousReasons : [];
      const network = Array.isArray(facts.networkReasons) ? facts.networkReasons : [];
      const target = String(facts.target ?? input.path ?? "");
      const executables = Array.isArray(facts.executables) ? facts.executables.map(String) : [];
      const retry = exactRetries.delete(fingerprint);

      const hardReasons = [
        ...destructive,
        ...network,
        ...(facts.outsideWorkspace ? ["target is outside the current workspace"] : []),
        ...(facts.criticalPath ? ["target is a protected system path"] : []),
        ...(facts.remote ? ["remote or SSH execution is never reviewer-approved"] : []),
        ...(SENSITIVE_PATH.test(target) ? ["target is credential, startup, or persistent configuration"] : []),
      ];
      if (hardReasons.length > 0) {
        return { status: "manual-required", source: "board-reviewer", fingerprint, retry, reasons: hardReasons, hard: true };
      }
      if (circuitOpen() && !retry) {
        return { status: "denied", source: "board-reviewer", fingerprint, reasons: ["reviewer circuit breaker is open after repeated unsafe requests; choose a materially safer path"], hard: false };
      }
      if (!ELIGIBLE_TOOLS.has(tool) || HUMAN_ONLY_TOOLS.has(tool) || facts.mcp || facts.persistent || ambiguous.length > 0 || !facts.withinWorkspace && (tool === "write" || tool === "edit")) {
        resetDenialRun();
        const reasons = [
          ...(ambiguous.length ? ambiguous : []),
          ...(facts.mcp ? ["unclassified MCP capability"] : []),
          ...(facts.persistent ? ["persistent state change"] : []),
          ...(!ELIGIBLE_TOOLS.has(tool) || HUMAN_ONLY_TOOLS.has(tool) ? ["tool is outside the deterministic reviewer allowlist"] : []),
          ...(!facts.withinWorkspace && (tool === "write" || tool === "edit") ? ["target scope could not be proven"] : []),
        ];
        return { status: "manual-required", source: "board-reviewer", fingerprint, retry, reasons, hard: false };
      }
      if (tool === "bash" && facts.networkBoundary === "shared" && (executables.length === 0 || executables.some((name) => !SHARED_NETWORK_LOCAL_EXECUTABLES.has(name)))) {
        resetDenialRun();
        return {
          status: "manual-required", source: "board-reviewer", fingerprint, retry,
          reasons: ["shared network permits automatic review only for a narrow local executable allowlist"], hard: false,
        };
      }
      resetDenialRun();
      return {
        status: "approved", source: "board-reviewer", fingerprint, retry,
        scope: { kind: "exact-action", taskId: String(request.taskId ?? ""), action: fingerprint },
        reasons: ["deterministic low-risk action within the current task and workspace"],
      };
    },
  };
}
