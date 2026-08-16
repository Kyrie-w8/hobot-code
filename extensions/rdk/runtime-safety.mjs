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
    [/(?:>|>>)(?:\s*['\"]?)\/(?:(?:boot|etc|proc|sys|usr|var\/lib)(?:\/|\b)|dev\/(?!null(?=(?:['\"])?(?:\s|[;&|)]|$))))/i, "writes to a protected system path"],
    [/\btee(?:\s+-a)?\s+(?:\s*['\"]?)\/(?:boot|dev|etc|proc|sys|usr|var\/lib)(?:\/|\b)/i, "writes to a protected system path"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?systemctl\s+(?:disable|halt|mask|poweroff|reboot|stop)\b/i, "changes or stops a system service"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:halt|poweroff|reboot|shutdown)\b/i, "stops or reboots the board"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:kill|killall|pkill)\b/i, "terminates running processes"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:apt(?:-get)?|dnf|yum)\s+(?:autoremove|purge|remove)\b/i, "removes installed software"],
  ];
  const highRiskChecks = [
    [/(?:\brm\b|\brmdir\b|\bmv\b|\bln\b)[^\n]*(?:\/root|\/home\/[^/\s]+)\/\.local\/state\/hobot-code(?:\/|\b)/i, "removes or replaces Hobot Code persistent task and conversation state"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?git\s+(?:clean\b|reset\s+--hard\b|push\b[^\n]*(?:--force(?:-with-lease)?\b|-f\b)|branch\s+(?:-D\b|--delete\s+--force\b))/i, "performs a destructive or forceful Git operation"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:cp|mv|install|ln|mkdir|touch)\b[^;&|\n]*(?:\s|=)['"]?\/(?:boot|dev|etc|proc|sys|usr|var\/lib)(?:\/|\b)/i, "modifies a protected system path"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:sed\s+[^;&|\n]*(?:-i\b|--in-place\b)|perl\s+[^;&|\n]*-[^;&|\n]*i\b)[^;&|\n]*\/(?:boot|dev|etc|proc|sys|usr|var\/lib)(?:\/|\b)/i, "edits a protected system path in place"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:chmod|chown|chgrp|setfacl)\b/i, "changes file ownership or access permissions"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?systemctl\s+(?:daemon-reload|disable|enable|halt|isolate|mask|poweroff|preset|reboot|reload|restart|start|stop|unmask)\b/i, "changes or stops a system service"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?service\s+\S+\s+(?:start|stop|restart|reload|force-reload)\b/i, "changes or stops a system service"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:apt(?:-get)?|dnf|yum|zypper|pacman)\s+(?:autoremove|dist-upgrade|full-upgrade|install|purge|remove|update|upgrade)\b/i, "changes installed software or package metadata"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:dpkg\s+(?:-i|--install|-r|--remove|-P|--purge)\b|rpm\s+(?:-[A-Za-z]*[eFiU]|--erase|--freshen|--install|--upgrade)\b)/i, "changes installed software"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:make|ninja)\s+(?:[^;&|\n]+\s+)?install\b/i, "installs build output into the system"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?cmake\s+--install\b/i, "installs build output into the system"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:pip3?|npm|pnpm|yarn|gem)\b[^;&|\n]*\b(?:install|add)\b[^;&|\n]*(?:--global\b|-g\b)/i, "installs a global language package"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:docker|podman)\s+(?:container\s+)?(?:rm|kill|stop|run\b[^;&|\n]*(?:--privileged\b|--pid\s*=\s*host\b|-v\s*\/(?:\s|:)|--volume\s*=\s*\/(?:\s|:)))/i, "performs a privileged or destructive container operation"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:umount|swapon|swapoff)\b/i, "changes mounted filesystems or swap"],
    // Bare `mount` lists current mounts. Arguments can select or change a
    // mount, so keep every non-bare invocation behind confirmation.
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?mount\b\s+(?![|;&)\n])/i, "changes mounted filesystems or swap"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:insmod|modprobe|rmmod)\b/i, "changes loaded kernel modules"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?sysctl\s+(?:-w|--write)\b/i, "changes kernel runtime settings"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:useradd|userdel|usermod|groupadd|groupdel|groupmod|passwd|chpasswd|visudo)\b/i, "changes users, groups, or authentication"],
    [/(?:>|>>)(?:\s*['"]?)\/(?:root|home\/[^/\s]+)\/(?:\.ssh|\.config\/hobot-code)(?:\/|\b)/i, "writes credentials or Hobot Code configuration"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:iptables|ip6tables|nft)\b/i, "changes firewall or packet-filter state"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?ip\s+(?:address|addr|link|route|rule)\s+(?:add|append|change|delete|del|flush|replace|set)\b/i, "changes network configuration"],
    [/(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:i2cset|gpioset|devmem|cansend|flash_erase|nandwrite|fw_setenv|efibootmgr)\b/i, "writes to board hardware or firmware state"],
    [/(?:^|[;&|()\n]\s*|\s)(?:curl|wget)\b[^\n]*(?:\||\|&)\s*(?:sudo\s+)?(?:ba|z|k)?sh\b/i, "downloads and executes remote content"],
  ];
  return [...new Set([...checks, ...highRiskChecks]
    .filter(([pattern]) => pattern.test(value))
    .map(([_pattern, reason]) => reason))];
}

export function networkShellReasons(command) {
  const value = String(command ?? "");
  const networkChecks = [
    /(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:curl|wget|ssh|scp|sftp|ftp|telnet|nc|ncat|netcat|socat|ping|traceroute|tracepath|dig|host|nslookup|ssh-keyscan)\b/i,
    /(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?git\s+(?:clone|fetch|pull|push|ls-remote|submodule\s+(?:add|update))\b/i,
    /(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:apt(?:-get)?|dnf|yum|zypper|pacman)\s+(?:download|install|refresh|update|upgrade|dist-upgrade|full-upgrade)\b/i,
    /(?:^|[;&|()\n]\s*|\s)(?:\/[^\s;|]+\/)?(?:pip3?|npm|npx|pnpm|yarn|bun|cargo|go|gem)\s+(?:add|ci|dlx|fetch|get|install|publish|update)\b/i,
    /(?:^|[;&|()\n]\s*|\s)(?:sudo\s+)?(?:\/[^\s;|]+\/)?(?:docker|podman)\s+(?:build|login|pull|push|run)\b/i,
    /(?:^|[;&|()\n]\s*|\s)(?:\/[^\s;|]+\/)?(?:gh|glab|kubectl)\b/i,
    /\/dev\/(?:tcp|udp)\//i,
  ];
  return networkChecks.some((pattern) => pattern.test(value))
    ? ["uses a recognized outbound network client while the OS sandbox shares host networking"]
    : [];
}

export function resolveShellSafety(command, networkAction = "ask") {
  const destructiveReasons = destructiveShellReasons(command);
  const networkReasons = networkShellReasons(command);
  if (networkReasons.length > 0 && networkAction === "deny") {
    return {
      blocked: true,
      approvalReasons: destructiveReasons,
      recognizedNetwork: true,
      rememberNetworkCall: false,
    };
  }
  return {
    blocked: false,
    approvalReasons: [
      ...destructiveReasons,
      ...(networkReasons.length > 0 && networkAction === "ask" ? networkReasons : []),
    ],
    recognizedNetwork: networkReasons.length > 0,
    rememberNetworkCall: destructiveReasons.length === 0
      && networkReasons.length > 0
      && networkAction === "ask",
  };
}

export function effectiveNetworkAction(configuredAction, networkMode) {
  return networkMode === "offline" ? "deny" : configuredAction;
}
