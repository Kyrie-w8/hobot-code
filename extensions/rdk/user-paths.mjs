import { homedir } from "node:os";
import { isAbsolute, resolve } from "node:path";

function absolutePath(name, value) {
  if (!isAbsolute(value)) throw new Error(`${name} must be an absolute path: ${value}`);
  return resolve(value);
}

export function resolveUserPaths(env = process.env, home = homedir()) {
  const configHome = absolutePath("XDG_CONFIG_HOME", env.XDG_CONFIG_HOME || resolve(home, ".config"));
  const stateHome = absolutePath("XDG_STATE_HOME", env.XDG_STATE_HOME || resolve(home, ".local", "state"));
  const configRoot = absolutePath("HOBOT_CODE_CONFIG_DIR", env.HOBOT_CODE_CONFIG_DIR || resolve(configHome, "hobot-code"));
  const agentDir = absolutePath("HOBOT_CODING_AGENT_DIR", env.HOBOT_CODING_AGENT_DIR || resolve(configRoot, "agent"));
  const stateRoot = absolutePath("HOBOT_CODE_STATE_DIR", env.HOBOT_CODE_STATE_DIR || resolve(stateHome, "hobot-code"));
  return {
    configRoot,
    agentDir,
    stateRoot,
    permissionPolicy: resolve(agentDir, "permissions.json"),
    memoryConfig: resolve(agentDir, "memory.json"),
    memoryDatabase: resolve(stateRoot, "memory", "memory.db"),
    goalConfig: resolve(agentDir, "goals.json"),
    goalDatabase: resolve(stateRoot, "goals", "goals.db"),
    hookConfig: resolve(agentDir, "hooks.json"),
    hookAudit: resolve(stateRoot, "audit", "hooks.jsonl"),
    notificationConfig: resolve(agentDir, "notifications.json"),
    lspConfig: resolve(agentDir, "lsp.json"),
  };
}
