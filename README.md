# Hobot Code

Hobot Code 是运行在地瓜机器人 RDK X5、RDK S100 和 RDK S600 上的终端开发 Agent。
0.5 开始直接使用 Pi 的官方交互运行时，因此编辑器、流式显示、thinking、工具调用、
会话树、分支、压缩、Skills、扩展和快捷键与 Pi 保持一致。Hobot Code 不重写 TUI，只增加
地瓜板卡所需的模型协议、硬件探测、安全策略和 ARM64 安装层。

## 设计边界

- Pi 0.84.1 官方 Linux ARM64 二进制按 SHA256 固定并原样装入发行包。
- `package.json` 使用 Pi 官方 `piConfig` 机制将主命令改为 `hobot`，项目配置目录改为 `.hobot`。
- `extensions/rdk/index.ts` 注册 D-Robotics Kimi Provider、实时 `system_snapshot`、版本感知的
  `rdk_docs_search`、紧凑 RDK 专家层以及板端命令。
- `prompts/rdk-expert.md` 只定义 Pi 不具备的板卡证据、版本路由、BPU 验收与硬件安全边界，
  并在每轮注入稳定的实机标识；详细知识通过工具和 Skills 按需加载。
- Pi 自带的 provider、`models.json`、extensions、packages、Skills、prompt templates 和
  themes 均可继续使用。
- 板端无需安装 Node、Bun、Go、Python 或容器。

Pi 上游版本和来源记录在 `pi-runtime/pi.lock`，许可证位于 `LICENSES/`。

## 构建 ARM64 包

构建机需要 `curl`、`tar` 和 SHA256 工具。发行脚本会下载并校验 Pi、fd 和 ripgrep
的官方 ARM64 产物：

```bash
make release VERSION=0.12.0
```

输出：

```text
dist/hobot-code-0.12.0-linux-arm64.tar.gz
```

在不能稳定访问 GitHub Release 的构建机上，可通过 `HOBOT_CODE_PI_CACHE_DIR` 复用 Pi
归档，并通过 `HOBOT_CODE_TOOL_BUNDLE_DIR` 提供已经解压的 `fd`、`rg` 及对应许可证。
脚本仍会按 `pi-runtime/pi.lock` 和 `pi-runtime/tools.lock` 校验每个文件：

```bash
HOBOT_CODE_PI_CACHE_DIR=/path/to/pi-cache \
HOBOT_CODE_TOOL_BUNDLE_DIR=/path/to/tool-bundle \
make release VERSION=0.12.0
```

## 安装

```bash
scp dist/hobot-code-0.12.0-linux-arm64.tar.gz root@RDK_IP:/tmp/
ssh root@RDK_IP
cd /tmp
tar -xzf hobot-code-0.12.0-linux-arm64.tar.gz
cd hobot-code-0.12.0-linux-arm64
./install.sh
```

安装器会把旧 `/etc/hobot-code` 与 `/var/lib/hobot-code` 备份后迁移到安装用户目录，并把
当前运行时备份到 `/usr/local/lib/hobot-code-backups/`。检测到运行中的 Hobot Code 时会拒绝升级。

## 模型配置

默认接入 D-Robotics Kimi 网关：

```bash
chmod 600 ~/.config/hobot-code/hobot.env
vi ~/.config/hobot-code/hobot.env
```

```text
ANTHROPIC_BASE_URL=https://ai-api.d-robotics.cc
ANTHROPIC_AUTH_TOKEN=your-token
ANTHROPIC_MODEL=kimi-k3
```

密钥只保存在当前用户可读的环境文件中。Kimi 网关当前只返回完整的 Anthropic 响应，
Hobot Code Provider 会将 thinking、文本、工具调用和 usage 转换为 Pi 原生事件，所以界面和
会话行为不需要特殊分支。

运行时也保留 Pi 的其他模型接入方式：

- 在 `/model` 中选择已配置厂商模型。
- 编辑 `~/.config/hobot-code/agent/models.json` 添加 Ollama、vLLM、LM Studio 或兼容网关。
- 使用 `/login <provider>` 配置 Pi 支持的登录型 Provider。
- 使用 `hobot install <package>` 安装 Pi 扩展包。

详细字段见 [配置说明](docs/configuration.md)。

## 使用

启动只需要：

```bash
hobot
```

常用 Pi 交互保持不变：

