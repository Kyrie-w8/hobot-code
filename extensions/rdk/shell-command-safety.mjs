import { basename } from "node:path";

const HELP_FLAGS = new Set(["--help", "-h", "--version", "-V", "-v", "--list", "-l", "-L"]);
const PROCESS_CONTROL_COMMANDS = new Set(["kill", "killall", "pkill"]);
const NETWORK_COMMANDS = new Set([
  "curl", "wget", "ssh", "scp", "sftp", "ftp", "telnet", "nc", "ncat", "netcat", "socat",
  "ping", "traceroute", "tracepath", "dig", "host", "nslookup", "ssh-keyscan",
]);
const PACKAGE_COMMANDS = new Set(["apt", "apt-get", "dnf", "yum", "zypper", "pacman"]);
const LANGUAGE_PACKAGE_COMMANDS = new Set(["pip", "pip3", "npm", "npx", "pnpm", "yarn", "bun", "cargo", "go", "gem"]);
const DESTRUCTIVE_FILE_COMMANDS = new Set(["rm", "rmdir", "unlink", "shred", "truncate", "wipefs"]);
const PARTITION_COMMANDS = new Set(["fdisk", "sfdisk", "parted"]);
const HARDWARE_COMMANDS = new Set(["i2cset", "gpioset", "devmem", "cansend", "flash_erase", "nandwrite", "fw_setenv", "efibootmgr"]);
const USER_ADMIN_COMMANDS = new Set(["useradd", "userdel", "usermod", "groupadd", "groupdel", "groupmod", "passwd", "chpasswd", "visudo"]);
const SHELL_INTERPRETERS = new Set(["sh", "bash", "zsh", "dash", "ksh", "fish"]);
const SHELL_WRAPPERS = new Set(["command", "env", "exec", "nice", "nohup", "setsid", "stdbuf", "time", "timeout", "gtimeout", "ionice"]);
const SHELL_CONTROL_PREFIXES = new Set(["!", "do", "elif", "else", "if", "then", "until", "while"]);
const SHELL_CONTROL_ONLY = new Set(["case", "done", "esac", "fi", "for", "select"]);

const OBSERVATION_COMMANDS = new Set([
  "addr2line", "arch", "blkid", "c++filt", "column", "dmidecode", "findmnt", "getconf", "getent", "getfacl",
  "groups", "hexdump", "iostat", "ipcs", "last", "lastlog", "lsattr", "lsblk", "lscpu", "lspci", "lsusb",
  "mpstat", "namei", "nm", "nproc", "objdump", "od", "pidstat", "pkg-config", "printenv", "pstree",
  "readelf", "sar", "sensors", "size", "sestatus", "getenforce", "top", "tree", "w", "whereis",
  "who", "xxd",
]);

const BUILD_COMMANDS = new Set([
  "ar", "ld", "meson", "nvcc", "objcopy", "ranlib", "strip",
]);

const PACKAGE_QUERY_COMMANDS = new Set([
  "apt-cache", "dpkg-query", "rpmquery",
]);

const ENVIRONMENT_COMMANDS = new Set(["conda", "mamba", "micromamba"]);

// This is an allowlist of normal board-development executables, not a security
// grant. Each still receives its command-specific risk checks below.
const SAFE_DEVELOPER_COMMANDS = new Set([
  "[", "[[", "alias", "awk", "basename", "cat", "cd", "cmp", "comm", "command", "cp", "cut", "date", "declare", "diff", "dirname", "dirs", "du", "echo", "env", "export", "expr", "false", "file", "find", "free", "git", "go", "grep", "head", "hostname", "id", "install", "jq", "less", "ln", "local", "locate", "logger", "ls", "lsof", "make", "md5sum", "mkdir", "mv", "ninja", "node", "npm", "npx", "numfmt", "popd", "printf", "pushd", "pwd", "python", "python3", "readlink", "readonly", "realpath", "rg", "sed", "set", "sha1sum", "sha256sum", "sleep", "sort", "stat", "strings", "tail", "tee", "test", "touch", "tr", "true", "type", "ulimit", "umask", "unalias", "uname", "uniq", "unset", "uptime", "wc", "which", "whoami", "xargs",
  "black", "cargo", "cc", "clang", "clang++", "cmake", "ctest", "deno", "eslint", "g++", "gcc", "gradle", "hbdk", "hbm_perf", "hobot", "hrt_model_exec", "java", "javac", "jest", "mvn", "poetry", "prettier", "pytest", "ruff", "rustc", "tsc", "uv", "vitest",
  "dmesg", "df", "ethtool", "ip", "journalctl", "lsmod", "mount", "nvidia-smi", "pgrep", "ps", "ss", "systemctl", "vmstat",
  "docker", "podman", "kubectl", "gh", "glab", "dd", "dpkg", "rpm", "service", "sysctl", "umount", "swapon", "swapoff", "insmod", "modprobe", "rmmod", "iptables", "ip6tables", "nft", "chmod", "chown", "chgrp", "setfacl", "busybox", "eval", "source", ".", "halt", "poweroff", "reboot", "shutdown", "kill", "killall", "pkill",
  ...OBSERVATION_COMMANDS,
  ...BUILD_COMMANDS,
  ...PACKAGE_QUERY_COMMANDS,
  ...ENVIRONMENT_COMMANDS,
  ...NETWORK_COMMANDS,
  ...PACKAGE_COMMANDS,
  ...LANGUAGE_PACKAGE_COMMANDS,
  ...DESTRUCTIVE_FILE_COMMANDS,
  ...PARTITION_COMMANDS,
  ...HARDWARE_COMMANDS,
  ...USER_ADMIN_COMMANDS,
  ...SHELL_INTERPRETERS,
]);

