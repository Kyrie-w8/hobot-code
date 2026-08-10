import { lstat, readlink, realpath } from "node:fs/promises";
import { basename, dirname, resolve } from "node:path";

const SECRET_ENV_NAME = /(?:^|_)(?:API_?KEY|AUTH|CREDENTIALS?|COOKIE|PASS(?:WORD|WD)?|PRIVATE_?KEY|SECRET|SESSION_?TOKEN|TOKEN)(?:_|$)/i;
const DYNAMIC_LOADER_ENV_NAME = /^(?:DYLD_|LD_)/;
const INJECTION_ENV_NAMES = new Set([
  "BASH_ENV",
  "BUN_OPTIONS",
  "CDPATH",
  "ENV",
  "GIT_ASKPASS",
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

export function destructiveShellReasons(command) {
  const value = String(command ?? "");
  const checks = [
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:(?:\/[^\s;|]+\/)?busybox\s+)?(?:\/[^\s;|]+\/)?(?:rm|rmdir|unlink|shred|truncate|wipefs)\b/i, "removes or destroys files"],
    [/\bfind\b[\s\S]*\s-delete\b/i, "deletes files through find"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:mkfs(?:\.[a-z0-9]+)?|fdisk|sfdisk|parted)\b/i, "changes a filesystem or partition table"],
    [/\bdd\b[\s\S]*\bof\s*=\s*\/dev\//i, "writes directly to a block or device node"],
    [/(?:>|>>|\btee(?:\s+-a)?\s+)(?:\s*['\"]?)\/(?:boot|dev|etc|proc|sys|usr|var\/lib)(?:\/|\b)/i, "writes to a protected system path"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?systemctl\s+(?:disable|halt|mask|poweroff|reboot|stop)\b/i, "changes or stops a system service"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:halt|poweroff|reboot|shutdown)\b/i, "stops or reboots the board"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:kill|killall|pkill)\b/i, "terminates running processes"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:apt(?:-get)?|dnf|yum)\s+(?:autoremove|purge|remove)\b/i, "removes installed software"],
  ];
  return checks.filter(([pattern]) => pattern.test(value)).map(([_pattern, reason]) => reason);
}
