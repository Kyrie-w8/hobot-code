# Hobot Code 0.12.1 配置

## 路径

| 路径 | 作用 |
|---|---|
| `~/.config/hobot-code/hobot.env` | 当前用户的模型密钥和端点 |
| `~/.config/hobot-code/agent/settings.json` | Pi/Hobot Code 全局设置 |
| `~/.config/hobot-code/agent/models.json` | 自定义 Provider 和模型 |
| `~/.config/hobot-code/agent/auth.json` | `/login` 保存的认证信息 |
| `~/.config/hobot-code/agent/permissions.json` | allow/ask/deny 工具权限策略 |
| `~/.config/hobot-code/agent/memory.json` | 持久化记忆开关、检索与长度上限 |
| `~/.config/hobot-code/agent/goals.json` | 持久目标默认 turn/token 预算 |
| `~/.config/hobot-code/agent/hooks.json` | PreToolUse/PostToolUse Hook 和失败策略 |
| `~/.config/hobot-code/agent/notifications.json` | SSH OSC/bell 通知触发条件 |
| `~/.config/hobot-code/agent/lsp.json` | 语言服务器命令、文件匹配和资源上限 |
| `~/.local/state/hobot-code/sessions` | Pi JSONL 会话 |
| `~/.local/state/hobot-code/memory/memory.db` | SQLite/FTS5 持久化记忆与审计事件 |
| `~/.local/state/hobot-code/goals/goals.db` | 持久目标状态机与事件 |
| `~/.local/state/hobot-code/audit/hooks.jsonl` | 脱敏 Hook 执行审计 |
| `/usr/local/lib/hobot-code/bin` | 固定版本的 fd 和 ripgrep |
| `/usr/local/lib/hobot-code/extensions` | RDK 扩展 |
| `/usr/local/lib/hobot-code/skills` | 板端 Skills |
| `/usr/local/lib/hobot-code/knowledge` | 版本化 RDK 板卡知识与官方来源索引 |
| `/usr/local/lib/hobot-code/prompts/rdk-expert.md` | 动态渲染、带长度预算的紧凑 RDK 角色模板 |
| `<project>/.hobot/quality-gates.json` | 项目默认质量门命令与单命令超时 |

启动器遵循 `XDG_CONFIG_HOME` 与 `XDG_STATE_HOME`，并设置对应的 Agent、会话和状态路径。项目目录使用 `.hobot/`
放置局部 settings、extensions、skills、prompts 和 themes；Pi 的 project trust 机制会在
首次加载项目资源前询问。

## D-Robotics Kimi

`~/.config/hobot-code/hobot.env`：

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

`~/.config/hobot-code/agent/memory.json` 默认值：

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

## 侧边 Agent 并发

`/btw` 在每个主会话中最多打开一个侧边 Agent。板卡级并发上限默认为 2，可设置为 1 到 8：

```sh
HOBOT_CODE_MAX_SIDE_AGENTS=2 hobot
```

多个终端进程通过板卡本地的原子租约共同计数，进程异常退出留下的陈旧租约会被自动回收。

## 持久目标

```json
{
  "schemaVersion": 1,
  "enabled": true,
  "defaultTurnBudget": 50,
  "defaultTokenBudget": null
}
```

`defaultTokenBudget=null` 表示新目标默认只限制 turn；用户可在 `/goal create` 中同时指定两种预算。
每个工作区只允许一个 active/paused 目标。每次 Pi turn 累加 token 和执行时间；预算耗尽时
状态变为 paused，只能由用户 `/goal extend` 增加预算。

## Tool Hook

```json
{
  "schemaVersion": 1,
  "enabled": true,
  "failurePolicy": "block",
  "timeoutMs": 5000,
  "maxOutputChars": 4000,
  "allowProjectHooks": false,
  "hooks": [
    {
      "name": "company-guard",
      "event": "PreToolUse",
      "tool": "bash",
      "command": ["/usr/local/sbin/company-guard"],
      "failurePolicy": "block"
    }
  ]
}
```

Hook stdin 是 `{schemaVersion,event,toolName,toolCallId,cwd,input,result?}` JSON。成功时可不输出，或输出
`{"block":true,"reason":"..."}`；PostToolUse 还可输出 `appendText` 和 `isError`。`failurePolicy=block`
会阻止 Pre 调用或将 Post 结果标错；`warn` 只在 TUI 告警。Hook 不通过 Shell 解析命令数组。

## SSH 通知

`notifications.json` 支持 `osc9`、`osc777`、`both` 协议，可分别控制批准等待、完成和失败通知。
`allowLocal=false` 表示只在检测到 `SSH_CONNECTION` 时发送；`minDurationMs` 避免短任务频繁弹出。
RPC、print 和 JSON 模式不写 OSC 序列。

## 资源感知 LSP

`lsp.json` 用 `extensions` 匹配文件，`command` 是不经 Shell 解析的 argv 数组。`maxProcesses`、
`maxMemoryMiB`、`idleTimeoutMs`、`requestTimeoutMs` 分别约束进程数、单进程 RSS、空闲回收和
单请求时间。超过进程数时回收最久未使用实例；超过 RSS 时强制停止语言服务器。
未安装命令时 `lsp status` 报告 `installed=false`，不会自动下载或常驻进程。
