import { homedir } from "node:os";
import { resolve } from "node:path";

export function resolveUserPaths(env = process.env, home = homedir()) {
  const configHome = resolve(env.XDG_CONFIG_HOME || resolve(home, ".config"));
  const stateHome = resolve(env.XDG_STATE_HOME || resolve(home, ".local", "state"));
  const configRoot = resolve(env.HOBOT_CODE_CONFIG_DIR || resolve(configHome, "hobot-code"));
  const agentDir = resolve(env.HOBOT_CODING_AGENT_DIR || resolve(configRoot, "agent"));
  const stateRoot = resolve(env.HOBOT_CODE_STATE_DIR || resolve(stateHome, "hobot-code"));
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