function commandName(value) {
  return basename(String(value ?? "")).toLowerCase();
}

function isAssignment(value) {
  return /^[A-Za-z_][A-Za-z0-9_]*=.*/.test(value);
}

function isCriticalPath(value) {
  const path = String(value ?? "");
  return /^\/(?:bin|boot|dev|etc|lib|lib32|lib64|opt|proc|run|sbin|srv|sys|usr)(?:\/|$)/.test(path)
    || path === "/var"
    || /^\/var\/(?!tmp(?:\/|$))/.test(path);
}

function isCredentialPath(value) {
  return /^(?:~|\/(?:root|home\/[^/]+))\/(?:\.ssh|\.config\/(?:autostart|hobot-code|systemd\/user)|\.local\/(?:bin|share\/systemd\/user)|\.(?:bash_profile|bashrc|gitconfig|netrc|profile|zlogin|zprofile|zshrc))(?:\/|$)/.test(String(value ?? ""));
}

function isHobotStatePath(value) {
  return /^\/(?:root|home\/[^/]+)\/\.local\/state\/hobot-code(?:\/|$)/.test(String(value ?? ""));
}

function pushUnique(target, values) {
  for (const value of values) if (!target.includes(value)) target.push(value);
}

function shellWord(value = "", dynamic = false) {
  return { value, dynamic, nestedPrograms: [] };
}

function readBalancedShellFragment(source, start) {
  let index = start;
  let depth = 1;
  let quote = "";
  while (index < source.length) {
    const char = source[index];
    if (char === "\\") {
      index += 2;
      continue;
    }
    if (quote) {
      if (char === quote) quote = "";
      index += 1;
      continue;
    }
    if (char === "'" || char === '"') {
      quote = char;
      index += 1;
      continue;
    }
    if (char === "(") depth += 1;
    if (char === ")") {
      depth -= 1;
      if (depth === 0) return { text: source.slice(start, index), end: index + 1, closed: true };
    }
    index += 1;
  }
  return { text: source.slice(start), end: source.length, closed: false };
}

function readBacktickFragment(source, start) {
  let index = start;
  while (index < source.length) {
    if (source[index] === "\\") {
      index += 2;
      continue;
    }
    if (source[index] === "`") return { text: source.slice(start, index), end: index + 1, closed: true };
    index += 1;
  }
  return { text: source.slice(start), end: source.length, closed: false };
}

