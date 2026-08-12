# Hobot Code 配置

Hobot Code 沿用 Pi 的配置机制，并使用独立的用户配置与状态目录。推荐优先通过 TUI 命令修改交互设置，只在需要精确控制或自动化部署时直接编辑 JSON。

## 配置入口

| 入口 | 用途 |
|---|---|
| `~/.config/hobot-code/hobot.env` | 模型端点、密钥和进程级覆盖 |
| `~/.config/hobot-code/agent` | Pi 设置、模型及 Hobot Code 功能配置 |
| `<project>/.hobot` | 受 Pi project trust 保护的项目配置与资源 |
| `~/.local/state/hobot-code` | 会话、记忆、目标与审计等可变状态 |

启动器遵循 `XDG_CONFIG_HOME` 和 `XDG_STATE_HOME`。完整文件清单、权限和迁移规则见[用户目录布局](user-directory-layout.md)。

安装器和启动器只在配置文件缺失时写入默认值，不覆盖已有用户设置。默认创建的用户配置文件权限为 `0600`；用户应持续保持该权限，并避免把配置目录暴露给其他账号。

## D-Robotics 模型

首次安装后推荐运行安全配置向导：

```bash
hobot setup
```

交互模式会从当前终端读取 API token，并在输入 token 时关闭回显。它只接受内置 D-Robotics 模型和 HTTPS 网关，使用同目录私有临时文件原子更新 `hobot.env`，不会在输出中显示 token。用于自动化时从标准输入传入 token，避免把凭据放进命令行参数或 Shell 历史：

```bash
printf '%s\n' "$DROBOTICS_TOKEN" | hobot setup --token-stdin --model kimi-k3
```

可增加 `--check` 在保存后执行一次最小模型路由检查。若 `agentd` 已在运行，向导不会停止任务或静默重启服务，而会提示先执行 `hobot daemon restart`；重启后新任务才会使用更新后的配置。

`hobot.env`、`agent/settings.json` 或 `agent/models.json` 在后台服务启动后发生变化时，模型查询、新任务和 Resume 会停止并直接给出重启命令，避免用户误以为新配置已生效。查看旧任务、审批和停止任务仍然可用。

编辑 `~/.config/hobot-code/hobot.env`：

```text
ANTHROPIC_BASE_URL=https://ai-api.d-robotics.cc
ANTHROPIC_AUTH_TOKEN=your-token
ANTHROPIC_MODEL=kimi-k3
API_TIMEOUT_MS=3000000
HOBOT_CODE_MODEL_CONTEXT_WINDOW=1000000
HOBOT_CODE_MODEL_MAX_TOKENS=8192
```

Hobot Code 内置 `drobotics/kimi-k3`、`drobotics/qwen3.8-max`、`drobotics/glm-5.2`、`drobotics/deepseek-v4-flash` 和 `drobotics/deepseek-v4-pro`，默认选择 Kimi K3，thinking 等级为 `max`。`ANTHROPIC_MODEL` 可覆盖默认选择，但不会移除其他内置模型。DeepSeek V4 使用同一网关的 OpenAI Chat Completions 路径；thinking off 映射为 `chat_template_kwargs.enable_thinking=false`。当前 D-Robotics DeepSeek V4 路由仅声明文本输入；不要向它附加图片。`API_TIMEOUT_MS` 是单次网关请求的硬超时，单位为毫秒，默认值为 3000000，并优先于 Pi 传入的 Provider 超时；数值会限制在 1000 到 3600000 之间，空值或非数值回退到默认值。Pi 的 Agent 请求超时和 HTTP 空闲超时也默认设为 3000000 ms。上下文窗口和最大输出来自上面的 Provider 环境变量，不由 `settings.json` 的 TUI 设置决定。

Kimi K3、Qwen 3.8 Max 和 GLM 5.2 使用 Hobot Code 的 Anthropic SSE 适配器，实时转发 thinking、文本、工具参数和 usage；端点明确不支持流式格式或返回普通 JSON 时，才回退到有字节上限的缓冲读取。DeepSeek V4 Flash 和 Pro 使用 Pi 的 OpenAI-compatible 流式实现，保留工具调用、中断、usage 和多轮历史语义。两条路径都受统一的超时、会话和缓存观测约束。

`hobot.env` 只按逐行 `KEY=VALUE` 数据解析；空行和以 `#` 开头的行会忽略，外层单引号或双引号会移除。变量替换、命令替换和其他 Shell 语法不会执行，危险的进程注入变量会被拒绝。决定配置文件自身位置的 `XDG_CONFIG_HOME`、`XDG_STATE_HOME` 和 `HOBOT_CODE_CONFIG_DIR` 也不能写在该文件中，必须在调用 `hobot` 前设置。