```text
/model          选择模型
/settings       打开设置
/new            新会话
/resume         恢复会话
/tree           浏览和切换会话分支
/fork           从历史消息分支
/compact        手动压缩上下文
/btw <task>     打开共享当前上下文的临时全能力 Agent
/reload         重载扩展、Skills、Prompt 和主题
/hotkeys        查看完整快捷键
/system-prompt  查看 Pi、RDK 与条件状态层的长度
/system-prompt full  展开最近一轮的完整系统 Prompt
/permissions    查看或修改工具权限
/init           生成项目 AGENTS.md 和质量门配置
/gate           查看、配置或运行质量门
/memory         查看和管理持久化记忆
/goal           创建和管理持久目标、预算与验证状态
/hooks          查看或重载 PreToolUse/PostToolUse Hook
/notifications  测试或开关 SSH 终端通知
/lsp            查看、重载或停止受限语言服务器
/quit           退出；Hobot Code 另外提供 /q 和 /exit
```

```text
Escape          中断当前模型或工具
Ctrl+C          清空编辑区
Ctrl+D          编辑区为空时退出
Ctrl+T          显示/隐藏 thinking
Ctrl+O          展开/折叠工具输出
Shift+Tab       切换 thinking 等级
Ctrl+L          打开模型选择器
Ctrl+P          切换下一个已启用模型
Alt+Enter       排队 follow-up 消息
```

`/btw` 在主 Agent 工作时也可立即执行。它从主会话的当前内存分支创建一次性快照，继承当前模型、
thinking 等级、系统 Prompt、有效工具、Skills、记忆上下文和项目信任状态，并在浮层中独立完成任务：

```text
/btw 检查当前改动是否遗漏了升级文档
```

侧边 Agent 的消息不会写入主会话；按 `Esc` 或 `Ctrl+C` 可终止，完成后按 `Enter`、空格或 `Esc`
关闭。关闭时临时会话、Prompt 和运行记录会被删除。它与主 Agent 共享工作区、进程、服务和设备，
因此文件或硬件修改会保留；需要交互批准的工具在隐藏子进程中保持 fail-closed。为控制板端内存，
每个 Hobot Code 会话同时只允许一个侧边 Agent。

非交互调用同样沿用 Pi：

```bash
hobot -p "检查这个项目并给出结论"
hobot --mode json "输出 JSON 事件流"
hobot --continue
hobot --resume
```

## 权限与质量门

全局权限策略位于 `~/.config/hobot-code/agent/permissions.json`，按顺序匹配工具名，支持
`allow`、`ask` 和 `deny`。`deny` 工具不会进入模型上下文；`ask` 在 TUI 中显示经过密钥
脱敏的工具、风险和目标，在非交互模式下默认拒绝：

```text
/permissions status
/permissions set bash ask
/permissions set mcp:* deny
/permissions default ask
/permissions reload
```

RDK 安全底线优先于通用策略：`/proc`、`/sys`、`/dev` 直接写入始终拒绝，工作区外写入和
破坏性 Shell 命令始终需要交互确认。

在项目根目录运行 `/init` 会创建 `AGENTS.md` 和 `.hobot/quality-gates.json`，自动识别
Make、Node、Go、Rust 或 pytest 验证命令，已有文件保持不变。质量门配置和最近结果随会话
持久化，通过结果绑定工作区指纹；门禁之后再次修改文件会变为 `stale`：

```text
/gate status
/gate set make check
/gate set ["npm run check","npm test"]
/gate timeout 300
/gate run
```

配置质量门后，Agent 只有在最终修改之后取得当前 `passed` 结果才能声明完成；否则 Hobot Code
会在回复中标记该完成声明不被接受。

## 持久化记忆

Hobot Code 使用板端 SQLite + FTS5 保存经用户同意的长期上下文，分为 `user`、`project`、
`board` 和 `session` 四个作用域。每轮只注入与当前问题相关的少量记忆；记忆只是可能过期
的上下文，不能覆盖当前用户指令或实机证据。

```text
/memory status
/memory list [user|project|board|session]
/memory search <query>
/memory add <scope> <preference|decision|fact|fix|instruction|note> <text>
/memory forget <memory-id>
/memory clear <scope>
/memory prune
/memory audit
```

模型可调用 `memory_search` 和 `memory_save`。默认允许检索，但每次模型写入都要交互确认；用户直接
执行 `/memory add` 视为明确写入。密钥、Bearer Token、私钥、常见 secret 赋值和疑似银行卡号
会被存储层拒绝。数据库位于 `~/.local/state/hobot-code/memory/memory.db`，文件权限为 `0600`。

## P1 长期工作能力