// Parse execution-relevant shell structure only. Normal argument text is kept
// as data: a `chmod` grep pattern or a Hobot schedule prompt is not executable.
function parseShellProgram(source, depth = 0) {
  const text = String(source ?? "");
  const program = { segments: [], ambiguousReasons: [] };
  if (depth > 12) {
    program.ambiguousReasons.push("shell nesting is too deep to classify safely");
    return program;
  }
  let segment = { words: [], redirections: [], operatorAfter: "", nestedPrograms: [] };
  let word = null;
  let awaitingRedirection = null;
  let index = 0;

  const ensureWord = () => {
    if (!word) word = shellWord();
    return word;
  };
  const pushWord = () => {
    if (!word) return;
    if (awaitingRedirection) {
      awaitingRedirection.target = word.value;
      awaitingRedirection.dynamic = word.dynamic || word.nestedPrograms.length > 0;
      awaitingRedirection = null;
    } else if (word.value || word.dynamic || word.nestedPrograms.length > 0) {
      segment.words.push(word);
    }
    word = null;
  };
  const closeSegment = (operator = "") => {
    pushWord();
    if (awaitingRedirection) {
      program.ambiguousReasons.push("shell redirection is missing its target");
      awaitingRedirection = null;
    }
    if (segment.words.length || segment.redirections.length) {
      segment.operatorAfter = operator;
      program.segments.push(segment);
    }
    segment = { words: [], redirections: [], operatorAfter: "", nestedPrograms: [] };
  };
  const appendNested = (fragment, marker) => {
    const current = ensureWord();
    const nested = parseShellProgram(fragment.text, depth + 1);
    current.value += marker;
    current.dynamic = true;
    current.nestedPrograms.push(nested);
    segment.nestedPrograms.push(nested);
    if (!fragment.closed) program.ambiguousReasons.push("shell command substitution is not closed");
  };

  while (index < text.length) {
    const char = text[index];
    if (/\s/.test(char)) {
      pushWord();
      index += 1;
      continue;
    }
    if (char === "#" && !word) {
      while (index < text.length && text[index] !== "\n") index += 1;
      continue;
    }
    if (char === "\\") {
      if (index + 1 < text.length) ensureWord().value += text[index + 1];
      else program.ambiguousReasons.push("shell escape is not complete");
      index += 2;
      continue;
    }
    if (char === "'" || char === '"') {
      const quote = char;
      const current = ensureWord();
      index += 1;
      let closed = false;
      while (index < text.length) {
        const quoted = text[index];
        if (quoted === quote) {
          closed = true;
          index += 1;
          break;
        }
        if (quote === '"' && quoted === "\\") {
          if (index + 1 >= text.length) {
            program.ambiguousReasons.push("shell escape is not complete");
            index += 1;
            break;
          }
          current.value += text[index + 1];
          index += 2;
          continue;
        }
        if (quote === '"' && quoted === "$" && text[index + 1] === "(") {
          const fragment = readBalancedShellFragment(text, index + 2);
          appendNested(fragment, "$()");
          index = fragment.end;
          continue;
        }
        if (quote === '"' && quoted === "`") {
          const fragment = readBacktickFragment(text, index + 1);
          appendNested(fragment, "``");
          index = fragment.end;
          continue;
        }
        current.value += quoted;
        if (quote === '"' && quoted === "$") current.dynamic = true;
        index += 1;
      }
      if (!closed) program.ambiguousReasons.push("shell quote is not closed");
      continue;
    }
    if (char === "$" && text[index + 1] === "(") {
      const fragment = readBalancedShellFragment(text, index + 2);
      appendNested(fragment, "$()");
      index = fragment.end;
      continue;
    }
    if (char === "`") {
      const fragment = readBacktickFragment(text, index + 1);
      appendNested(fragment, "``");
      index = fragment.end;
      continue;
    }
    if ((char === "<" || char === ">") && text[index + 1] === "(") {
      const fragment = readBalancedShellFragment(text, index + 2);
      appendNested(fragment, "process-substitution");
      index = fragment.end;
      continue;
    }
    if (char === "$") {
      const current = ensureWord();
      current.dynamic = true;
      current.value += char;
      index += 1;
      continue;
    }
    if (char === ";" || char === "\n") {
      closeSegment(";");
      index += 1;
      continue;
    }
    if (char === "&" || char === "|") {
      if (char === "&" && text[index + 1] === ">") {
        pushWord();
        const redirection = { operator: text[index + 2] === ">" ? "&>>" : "&>", target: "", dynamic: false };
        segment.redirections.push(redirection);
        awaitingRedirection = redirection;
        index += text[index + 2] === ">" ? 3 : 2;
        continue;
      }
      const operator = text[index + 1] === char || (char === "|" && text[index + 1] === "&")
        ? `${char}${text[index + 1]}`
        : char;
      closeSegment(operator);
      index += operator.length;
      continue;
    }
    if (char === "<" || char === ">") {
      pushWord();
      let descriptor = "";
      const previous = segment.words.at(-1);
      if (previous && /^\d+$/.test(previous.value) && !previous.dynamic) descriptor = segment.words.pop().value;
      let operator = char;
      index += 1;
      if (text[index] === char || (char === "<" && text[index] === ">")) {
        operator += char;
        index += 1;
      }
      if (text[index] === "&") {
        operator += "&";
        index += 1;
      }
      const redirection = { operator: `${descriptor}${operator}`, target: "", dynamic: false };
      segment.redirections.push(redirection);
      awaitingRedirection = redirection;
      continue;
    }
    if (char === "(" || char === ")" || char === "{") {
      program.ambiguousReasons.push("shell control syntax cannot be classified safely");
      index += 1;
      continue;
    }
    ensureWord().value += char;
    index += 1;
  }
  closeSegment();
  return program;
}

function processControlIsObservation(args) {
  const first = args[0] ?? "";
  if (["-l", "-L", "--list", "--help", "--version"].includes(first)) return true;
  let observedSignal = false;
  for (let index = 0; index < args.length; index += 1) {
    const value = args[index];
    if (value === "--") break;
    if (value === "-s" || value === "--signal" || value === "-n") {
      if (args[++index] !== "0") return false;
      observedSignal = true;
      continue;
    }
    if (value === "-0" || value === "--signal=0") {
      observedSignal = true;
      continue;
    }
    if (value.startsWith("-")) return false;
  }
  return observedSignal;
}

function isHelpOrVersion(args) {
  return args.length > 0 && args.every((arg) => HELP_FLAGS.has(arg));
}

function optionValueIndex(words, index, optionsWithValues) {
  return optionsWithValues.has(words[index]?.value) ? index + 2 : index + 1;
}