启动器只接受普通的可信凭据文件：路径不能是符号链接，文件必须属于当前用户，且不能向组或其他用户开放权限。不满足条件时启动会直接失败，而不是带着不可信环境继续运行。不要提交该文件，也不要把真实 token 写入会话、项目配置或 issue。

## 添加其他模型

Pi 支持 Anthropic Messages、OpenAI Chat Completions、OpenAI Responses 和 Google Generative AI 等 Provider。可使用 `/login <provider>` 配置 Pi 支持的登录型 Provider，或编辑 `~/.config/hobot-code/agent/models.json` 添加兼容服务。

本机 Ollama 示例：

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

API key 可以写成 `$ENV_NAME` 引用，避免把真实密钥放进 JSON。保存后打开 `/model` 重新选择模型即可。

## Pi 交互与扩展

推荐使用 `/settings`、`/model`、`/scoped-models` 和 `/hotkeys`。默认设置启用自动压缩、最多三次 Agent 级重试、可见 thinking 和 fullscreen TUI；后者用于提供稳定的分屏布局与按指针区域滚动。

扩展、Skills 和 Prompt 使用 Pi 原生命令管理：

```bash
hobot install npm:@scope/package@1.0.0
hobot install git:github.com/owner/repository@v1
hobot list
hobot config
hobot update --extensions
```

第三方扩展与 Skills 不是沙箱内容，它们拥有当前用户权限。安装前应审查来源和代码；root 会话尤其不应加载来源不明的 package。

基础 Hobot Code 运行时不依赖板端 Node.js。Pi package 若包含 npm 依赖，安装过程仍需要可用的 `npm`，或在 `settings.json` 中配置 `npmCommand`；Git 来源还需要 `git` 和相应网络或 SSH 凭据。

## Prompt 缓存观测

`/cache` 显示当前 Hobot Code 进程内 D-Robotics Provider 已完成请求的缓存统计；`/cache reset` 只清空本地观测，不清理模型网关缓存。统计直接使用网关返回的 `input`、`cacheRead` 和 `cacheWrite`，命中率定义为：

```text
cacheRead / (input + cacheRead + cacheWrite)
```

输出还包含系统 Prompt 与有序工具契约的 SHA-256 指纹，用来判断相邻请求是否更换模型或改变稳定前缀。这里只保存哈希，不记录 Prompt、工具说明、会话正文或凭据。部分兼容网关可能不返回缓存字段，此时 `0%` 只表示 Hobot Code 没有收到可计量的 cache-read token，不能据此证明上游未使用缓存。实机基线与适用边界见[缓存效率](cache-efficiency.md)。

## 路径与开发覆盖

所有路径覆盖必须使用绝对路径。启动器和 RDK 扩展都会拒绝相对值，不会按当前工作目录静默展开。前三项决定 `hobot.env` 的查找位置，只能在启动进程的外部环境中设置；其余项也可以写入 `hobot.env`：

| 环境变量 | 默认值或用途 |
|---|---|
| `XDG_CONFIG_HOME` | 默认 `$HOME/.config`；仅限启动前设置 |
| `XDG_STATE_HOME` | 默认 `$HOME/.local/state`；仅限启动前设置 |
| `HOBOT_CODE_CONFIG_DIR` | `${XDG_CONFIG_HOME:-$HOME/.config}/hobot-code`；仅限启动前设置 |
| `HOBOT_CODE_STATE_DIR` | `${XDG_STATE_HOME:-$HOME/.local/state}/hobot-code` |
| `HOBOT_CODE_AGENTD_SOCKET` | 当前用户 agentd 的绝对 Unix socket 路径 |
| `HOBOT_CODE_MAX_BACKGROUND_TASKS` | 同时活跃的后台 Agent 数，取值 `1..8`，默认 `2` |
| `HOBOT_CODE_MAX_RETAINED_TASKS` | 当前用户可保留的任务总数，取值 `10..1000`，默认 `100` |
| `HOBOT_CODE_MAX_EVENT_MIB` | 单个后台任务事件日志上限，取值 `1..64` MiB，默认 `16` |
| `HOBOT_CODING_AGENT_DIR` | `<config-root>/agent` |
| `HOBOT_CODING_AGENT_SESSION_DIR` | `<state-root>/sessions` |
| `HOBOT_CODE_PERMISSION_POLICY` | 权限策略文件 |
| `HOBOT_CODE_MEMORY_CONFIG`、`HOBOT_CODE_MEMORY_DB` | 记忆配置与数据库 |
| `HOBOT_CODE_GOAL_CONFIG`、`HOBOT_CODE_GOAL_DB` | 目标配置与数据库 |
| `HOBOT_CODE_HOOK_CONFIG`、`HOBOT_CODE_HOOK_AUDIT` | Hook 配置与审计 |
| `HOBOT_CODE_NOTIFICATION_CONFIG` | 通知配置 |
| `HOBOT_CODE_LSP_CONFIG` | LSP 配置 |
| `HOBOT_CODE_RDK_KNOWLEDGE_DIR`、`HOBOT_CODE_RDK_EXPERT_PROMPT` | 版本化知识目录与专家 Prompt 文件 |

