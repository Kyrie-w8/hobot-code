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

`hobot.env`、`agent/settings.json`、`agent/models.json` 或 `agent/providers.json` 在后台服务启动后发生变化时，模型查询、新任务和 Resume 会停止并直接给出重启命令，避免用户误以为新配置已生效。查看旧任务、审批和停止任务仍然可用。

编辑 `~/.config/hobot-code/hobot.env`：

```text
ANTHROPIC_BASE_URL=https://ai-api.d-robotics.cc
ANTHROPIC_AUTH_TOKEN=your-token
ANTHROPIC_MODEL=kimi-k3
API_TIMEOUT_MS=3000000
HOBOT_CODE_MODEL_CONTEXT_WINDOW=1000000
HOBOT_CODE_MODEL_MAX_TOKENS=8192
```

Hobot Code 内置 `drobotics/kimi-k3`、`drobotics/qwen3.8-max`、`drobotics/glm-5.2`、`drobotics/deepseek/deepseek-v4-flash` 和 `drobotics/deepseek-v4-pro`，默认选择 Kimi K3，thinking 等级为 `max`。`ANTHROPIC_MODEL` 可覆盖默认选择，但不会移除其他内置模型。DeepSeek V4 使用同一网关的 OpenAI Chat Completions 路径；thinking off 映射为 `chat_template_kwargs.enable_thinking=false`。当前 D-Robotics DeepSeek V4 路由仅声明文本输入；不要向它附加图片。`API_TIMEOUT_MS` 是单次网关请求的硬超时，单位为毫秒，默认值为 3000000，并优先于 Pi 传入的 Provider 超时；数值会限制在 1000 到 3600000 之间，空值或非数值回退到默认值。Pi 的 Agent 请求超时和 HTTP 空闲超时也默认设为 3000000 ms。上下文窗口和最大输出来自上面的 Provider 环境变量，不由 `settings.json` 的 TUI 设置决定。

Kimi K3、Qwen 3.8 Max 和 GLM 5.2 使用 Hobot Code 的 Anthropic SSE 适配器，实时转发 thinking、文本、工具参数和 usage；端点明确不支持流式格式或返回普通 JSON 时，才回退到有字节上限的缓冲读取。DeepSeek V4 Flash 和 Pro 使用 Pi 的 OpenAI-compatible 流式实现，保留工具调用、中断、usage 和多轮历史语义。两条路径都受统一的超时、会话和缓存观测约束。

`hobot.env` 只按逐行 `KEY=VALUE` 数据解析；空行和以 `#` 开头的行会忽略，外层单引号或双引号会移除。变量替换、命令替换和其他 Shell 语法不会执行，危险的进程注入变量会被拒绝。决定配置文件自身位置的 `XDG_CONFIG_HOME`、`XDG_STATE_HOME` 和 `HOBOT_CODE_CONFIG_DIR` 也不能写在该文件中，必须在调用 `hobot` 前设置。

启动器只接受普通的可信凭据文件：路径不能是符号链接，文件必须属于当前用户，且不能向组或其他用户开放权限。不满足条件时启动会直接失败，而不是带着不可信环境继续运行。不要提交该文件，也不要把真实 token 写入会话、项目配置或 issue。

## 添加其他模型

Hobot Code 把第三方模型分为三条路径，避免把登录、元数据和密钥混在一个文件里：

| 场景 | 推荐入口 | 凭据边界 | Studio |
|---|---|---|---|
| API Key 兼容网关 | `hobot provider` | Hobot Code 受管 | 显示并可切换 |
| Pi 原生 OAuth/登录 | `/login <provider>` | Pi 原生 | 默认不自动展开 |
| 本机、无密钥或高级自管 Provider | `agent/models.json` | Pi/Provider 自行负责 | 默认不自动展开 |

受管 Provider 支持 `anthropic-messages`、`openai-completions`、`openai-responses` 和 `google-generative-ai`。常见的单模型 API Key 服务可直接执行：

```bash
hobot provider add acme \
  --base-url https://models.example.com/v1 \
  --api openai-completions \
  --model coder-v2 \
  --model-name "Acme Coder" \
  --context-window 65536 \
  --max-tokens 4096 \
  --reasoning --image
```

命令会从控制终端隐藏读取 API key；自动化场景使用 `--token-stdin`，不要把密钥作为命令参数。查看和删除配置：