function executableForSegment(segment) {
  const words = segment.words;
  let index = 0;
  while (index < words.length && isAssignment(words[index].value)) index += 1;
  const wrappers = [];
  while (index < words.length) {
    const word = words[index];
    if (word.dynamic) return { ambiguous: "shell command name is dynamic", wrappers, command: "", args: [] };
    const name = commandName(word.value);
    if (SHELL_CONTROL_PREFIXES.has(name)) {
      index += 1;
      continue;
    }
    if (SHELL_CONTROL_ONLY.has(name)) return { command: "", args: [], wrappers };
    if (name === "sudo") {
      wrappers.push(name);
      index += 1;
      while (index < words.length && words[index].value.startsWith("-")) {
        index = optionValueIndex(words, index, new Set(["-u", "-g", "-h", "-C", "-c", "-r", "-t", "-T"]));
      }
      continue;
    }
    if (name === "busybox") {
      wrappers.push(name);
      index += 1;
      if (index >= words.length || words[index].dynamic) return { ambiguous: "BusyBox applet is dynamic", wrappers, command: "", args: [] };
      return { command: commandName(words[index].value), args: words.slice(index + 1), wrappers };
    }
    if (SHELL_WRAPPERS.has(name)) {
      wrappers.push(name);
      index += 1;
      if (words[index]?.value === "--") index += 1;
      if (name === "command") {
        // `command -v` and `command -V` query command resolution; their
        // operand is data, not an executable selected by the Agent.
        if (["-v", "-V", "--help", "--version"].includes(words[index]?.value ?? "")) {
          return { command: "command", args: words.slice(index), wrappers };
        }
        while (words[index]?.value === "-p") index += 1;
      } else if (name === "env") {
        while (index < words.length && (words[index].value.startsWith("-") || isAssignment(words[index].value))) {
          index = optionValueIndex(words, index, new Set(["-u", "--unset", "-C", "--chdir", "-S", "--split-string"]));
        }
      } else if (name === "timeout" || name === "gtimeout") {
        while (index < words.length && words[index].value.startsWith("-")) {
          index = optionValueIndex(words, index, new Set(["-k", "--kill-after", "-s", "--signal"]));
        }
        if (index < words.length) index += 1;
      } else if (name === "nice") {
        while (index < words.length && words[index].value.startsWith("-")) index = optionValueIndex(words, index, new Set(["-n", "--adjustment"]));
      } else if (name === "ionice") {
        while (index < words.length && words[index].value.startsWith("-")) index = optionValueIndex(words, index, new Set(["-c", "-n", "-p", "-P", "-t"]));
      } else if (name === "stdbuf") {
        while (index < words.length && words[index].value.startsWith("-")) index = optionValueIndex(words, index, new Set(["-i", "-o", "-e"]));
      } else if (name === "time") {
        while (index < words.length && ["-p", "-v", "--portability", "--verbose"].includes(words[index].value)) index += 1;
      }
      continue;
    }
    return { command: name, args: words.slice(index + 1), wrappers };
  }
  return { command: "", args: [], wrappers, ambiguous: words.length ? "shell command is missing an executable" : undefined };
}

function argumentValues(args) {
  return args.map((arg) => arg.value);
}

function firstAction(args) {
  return argumentValues(args).find((value) => value && !value.startsWith("-")) ?? "";
}

function actionAfterGlobalOptions(values, optionsWithValues = new Set()) {
  let index = 0;
  while (index < values.length && values[index].startsWith("-")) {
    if (optionsWithValues.has(values[index])) index += 2;
    else index += 1;
  }
  return { action: values[index] ?? "", index };
}

function gitAction(values) {
  return actionAfterGlobalOptions(values, new Set([
    "-C", "-c", "--config-env", "--exec-path", "--git-dir", "--namespace", "--super-prefix", "--work-tree",
  ])).action;
}

function dockerAction(values) {
  const start = actionAfterGlobalOptions(values, new Set([
    "-c", "--config", "--context", "-H", "--host", "-l", "--log-level", "--tlscacert", "--tlscert", "--tlskey",
  ])).index;
  return values[start] === "container" ? values[start + 1] ?? "" : values[start] ?? "";
}

function firstNonOption(values) {
  return values.find((value) => value && !value.startsWith("-")) ?? "";
}

function ipMutation(values) {
  const objects = new Set(["address", "addr", "link", "route", "rule", "neighbour", "neighbor", "netns", "tunnel"]);
  const mutations = new Set(["add", "append", "change", "delete", "del", "flush", "replace", "set"]);
  const objectIndex = values.findIndex((value) => objects.has(value));
  if (objectIndex < 0) return false;
  return values.slice(objectIndex + 1).some((value) => mutations.has(value));
}

function environmentMutation(values) {
  const action = firstNonOption(values);
  if (["create", "install", "remove", "uninstall", "update", "upgrade", "rename", "clean"].includes(action)) return true;
  if (action === "env") {
    const subcommand = values.slice(values.indexOf(action) + 1).find((value) => value && !value.startsWith("-"));
    return ["create", "remove", "update"].includes(subcommand);
  }
  if (action === "config") {
    return values.some((value) => ["--add", "--append", "--prepend", "--remove", "--remove-key", "--set"].includes(value));
  }
  return false;
}

function kubectlMutation(values) {
  const mutating = new Set(["annotate", "apply", "attach", "autoscale", "cordon", "cp", "create", "delete", "drain", "edit", "exec", "expose", "label", "patch", "replace", "run", "scale", "set", "taint", "uncordon"]);
  if (values.some((value) => mutating.has(value))) return true;
  if (values.includes("rollout") && values.some((value) => ["pause", "restart", "resume", "undo"].includes(value))) return true;
  if (values.includes("certificate") || (values.includes("auth") && values.includes("reconcile"))) return true;
  return values.includes("config") && values.some((value) => value === "use-context" || value.startsWith("set-") || value === "delete-context" || value === "delete-cluster" || value === "delete-user" || value === "rename-context");
}