例如，在调用 `hobot` 前为一次测试使用隔离目录：

```bash
HOBOT_CODE_CONFIG_DIR=/tmp/hobot-config \
HOBOT_CODE_STATE_DIR=/tmp/hobot-state \
hobot
```

`PI_SKIP_VERSION_CHECK=1` 默认开启，避免 Pi 的自更新提示绕过 Hobot Code 的版本锁。升级 Pi 运行时必须更新 `pi-runtime/pi.lock`、重新构建并完成板端回归。

知识库与专家 Prompt 可在开发时覆盖：

```bash
HOBOT_CODE_RDK_KNOWLEDGE_DIR=/path/to/knowledge \
HOBOT_CODE_RDK_EXPERT_PROMPT=/path/to/rdk-expert.md \
hobot
```

生产环境应使用安装包内的版本化知识目录。每篇知识文档必须在正文中写明与 manifest 一致的核对日期，并在“官方来源”章节引用至少两个 D-Robotics 官方文档或官方 GitHub 链接。知识更新需要同步修改 `knowledge/manifest.json` 的 `knowledgeVersion` 和 `updatedAt`，再运行 `make check`；校验器也会拒绝未登记文档、遗漏来源和疑似凭据。

## 构建覆盖

`make release` 默认把下载的 Pi 归档缓存在 `dist/pi-cache`。无法稳定访问 GitHub Releases 时，可复用已下载归档，并提供已解压的 `fd`、`rg` 及许可证：

```bash
HOBOT_CODE_PI_CACHE_DIR=/path/to/pi-cache \
HOBOT_CODE_TOOL_BUNDLE_DIR=/path/to/tool-bundle \
make release
```

构建脚本仍会依据 `pi-runtime/pi.lock` 和 `pi-runtime/tools.lock` 校验版本、文件与 SHA256；缓存不会绕过完整性检查。

正式发行默认拒绝脏工作区，确保归档可追溯到确定提交。本地验证尚未提交的改动时可以显式构建开发包：

```bash
HOBOT_CODE_ALLOW_DIRTY_BUILD=1 make release
```

开发包会在发行元数据中标记为 dirty，不应作为正式产物分发。发行目录中的 `BUILD_INFO.json` 记录提交、构建时间和锁定组件，`MANIFEST.sha256` 覆盖包内文件；归档旁还会生成同名 `.sha256` 文件，传输到板卡后应在解压前校验。

受控构建可以在调用前设置非负整数 `SOURCE_DATE_EPOCH`，控制 `BUILD_INFO.json` 的构建时间，并统一包内文件与目录的时间戳。它只是可复现构建的一部分；归档排序、权限规范化和确定性的 gzip 输出同样由构建流程负责。

## 工具权限

`~/.config/hobot-code/agent/permissions.json` 按数组顺序匹配，第一条命中规则生效；未命中时使用 `default`。`mcp:*` 匹配所有 MCP 来源工具，普通 `*` 可用于工具名通配。

```json
{
  "schemaVersion": 2,
  "rootMode": "confirm",
  "default": "ask",
  "rules": [
    { "tool": "read", "action": "allow" },
    { "tool": "bash", "action": "ask" },
    { "tool": "mcp:*", "action": "deny" }
  ]
}
```

`/permissions set <pattern> <action>` 将规则放到数组开头并原子写回。配置缺失或无效时使用内置保守默认值并显示警告。`deny` 工具从活跃工具集合移除，调用时仍会复核；旧版 schema 1 中可能修改系统的 `allow` 规则会降级为 `ask`。

`/permissions preset developer` 可一次启用日常开发权限：允许 `read`、`ls`、`find`、`grep`、`write`、`edit`、`bash`、板卡诊断、知识检索、只读记忆、目标进度和 LSP。Developer 使用风险审批，即使会话以 root 运行，帮助查询、状态检查、构建和工作区内编辑等普通操作也不会反复确认；质量门执行、持久记忆写入、目标完成、MCP 和未知工具仍然确认。该操作原子替换当前规则，`/permissions status` 会分别展示各已注册工具的有效权限和原始规则；原始规则按顺序匹配，较后的条目可能已被通配规则遮蔽。

