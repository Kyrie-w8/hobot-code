import { lstat, readlink, realpath } from "node:fs/promises";
import { basename, dirname, resolve } from "node:path";

import { analyzeShellCommand } from "./shell-command-safety.mjs";

const SECRET_ENV_NAME = /(?:^|_)(?:API_?KEY|AUTH|CREDENTIALS?|COOKIE|PASS(?:WORD|WD)?|PRIVATE_?KEY|SECRET|SESSION_?TOKEN|TOKEN)(?:_|$)/i;
const DYNAMIC_LOADER_ENV_NAME = /^(?:DYLD_|LD_)/;
const INJECTION_ENV_NAMES = new Set([
  "BASH_ENV",
  "BUN_OPTIONS",
  "CDPATH",
  "ENV",
  "GIT_ASKPASS",
  "HOBOT_CODE_AGENT_ROLE",
  "HOBOT_CODE_BACKGROUND_TASK",
  "HOBOT_CODE_BACKGROUND_TASK_ID",
  "HOBOT_CODE_PARENT_TASK_ID",
  "HOBOT_CODE_SIDE_AGENT",
  "HOBOT_CODE_SIDE_COLLABORATION_FILE",
  "HOBOT_CODE_SIDE_PARENT_SESSION",
  "HOBOT_CODE_SOURCE_TASK_ID",
  "LD_PRELOAD",
  "NODE_PATH",
  "NODE_OPTIONS",
  "PERL5LIB",
  "PERLLIB",
  "PYTHONHOME",
  "PYTHONPATH",
  "PYTHONSTARTUP",
  "RUBYLIB",
  "PERL5OPT",
  "PROMPT_COMMAND",
  "RUBYOPT",
  "SSH_ASKPASS",
  "SSH_AUTH_SOCK",
  "ZDOTDIR",
]);
const CRITICAL_ROOTS = ["/boot", "/dev", "/etc", "/proc", "/sys", "/usr", "/var/lib"];

export function sanitizedChildEnv(source = process.env, extra = {}) {
  const env = {};
  for (const [name, value] of Object.entries(source)) {
    if (value === undefined || SECRET_ENV_NAME.test(name) || DYNAMIC_LOADER_ENV_NAME.test(name)
      || INJECTION_ENV_NAMES.has(name)) continue;
    env[name] = value;
  }
  return { ...env, ...extra };
}

export function terminateProcessGroup(child, signal = "SIGTERM") {
  const pid = child?.pid;
  if (!pid) return false;
  if (process.platform !== "win32") {
    try {
      process.kill(-pid, signal);
      return true;
    } catch {
      // Fall back to the direct child when it is not a process-group leader.
    }
  }
  return child.kill(signal);
}

async function canonicalizeWithMissingTail(path, visitedLinks = new Set()) {
  let current = resolve(path);
  const missing = [];
  while (true) {
    try {
      const info = await lstat(current);
      if (info.isSymbolicLink()) {
        if (visitedLinks.has(current)) {
          const error = new Error(`symbolic link cycle detected at ${current}`);
          error.code = "ELOOP";
          throw error;
        }
        visitedLinks.add(current);
        const link = await readlink(current);
        return resolve(await canonicalizeWithMissingTail(resolve(dirname(current), link), visitedLinks), ...missing);
      }
      return resolve(await realpath(current), ...missing);
    } catch (error) {
      if (error?.code !== "ENOENT") throw error;
      const parent = dirname(current);
      if (parent === current) throw error;
      missing.unshift(basename(current));
      current = parent;
    }
  }
}

function isAtOrBelow(path, root) {
  return path === root || path.startsWith(`${root}/`);
}

export async function inspectResolvedPath(cwd, requestedPath) {
  const [workspaceRoot, target, criticalRoots] = await Promise.all([
    canonicalizeWithMissingTail(cwd),
    canonicalizeWithMissingTail(resolve(cwd, requestedPath)),
    Promise.all(CRITICAL_ROOTS.map(async (root) => ({
      root,
      canonical: await canonicalizeWithMissingTail(root),
    }))),
  ]);
  const criticalRoot = criticalRoots.find(({ canonical }) => isAtOrBelow(target, canonical))?.root;
  return {
    workspaceRoot,
    target,
    withinWorkspace: isAtOrBelow(target, workspaceRoot),
    criticalRoot,
  };
}

export function processControlShellReasons(command) {
  return analyzeShellCommand(command).destructiveReasons
    .filter((reason) => reason === "terminates running processes");
}

export function destructiveShellReasons(command) {
  return analyzeShellCommand(command).destructiveReasons;
}

export function networkShellReasons(command) {
  return analyzeShellCommand(command).networkReasons;
}

export function shellReviewFacts(command, networkBoundary) {
  const analysis = analyzeShellCommand(command);
  return {
    destructiveReasons: analysis.destructiveReasons,
    networkReasons: analysis.networkReasons,
    ambiguousReasons: analysis.ambiguousReasons,
    executables: analysis.executables,
    networkBoundary,
  };
}

// A remote find over shared/FUSE storage can wait indefinitely for metadata.
// Require an explicit timeout before the shell process is started so the Agent
// remains cancellable and the UI can return to the composer on slow mounts.
export function unboundedRemoteScanReasons(command) {
  return analyzeShellCommand(command).remoteScanReasons;
}

export function resolveShellSafety(command, networkAction = "ask", options = {}) {
  const analysis = analyzeShellCommand(command);
  const recognizedNetwork = analysis.networkReasons.length > 0;
  const approvalAmbiguities = options.managedSandbox
    ? analysis.ambiguousReasons.filter((reason) => !reason.startsWith("runs an unclassified external command:")
      && reason !== "writes to a dynamic path that requires an OS sandbox boundary")
    : analysis.ambiguousReasons;
  const unclassifiedOnSharedNetwork = analysis.ambiguousReasons.length > 0
    && options.networkBoundary === "shared";
  // A denied shared-network policy blocks unknown egress candidates. Under a
  // restricted OS network namespace, unknown project executables can run with
  // the sandbox boundary enforcing network isolation instead.
  if ((recognizedNetwork || unclassifiedOnSharedNetwork) && networkAction === "deny") {
    return {
      blocked: true,
      approvalReasons: analysis.destructiveReasons,
      recognizedNetwork,
      rememberNetworkCall: false,
      ...(recognizedNetwork ? {} : { blockedReason: "unclassified-egress" }),
    };
  }
  return {
    blocked: false,
    approvalReasons: [
      ...analysis.destructiveReasons,
      ...approvalAmbiguities,
      ...(recognizedNetwork && networkAction === "ask" ? analysis.networkReasons : []),
    ],
    recognizedNetwork,
    rememberNetworkCall: analysis.destructiveReasons.length === 0
      && approvalAmbiguities.length === 0
      && recognizedNetwork
      && networkAction === "ask",
  };
}

export function effectiveNetworkAction(configuredAction, networkMode) {
  return networkMode === "shared" ? configuredAction : "deny";
}
