import { homedir } from "node:os";
import { isAbsolute, resolve } from "node:path";

const RUNTIME_ROOT = "/usr/local/lib/hobot-code";

function absolutePath(name, value) {
  if (!isAbsolute(value)) throw new Error(`${name} must be an absolute path: ${value}`);
  return resolve(value);
}

export function resolveUserPaths(env = process.env, home = homedir()) {
  const userHome = absolutePath("HOME", home);
  const configHome = absolutePath("XDG_CONFIG_HOME", env.XDG_CONFIG_HOME || resolve(userHome, ".config"));
  const stateHome = absolutePath("XDG_STATE_HOME", env.XDG_STATE_HOME || resolve(userHome, ".local", "state"));
  const configRoot = absolutePath("HOBOT_CODE_CONFIG_DIR", env.HOBOT_CODE_CONFIG_DIR || resolve(configHome, "hobot-code"));
  const agentDir = absolutePath("HOBOT_CODING_AGENT_DIR", env.HOBOT_CODING_AGENT_DIR || resolve(configRoot, "agent"));
  const stateRoot = absolutePath("HOBOT_CODE_STATE_DIR", env.HOBOT_CODE_STATE_DIR || resolve(stateHome, "hobot-code"));
  const sessionDir = absolutePath(
    "HOBOT_CODING_AGENT_SESSION_DIR",
    env.HOBOT_CODING_AGENT_SESSION_DIR || resolve(stateRoot, "sessions"),
  );
  return {
    configRoot,
    agentDir,
    stateRoot,
    sessionDir,
    permissionPolicy: absolutePath(
      "HOBOT_CODE_PERMISSION_POLICY",
      env.HOBOT_CODE_PERMISSION_POLICY || resolve(agentDir, "permissions.json"),
    ),
    memoryConfig: absolutePath(
      "HOBOT_CODE_MEMORY_CONFIG",
      env.HOBOT_CODE_MEMORY_CONFIG || resolve(agentDir, "memory.json"),
    ),
    memoryDatabase: absolutePath(
      "HOBOT_CODE_MEMORY_DB",
      env.HOBOT_CODE_MEMORY_DB || resolve(stateRoot, "memory", "memory.db"),
    ),
    goalConfig: absolutePath(
      "HOBOT_CODE_GOAL_CONFIG",
      env.HOBOT_CODE_GOAL_CONFIG || resolve(agentDir, "goals.json"),
    ),
    goalDatabase: absolutePath(
      "HOBOT_CODE_GOAL_DB",
      env.HOBOT_CODE_GOAL_DB || resolve(stateRoot, "goals", "goals.db"),
    ),
    hookConfig: absolutePath(
      "HOBOT_CODE_HOOK_CONFIG",
      env.HOBOT_CODE_HOOK_CONFIG || resolve(agentDir, "hooks.json"),
    ),
    hookAudit: absolutePath(
      "HOBOT_CODE_HOOK_AUDIT",
      env.HOBOT_CODE_HOOK_AUDIT || resolve(stateRoot, "audit", "hooks.jsonl"),
    ),
    notificationConfig: absolutePath(
      "HOBOT_CODE_NOTIFICATION_CONFIG",
      env.HOBOT_CODE_NOTIFICATION_CONFIG || resolve(agentDir, "notifications.json"),
    ),
    lspConfig: absolutePath(
      "HOBOT_CODE_LSP_CONFIG",
      env.HOBOT_CODE_LSP_CONFIG || resolve(agentDir, "lsp.json"),
    ),
    rdkKnowledgeDir: absolutePath(
      "HOBOT_CODE_RDK_KNOWLEDGE_DIR",
      env.HOBOT_CODE_RDK_KNOWLEDGE_DIR || resolve(RUNTIME_ROOT, "knowledge"),
    ),
    rdkExpertPrompt: absolutePath(
      "HOBOT_CODE_RDK_EXPERT_PROMPT",
      env.HOBOT_CODE_RDK_EXPERT_PROMPT || resolve(RUNTIME_ROOT, "prompts", "rdk-expert.md"),
    ),
  };
}