```bash
hobot provider list
hobot provider rotate acme
hobot provider remove acme --yes
hobot daemon restart
hobot model runtime-probe acme/coder-v2
```

`list` 只返回 Provider、协议、模型、`ready`/`missing` 和共享使用数量，不返回密钥、密钥变量名或私有端点。`rotate` 只原子替换私有凭据，Provider 元数据和模型配置保持不变；如果高级配置让多个 Provider 共用同一个密钥，必须显式加 `--yes-shared` 确认影响全部使用者。删除默认清理已经没有其他 Provider 引用的密钥；`--keep-credential` 可显式保留。添加、轮换与删除使用跨进程锁、私有 `0600` 文件和耐久原子重命名。异常中断最多留下未引用密钥，不会发布一个缺少凭据的 Provider。

Studio 标题栏的钥匙入口提供同一套新增、轮换、删除和应用操作。密钥只在非受控密码框中短暂停留，并通过固定短时 SSH 命令的标准输入传到板端；不会保存到 Mac 配置、浏览器存储、任务历史或长期 bridge。正在运行的 Agent 会阻止安全重启，此时配置已经保存，Studio 会保留“Apply”提示，待任务空闲后再应用。

向导当前覆盖一个 Provider 的单模型常用字段。多模型、`thinkingLevelMap` 或 `compat` 等高级配置仍可直接编辑严格 schema。元数据写入私有的 `~/.config/hobot-code/agent/providers.json`，真实密钥只写入同样私有的 `~/.config/hobot-code/hobot.env`。等价的高级配置示例：

```json
{
  "schemaVersion": 1,
  "providers": [
    {
      "id": "acme",
      "name": "Acme Gateway",
      "baseUrl": "https://models.example.com/v1",
      "api": "openai-completions",
      "credentialEnv": "HOBOT_CODE_PROVIDER_KEY_ACME",
      "models": [
        {
          "id": "coder-v2",
          "name": "Acme Coder",
          "reasoning": true,
          "input": ["text", "image"],
          "contextWindow": 65536,
          "maxTokens": 4096
        }
      ]
    }
  ]
}
```

对应的 `hobot.env` 项为：

```text
HOBOT_CODE_PROVIDER_KEY_ACME=your-token
```

修改后必须执行 `hobot daemon restart`。未配置密钥的 Provider 会被单独跳过，不影响 D-Robotics 或其他 Provider；`hobot provider list`、`hobot extensions` 和 `/doctor` 只显示脱敏状态。配置拒绝未知字段、重复 JSON key、明文 `apiKey`/任意 Header、带凭据或查询参数的 URL，以及非本机 HTTP。`authHeader` 仅在兼容代理明确要求时设置；不同协议沿用 Pi 对应适配器的原生认证方式。

受管密钥与 D-Robotics token 一起在启动时从普通环境移除，通过匿名文件描述符传给非沙箱进程，通过沙箱 tmpfs 内的一次性私有文件传给隔离进程，并由 Side Agent 继承同一份有界凭据包。`providers.json` 不含密钥；运行时探测只复制当前所选 Provider 的已校验元数据。该机制不构成同进程第三方扩展或主机管理员之间的安全边界。

Pi 原生登录继续使用 `/login <provider>`。只有需要本地服务、无密钥服务或 Pi 高级字段时，才编辑 `models.json`。本机 Ollama 示例：

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

自管 `models.json` 的 API key 可以写成 `$ENV_NAME` 引用，避免把真实密钥放进 JSON；它不自动获得上述 Hobot Code 受管凭据隔离。保存后打开 `/model` 重新选择模型即可。

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

`hobot extensions` 显示发行包内置能力，以及当前用户的 Provider、Hook、LSP、Pi extensions、Skills、Prompt templates、themes 和 package 声明；`hobot extensions --json` 供自动化读取。`hobot extensions --task TASK_ID` 增加该任务的项目上下文。Studio 标题栏的 Capabilities 入口会自动使用当前任务，并提供搜索、类型筛选、板型适配、来源、信任、依赖和声明权限检查。清单每次读取都会刷新，不要求重启 agentd，但它只用于发现和兼容性判断，不执行扩展、不改变启用状态，也不授予权限。