function containerMutation(values) {
  const start = actionAfterGlobalOptions(values, new Set([
    "-c", "--config", "--context", "-H", "--host", "-l", "--log-level", "--tlscacert", "--tlscert", "--tlskey",
  ])).index;
  const commandValues = values.slice(start);
  const action = dockerAction(values);
  if (["exec", "kill", "pause", "prune", "rename", "restart", "rm", "stop", "unpause", "update"].includes(action)) return true;
  if (["image", "network", "system", "volume"].includes(commandValues[0]) && ["prune", "rm"].includes(commandValues[1])) return true;
  if (commandValues[0] === "compose" && ["down", "kill", "rm", "stop"].includes(commandValues[1])) return true;
  return action === "run" && hasOptionFromValues(commandValues, "--privileged", "--pid=host", "-v", "--volume");
}

function hasOptionFromValues(values, ...options) {
  return values.some((value) => options.includes(value) || options.some((option) => value.startsWith(`${option}=`)));
}

function hasOption(args, ...options) {
  return argumentValues(args).some((value) => options.includes(value) || options.some((option) => value.startsWith(`${option}=`)));
}

function lastPathArgumentWord(args) {
  const values = argumentValues(args);
  for (let index = 0; index < values.length; index += 1) {
    if (["-t", "--target-directory"].includes(values[index]) && values[index + 1]) return args[index + 1];
    if (values[index].startsWith("--target-directory=")) return args[index];
  }
  for (let index = values.length - 1; index >= 0; index -= 1) if (!values[index].startsWith("-")) return args[index];
  return undefined;
}

function lastPathArgument(args) {
  const word = lastPathArgumentWord(args);
  if (!word) return "";
  return word.value.startsWith("--target-directory=") ? word.value.slice("--target-directory=".length) : word.value;
}

function pathsInArguments(args) {
  return argumentValues(args).filter((value) => value && value !== "--" && !value.startsWith("-"));
}

function isDangerousHobotInvocation(args) {
  const values = argumentValues(args);
  if (values.some((value) => HELP_FLAGS.has(value))) return "";
  if (values[0] === "permissions" && !["status", "reload"].includes(values[1] ?? "status")) return "changes Hobot Code permissions";
  if (values[0] === "task" && ["delete", "purge"].includes(values[1])) return "deletes Hobot Code task state";
  if (["update", "upgrade"].includes(values[0])) return "installs or updates Hobot Code";
  return "";
}

function interpreterPayload(args) {
  const values = argumentValues(args);
  for (let index = 0; index < values.length; index += 1) if (["-c", "--command"].includes(values[index])) return args[index + 1];
  return undefined;
}

function sshPayload(args) {
  const values = argumentValues(args);
  const optionValues = new Set(["-b", "-c", "-D", "-E", "-F", "-i", "-J", "-L", "-l", "-m", "-O", "-o", "-p", "-Q", "-R", "-S", "-W", "-w"]);
  let index = 0;
  while (index < values.length) {
    if (values[index] === "--") {
      index += 1;
      break;
    }
    if (!values[index].startsWith("-")) break;
    index = optionValueIndex(args, index, optionValues);
  }
  if (index >= args.length) return undefined;
  return args.slice(index + 1);
}

function findExecPayloads(args) {
  const payloads = [];
  for (let index = 0; index < args.length; index += 1) {
    if (!["-exec", "-execdir", "-ok", "-okdir"].includes(args[index].value)) continue;
    const words = [];
    index += 1;
    while (index < args.length && ![";", "+"].includes(args[index].value)) words.push(args[index++]);
    if (words.length) payloads.push({ segments: [{ words, redirections: [], operatorAfter: "", nestedPrograms: [] }], ambiguousReasons: [] });
  }
  return payloads;
}

function xargsPayload(args) {
  let index = 0;
  const optionsWithValues = new Set(["-E", "-e", "-I", "-L", "-n", "-P", "-s", "--eof", "--max-args", "--max-lines", "--max-procs", "--max-chars", "--replace"]);
  while (index < args.length && args[index].value.startsWith("-")) index = optionValueIndex(args, index, optionsWithValues);
  return index < args.length ? { segments: [{ words: args.slice(index), redirections: [], operatorAfter: "", nestedPrograms: [] }], ambiguousReasons: [] } : undefined;
}

function commandIsKnown(name) {
  return SAFE_DEVELOPER_COMMANDS.has(name) || name.startsWith("hrt_") || name.startsWith("hb_") || name.startsWith("mkfs");
}