root 会话默认使用 `rootMode: "confirm"`，对 `bash`、`write`、`edit` 逐次审批。Developer 预设或 `/permissions root policy` 会改用策略判定，但不会关闭硬安全边界；`/permissions root confirm` 可恢复严格模式。完全相同且非危险的确认调用可以在当前任务内记住，危险 Shell 每次都必须审批。

硬安全边界高于用户规则：

- 内置 `write`、`edit` 禁止修改 `/boot`、`/dev`、`/etc`、`/proc`、`/sys`、`/usr` 和 `/var/lib`。
- 内置工具写入工作区外，以及 Shell 命中破坏性规则时，需要交互确认。
- Developer 下的普通读取、构建、测试和工作区内编辑按 allow 规则直接执行；Ask 和 root strict 模式仍逐次确认变更工具。
- 工作区外写入，以及文件删除、受保护路径修改、服务与软件包变更、设备/固件写入、重启、结束进程等高风险 Shell 命令仍需确认，关键系统目录的内置 `write`/`edit` 会被阻止。
- 确认详情会尽力隐藏 token、Bearer Token 和常见 secret 字段。

默认策略允许 `memory_search`，将 `memory_save` 设为 `ask`。这意味着默认每次由模型发起的记忆写入都要确认，但用户可以修改该规则；直接执行 `/memory add` 本身就是明确的用户操作。

## 质量门

项目质量门位于 `<project>/.hobot/quality-gates.json`：

```json
{
  "schemaVersion": 1,
  "timeoutMs": 120000,
  "commands": ["make check"]
}
```

`/init` 可以在缺失时创建该文件和 `AGENTS.md`。每个会话从项目配置初始化，`/gate set`、`add`、`remove`、`timeout` 与 `clear` 只修改当前会话覆盖；`/gate reload` 重新加载项目文件。

命令依次执行，首个失败即停止，输出会脱敏并截断。通过结果绑定运行后的工作区指纹；之后的修改会将其标记为 `stale`。

## 持久记忆

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

`maxInjected` 是每轮自动召回上限，`maxSearchResults` 是显式检索上限，`defaultExpiresDays=null` 表示默认不自动过期。修改后执行 `/memory reload`。

记忆按 `user`、`project`、`board`、`session` 隔离，可使用 `preference`、`decision`、`fact`、`fix`、`instruction`、`note` 类型。重复内容刷新时间而不新增副本。审计只保存内容哈希和作用域，不复制记忆正文；疑似密钥、私钥和银行卡号会在存储层被拒绝。

开发测试还可使用 `HOBOT_CODE_MEMORY_USER` 覆盖本地用户键。记忆是可能过期的辅助上下文，不能覆盖当前用户指令或实时板卡证据。

## 侧边 Agent 并发

每个主会话最多打开一个 `/btw` 侧边 Agent。同一 OS 用户的全部 Hobot Code 进程默认合计最多运行两个：

```bash
HOBOT_CODE_MAX_SIDE_AGENTS=2 hobot
```