全局私有配置只有在属于当前用户、不是符号链接、仅当前用户可读写且大小受限时才会进入清单。资源目录和文件还必须不可由组或其他用户写入；扫描限制深度和数量，不跟随符号链接。项目资源不能用路径查询，只能绑定 agentd 已持久化且创建时批准项目资源的任务；未信任任务显示 `Not trusted` 并保持不读取。返回内容不会包含 Provider 地址或 token、Hook 命令、LSP 参数、设置中的绝对路径或 package URL。某个来源无效时只跳过该来源并显示诊断，不影响内置能力。

第三方 package 的安装、更新和加载继续由 Pi 负责。清单中的 `declared` 表示配置声明，`discovered` 表示在 Pi 固定目录找到候选文件，都不证明运行时已经加载。Pi 的 MCP 通常由 extension/package 提供；没有独立、可验证的运行时注册信息时，Hobot Code 不根据文件名猜测一个 MCP 条目。所有实际工具调用仍由板端策略判定。

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
| `HOBOT_CODE_EXTENSION_CATALOG` | 发行包内置扩展清单的绝对路径；仅用于受管安装和开发测试 |
| `HOBOT_CODE_MAX_BACKGROUND_TASKS` | 同时活跃的后台 Agent 数，取值 `1..8`，默认 `2` |
| `HOBOT_CODE_MAX_RETAINED_TASKS` | 当前用户可保留的任务总数，取值 `10..1000`，默认 `100` |
| `HOBOT_CODE_MAX_EVENT_MIB` | 单个后台任务滚动事件历史上限，取值 `1..64` MiB，默认 `16`；保留最新连续事件并继续持久化 |
| `HOBOT_CODING_AGENT_DIR` | `<config-root>/agent` |
| `HOBOT_CODING_AGENT_SESSION_DIR` | `<state-root>/sessions` |
| `HOBOT_CODE_BWRAP` | 自动发现 `/usr/bin/bwrap` 或 `/bin/bwrap`；仅在自定义安装路径时设置绝对路径 |
| `HOBOT_CODE_TUI_SANDBOX` | 前台 TUI 默认 OS 隔离档位：`system`（默认）、`workspace`、`review` 或 `off` |
| `HOBOT_CODE_TUI_NETWORK` | 前台 TUI 默认网络边界：`shared`（默认）、`model-only` 或 `offline`；后两者要求 Bubblewrap |
| `HOBOT_CODE_PERMISSION_POLICY` | 权限策略文件 |
| `HOBOT_CODE_MEMORY_CONFIG`、`HOBOT_CODE_MEMORY_DB` | 记忆配置与数据库 |
| `HOBOT_CODE_GOAL_CONFIG`、`HOBOT_CODE_GOAL_DB` | 目标配置与数据库 |
| `HOBOT_CODE_HOOK_CONFIG`、`HOBOT_CODE_HOOK_AUDIT` | Hook 配置与审计 |
| `HOBOT_CODE_NOTIFICATION_CONFIG` | 通知配置 |
| `HOBOT_CODE_LSP_CONFIG` | LSP 配置 |
| `HOBOT_CODE_MANAGED_PROVIDER_CONFIG` | 受管 Provider 元数据配置；默认 `<agent-dir>/providers.json` |
| `HOBOT_CODE_RDK_KNOWLEDGE_DIR`、`HOBOT_CODE_RDK_EXPERT_PROMPT` | 版本化知识目录与专家 Prompt 文件 |

例如，在调用 `hobot` 前为一次测试使用隔离目录：

```bash
HOBOT_CODE_CONFIG_DIR=/tmp/hobot-config \
HOBOT_CODE_STATE_DIR=/tmp/hobot-state \
hobot
```

`PI_SKIP_VERSION_CHECK=1` 默认开启，避免 Pi 的自更新提示绕过 Hobot Code 的版本锁。升级 Pi 运行时必须更新 `pi-runtime/pi.lock`、重新构建并完成板端回归。

前台直接运行 `hobot` 与 `hobot persistent` 时默认使用 `system` 档位：宿主根文件系统只读，当前工作目录和 Hobot Code 自身的会话、配置、记忆与租约可写，只映射已识别的 RDK BPU、多媒体和加速设备，并丢弃 Linux capabilities。可为单次会话显式选择其他档位：