function addPathWriteReasons(reasons, ambiguities, name, args, redirections) {
  for (const redirection of redirections) {
    if (!redirection.operator.includes(">")) continue;
    if (redirection.dynamic) pushUnique(ambiguities, ["writes to a dynamic path that requires an OS sandbox boundary"]);
    if (isCriticalPath(redirection.target) && redirection.target !== "/dev/null") pushUnique(reasons, ["writes to a protected system path"]);
    if (isCredentialPath(redirection.target)) pushUnique(reasons, ["writes user credentials, startup, or persistent configuration"]);
  }
  if (name === "tee") {
    if (args.some((word) => word.dynamic && word.value !== "--" && !word.value.startsWith("-"))) {
      pushUnique(ambiguities, ["writes to a dynamic path that requires an OS sandbox boundary"]);
    }
    for (const path of pathsInArguments(args)) {
      if (isCriticalPath(path) && path !== "/dev/null") pushUnique(reasons, ["writes to a protected system path"]);
      if (isCredentialPath(path)) pushUnique(reasons, ["writes user credentials, startup, or persistent configuration"]);
    }
  }
  if (["cp", "mv", "install", "ln", "mkdir", "touch"].includes(name)) {
    const destinationWord = lastPathArgumentWord(args);
    const destination = lastPathArgument(args);
    if (destinationWord?.dynamic) pushUnique(ambiguities, ["writes to a dynamic path that requires an OS sandbox boundary"]);
    if (isCriticalPath(destination)) pushUnique(reasons, ["modifies a protected system path"]);
    if (isCredentialPath(destination)) pushUnique(reasons, ["writes user credentials, startup, or persistent configuration"]);
    if (["cp", "mv", "install", "ln"].includes(name) && isHobotStatePath(destination)) {
      pushUnique(reasons, ["removes or replaces Hobot Code persistent task and conversation state"]);
    }
  }
  if (name === "mv" && pathsInArguments(args).some(isHobotStatePath)) pushUnique(reasons, ["removes or replaces Hobot Code persistent task and conversation state"]);
}

