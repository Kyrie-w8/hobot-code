# Hobot Code 0.10 配置

## 路径

| 路径 | 作用 |
|---|---|
| `/etc/hobot-code/hobot.env` | root-only 模型密钥和端点 |
| `/etc/hobot-code/agent/settings.json` | Pi/Hobot Code 全局设置 |
| `/etc/hobot-code/agent/models.json` | 自定义 Provider 和模型 |
| `/etc/hobot-code/agent/auth.json` | `/login` 保存的认证信息 |
| `/etc/hobot-code/agent/permissions.json` | allow/ask/deny 工具权限策略 |
| `/etc/hobot-code/agent/memory.json` | 持久化记忆开关、检索与长度上限 |
| `/etc/hobot-code/agent/bin` | 固定版本的 fd 和 ripgrep |
| `/var/lib/hobot-code/sessions` | Pi JSONL 会话 |
| `/var/lib/hobot-code/memory/memory.db` | root-only SQLite/FTS5 持久化记忆与审计事件 |
| `/usr/local/lib/hobot-code/extensions` | RDK 扩展 |
| `/usr/local/lib/hobot-code/skills` | 板端 Skills |
| `/usr/local/lib/hobot-code/knowledge` | 版本化 RDK 板卡知识与官方来源索引 |
| `/usr/local/lib/hobot-code/prompts/rdk-expert.md` | 动态渲染的地瓜开发专家角色模板 |
| `<project>/.hobot/quality-gates.json` | 项目默认质量门命令与单命令超时 |

启动器设置 `HOBOT_CODING_AGENT_DIR=/etc/hobot-code/agent`。项目目录使用 `.hobot/`
放置局部 settings、extensions、skills、prompts 和 themes；Pi 的 project trust 机制会在
首次加载项目资源前询问。

## D-Robotics Kimi

`/etc/hobot-code/hobot.env`：

```text
ANTHROPIC_BASE_URL=https://ai-api.d-robotics.cc
ANTHROPIC_AUTH_TOKEN=your-token
ANTHROPIC_MODEL=kimi-k3
```

文件权限必须是 `0600`。Hobot Code 不把 token 写入 `models.json`、会话或日志。默认设置选择
`drobotics/kimi-k3`，thinking 等级为 `max`，Provider 请求超时为 3000000 ms。

## 添加模型

Pi 支持在 `models.json` 中定义 Anthropic Messages、OpenAI Chat Completions、OpenAI
Responses 和 Google Generative AI 兼容服务。例如本机 Ollama：

```json
{
  "providers": {
    "ollama": {
      "baseUrl": "http://127.0.0.1:11434/v1",
      "api": "openai-completions",
      "apiKey": "ollama",
      "models": [
        {
          "id": "qwen2.5-coder:7b",
          "contextWindow": 32768,
          "maxTokens": 4096
        }
      ]
    }
  }
}
```

保存后打开 `/model`；文件会重新读取，不需要重启。API key 可以写成 `$ENV_NAME` 引用，
不要把真实密钥直接写进 JSON。

## 交互设置

推荐直接使用 `/settings`、`/model`、`/scoped-models` 和 `/hotkeys`。默认 settings
启用自动压缩、三次 Agent 级重试、1M Kimi 上下文、可见 thinking 和 regular TUI。

扩展、Skills 和 Prompt 可用 Pi 原生命令管理：

```bash
hobot install <npm-or-git-source>
hobot list
hobot config
hobot update --extensions
```

第三方扩展拥有当前用户的完整系统权限，安装前必须审查源码。在 root 板端尤其不要加载
来源不明的 package 或 Skill。

## 环境覆盖

可按进程覆盖：

```bash
HOBOT_CODING_AGENT_DIR=/tmp/hobot-agent hobot
HOBOT_CODING_AGENT_SESSION_DIR=/tmp/hobot-sessions hobot
```

`PI_SKIP_VERSION_CHECK=1` 默认开启，避免 Hobot Code 被 Pi 的自更新提示误导。Hobot Code 的 Pi
运行时升级必须修改 `pi-runtime/pi.lock`、重新构建并完成板端回归。

知识目录可在开发和测试时按进程覆盖：

```bash
HOBOT_CODE_RDK_KNOWLEDGE_DIR=/path/to/knowledge hobot
HOBOT_CODE_RDK_EXPERT_PROMPT=/path/to/rdk-expert.md hobot
```

生产环境建议使用安装包内的只读知识目录。更新知识时修改 `knowledge/manifest.json` 的
`knowledgeVersion` 和 `updatedAt`，运行 `make pi-check` 后重新打包；不要直接在板端堆放
没有版本和来源的零散说明。

专家模板中的 `BOARD_NAME`、`BOARD_ID`、`RDK_OS_VERSION`、`DOCUMENTATION_TRACK`、
`HOSTNAME` 和 `ARCHITECTURE` 占位符由扩展动态替换。修改模板后运行 `make pi-check`，
可在板端用 `/system-prompt` 检查最终内容。

## 工具权限

规则按数组顺序匹配，第一条命中规则生效；未命中时使用 `default`。`mcp:*` 匹配所有 MCP
来源工具，普通 `*` 可用于工具名通配。示例：

```json
{
  "schemaVersion": 1,
  "default": "ask",
  "rules": [
    { "tool": "read", "action": "allow" },
    { "tool": "bash", "action": "ask" },
    { "tool": "mcp:*", "action": "deny" }
  ]
}
```

`/permissions set <pattern> <action>` 会把规则放到数组开头并原子写回配置。配置不存在或无效时
使用内置的保守默认值并显示警告。deny 工具从 Pi 活跃工具集合中移除，工具调用阶段仍会再次
检查，防止动态插件绕过。密钥、Bearer Token 和常见 secret 字段不会出现在确认详情中。

## 质量门

项目配置格式：

```json
{
  "schemaVersion": 1,
  "timeoutMs": 120000,
  "commands": ["make check"]
}
```

每个会话会从项目配置初始化，然后用 Pi custom entry 持久化会话覆盖和最近运行结果。
`/gate set`、`add`、`remove`、`timeout` 和 `clear` 只改变当前会话，`/gate reload` 重新加载项目
配置。命令顺序执行，首个失败即停止，输出脱敏并截断；通过结果记录当前工作区指纹。

## 持久化记忆

`/etc/hobot-code/agent/memory.json` 默认值：

```json
{
  "schemaVersion": 1,
  "enabled": true,
  "autoRecall": true,
  "maxInjected": 6,
  "maxSearchResults": 10,
  "maxContentChars": 4000,
  "defaultExpiresDays": null
}
```

`maxInjected` 是每轮自动召回的最大条数，`maxSearchResults` 是单次显式检索上限，
`defaultExpiresDays` 为 `null` 时不自动过期。修改后执行 `/memory reload`。开发测试可用
`HOBOT_CODE_MEMORY_CONFIG`、`HOBOT_CODE_MEMORY_DB` 和 `HOBOT_CODE_MEMORY_USER` 覆盖路径或本地用户键。

作用域为 `user`、`project`、`board`、`session`；类型为 `preference`、`decision`、`fact`、
`fix`、`instruction`、`note`。重复内容会刷新时间而不是新建副本。写入、检索、删除、
清空和过期操作都写审计事件，审计详情只保存内容哈希和作用域，不复制记忆正文。