```bash
hobot tui --sandbox review
hobot tui --sandbox workspace
hobot tui --sandbox system
hobot tui --sandbox off
hobot tui --sandbox workspace --network model-only
hobot tui --sandbox review --network offline
```

`review` 保持当前目录只读；`workspace` 允许当前目录写入但不开放 RDK 硬件设备；`system` 在同一文件边界上开放受支持设备白名单；`off` 关闭 OS 隔离，必须由用户明确选择。`workspace` 与 `system` 拒绝把 `/` 或受保护系统目录当作工作区；从 `$HOME` 启动仍可工作，但 SSH/GPG、常见云凭据、Git 凭据、Hobot 配置和 daemon 状态会重新锁为只读。建议进入具体项目目录，以缩小 Agent 的可写范围。四档都继续使用 Pi 参数，例如 `hobot tui --sandbox system -- --resume`。网络默认为 `shared`；`model-only` 使用独立网络命名空间并仅挂载 agentd 的私有模型 Socket；`offline` 连该 Socket 也会隐藏，只适合本地模型。受限网络不能与 `--sandbox off` 组合。

Hobot Code 会把内置 D-Robotics provider 的 `ANTHROPIC_AUTH_TOKEN` 和所有 `HOBOT_CODE_PROVIDER_KEY_*` 受管密钥从长期进程及工具子进程环境中移除。`shared` 使用匿名文件描述符和沙箱 tmpfs 一次性文件把所选凭据交给 Provider；`model-only` 不把任何模型密钥交给 worker，而由 agentd 按启动时冻结的 Provider/模型白名单固定 HTTPS 目标、协议路径、认证头、方法、请求/响应上限和禁止重定向后代发。当前支持 D-Robotics、Anthropic Messages、OpenAI Chat Completions 和 OpenAI Responses；Google Generative AI、Pi 登录和自管 `models.json` 必须使用 `shared`。后台 Pi worker 从全局配置生成任务私有、可写的 `settings.json` 与 `models.json` 快照，以支持 Pi 的锁文件机制而不开放全局配置写入；只有 `shared` 模式会复制 Pi 登录所需的 `auth.json`，受限网络任务会主动移除它。Provider 或密钥配置变化后运行 `hobot daemon restart`，否则配置指纹检查会拒绝模型相关操作。托管沙箱内的宿主 `hobot.env` 会被遮蔽。`/doctor` 可查看脱敏状态。这不是同用户宿主进程或 root 管理员之间的边界；能访问模型 Socket 的同用户进程仍可向已批准的模型网关发送数据并消耗额度。

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

`/permissions preset developer` 可一次启用日常开发权限：允许 `read`、`ls`、`find`、`grep`、`write`、`edit`、`bash`、板卡诊断、知识检索、只读记忆、目标进度和 LSP。Developer 使用风险审批，即使会话以 root 运行，帮助查询、状态检查、构建和工作区内编辑等普通操作也不会反复确认；质量门执行、持久记忆写入、目标完成、MCP、未知工具和识别出的外联命令仍然确认。该操作原子替换当前规则，`/permissions status` 会分别展示各已注册工具的有效权限和原始规则；原始规则按顺序匹配，较后的条目可能已被通配规则遮蔽。

`shared` 模式用虚拟 `network` 权限识别 `curl`、`wget`、SSH/SCP、远程 Git、软件包客户端、容器仓库和常见网络诊断命令；默认与 Developer 预设均为 `ask`。可用 `/permissions set network allow|ask|deny` 调整。`allow` 只跳过这层外联提示，下载并执行、系统修改等独立高风险规则仍会审批；该检测是启发式策略，自定义程序可能绕过。对受支持模型，`model-only` 改用内核网络命名空间和固定 Unix Socket 模型代理，工具无法访问通用网络；这比命令识别更强，但不是“模型看不到项目数据”，因为 Agent 上下文本来就会发送给所选模型。`offline` 切断全部网络。不支持注入自定义传输的协议不会被伪装成受保护状态。

root 会话默认使用 `rootMode: "confirm"`，对 `bash`、`write`、`edit` 逐次审批。Developer 预设或 `/permissions root policy` 会改用策略判定，但不会关闭硬安全边界；`/permissions root confirm` 可恢复严格模式。普通审批只提供本次允许；只有能够用清晰安全边界表达的权限才提供任务级选项，例如「当前任务允许网络」和「当前任务信任该构建机」。新审批不再创建难以区分的「记住完全相同调用」规则；旧任务中已有的精确调用规则仍可读取，仅用于兼容 Resume。