有效范围为 1 到 8。租约存放在按 UID 隔离的本地临时目录中，因此这是同一用户的并发限制，不是跨用户的整板全局限制；陈旧租约会自动回收。上下文继承、禁止能力和副作用边界见 README 的[侧边 Agent](../README.md#侧边-agent)章节。

全屏模式下，`/btw` 将主 Agent 与侧边 Agent 等分显示，打开时不抢占主输入焦点。点击任一半屏即可切换到对应 Agent；也可使用 `Ctrl+Shift+Right` 切换到侧边 Agent，使用 `Ctrl+Shift+Left` 返回主 Agent。点击事件仍交给 Pi 的选择层处理，因此拖动选取、链接和滚轮不会被焦点切换功能吞掉。

## SSH 断线续跑

无界面任务可直接交给 `agentd`，无需安装 `tmux`：

```bash
hobot task start [--name NAME] [--cwd DIR] [--model PROVIDER/MODEL] \
  [--permissions review|ask|developer] [--trust-project] -- PROMPT
hobot task list
hobot task show TASK_ID
hobot task logs TASK_ID [--after SEQUENCE] [--follow]
hobot task attach TASK_ID [--after SEQUENCE]
hobot task send TASK_ID PROMPT
hobot task abort TASK_ID
hobot task respond TASK_ID REQUEST_ID yes|no|cancel|VALUE
hobot task approvals TASK_ID
hobot task resume TASK_ID [PROMPT]
hobot task rename TASK_ID NAME
hobot task archive TASK_ID
hobot task unarchive TASK_ID
hobot task list --all
hobot task delete TASK_ID --yes
hobot task stop TASK_ID
```

`hobot task` 会在需要时自动启动当前用户的 daemon。默认最多两个活跃任务，任务空闲时 worker 仍然存活并可继续多轮对话。`attach` 会先重放已持久化事件再跟随实时输出；失去 SSH 连接不会终止任务。daemon 或板卡重启后，活动任务标记为 `interrupted`；`resume` 从已校验的 Pi session 续接对话，但不重放中断的 Prompt、审批或工具调用。归档任务从普通 `list` 中隐藏，但可用 `list --all` 查看；只有已停止且已归档的任务才能显式删除。完整接口见 [agentd 协议](agentd-protocol.md)。

桌面客户端应在 SSH 连接上运行 `hobot bridge --stdio`。控制请求和长时订阅各使用一个 bridge 进程；每行是一个 agentd JSON 请求。桥接只转发到当前用户的 Unix socket，不替代板端权限判定。

需要保留完整 TUI、编辑区和侧边 Agent 时，继续使用 `hobot persistent`：

`hobot persistent` 使用当前 OS 用户的 `tmux` 服务托管完整交互进程：

```bash
hobot persistent
hobot persistent start [name] [-- hobot-options...]
hobot persistent attach [name]
hobot persistent list
hobot persistent stop [name]
```

省略动作时等价于 `hobot persistent start main`。名称默认为 `main`，只允许 1 到 48 个字母、数字、下划线或连字符，且必须以字母或数字开头。实际 `tmux` 会话带有 `hobot-code-` 前缀，并运行在当前 OS 用户的 `hobot-code` 专用 socket 上；`list` 和 `stop` 无法看到或操作普通 `tmux` 服务。随包配置只作用于该专用服务，启用鼠标、扩展按键、焦点事件和 `tmux-256color`。若已经位于其他 `tmux` 客户端中，需先分离，避免跨服务嵌套。Hobot 参数必须放在 `--` 后，例如 `hobot persistent start review -- --resume`。

该模式需要系统安装 `tmux`。它保证 SSH 断开后进程继续运行，但不提供跨板卡重启或程序崩溃恢复；后者只能从已持久化的 Pi 会话重新开始。

## 持久目标

`~/.config/hobot-code/agent/goals.json` 默认值：

```json
{
  "schemaVersion": 1,
  "enabled": true,
  "defaultTurnBudget": 50,
  "defaultTokenBudget": null
}
```

`defaultTokenBudget=null` 表示新目标默认只限制 turn。每个工作区只允许一个 active 或 paused 目标；预算耗尽后状态变为 paused，只能由用户通过 `/goal extend` 增加预算。模型完成目标时仍需满足当前质量门。

## 工具 Hook

`~/.config/hobot-code/agent/hooks.json` 示例：

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

Hook 命令是未经 Shell 解析的 argv 数组。stdin 为 `{schemaVersion,event,toolName,toolCallId,cwd,input,result?}` JSON；成功时可不输出，或输出 `{"block":true,"reason":"..."}`。PostToolUse 还可返回 `appendText` 与 `isError`。

`failurePolicy=block` 会阻止 Pre 调用或把 Post 结果标记为错误，`warn` 只在 TUI 告警。项目 `.hobot/hooks.json` 默认不执行，必须由全局配置显式设置 `allowProjectHooks=true`。

## SSH 通知

`~/.config/hobot-code/agent/notifications.json` 支持 `osc9`、`osc777` 和 `both`，可分别控制批准等待、完成与失败通知。`minDurationMs` 用于抑制短任务通知。

通知只在交互 TUI 中尝试发送，并要求 `stderr` 是 TTY。`allowLocal=false` 时还必须检测到 `SSH_CONNECTION`；print、JSON 和 RPC 模式不会写入 OSC 通知序列。使用 `/notifications test` 验证当前终端是否支持，或用 `/notifications off` 关闭。

## 资源感知 LSP

`~/.config/hobot-code/agent/lsp.json` 使用 `extensions` 匹配文件，`command` 是未经 Shell 解析的 argv 数组。`maxProcesses`、`maxMemoryMiB`、`idleTimeoutMs` 和 `requestTimeoutMs` 分别约束进程数、单进程 RSS、空闲回收与单次请求时间。

语言服务器只在实际请求且命令存在时启动。超过进程数时回收最久未使用实例，超过 RSS 时停止对应服务；未安装命令时 `lsp status` 显示 `installed=false`，不会自动下载。基础发行包不捆绑语言服务器。