export function analyzeShellCommand(command) {
  const root = parseShellProgram(command);
  const result = { destructiveReasons: [], networkReasons: [], ambiguousReasons: [...root.ambiguousReasons], remoteScanReasons: [] };
  const visited = new Set();
  const inspectProgram = (program, inheritedTimeout = false) => {
    if (visited.has(program)) return;
    visited.add(program);
    pushUnique(result.ambiguousReasons, program.ambiguousReasons ?? []);
    for (const segment of program.segments) {
      const executable = executableForSegment(segment);
      if (executable.ambiguous) pushUnique(result.ambiguousReasons, [executable.ambiguous]);
      const { command: name, args, wrappers } = executable;
      const values = argumentValues(args);
      const helpOnly = isHelpOrVersion(values);
      const timed = inheritedTimeout || wrappers.includes("timeout") || wrappers.includes("gtimeout");
      for (const nested of segment.nestedPrograms) inspectProgram(nested, timed);
      const networkRedirection = segment.redirections.some((entry) => /^\/dev\/(?:tcp|udp)\//.test(entry.target));
      if (!name) {
        if (!helpOnly) addPathWriteReasons(result.destructiveReasons, result.ambiguousReasons, name, args, segment.redirections);
        if (networkRedirection) pushUnique(result.networkReasons, ["uses a recognized outbound network client while the OS sandbox shares host networking"]);
        continue;
      }
      if (!commandIsKnown(name)) pushUnique(result.ambiguousReasons, [`runs an unclassified external command: ${name}`]);
      if (["eval", ".", "source"].includes(name)) pushUnique(result.ambiguousReasons, [name === "eval" ? "evaluates shell text that cannot be classified safely" : "loads shell code that cannot be classified safely"]);

      if (!helpOnly) {
        if (DESTRUCTIVE_FILE_COMMANDS.has(name)) {
          pushUnique(result.destructiveReasons, ["removes or destroys files"]);
          if (pathsInArguments(args).some(isHobotStatePath)) pushUnique(result.destructiveReasons, ["removes or replaces Hobot Code persistent task and conversation state"]);
        }
        if (name === "find") {
          if (values.includes("-delete")) pushUnique(result.destructiveReasons, ["deletes files through find"]);
          for (const payload of findExecPayloads(args)) inspectProgram(payload, timed);
        }
        if (name === "dd" && values.some((value) => /^of=\/dev\//.test(value))) pushUnique(result.destructiveReasons, ["writes directly to a block or device node"]);
        if (name.startsWith("mkfs") || PARTITION_COMMANDS.has(name)) pushUnique(result.destructiveReasons, ["changes a filesystem or partition table"]);
        if (["chmod", "chown", "chgrp", "setfacl"].includes(name)) pushUnique(result.destructiveReasons, ["changes file ownership or access permissions"]);
        if (PROCESS_CONTROL_COMMANDS.has(name) && !processControlIsObservation(values)) pushUnique(result.destructiveReasons, ["terminates running processes"]);
        if (name === "git") {
          const action = gitAction(values);
          if (action === "clean" || (action === "reset" && values.includes("--hard")) || (action === "push" && hasOption(args, "--force", "--force-with-lease", "-f")) || (action === "branch" && hasOption(args, "-D", "--delete"))) {
            pushUnique(result.destructiveReasons, ["performs a destructive or forceful Git operation"]);
          }
        }
        const systemctlAction = name === "systemctl"
          ? actionAfterGlobalOptions(values, new Set(["-H", "--host", "-M", "--machine", "-p", "--property", "--root", "--image", "-s", "--signal", "-t", "--type", "--state", "--job-mode", "--kill-whom"])).action
          : "";
        if (name === "systemctl" && ["daemon-reload", "disable", "enable", "halt", "isolate", "mask", "poweroff", "preset", "reboot", "reload", "restart", "start", "stop", "unmask"].includes(systemctlAction)) {
          pushUnique(result.destructiveReasons, ["changes or stops a system service"]);
        }
        if (name === "systemctl" && ["edit", "kill", "link", "revert", "set-default", "set-property"].includes(systemctlAction)) {
          pushUnique(result.destructiveReasons, ["changes system service configuration or process state"]);
        }
        if (name === "service" && ["start", "stop", "restart", "reload", "force-reload"].includes(values.at(-1))) pushUnique(result.destructiveReasons, ["changes or stops a system service"]);
        if (["halt", "poweroff", "reboot", "shutdown"].includes(name)) pushUnique(result.destructiveReasons, ["stops or reboots the board"]);
        if (PACKAGE_COMMANDS.has(name) && values.some((value) => ["autoremove", "dist-upgrade", "full-upgrade", "install", "purge", "remove", "update", "upgrade"].includes(value))) pushUnique(result.destructiveReasons, ["changes installed software or package metadata"]);
        if (name === "dpkg" && values.some((value) => /^(?:-i|--install|-r|--remove|-P|--purge)$/.test(value))) pushUnique(result.destructiveReasons, ["changes installed software"]);
        if (name === "rpm" && values.some((value) => /^(?:-[A-Za-z]*[eFiU]|--erase|--freshen|--install|--upgrade)$/.test(value))) pushUnique(result.destructiveReasons, ["changes installed software"]);
        if ((name === "make" || name === "ninja") && values.includes("install")) pushUnique(result.destructiveReasons, ["installs build output into the system"]);
        if ((name === "cmake" && values.includes("--install")) || (name === "meson" && firstAction(args) === "install")) pushUnique(result.destructiveReasons, ["installs build output into the system"]);
        if (["pip", "pip3", "npm", "pnpm", "yarn", "gem"].includes(name) && values.some((value) => ["install", "add"].includes(value)) && hasOption(args, "--global", "-g")) pushUnique(result.destructiveReasons, ["installs a global language package"]);
        if (ENVIRONMENT_COMMANDS.has(name) && environmentMutation(values)) pushUnique(result.destructiveReasons, ["changes a managed language environment"]);
        if (["docker", "podman"].includes(name)) {
          if (containerMutation(values)) {
            pushUnique(result.destructiveReasons, ["performs a privileged or destructive container operation"]);
          }
        }
        if (name === "kubectl" && kubectlMutation(values)) pushUnique(result.destructiveReasons, ["changes cluster state or executes inside a workload"]);
        if (["umount", "swapon", "swapoff"].includes(name) || (name === "mount" && values.length > 0)) pushUnique(result.destructiveReasons, ["changes mounted filesystems or swap"]);
        if (["insmod", "modprobe", "rmmod"].includes(name)) pushUnique(result.destructiveReasons, ["changes loaded kernel modules"]);
        if (name === "sysctl" && (hasOption(args, "-w", "--write") || values.some((value) => /^[^=\s]+=/.test(value)))) pushUnique(result.destructiveReasons, ["changes kernel runtime settings"]);
        if (USER_ADMIN_COMMANDS.has(name)) pushUnique(result.destructiveReasons, ["changes users, groups, or authentication"]);
        if (["iptables", "ip6tables", "nft"].includes(name)) pushUnique(result.destructiveReasons, ["changes firewall or packet-filter state"]);
        if (name === "ip" && ipMutation(values)) pushUnique(result.destructiveReasons, ["changes network configuration"]);
        if (name === "ethtool" && hasOption(args, "-s", "--change", "-A", "--pause", "-C", "--coalesce", "-E", "--change-eeprom", "-G", "--set-ring", "-K", "--offload", "-L", "--set-channels", "-N", "-U", "-X", "--set-eee", "--set-phy-tunable", "--set-priv-flags", "--set-rxfh-indir", "--set-tunable")) pushUnique(result.destructiveReasons, ["changes network device settings"]);
        if (name === "nvidia-smi" && hasOption(args, "-ac", "--applications-clocks", "-am", "--accounting-mode", "--auto-boost-default", "--auto-boost-permission", "-c", "--compute-mode", "-cc", "--conf-compute", "-caa", "--clear-accounted-apps", "-dc", "--drain-control", "-dm", "--driver-model", "-e", "--ecc-config", "-fdm", "--force-driver-model", "-gtt", "--gpu-target-temp", "-lgc", "--lock-gpu-clocks", "-lmc", "--lock-memory-clocks", "-mig", "--multi-instance-gpu", "-pm", "--persistence-mode", "-pl", "--power-limit", "-r", "--gpu-reset", "-rac", "--reset-applications-clocks", "--reset-ecc-errors", "-rgc", "--reset-gpu-clocks", "-rmc", "--reset-memory-clocks", "-vm", "--virt-mode")) pushUnique(result.destructiveReasons, ["changes GPU runtime or persistence settings"]);
        if (name === "dmesg" && hasOption(args, "-C", "--clear", "-c", "--read-clear")) pushUnique(result.destructiveReasons, ["clears kernel logs"]);
        if (name === "journalctl" && values.some((value) => value === "--rotate" || value === "--flush" || value === "--sync" || value.startsWith("--vacuum-"))) pushUnique(result.destructiveReasons, ["changes or removes system journal state"]);
        if (name === "sensors" && hasOption(args, "-s", "--set")) pushUnique(result.destructiveReasons, ["writes hardware monitoring limits"]);
        if (HARDWARE_COMMANDS.has(name)) pushUnique(result.destructiveReasons, ["writes to board hardware or firmware state"]);
        if (name === "sed" || name === "perl") {
          if (hasOption(args, "-i", "--in-place") && pathsInArguments(args).some(isCriticalPath)) pushUnique(result.destructiveReasons, ["edits a protected system path in place"]);
          if (hasOption(args, "-i", "--in-place") && pathsInArguments(args).some(isCredentialPath)) pushUnique(result.destructiveReasons, ["writes user credentials, startup, or persistent configuration"]);
          if (hasOption(args, "-i", "--in-place") && args.at(-1)?.dynamic) pushUnique(result.ambiguousReasons, ["writes to a dynamic path that requires an OS sandbox boundary"]);
        }
        if (name === "hobot") {
          const reason = isDangerousHobotInvocation(args);
          if (reason) pushUnique(result.destructiveReasons, [reason]);
        }
        addPathWriteReasons(result.destructiveReasons, result.ambiguousReasons, name, args, segment.redirections);
      }

      if (NETWORK_COMMANDS.has(name)) pushUnique(result.networkReasons, ["uses a recognized outbound network client while the OS sandbox shares host networking"]);
      if (name === "git" && ["clone", "fetch", "pull", "push", "ls-remote", "submodule"].includes(gitAction(values))) pushUnique(result.networkReasons, ["uses a recognized outbound network client while the OS sandbox shares host networking"]);
      if (PACKAGE_COMMANDS.has(name) && values.some((value) => ["download", "install", "refresh", "update", "upgrade", "dist-upgrade", "full-upgrade"].includes(value))) pushUnique(result.networkReasons, ["uses a recognized outbound network client while the OS sandbox shares host networking"]);
      if (LANGUAGE_PACKAGE_COMMANDS.has(name) && values.some((value) => ["add", "ci", "dlx", "fetch", "get", "install", "publish", "update"].includes(value))) pushUnique(result.networkReasons, ["uses a recognized outbound network client while the OS sandbox shares host networking"]);
      if (["docker", "podman"].includes(name) && ["build", "login", "pull", "push", "run"].includes(dockerAction(values))) pushUnique(result.networkReasons, ["uses a recognized outbound network client while the OS sandbox shares host networking"]);
      if (["gh", "glab", "kubectl"].includes(name) || networkRedirection) pushUnique(result.networkReasons, ["uses a recognized outbound network client while the OS sandbox shares host networking"]);

      if (SHELL_INTERPRETERS.has(name)) {
        const payload = interpreterPayload(args);
        if (payload) inspectProgram(parseShellProgram(payload.value), timed);
        else if (values.some((value) => ["-c", "--command"].includes(value))) pushUnique(result.ambiguousReasons, ["shell interpreter received dynamic script text"]);
      }
      if (name === "ssh") {
        const payload = sshPayload(args);
        if (payload?.length) {
          const remoteProgram = parseShellProgram(payload.map((word) => word.value).join(" "));
          const remoteFind = remoteProgram.segments.some((candidate) => {
            const remote = executableForSegment(candidate);
            return remote.command === "find" && argumentValues(remote.args).some((value) => /^\/(?:mnt\/data|cache|home)(?:\/|$)/.test(value));
          });
          const remoteTimed = timed || remoteProgram.segments.some((candidate) => {
            const remote = executableForSegment(candidate);
            return remote.wrappers.includes("timeout") || remote.wrappers.includes("gtimeout");
          });
          if (remoteFind && !remoteTimed) pushUnique(result.remoteScanReasons, ["remote recursive scan has no timeout and targets shared storage"]);
          inspectProgram(remoteProgram, timed);
        }
      }
      if (name === "xargs") {
        const payload = xargsPayload(args);
        if (payload) inspectProgram(payload, timed);
      }
    }
    for (let index = 0; index < program.segments.length; index += 1) {
      const left = executableForSegment(program.segments[index]);
      if (!["curl", "wget"].includes(left.command) || !["|", "|&"].includes(program.segments[index].operatorAfter)) continue;
      for (let next = index + 1; next < program.segments.length; next += 1) {
        const right = executableForSegment(program.segments[next]);
        if (SHELL_INTERPRETERS.has(right.command)) {
          pushUnique(result.destructiveReasons, ["downloads and executes remote content"]);
          break;
        }
        if (!["|", "|&"].includes(program.segments[next].operatorAfter)) break;
      }
    }
  };
  inspectProgram(root);
  return result;
}