任务级「允许网络」只会将虚拟 `network` 规则设为 `allow`，不会连带授予文件写入、root、硬件访问或破坏性命令权限。「信任该构建机」仅在 `openexplorer_build_host` 探测成功后生效，只为当前任务和当前 SSH 目标免除重复的主机网络确认；`openexplorer_remote_run` 的具体远程命令仍会独立审批。

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

全屏模式下，`/btw` 将主 Agent 与侧边 Agent 等分显示，打开时不抢占主输入焦点。点击任一半屏即可切换到对应 Agent；也可使用 `Ctrl+Shift+Right` 切换到侧边 Agent，使用 `Ctrl+Shift+Left` 返回主 Agent。工具审批、选择和输入弹窗始终优先：弹窗持有焦点时，鼠标、切换快捷键和关闭侧边 Agent 都不会把焦点抢回输入框。其他点击事件仍交给 Pi 的选择层处理，因此拖动选取、链接和滚轮不会被焦点切换功能吞掉。

主 Agent 处于运行或等待审批状态时，侧边 Agent 仍从最近的完整会话节点分叉，以避免产生缺失 `toolResult` 的非法模型历史；同时首轮会额外获得一份只读、脱敏、有长度上限的当前状态快照，包含当前用户请求、主 Agent 已可见文本、工具目标和完成/进行状态。图片数据、思考链和不完整的原始工具消息不会复制进侧边会话。这是创建时快照，不会在两个 Agent 之间实时合并后续消息。

## SSH 断线续跑

无界面任务可直接交给 `agentd`，无需安装 `tmux`：

```bash
hobot task start [--name NAME] [--cwd DIR] [--workspace shared|worktree] [--model PROVIDER/MODEL] \
  [--permissions review|ask|developer] [--sandbox review|workspace|system|off] [--network shared|model-only|offline] [--trust-project] -- PROMPT
hobot task list
hobot task show TASK_ID [--details]
hobot task logs TASK_ID [--after SEQUENCE] [--follow]
hobot task attach TASK_ID [--after SEQUENCE | --replay-all]
hobot task send TASK_ID PROMPT
hobot task abort TASK_ID
hobot task respond TASK_ID REQUEST_ID yes|no|cancel|VALUE
hobot task approvals TASK_ID [--details]
hobot workspace inspect [DIR]
hobot workspace list
hobot workspace writes
hobot workspace delivery TASK_ID
hobot workspace apply TASK_ID --yes
hobot workspace cleanup TASK_ID --yes
hobot task resume TASK_ID [PROMPT]
hobot task rename TASK_ID NAME
hobot task archive TASK_ID
hobot task unarchive TASK_ID
hobot task list --all
hobot task delete TASK_ID --yes
hobot task stop TASK_ID
```

后台任务默认使用 OS 隔离：只读权限对应 `review`，普通开发对应 `workspace`，模型部署对应 `system`。`system` 只额外开放 BPU、ION/Hbmem、DMA heap、video 和 media 设备，而不是整个 `/dev`；它仍限制宿主文件写入并丢弃 Linux capabilities。CLI 为兼容现有工作流默认 `shared`；D-Robotics 及受支持的 Hobot 受管 Anthropic/OpenAI 任务可选更安全的 `--network model-only`，本地模型任务可选 `offline`。需要完全无隔离的系统维护必须显式使用 `--sandbox off`。

`hobot task` 会在需要时自动启动当前用户的 daemon。默认最多两个活跃任务，任务空闲时 worker 仍然存活并可继续多轮对话。首次 `attach` 会显示仍保留的持久事件，之后按已成功显示的事件断点继续跟随；断点每两秒和退出时写入当前用户私有状态，`--replay-all` 会从当前滚动保留窗口的起点回放。长任务达到事件空间上限后会原子滚动旧历史并继续持久化，断点早于保留窗口时会显示实际重放起点。失去 SSH 连接不会终止任务。daemon 或板卡重启后，活动任务标记为 `interrupted`；`resume` 从已校验的 Pi session 续接对话，但不重放中断的 Prompt、审批或工具调用。`show` 与 `approvals` 默认省略 session、审批正文和部署绝对路径，只有显式 `--details` 才返回完整本地记录。归档任务从普通 `list` 中隐藏，但可用 `list --all` 查看；只有已停止且已归档的任务才能显式删除。完整接口见 [agentd 协议](agentd-protocol.md)。

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