持久目标只能由用户显式创建，每个项目同时最多一个 active/paused 目标。它记录 turn/token
预算、实际消耗、执行耗时、跨会话继续次数、进展和验证指纹；上下文压缩不会完成目标，
预算耗尽会自动暂停：

```text
/goal create --turns 50 --tokens 500000 <objective>
/goal status
/goal progress <verified milestone or blocker>
/goal pause
/goal resume
/goal extend <extra-turns> [extra-tokens]
/goal complete <outcome>
/goal cancel <reason>
/goal history
```

模型通过 `goal_status`、`goal_progress`、`goal_complete` 工具参与目标管理。项目配置了质量门时，
模型只能在最终修改后取得当前 `passed` 指纹才能完成目标；用户的 `/goal complete` 是明确人工裁决。

Hook 从 `~/.config/hobot-code/agent/hooks.json` 读取。`PreToolUse` 可在工具执行前阻断，`PostToolUse`
可追加结果或标记失败。Hook 命令使用 argv 数组，不经 Shell 解析；结构化 JSON 从 stdin 输入，
stdout 可返回 `block`、`reason`、`appendText`、`isError`。每次运行都有超时、输出上限和脱敏审计。
项目 `.hobot/hooks.json` 默认不执行，必须由全局配置显式开启。

SSH TUI 在等待批准、长任务完成或失败时发送 OSC 9 和/或 bell，可用 `/notifications test`
验证当前终端，也可用 `/notifications off` 关闭。RPC/JSON 模式不会插入终端控制序列。

`lsp` 工具提供 `status`、`diagnostics`、`hover`、`definition`、`references`、`symbols`、`stop`。
语言服务器只在实际请求且命令存在时启动，默认最多 1 个进程、256 MiB RSS、60 秒空闲时间；
超限会自动结束，不影响主 Agent。基础包不捆绑 clangd、pylsp、gopls 等大型语言服务器。

## RDK 适配

`/rdk` 显示简要板卡状态，`/doctor` 显示完整诊断。模型需要实时硬件信息时会调用
`system_snapshot`，读取板卡型号、完整 RDK OS 版本、CPU、内存、负载、温度、BPU 设备
节点和 RDK 工具。实时硬件详情不会自动加入每次系统 Prompt。

板卡专业知识位于 `/usr/local/lib/hobot-code/knowledge`。Hobot Code 根据 device tree 中的型号和
`/etc/version` 自动选择 X5 3.x、S100 4.x 或 S600 5.x 资料，通过 `rdk_docs_search` 按需
返回短摘要、适用版本和地瓜官方来源，不把整套文档塞进上下文。用户也可直接运行：

```text
/knowledge BPU 模型如何转换和验证
/knowledge S600 的 VDSP 有几个核心
```

知识结果是版本化文档，实机状态由 `system_snapshot` 证明；两者不一致时 Hobot Code 会明确
提示版本不匹配。知识清单与来源索引见 `knowledge/manifest.json`。

紧凑 RDK 角色位于 `prompts/rdk-expert.md`。它与 Pi 基础 Prompt 同为英文，只追加 Pi 不具备的
板卡证据、版本路由、BPU 验收和硬件安全约束，并要求跟随用户语言回答。板卡、RDK OS、文档线、
主机名和架构会动态填充；没有配置的质量门、没有召回结果的记忆和不存在的目标不会产生空段落。
详细平台流程继续由 `rdk_docs_search` 和 Skills 按需提供，维护校验禁止 RDK Prompt 超过 1700 字符。

会话、记忆、目标和 Hook 审计保存在 `~/.local/state/hobot-code`，全局配置位于
`~/.config/hobot-code/agent`，板端扩展和 Skills 位于 `/usr/local/lib/hobot-code`。完整目录和
旧系统布局迁移规则见 `docs/user-directory-layout.md`。

## 回滚

```bash
hobot-rollback
```

该命令恢复最近一次安装前的 Hobot Code 命令和运行时；会话、模型密钥和用户配置不会被删除。

## 安全边界

- 对工作目录外的写入和高风险 Shell 命令要求交互确认；非交互模式默认拒绝。
- `/proc`、`/sys`、`/dev` 下的直接文件写入被文件工具阻止。
- `system_snapshot` 只证明当前节点和工具存在，不证明任意模型已完成 BPU 转换。
- Hobot Code 是控制面 Agent，不应进入电机、CAN、GPIO、安全或急停的硬实时闭环。

Hobot Code 仅提供 `hobot` 和 `hobot-rollback`，不携带其他历史命令或运行时。