新安装默认关闭 LSP。需要时安装对应语言服务器后，将 `enabled` 改为 `true`；Studio 会把未安装的可选服务器折叠显示，不再将其呈现为产品故障。

## OpenExplorer LLM 板端运行时

S600 上可在 `~/.config/hobot-code/hobot.env` 设置 `HOBOT_CODE_OPENEXPLORER_LLM_ROOT`，指向同时包含 `oellm_runtime/lib`、`oellm_runtime/include` 和 `oellm_runtime/examples` 的软件包根目录。Hobot Code 只做有界只读检查：目录所有权、写权限、运行时版本、AArch64 ELF 架构和关键 sample 是否齐全。检查通过后才会在 Capabilities 中显示 `OpenExplorer LLM runtime`；主机侧 x86_64/CUDA 量化编译组件不会被声明为板端能力。

如果同一外部包还包含 `.skillshare/skills/` 和 `docs/03_SKILLS_CATALOG.md`，Hobot Code 会额外执行所有权、符号链接、数量、大小、frontmatter、目录名和客户目录一致性检查。只有客户目录登记的 Skill 会写入每个后台任务的私有 Pi settings；包内存在但目录未登记的 Skill 只显示在 Capabilities，默认不加载。Skill 文件始终从外部包只读使用，不会复制进 Hobot Code 安装包。

当前 OpenExplorer LLM 2.0.4 交付包的实测库存是 24 个目录，而客户目录声明 23 个；`drobotics-convert-bc-hbm-compare` 因未进入目录而默认禁用。客户目录的“是否测过”为空，因此界面会显示来源和发现状态，但不会将其表述为 Hobot Code 或发版方已验证。

### OpenExplorer x86/CUDA 构建机

量化、校准、模型适配、BC 和 HBM 编译等主机侧步骤不能在 ARM64 S600 上运行。S600 必须能够使用自己的 OpenSSH 配置直连构建机。建议为 Hobot Code 创建专用密钥和别名：

```sshconfig
Host openexplorer-builder
    HostName 192.0.2.10
    User builder
    Port 22
    IdentityFile /root/.config/hobot-code/ssh/openexplorer-builder
    IdentitiesOnly yes
    StrictHostKeyChecking yes
```

私钥必须只保存在板端并设置为 `0600`；不要把私钥、密码或 token 放入 Prompt。构建机的 `authorized_keys` 建议为该公钥增加 `restrict`，以关闭转发、Agent、X11 和 PTY 能力，同时保留 OpenExplorer 工作流需要的远程命令执行。

当 Agent 首次进入主机侧 Skill 阶段时，`openexplorer_build_host` 会要求用户输入 SSH 别名或 `user@hostname`，将选择保存为任务私有状态，并验证远端架构；CUDA Skill 还会检查 `nvidia-smi`。首次审批可选「Trust this build host for this task」：只有探测成功才会记录信任，同一任务对同一构建机的后续探测不再重复请求主机网络确认。选「Allow once」不会记录信任；更换构建机或探测失败也不会保留新信任。后续命令通过 `openexplorer_remote_run` 执行，每条命令仍由板端权限策略审批。任务必须使用 **Network: Network/shared**；`model-only` 或 `offline` 会阻止构建机连接。

选择构建机后，Agent 不得再用通用 `bash` 直接 `ssh` 到该目标，否则会绕过任务级主机验证与远程命令审批。Hobot Code 会拒绝这类调用并要求 Agent 改用上述两个专用工具。

Skill 指令从 S600 的外部包加载，但 Hobot Code 不会自动把 OpenExplorer 源码、Skill 脚本、模型或校准数据复制到构建机。进入远端阶段前，Agent 必须向用户确认构建机上的 OpenExplorer 工作目录、模型路径和输出目录；缺少其中任何一项时应停下来询问，不能根据目录名猜测。

来源：用户提供的 OpenExplorer LLM 正式交付包中的 `.skillshare/skills/`、`docs/02_SkillShare初始化与使用.md` 和 `docs/03_SKILLS_CATALOG.md`。外部包的许可和交付约束由其自身文件及发版方负责。
