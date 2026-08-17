# Hobot Code 完整用户手册

| 文档信息 | 内容 |
|---|---|
| 适用版本 | Hobot Code 0.27.x |
| 适用设备 | RDK X5、RDK S100、RDK S600 |
| 适用客户端 | 板端 TUI、Mac 版 Hobot Code Studio |
| 最后核对 | 2026-08-17 |

Hobot Code 是面向地瓜机器人 RDK 的板端开发 Agent。它可以在 RDK 上理解项目、编辑代码、执行命令、调用 BPU 与多媒体工具、检索板型知识，并通过终端或 Mac 应用持续处理任务。

本手册覆盖软件的安装、配置、日常使用、模型接入、任务管理、安全权限、Side Agent、RDK 专业能力、诊断更新和故障恢复。文中标为“当前限制”的内容不能视为已支持能力。

## 目录

1. [产品组成与核心概念](#1-产品组成与核心概念)
2. [支持范围与使用前提](#2-支持范围与使用前提)
3. [安装、验证与首次配置](#3-安装验证与首次配置)
4. [选择使用入口](#4-选择使用入口)
5. [Mac Studio 完整操作](#5-mac-studio-完整操作)
6. [板端 TUI 完整操作](#6-板端-tui-完整操作)
7. [项目、工作区与代码变更](#7-项目工作区与代码变更)
8. [任务生命周期与后台运行](#8-任务生命周期与后台运行)
9. [模型、Provider 与可用性验证](#9-模型provider-与可用性验证)
10. [Approvals、Board access 与 Network](#10-approvalsboard-access-与-network)
11. [Side Agent](#11-side-agent)
12. [RDK 专家能力与知识库](#12-rdk-专家能力与知识库)
13. [模型部署与验收](#13-模型部署与验收)
14. [工程上下文、记忆与质量控制](#14-工程上下文记忆与质量控制)
15. [Capabilities、Skills 与扩展](#15-capabilitiesskills-与扩展)
16. [图片、文档与链接](#16-图片文档与链接)
17. [诊断、监控与技术支持](#17-诊断监控与技术支持)
18. [版本、更新、回滚与卸载](#18-版本更新回滚与卸载)
19. [数据目录、隐私与安全边界](#19-数据目录隐私与安全边界)
20. [常见问题与恢复方法](#20-常见问题与恢复方法)
21. [CLI 命令参考](#21-cli-命令参考)
22. [TUI 命令与快捷键参考](#22-tui-命令与快捷键参考)
23. [术语表与相关文档](#23-术语表与相关文档)

## 1. 产品组成与核心概念

### 1.1 产品架构

Hobot Code 由三个部分组成：

| 组件 | 运行位置 | 作用 |
|---|---|---|
| `agentd` | RDK 板端 | 托管 Agent、模型、工具、权限、会话、持久任务、RDK 知识和硬件状态 |
| Hobot TUI | RDK 板端终端 | 适合 SSH、现场调试、弱网、无桌面和系统恢复 |
| Hobot Code Studio | Mac | 提供项目列表、对话、审批、代码变更、诊断和板卡监控 |

Studio 通过 SSH 启动 `hobot bridge --stdio` 与板端通信，不要求 RDK 开放新的 TCP 端口。模型密钥、权限判定和工具执行始终留在板端，桌面客户端不能绕过板端策略。

### 1.2 六个重要概念

| 概念 | 含义 |
|---|---|
| **Board** | 一台安装了 Hobot Code 的 RDK，以及连接它所需的 SSH 信息 |
| **Project** | 板端的一个工作目录；一个项目可包含多个对话 |
| **Conversation / Task** | 一条可持续、多轮、可恢复的 Agent 工作记录 |
| **Turn** | 用户的一次输入以及由此产生的 thinking、工具调用和回答 |
| **Worker** | 真正运行某个 Agent 的板端进程；任务显示 Ready 时 worker 仍可能存在 |
| **Side Agent** | 从主任务稳定上下文派生的独立多轮 Agent，不把对话回写主任务 |

“对话”和“任务”在界面中指同一份工作记录。删除对话不会删除项目目录，也不会自动撤销 Agent 已经产生的文件或外部副作用。

## 2. 支持范围与使用前提

### 2.1 支持的板卡

| 板卡 | RDK OS 线路 | 已验证基线 | 架构 |
|---|---|---|---|
| RDK X5 | 3.x | 3.5.0 | Linux ARM64 |
| RDK S100 | 4.x | 4.0.5、4.0.5-Beta | Linux ARM64 |
| RDK S600 | 5.x | 5.1.0 | Linux ARM64 |

同一主版本中的其他系统版本通常仍可用于日常 Agent 工作，但 Studio 会标记 **Hardware unverified**。这表示尚未完成该镜像上的硬件专项验证，不表示连接失败。

### 2.2 客户端与依赖

- 板端需要 Linux ARM64、可用的 SSH 环境和 `curl`。
- 内置 Agent 运行时自包含，不要求另外安装 Node.js、Bun、Go 或 Python。
- `hobot persistent` 需要板端安装 `tmux`。
- Mac Studio 当前提供 Apple Silicon (`arm64`) 安装包。
- 第三方 package 可能依赖 `git`、`npm` 或自定义命令。
- Hook 和 LSP 只在相应外部程序已经安装时可用。

### 2.3 兼容性结论

Studio 连接板卡后可能显示：

| 结果 | 含义 | 建议 |
|---|---|---|
| **Supported** | 协议、功能和目标处于已验证范围 | 可正常使用 |
| **Limited** | 日常 Agent 可用，但部分功能、构建身份或硬件基线未完整验证 | 查看 Technical details，升级或运行诊断 |
| **Upgrade required** | 协议或事件格式低于最低要求 | 先更新板端 Hobot Code |

Studio 与板端应使用相同的 major/minor 版本。patch 不同可以连接，但产品发布和生产验证建议保持完全一致。

## 3. 安装、验证与首次配置

### 3.1 一条命令安装板端程序

在 RDK 上执行：

```bash
curl -fsSL https://github.com/bryant-w/hobot-code/releases/latest/download/hobot-install.sh | sh
```

安装器会检查 Linux ARM64、RDK 型号、归档结构和 SHA-256。普通用户执行时会通过 `sudo` 安装程序，但配置和会话属于发起安装的用户；root 直接执行时数据属于 root。

安装指定版本：

```bash
curl -fsSL https://github.com/bryant-w/hobot-code/releases/latest/download/hobot-install.sh \
  | sh -s -- --version <version>
```

### 3.2 离线安装

从 [GitHub Releases](https://github.com/bryant-w/hobot-code/releases) 下载 Linux ARM64 归档和同名 `.sha256`，传到板端后执行：

```bash
sha256sum -c hobot-code-<version>-linux-arm64.tar.gz.sha256
tar -xzf hobot-code-<version>-linux-arm64.tar.gz
cd hobot-code-<version>-linux-arm64
sudo ./install.sh
```

root 登录时可直接使用 `./install.sh`。不要安装来源不明或校验失败的归档。

### 3.3 首次配置

```bash
hobot setup
hobot doctor
hobot version
# `hobot --version` 与 `hobot -v` 等价
```

- `hobot setup` 以隐藏输入方式配置内置 D-Robotics 模型凭据。
- `hobot doctor` 进行不调用模型、不读取项目内容的只读体检。
- `hobot version`、`hobot --version` 和 `hobot -v` 都只显示当前板端版本，不会启动 Agent 对话或读取模型配置。

如果修改了 Provider、模型或密钥，而 daemon 已经运行，需要执行：

```bash
hobot daemon restart
```

配置指纹不一致时，Hobot Code 会拒绝继续模型操作，而不是让旧 daemon 静默使用过期配置。

### 3.4 安装 Mac Studio

1. 从 [GitHub Releases](https://github.com/bryant-w/hobot-code/releases) 下载 `hobot-code-<version>-macos-arm64.dmg`。
2. 打开 DMG，将 **Hobot Code** 拖入 Applications。
3. 启动应用，添加板卡名称、IP、SSH 用户、端口和可选私钥路径。
4. 完成首次连接和兼容性检查。

Studio 使用 macOS 自带的 OpenSSH 和 `known_hosts`。它不保存 SSH 密码，也不把模型 Token 存在 Mac 应用配置中。

## 4. 选择使用入口

| 场景 | 推荐入口 |
|---|---|
| 现场调试、无桌面、弱网或恢复环境 | `hobot` TUI |
| SSH 会断开但需要保留完整交互界面 | `hobot persistent` |
| 无界面自动化、长期任务和脚本集成 | `hobot task ...` |
| 多项目、多对话、Diff、集中审批和板卡监控 | Mac Studio |

两种界面共享同一板端安全边界，但并不等于两个界面可以同时编辑同一条活动会话。连接同一工作目录的多个 Agent 还会受到工作区写租约保护。

## 5. Mac Studio 完整操作

### 5.1 添加与管理板卡

添加板卡时填写：

- **Name**：自定义名称，例如 `Lab S600`。
- **Host**：IP 地址或可解析主机名。
- **User**：板端 Linux 用户，例如 `root` 或普通开发用户。
- **Port**：SSH 端口，默认 `22`。
- **Identity**：可选的私钥文件路径。

保存前 Studio 会验证 SSH、桥接协议、能力和实际板型。删除板卡连接只删除 Mac 上的连接项，不会停止板端任务或删除板端数据。

### 5.2 顶部工具栏

从左到右，顶部工具用于：

| 入口 | 作用 |
|---|---|
| **Board selector** | 切换、添加或管理板卡 |
| **Appearance** | 选择跟随 macOS、浅色或深色主题；偏好仅保存在当前 Mac |
| **Version and updates** | 查看 Studio/板端版本、兼容性和更新方式 |
| **Capabilities** | 查看模型、Tools、Skills、Extensions、Prompts、Themes、Hooks 与 LSP 库存 |
| **Model providers** | 添加、轮换或删除板端受管 Provider |
| **Board readiness** | 运行板端只读体检并显示可执行修复 |
| **Save private support bundle** | 生成脱敏支持包并保存到 Mac |
| **Sync board now** | 重新读取板卡、任务和事件状态；离线时用于重连 |
| **Board monitor** | 打开兼容性、硬件、资源和 Agent 边界面板 |

图标不可用时，通常是板端版本不支持相应能力、当前离线，或另一个互斥操作正在执行。悬停图标可以查看名称。

### 5.3 项目与左侧栏

- 左侧栏以 **Project → Conversation** 的层级展示任务。
- 项目可折叠，一个项目可创建多个对话。
- `+` 可在现有项目中新建对话，也可浏览板端目录或创建新文件夹。
- 对话名称默认根据首条指令生成，可双击标题或从 `…` 菜单修改。
- 删除单个对话需要确认；删除项目会移除其对话记录，但不会删除板端工作目录。
- Side Agent 始终显示为主对话的同级分支，不形成无意义的多层嵌套。

### 5.4 新建项目或对话

1. 点击 **New conversation**。
2. 浏览板端目录，选择已有目录或创建新目录。
3. 确认 Shared project 或 Isolated worktree。
4. 在输入框底部选择模型、Approvals、Board access 和 Network。
5. 输入第一条需求并发送。

首条消息发送前属于草稿任务，可以自由修改进程级安全设置。任务启动后，部分设置会锁定，详见[第 10 章](#10-approvalsboard-access-与-network)。

### 5.5 对话区

对话按 turn 展示以下内容：

- 用户消息；
- 可展开或隐藏的 thinking；
- 工具调用、执行状态、用时和输出摘要；
- Agent 最终回答；
- 审批、恢复和错误提示。

回复中的 HTTP/HTTPS 链接交给 Mac 默认浏览器打开。代码块保持原始内容，普通文本不会因为参数中的双连字符而被错误渲染成删除线。

### 5.6 输入框

| 操作 | 行为 |
|---|---|
| `Enter` | 发送消息 |
| `Shift+Enter` | 换行 |
| 发送/停止按钮 | 空闲时发送；运行时在同一位置变为停止 |
| 模型菜单 | 选择当前任务模型；活动 turn 中不可切换 |
| 图片按钮 | 在模型声明支持图片时添加附件 |
| Readiness | 查看当前模型资格状态 |
| Task settings | 设置 Approvals、Board access 和 Network |

中文输入法确认候选词不会误发送。连接中断时草稿保留，发送按钮切换为重连，避免把内容发到错误状态。

### 5.7 编辑历史消息

编辑一条旧用户消息并不是在末尾追加新消息。Hobot Code 会：

1. 保留该消息之前的上下文；
2. 用修改后的内容替换该消息；
3. 隐藏这之后的旧时间线；
4. 从该时间点继续生成新的任务分支。

如果原消息含图片，界面会要求重新附加或确认移除。旧时间线不会自动撤销已经发生的文件、进程或硬件副作用。

### 5.8 Changes

任务标题栏的 **Changes** 显示当前绑定工作区的 Git 状态和有界文本 Diff。

- 未跟踪文件只显示名称，不自动读取全部内容。
- Shared project 中的变化可能来自主 Agent、Side Agent 或人工操作，不能全部归因于当前 Agent。
- Isolated worktree 可使用 **Apply to project** 将确认后的变化以 staged changes 应用回原项目。
- Apply 不会自动 commit 或 push。

### 5.9 Board monitor

右侧监控面板可展示：

- Studio/板端兼容性及 Technical details；
- 板卡身份、RDK OS 和在线状态；
- BPU 核心、频率和负载；
- CPU、内存、磁盘、温度；
- ION/Hbmem heap 与相关进程；
- 当前硬件租约和正在写入的工作区；
- 任务恢复提示和日志保留提示；
- 当前 Agent 的文件、设备、权限与网络边界。

这些数据用于诊断，不等于模型部署验收或硬件安全认证。

## 6. 板端 TUI 完整操作

### 6.1 启动

进入项目目录后执行：

```bash
cd /path/to/project
hobot
```

显式选择隔离和网络：

```bash
hobot tui --sandbox workspace --network model-only
```

默认前台 TUI 会随普通 SSH 会话结束。需要跨断线保留完整界面时使用：

```bash
hobot persistent
```

### 6.2 基本交互

- 直接输入自然语言需求并按 `Enter`。
- `Escape` 中断当前模型或工具调用，保留对话和 Agent worker；输入框旁的方形按钮与该快捷键行为一致。需要释放 worker 时使用 **Task settings → Stop Agent**。
- 输入区为空时按 `Ctrl+D` 退出。
- `Ctrl+T` 显示或隐藏 thinking。
- `/model` 切换模型，`/settings` 修改 Pi 设置。
- `/new` 开始新会话，`/resume` 选择历史会话。
- `/hotkeys` 查看当前版本的完整快捷键。

### 6.3 复制输出

- 鼠标拖选后，TUI 通过 OSC 52 尝试复制到本地剪贴板。
- `/copy` 复制最近一条 Agent 回复。
- 如果本地终端禁止远程剪贴板写入，按住 `Shift` 再拖选，使用终端自身复制。

### 6.4 持久 TUI

```bash
hobot persistent                       # 启动或返回默认 main 会话
hobot persistent start review          # 创建命名会话
hobot persistent attach review         # 重新进入
hobot persistent list                  # 查看持久会话
hobot persistent stop review           # 停止指定会话
```

主动离开但保留任务：

- 在输入框中执行 `/detach`；或
- 使用 tmux 原生按键：先按 `Ctrl+B`，松开后按 `D`。

`/detach` 是 TUI 命令，不是在 Linux Shell 中执行。它避免了 VS Code、浏览器或终端对 `Ctrl+B` 的快捷键冲突。

## 7. 项目、工作区与代码变更

### 7.1 Shared project

任务直接使用原目录，适合：

- 非 Git 目录；
- 尚无提交的 Git 项目；
- 已存在未提交或未跟踪内容的项目；
- 明确需要和人工共享实时文件的工作。

共享目录中的改动可能相互影响。Hobot Code 会使用工作区写租约降低多个 Agent 同时修改的风险，但它不能阻止用户或其他程序在目录中改文件。

### 7.2 Isolated worktree

仅当项目是有 `HEAD`、没有 tracked/untracked 改动的干净 Git 仓库时可用。每个根任务获得独立的受管 worktree：

- 不污染原项目；
- 适合不同根任务并行开发；
- 主任务、其 Side Agent 和编辑历史分支仍共享同一个受管 worktree；
- 完成后由用户审阅并应用回原项目。

### 7.3 应用与清理

```bash
hobot workspace inspect [DIR]
hobot workspace list
hobot workspace writes
hobot workspace delivery <task-id>
hobot workspace apply <task-id> --yes
hobot workspace cleanup <task-id> --yes
```

Apply 前会校验原项目仍然干净、基线未变化、交付摘要与文件 digest 一致，并停止相关空闲 Agent。应用结果以 staged changes 进入原项目，不自动创建提交。

Cleanup 只允许在没有任务引用、没有未提交修改、没有额外提交时执行。忽略文件中的运行产物不属于交付内容。

## 8. 任务生命周期与后台运行

### 8.1 状态说明

| 状态 | 含义 |
|---|---|
| `queued` | 请求已持久化，等待 worker 空位 |
| `starting` | 正在创建运行环境和 Agent 进程 |
| `running` | Agent 正在思考或使用工具 |
| `waiting` | 等待审批、选择或输入 |
| `idle` / Ready | 当前 turn 完成，worker 仍活着并等待下一条消息 |
| `stopping` | 正在结束 worker |
| `stopped` | worker 已停止，会话记录保留 |
| `failed` | 任务因明确错误结束 |
| `interrupted` | daemon、板卡或进程异常导致活动任务中断 |

### 8.2 并发和排队

每个 OS 用户默认最多保留 2 个后台 worker，可配置为 1 至 8。需要空位时，服务会先挂起最久未使用的 Ready worker；不会自动中断 running、waiting、starting 或 stopping 的任务。

如果所有 worker 都在工作，新任务进入持久 FIFO 队列。不要因为短暂显示 queued 而重复创建相同任务。

### 8.3 断线续跑

Studio 创建的任务由板端 `agentd` 托管。以下情况通常不会停止任务：

- 关闭 Mac 应用；
- Mac 休眠；
- SSH 或 VPN 暂时断开；
- Studio 到板端的连接重建。

重连后 Studio 按事件序号补齐仍在保留范围内的输出。长任务只保留最新的有界事件历史，过期游标会明确提示实际重放起点。

### 8.4 Stop、Abort、Resume 与 Restart

| 操作 | 作用 |
|---|---|
| **Stop** | 结束任务 worker，保留任务、日志和可用 session |
| **Abort turn** | 中止当前一轮，任务本身仍可继续使用 |
| **Resume** | 使用已验证的 Pi session 延续同一上下文 |
| **Restart / New session** | 清除 session 绑定，在同一任务记录中开始全新上下文 |

板卡重启、断电或 daemon 崩溃会把活动任务标为 `interrupted`。Hobot Code 不自动重放未完成 Prompt、工具调用或审批，以免重复写文件、启动进程或操作硬件。

出现 “task has no resumable Pi session” 时应使用 **New session** 或 `hobot task restart`，而不是强行 Resume。

### 8.5 归档和删除

- Archive 只把任务从日常列表隐藏。
- Delete 要求任务已停止并归档。
- 删除任务记录不删除项目代码、worktree 变更、已启动进程或硬件副作用。
- 默认最多保留 100 个任务，达到上限时会拒绝新任务，不会静默删历史。

## 9. 模型、Provider 与可用性验证

### 9.1 内置模型

当前内置 D-Robotics 模型：

| 模型标识 | 图片输入 |
|---|---:|
| `drobotics/kimi-k3` | 是 |
| `drobotics/qwen3.8-max` | 是 |
| `drobotics/glm-5.2` | 是 |
| `drobotics/deepseek/deepseek-v4-flash` | 否 |
| `drobotics/deepseek-v4-pro` | 否 |

模型能力来自板端运行时契约；Studio 不应根据模型名称猜测图片或 reasoning 支持。

### 9.2 添加自定义 Provider

```bash
hobot provider add my-provider \
  --base-url https://api.example.com \
  --api openai-completions \
  --model my-model \
  --name "My Provider" \
  --model-name "My Model" \
  --context-window 128000 \
  --max-tokens 16384 \
  --reasoning
```

支持的协议：

- `anthropic-messages`
- `openai-completions`
- `openai-responses`
- `google-generative-ai`

未使用 `--token-stdin` 时，API key 从控制终端隐藏读取，不写入 `providers.json`。Studio 的 **Model providers** 也可添加、轮换和删除 Provider；密钥仍只保存在板端。

其他接入方式：

- Pi 原生 OAuth：TUI 中使用 `/login`。
- 本地或高级模型配置：编辑板端 `agent/models.json`。

### 9.3 Provider 管理

```bash
hobot provider list
hobot provider rotate my-provider
hobot provider remove my-provider --yes
```

Provider 配置变化后重启 daemon。轮换共享密钥时需明确确认，以免让其他引用同一凭据的 Provider 意外失效。

### 9.4 四层模型验证

| 检查 | 命令 | 证明什么 |
|---|---|---|
| Route check | `hobot model check PROVIDER/MODEL` | 路由、认证和基本响应可用 |
| Gateway probe | `hobot model probe PROVIDER/MODEL` | 流式、工具续接及声明的图片能力 |
| Runtime probe | `hobot model runtime-probe PROVIDER/MODEL` | 隔离 Pi 环境中的工具、thinking、审批、压缩和恢复 |
| RDK probe | `hobot model rdk-probe PROVIDER/MODEL` | 指定只读 RDK 专业工作流 |

查看已保存资格证据：

```bash
hobot model status PROVIDER/MODEL
hobot model profiles PROVIDER/MODEL
```

Route check 缓存 5 分钟，Gateway probe 缓存 1 小时；可用 `--force` 重新执行。配置、构建、Pi、板卡、系统 Prompt、扩展或知识发生变化时，旧证据可能变为 stale。

### 9.5 RDK probe 档案

当前提供：

- `read-only-rdk-diagnostic-v1`
- `read-only-model-deployment-planning-v1`
- `read-only-multimedia-planning-v1`
- `read-only-hardware-safety-planning-v1`

`isolated-workspace-coding-v1` 仍是规划项，不能作为已实现资格。只读 planning probe 证明模型能生成受约束方案，不证明模型已经完成实际转换、部署、多媒体运行或硬件控制。

## 10. Approvals、Board access 与 Network

这三个设置互相独立：

| 设置 | 回答的问题 | 最终判定位置 |
|---|---|---|
| **Approvals** | 哪些工具操作需要先询问？ | 板端权限策略 |
| **Board access** | 即使同意，Agent 最多能访问哪些文件、设备和权限？ | 板端 Linux sandbox |
| **Network** | 工具和模型能访问哪些网络？ | 板端网络命名空间和模型代理 |

### 10.1 Approvals

| 选项 | 行为 | 场景 |
|---|---|---|
| **Review only** | 禁止修改项目和系统状态 | 代码审查、日志分析、方案讨论 |
| **Ask for changes** | 变更前询问 | 初次使用、陌生项目、高风险任务 |
| **Developer** | 普通读取、构建、测试和工作区编辑尽量不打断 | 受信项目的日常开发 |

Developer 不是“允许所有命令”。以下行为仍可能询问或被拒绝：

- 删除、覆盖和破坏性 Git/文件系统操作；
- 工作区外写入和关键系统路径；
- 软件安装、服务、内核或网络配置；
- 终止进程和硬件操作；
- MCP、未知工具、持久记忆或目标状态变更。

root 会话默认对 `bash`、`write`、`edit` 逐次确认。需要让普通操作遵循策略而不是 root 全部确认，可在 TUI 使用：

```text
/permissions root policy
/permissions preset developer
```

硬安全规则仍然生效。内置 `write`/`edit` 永远拒绝直接修改 `/boot`、`/dev`、`/etc`、`/proc`、`/sys`、`/usr` 和 `/var/lib`。

### 10.2 Board access

| 选项 | 项目写入 | RDK 设备 | 说明 |
|---|---:|---:|---|
| **Read only** (`review`) | 否 | 最小设备 | 宿主文件只读，丢弃 Linux capabilities |
| **Workspace** (`workspace`) | 当前工作区 | 不开放 RDK 设备 | 普通编码、构建和测试 |
| **Board hardware** (`system`) | 当前工作区 | 白名单 BPU、ION/Hbmem、DMA、video、media、ISP、VPU、DRI | 模型推理和多媒体调试 |
| **No sandbox** (`off`) | 当前用户可访问位置 | 当前用户可访问设备 | 关闭 OS sandbox，供明确的系统维护任务使用 |

前三档都有 Bubblewrap sandbox。`Board hardware` 仍保持宿主根文件系统只读，并丢弃 Linux capabilities；它不是完整 root 权限。

`No sandbox` 的命名表示确实没有 OS 级隔离。root 用户选择它时基本等同整板 root 工具访问，应当只在其他档位无法完成的系统维护中使用。

### 10.3 Network

| 选项 | 模型 | 工具网络 | 说明 |
|---|---:|---:|---|
| **Network** (`shared`) | 允许 | 允许 | 使用板端宿主网络，外联命令仍受 Approvals 检查 |
| **Model only** (`model-only`) | 受支持模型 | 禁止 | 工具断网，模型经 agentd 私有 Unix Socket 访问 |
| **Offline** (`offline`) | 仅本地模型 | 禁止 | 不提供模型代理或互联网 |

Model only 支持内置 D-Robotics，以及板端受管的 Anthropic Messages、OpenAI Chat Completions 和 OpenAI Responses Provider。Google、Pi 登录和自管 `models.json` 当前需要 Network。

受限网络依赖 sandbox，因此 No sandbox 只能与 Network 组合。

### 10.4 为什么设置会变灰

- Approvals 是动态策略，任务 Ready 时可修改。
- Board access 和 Network 决定 worker 创建时的 mount、device 和 namespace，不能热切换。
- Ready 仅表示等待下一条消息，不表示 worker 已停止。

要修改 Board access 或 Network：

1. 打开 **Task settings**。
2. 点击 **Stop Agent** 结束空闲 worker。
3. 修改设置。
4. 使用 **Resume** 延续已有 session；没有 session 时使用 **New session**。

### 10.5 推荐组合

| 目标 | Approvals | Board access | Network |
|---|---|---|---|
| 只读分析 | Review only | Read only | Model only |
| 日常开发 | Developer | Workspace | Model only |
| 下载依赖或远程 Git | Developer | Workspace | Network |
| BPU 模型部署 | Developer | Board hardware | Model only 或 Network |
| Camera/编解码调试 | Ask for changes 或 Developer | Board hardware | 按需 |
| 系统维护 | Ask for changes | No sandbox | Network |

## 11. Side Agent

### 11.1 工作方式

Side Agent 是独立 Agent，不是主 Agent 的一次性问答框。它：

- 从主任务最近的稳定上下文快照开始；
- 共享模型、系统 Prompt、工具、项目和安全边界；
- 支持独立多轮对话和工具调用；
- 不把对话或回答写回主任务；
- 关闭后删除临时会话，不写入持久记忆和目标状态。

主任务正在进行的未完成工具调用不会复制到快照。Side Agent 每轮可看到主任务经过白名单约束的公开状态，例如正在思考、使用哪个工具或等待审批；它不会读取主任务的隐藏思考、原始工具输出或未完成消息，也不能替主任务审批、停止或继续工作。

Side Agent 与主 Agent 共享 OS 用户和工作区，因此它写过的文件、启动的进程或硬件副作用不会因关闭对话而撤销。为避免同时改同一份代码，共享目录中主任务在运行或等待审批时拥有写入优先级：Side Agent 可继续检索和分析，但写操作会被暂停；实时状态暂时不可验证时也不会冒险放行。使用独立 worktree 的任务可并行写入，文件写租约、BPU、相机和媒体管线租约仍在板端统一判定。

### 11.2 Studio 中使用

1. 打开主对话。
2. 点击标题栏 **Side Agent**。
3. 输入一个独立任务。
4. 在左侧主任务下面选择 Side Agent 继续多轮对话。
5. 从 `…` 菜单删除 Side Agent。

每个主任务默认最多保留两个正在运行或等待首条消息的 Side Agent，可通过 `HOBOT_CODE_MAX_SIDE_AGENTS=1..8` 调整。超过上限时创建会被明确拒绝；已关闭且不再等待输入的 Side Agent 不占额度。任务 worker 容量不足时会排队或先挂起最久未使用的 Ready worker。

Side Agent 始终作为同一个主任务下的同级分支展示。即使从另一个 Side Agent 发起，系统也会记录真实来源用于审计，但不会形成难以管理的多层 UI。

### 11.3 资源冲突规则

| 资源 | 默认协调方式 | 冲突时行为 |
|---|---|---|
| 共享工作区 | 主任务写优先级 + 跨进程写租约 | Side Agent 保持只读并等待；不会自动覆盖或合并 |
| 独立 worktree | 各自写入，交付时复核 | 可并行；仍需处理真实 Git 冲突 |
| BPU、相机、媒体管线 | 板端硬件租约 | 后到的调用在执行前拒绝，并显示占用者摘要 |
| Agent worker 槽 | 私有 FIFO 队列 | 空闲槽释放后按顺序启动 |
| 模型网关 | 有界并发 | 受 Provider 并发上限约束，不保证任务级算力隔离 |
| CPU、内存、温度 | 系统诊断和任务上限 | 当前不会预留资源；重负载任务仍应错峰或使用独立板卡 |

### 11.4 TUI 中使用

```text
/btw 检查当前项目的测试覆盖并给出改进建议
```

- 全屏终端会等分主/侧区域；窄终端回退为侧边浮层。
- 打开后默认不抢占主输入焦点。
- 点击任一半屏可切换输入焦点。
- `Ctrl+Shift+Right` 进入侧边，`Ctrl+Shift+Left` 返回主 Agent。
- 鼠标滚轮滚动指针所在区域。
- 侧边输入区按 `Enter` 继续多轮追问。
- `Escape` 中断侧边当前 turn，空闲时关闭。
- `Ctrl+D` 关闭 Side Agent。

审批弹窗出现时由弹窗优先持有焦点，避免点击或快捷键把输入误送到另一个 Agent。

## 12. RDK 专家能力与知识库

### 12.1 专家上下文

Hobot Code 启动时识别：

- RDK X5、S100 或 S600；
- RDK OS 版本；
- CPU、BPU、内存、存储和温度；
- OpenExplorer、Runtime、编译器及板端工具是否存在；
- 当前项目和模型产物线索。

实时 `system_snapshot` 优先于静态文档。文档说某能力应当存在，但实机没有对应设备或命令时，应以实机证据为准。

### 12.2 知识检索

RDK 专业资料不会全部塞进系统 Prompt。Agent 按问题、板型和 RDK OS 使用 `rdk_docs_search` 检索，覆盖：

- 板卡规格、镜像、烧录、升级和恢复；
- OpenExplorer、PTQ/QAT、ONNX、HBM、Runtime 与 Model Zoo；
- Camera、Sensor、MIPI、VIN/ISP/PYM/GDC、编解码和显示；
- TROS/ROS 2、MCU、IPC/RPMSG、CAN；
- GPIO/I2C/SPI/UART/PWM、VDSP/GPU、驱动、网络和性能调试。

随包知识文档在正文附近标注 D-Robotics 官方文档或官方 GitHub 来源和核对日期。执行 `/knowledge` 可以查看当前知识来源；`/system-prompt` 查看实际角色与上下文组成。

### 12.3 安全定位

Hobot Code 是开发和诊断工具，不是电机、CAN、GPIO 或急停的硬实时控制面。影响人员、机械设备或不可恢复数据的动作必须有独立限位、急停、权限隔离和人工确认。

## 13. 模型部署与验收

### 13.1 Studio 工作流

**Deploy model** 向导会：

1. 在当前项目内有界扫描 ONNX、HBM 等候选产物；
2. 结合实机板型标记待转换、疑似匹配或明确不匹配；
3. 让用户选择产物、目标、Agent 模型和权限；
4. 创建可断线续跑的部署任务；
5. 在 Board monitor 中展示经过板端校验的结果。

扩展名、文件名和 march 只能提供候选提示，不能证明模型可以运行。

### 13.2 CLI 工作流

```bash
cd /path/to/project
hobot deploy inspect
hobot deploy start --goal deploy-and-validate model.onnx
hobot task attach <task-id>
hobot deploy status <task-id>
```

已有板端产物只做验证和 benchmark：

```bash
hobot deploy start --goal benchmark model.hbm
```

### 13.3 Verified deployment 的证据要求

新任务使用 schema v2。报告至少绑定：

- 实际板卡和 RDK OS；
- 源模型和部署产物 SHA-256；
- 冻结数据集、样本数量和量化前后数值指标；
- 模型 p50/p95、端到端 p50/p95、吞吐和迭代次数；
- 基线、峰值、结束阶段的 BPU、温度、系统内存和 ION/Hbmem；
- 预设阈值、最终结论和失败原因。

Agent 写出的 JSON 只是候选报告。`agentd` 会再次验证 schema、路径、工作区边界、普通文件类型、digest 和指标完整性后，Studio 才显示 Verified。

### 13.4 能力边界

- 候选扫描不运行模型。
- 生成部署方案不等于转换成功。
- 模型能加载不等于输出正确。
- 单次延迟不等于稳定性能。
- 数值相似度不等于任务精度；任务精度必须用对应数据集和真值评估。
- 示例中的 RegNet X5 和 RT-IGEV 流程是参考档案，不是所有模型的普遍性能承诺。

## 14. 工程上下文、记忆与质量控制

### 14.1 项目初始化

TUI 中运行：

```text
/init
```

可在缺失时创建：

- `AGENTS.md`：项目级开发约定；
- `.hobot/quality-gates.json`：质量门配置。

Agent 会在任务开始时读取受信项目的约定。不要把密钥写入这些文件。

### 14.2 质量门

`/gate` 管理完成任务前应执行的验证命令。质量门与工作区指纹绑定，避免测试通过后代码又变化却仍宣称完成。

常用操作包括 set、add、remove、timeout、clear 和 reload。项目配置写入 `.hobot/quality-gates.json`，会话覆盖只作用于当前会话。

### 14.3 持久记忆

`/memory` 管理以下作用域：

- user：当前 OS 用户；
- project：当前项目；
- board：当前板卡；
- session：当前会话。

记忆类型包括 preference、decision、fact、fix、instruction 和 note。写入会检查常见 secret；模型发起持久写入时需要审批。Side Agent 禁止写持久记忆。

### 14.4 持久目标

`/goal` 用于创建、查看和更新带预算的长期目标。目标完成状态需要满足协议约束，不能因为 token 接近上限就自动宣称完成。Side Agent 不能修改持久目标状态。

### 14.5 Hooks

`/hooks` 管理 `PreToolUse` 和 `PostToolUse` Hook。Hook 通过 JSON stdin/stdout 工作，具有超时和审计记录。

项目 `.hobot/hooks.json` 默认不执行，只有全局配置显式启用 `allowProjectHooks` 后才加载。Hook 是当前用户权限下的代码，使用前必须审查。

### 14.6 通知、LSP 和缓存

- `/notifications`：配置审批等待、完成和失败时的终端铃声或标题通知。
- `/lsp`：查看语言服务器状态。只有配置且命令存在时才启动，并受进程数、内存、空闲时间和请求超时限制。
- `/cache`：展示网关返回的 Prompt cache usage、累计命中率和最近一轮命中率。

## 15. Capabilities、Skills 与扩展

### 15.1 Capabilities 的含义

Studio 的 **Capabilities** 是板端能力库存，不是“所有条目都已安装且可执行”的承诺。常见状态：

| 状态 | 含义 |
|---|---|
| Available / Ready | 声明和运行前提均满足 |
| Declared / Discovered | 找到了配置或清单，不代表已加载、审查或执行 |
| Command missing | 配置引用的外部命令在当前板端 PATH 中不存在 |
| Optional | 可选能力，不影响核心 Agent 工作 |
| Unavailable | 当前版本、依赖、信任或任务上下文不满足 |

因此大量 **Command missing** 通常表示可选 Hook、LSP 或第三方工具没有安装，而不是 Hobot Code 核心损坏。默认界面会折叠不可用的可选能力，可用 **Show optional** 查看。

### 15.2 能力来源

库存可包含：

- Tools 与 MCP servers；
- Extensions 和 packages；
- Skills；
- Prompt templates 与 themes；
- Hooks；
- LSP；
- 当前任务中受信项目的 `.pi` 或 `.agents` 资源。

项目资源只在项目被信任后参与扫描。扫描有所有权、符号链接、目录权限、深度和数量限制。

### 15.3 Pi 扩展命令

```bash
hobot install <package-or-source>
hobot list
hobot config
hobot update --extensions
hobot extensions
hobot extensions --task <task-id>
```

`hobot update --extensions` 只更新 Pi 扩展，不更新 Hobot Code 产品本身。

第三方 Extension、Skill、Hook 和 LSP 可以影响模型上下文或以当前用户权限运行。Capabilities 清单不是安全审查，root 用户尤其需要先审查来源。

### 15.4 OpenExplorer LLM Skills

在 S600 配置完整的 OpenExplorer LLM 外部包后，Capabilities 会分别展示板端 Runtime、Skill Pack 和各个 Skill。Hobot Code 从包内 `.skillshare/skills/<name>/SKILL.md` 读取 Skill，以 `docs/03_SKILLS_CATALOG.md` 作为默认启用清单，不修改也不重新发布官方文件。

- `Available`：已进入客户目录并加载到任务；不表示厂商或 Hobot Code 已完成真实模型验证。
- `Optional · off`：包内存在但未进入客户目录，默认不加载。
- `OpenExplorer Skills · Partially inspected`：实际目录和客户目录数量不一致；展开条目可查看来源与证据说明。

当 Skill 进入量化、模型适配、校准、BC 或 HBM 编译阶段时，Agent 会要求选择 S600 可直连的 x86_64 SSH 构建机。输入预先配置在板端 `~/.ssh/config` 中的别名最方便。Hobot Code 会先探测远端架构和 CUDA；Ask 模式对远端访问进行审批，Developer 模式直接执行普通远端命令，但两种模式都会继续拦截删除、系统修改等破坏性操作。主机选择只保存在当前任务，不会回写官方 Skill。

审批不再提供「Allow this exact call for this task」。对可安全限定范围的请求，界面改为显示语义明确的选项，例如 **Allow network for this task**。它只放开当前任务的通用网络检查，不会放开 root、文件、硬件或破坏性命令权限。

选择构建机不会自动上传外部包、源码或模型。请在对话中提供构建机上的 OpenExplorer 工作目录、模型路径和输出目录；需要使用包内脚本的 Skill 还要求该脚本在构建机上可访问。

如果提示无法解析主机或认证失败，应先在 S600 终端验证 `ssh -T <别名>`，检查板端 DNS、端口、`known_hosts`、专用私钥和构建机 `authorized_keys`。Mac 能连接并不能证明 S600 能连接。

## 16. 图片、文档与链接

### 16.1 图片

Studio 支持 JPEG、PNG、WebP 和 GIF：

- 每条消息最多 4 张；
- 大图在 Mac 本地进行有界缩放和压缩；
- 只有模型明确声明 image input 时附件按钮才可用；
- 图片通过已有 SSH/RPC 通道发送，不创建公开上传 URL；
- 任务事件只保存文件名和 MIME 摘要，不复制完整图片数据。

### 16.2 文档

当前版本不支持直接把 PDF、Word 或任意文件作为 Studio 对话附件上传并解析。不要把“图片可上传”理解成通用文档上传。

临时方案是在受信项目目录中放置文件，再明确要求 Agent 使用板端已有的解析工具；这是否可行取决于文件格式、工具是否安装以及任务权限。

### 16.3 链接

Agent 回复中的 HTTP/HTTPS 链接由 Mac 默认浏览器打开。板端 TUI 是否能直接打开链接取决于当前终端，不应假设有桌面浏览器。

## 17. 诊断、监控与技术支持

### 17.1 只读体检

```bash
hobot doctor
hobot doctor --json
```

Doctor 不调用模型、不读取对话或项目内容，也不创建支持文件。结果分为：

- **Healthy**：已检查项正常；
- **Attention**：可继续使用，但存在应处理的警告；
- **Action required**：核心配置或运行条件不满足。

检查范围包括配置是否被 daemon 加载、模型、发布身份、板型、私有目录、隔离、资源和任务生命周期。

### 17.2 安全修复

Doctor 只提供两个白名单修复，并且必须显式确认：

```bash
hobot doctor --repair private-runtime-permissions --yes
hobot doctor --repair restart-daemon --yes
```

它不会自动修改系统版本、依赖、凭据、项目文件或未知路径。

### 17.3 支持包

```bash
hobot diagnose
hobot diagnose --json
```

支持包包含板型、RDK OS、资源摘要、BPU/Hbmem 状态、固定工具可用性、daemon 版本、健康检查、任务状态统计和恢复建议。

支持包不包含：

- 对话和 session 内容；
- 系统 Prompt 或用户 Prompt；
- 工具输入输出；
- 环境变量和凭据；
- 项目文件和工作区内容；
- 原始任务日志。

主机名、路径、任务 ID 和错误原文会被替换或归类。板端只保留最近 5 份。任何自动脱敏都不能替代分享前的人工检查。

### 17.4 常用状态检查

```bash
hobot --version
hobot daemon status
hobot task list --all
hobot workspace writes
hobot doctor
```

Studio 的 Board monitor 适合持续观察；Doctor 用于结构化就绪性检查；Diagnose 用于向支持人员交付最小化证据。三者用途不同。

## 18. 版本、更新、回滚与卸载

### 18.1 检查和执行更新

```bash
hobot update --check
hobot update
hobot update --version <version>
```

显式降级：

```bash
hobot update --version <version> --allow-downgrade
```

更新时如果存在前台 TUI、persistent、自动化、Studio bridge 或活动后台任务，安装器会拒绝继续。先处理或停止相关工作，避免升级中破坏会话。

Mac Studio 右上角的版本号会打开 **Version & updates**。该页面独立检查两个发行面：

- **Studio for Mac** 从固定的 Hobot Code GitHub stable Release 检查新版本。只有版本化 ARM64 DMG、同名 SHA-256 文件和官方 Release 地址同时匹配时才显示下载按钮；点击后由默认浏览器直接下载对应 DMG。安装仍由 macOS 和 Gatekeeper 控制，更新 Studio 不会停止板端任务。
- **Board service** 通过当前 SSH 连接调用板端事务更新。存在活动任务时只阻止板端按钮，不影响 Studio 更新。
- 两侧都有新版本时显示 **Update board, then Studio**。它先完成板端校验、更新和自动重连，再下载 Studio DMG；活动任务结束前该按钮不可用。

Mac 应用目前不会在运行中自行覆盖 `/Applications/Hobot Code.app`。下载新 DMG 后，将新版拖入 Applications 并重新打开；未签名的本地开发构建不应被当作公开更新来源。只更新板端不会替换 Mac 应用，所以右上角仍可能显示旧 Studio 版本。

### 18.2 回滚

```bash
sudo /usr/local/sbin/hobot-rollback
```

回滚要求安装器保留的完整前一版本备份。首次安装通常没有回滚点。回滚保留用户配置和状态，但仍应先停止所有 Hobot Code 进程。

### 18.3 卸载

保留配置、会话、记忆、目标和备份：

```bash
hobot uninstall
```

同时永久清除当前安装用户的 Hobot Code 数据和备份：

```bash
hobot uninstall --purge
```

`--purge` 不可恢复，执行前必须确认目标 OS 用户和 home 目录。卸载不会删除项目代码。

## 19. 数据目录、隐私与安全边界

### 19.1 安装位置

| 路径 | 内容 |
|---|---|
| `/usr/local/bin/hobot` | 用户入口 |
| `/usr/local/lib/hobot-code` | 当前产品运行时 |
| `/usr/local/lib/hobot-code-backups` | 可用的历史版本备份 |
| `/usr/local/sbin/hobot-rollback` | 回滚命令 |

### 19.2 用户配置与状态

| 路径 | 内容 |
|---|---|
| `~/.config/hobot-code/hobot.env` | 内置模型凭据和环境配置 |
| `~/.config/hobot-code/agent/` | settings、models、providers、权限、hooks、通知和 LSP |
| `~/.local/state/hobot-code/` | sessions、memory/goals DB、审计、agentd 日志、socket、tasks、模型资格和支持包 |

目录默认收紧为 `0700`，敏感文件为 `0600`。root 和普通用户拥有不同的配置与任务，不能因为同一台板上都执行 `hobot` 就假设它们共享模型或历史。

### 19.3 模型数据流

完成任务所需的系统上下文、对话和工具结果会发送给所选模型 Provider。使用外部 Provider 前，应确认其数据政策符合组织要求。

不要把 Token、私钥或不必要的敏感数据放进 Prompt、项目说明、记忆、Hook 输出或 issue。怀疑泄漏时应在 Provider 侧撤销并轮换，删除本地记录并不能让已泄漏凭据失效。

### 19.4 sandbox 的真实边界

Hobot Code 的 sandbox 用于减少误操作和限制 Agent 进程树的写入、设备及 Linux capabilities，但它不是用于运行任意恶意代码的完整 VM：

- 宿主只读文件仍可能被读取；
- 同一 OS 用户的其他宿主进程不与 Agent 完全隔离；
- 已获当前用户权限的恶意扩展、Hook、LSP 或 Shell 可以绕过 Agent 意图层；
- Model only 仍会把任务上下文发送给所选模型服务。

高价值环境应再使用独立 Linux 用户、文件权限、容器、网络出口控制或系统级强制访问控制。

## 20. 常见问题与恢复方法

### 20.1 Board access 或 Network 突然不能点击

任务的 worker 仍存在。Ready 是“等待输入”，不是“进程已停止”。在 **Task settings** 中点击 **Stop Agent**，再修改并 Resume；也可新建对话并在首条消息前设置。

### 20.2 Model only 不能选择

可能原因：

- 当前 Provider 尚未接入板端模型代理；
- 板端受管凭据缺失；
- 选择了 No sandbox；
- 板端版本不支持该能力。

切换到内置 D-Robotics 或受支持的受管 Provider，并选择 Read only、Workspace 或 Board hardware。

### 20.3 Developer 仍然询问 Allow bash

检查审批提示中的 **Reason**：

- `writes to a protected system path`：命令可能写工作区外或关键路径；
- `permission policy requires confirmation`：该工具规则仍是 ask；
- root mutation 提示：root 仍处于逐次确认模式；
- destructive/network/hardware：命中硬安全检查。

root 用户可执行 `/permissions root policy` 和 `/permissions preset developer`。Developer 默认允许普通外联，但不会关闭破坏性、系统路径和硬件保护；未知 MCP 仍保守询问。

### 20.4 `/permissions set * allow` 后仍然询问

通配规则只改变普通工具策略，不覆盖硬安全边界，也不能把 sandbox 外的路径或设备变成可访问。查看具体 Reason，而不是把任意审批都视为规则失效。

### 20.5 “background task limit reached” 或任务一直 queued

新版会把请求排队，并优先挂起最久未使用的 Ready worker。如果所有任务都在 running 或 waiting：

```bash
hobot task list --all
hobot task approvals <task-id>
hobot task stop <unused-task-id>
```

先处理审批或停止不再需要的任务。不要重复提交同一需求。

### 20.6 Resume 报 “no resumable Pi session”

该任务没有安全可用的 Pi session，常见于首次消息未真正启动、旧任务格式、session 被清理或失败发生在持久化之前。使用 **New session** 或：

```bash
hobot task restart <task-id> -- "继续完成原任务，并先检查当前工作区状态"
```

### 20.7 Resume 后模型报 tool_calls 缺少 tool 响应

这通常说明旧 session 在工具调用中间被中断，历史链不完整。新版恢复会校验并修复可安全闭合的历史；仍失败时不要反复 Resume，改用 Restart/New session，并让 Agent 先检查当前工作区和进程状态，避免重复副作用。

### 20.8 Board diagnostics format incompatible

1. 检查 Studio 和板端 major/minor 是否一致。
2. 板端执行 `hobot doctor --json`。
3. Studio 点击 **Sync board now** 并重新连接。
4. 仍异常时更新板端与 Mac 应用到同一 Release。
5. 保存支持包用于排查。

### 20.9 Capabilities 显示很多 Command missing

先关闭 **Show optional**，确认缺失的是不是可选 LSP、Hook 或第三方工具。只有核心命令缺失或实际任务依赖该命令时才需要安装。Capabilities 负责诚实展示依赖，不会为了界面好看把缺失项伪装成可用。

### 20.10 断线后任务是否继续

- Studio/agentd 后台任务：继续。
- `hobot persistent`：继续。
- 普通前台 `hobot`：依赖 SSH 终端，断开后不保证继续。
- 板卡重启、断电、OOM 或程序崩溃：实时进程会中断，需按恢复证据 Resume 或 Restart。

### 20.11 不能复制 TUI 内容

先试 `/copy`。拖选无效时使用 `Shift` + 鼠标拖选，或在终端设置中允许 OSC 52。远程复制最终还受本地终端和 VS Code 设置控制。

### 20.12 Side Agent 卡住或主输入不能使用

确认焦点所在区域：点击主/侧半屏切换，或用 `Ctrl+Shift+Left/Right`。若审批弹窗存在，先完成或取消审批。滚轮会滚动指针所在区域。需要关闭侧边时按 `Ctrl+D`。

### 20.13 Mac 仍显示旧版本

板端 `hobot update` 只升级板端。点击 Mac 右上角版本号，在 **Studio for Mac** 中选择 **Check Studio**；发现新版后点击下载并用 DMG 替换 Applications 中的旧版。若仍显示旧版本，确认启动的是 `/Applications/Hobot Code.app`，而不是 Downloads 中的旧副本。

### 20.14 模型报错后为什么仍显示 Working

Hobot Code 会对临时网关、限流和连接错误最多自动重试五次。Studio 在重试期间显示 **Automatic retry n/5**，成功后保留一条简短的恢复记录，不再显示中间失败卡；只有重试耗尽后才显示最终错误和恢复操作。旧任务记录会按事件当时的上限显示，例如 `1/3`，新任务使用 `1/5`。

### 20.15 Agent 说会定时汇报，是否真的会执行

从支持 `schedules.v1` 的板端版本起，只有成功创建板端计划后，周期汇报才会实际发生。计划由 `agentd` 持久保存，支持一次性 RFC3339 时间（必须含时区）或 1 分钟至 30 天的固定间隔；每块板最多 100 个计划。Studio 右上角的时钟入口可查看、创建、暂停、立即运行或删除计划。旧板端会隐藏入口并提示升级。

计划始终绑定一个已有的普通主任务，复用该任务当前的 Pi session、模型、权限、sandbox、网络和工作目录。Side Agent 与编辑分支不能作为目标。任务正处于运行、等待审批、启动、停止或排队时，多个到期事件只合并为一轮，空闲后再执行；断电或重启错过的时间也最多合并一次，不会补跑一串历史任务。计划在安全落盘 claim 后才派发，因此不会因为重启无声重放旧工具调用。

停止任务只停止当前一轮，不会取消下一次计划；使用 **Pause** 或 **Delete** 才会停止未来触发。删除或归档仍被关联的任务会被拒绝，需先删除关联计划。计划列表和默认详情不会返回 Prompt；显式 `--details` 才显示。支持包、错误和日志不包含计划 Prompt。

恢复一个间隔计划时会保留暂停前的下次时间；若它已经到期，板端只合并为一轮立即待执行，不会补跑多个遗漏间隔。

终端等价命令如下：

```bash
# 在运行中的主 Agent 内，任务 ID 和名称均可自动确定
hobot schedule create --every 30m --prompt "检查当前任务，只有有新进展时汇报。"

# 在终端中管理其他任务时，显式指定任务 ID；名称可选
hobot schedule create --name "一次检查" --task <task-id> --at 2026-08-18T09:00:00+08:00 --prompt "检查模型转换结果并汇报。"
hobot schedule list --all
hobot schedule show <schedule-id> --details
hobot schedule pause <schedule-id>
hobot schedule resume <schedule-id>
hobot schedule run <schedule-id>
hobot schedule delete <schedule-id> --yes
```

Agent 不应仅口头承诺周期汇报：需要该能力时应先创建计划并确认返回的计划 ID；可在其任务 worker 内使用同一组 `hobot schedule` 命令管理自身计划，但不能管理其他任务。

## 21. CLI 命令参考

### 21.1 主命令

```text
hobot tui [--sandbox review|workspace|system|off]
          [--network shared|model-only|offline] [-- PI_OPTIONS...]
hobot daemon start|status
hobot daemon stop|restart [--force]
hobot bridge --stdio
hobot doctor [--json] [--repair ACTION --yes]
hobot diagnose [--json]
hobot extensions [--json] [--task ID]
```

`bridge --stdio` 主要供 Studio 使用，不是普通用户的交互入口。

### 21.2 Provider 与模型

```text
hobot provider list [--json]
hobot provider add PROVIDER --base-url URL --model MODEL [options]
hobot provider rotate PROVIDER [--token-stdin] [--yes-shared]
hobot provider remove PROVIDER --yes [--keep-credential]

hobot model check [--force] [--json] PROVIDER/MODEL
hobot model probe [--force] [--json] PROVIDER/MODEL
hobot model runtime-probe [--json] PROVIDER/MODEL
hobot model rdk-probe [--profile ID] [--json] PROVIDER/MODEL
hobot model profiles [--json] PROVIDER/MODEL
hobot model status [--json] PROVIDER/MODEL
```

### 21.3 部署与工作区

```text
hobot deploy inspect [--cwd DIR]
hobot deploy start [--cwd DIR]
                   [--goal deploy-and-validate|benchmark]
                   [--profile PROFILE] [--name NAME]
                   [--model PROVIDER/MODEL]
                   [--permissions ask|developer]
                   [--sandbox system|off] ARTIFACT
hobot deploy status TASK_ID

hobot workspace inspect [DIR]
hobot workspace list
hobot workspace writes
hobot workspace delivery TASK_ID
hobot workspace apply TASK_ID --yes
hobot workspace cleanup TASK_ID --yes
```

### 21.4 后台任务

```text
hobot task start [--name NAME] [--cwd DIR]
                 [--workspace shared|worktree]
                 [--model PROVIDER/MODEL]
                 [--permissions review|ask|developer]
                 [--sandbox review|workspace|system|off]
                 [--network shared|model-only|offline]
                 [--trust-project] -- PROMPT
hobot task list [--all]
hobot task show TASK_ID [--details]
hobot task logs TASK_ID [--after SEQUENCE] [--follow]
hobot task attach TASK_ID [--after SEQUENCE | --replay-all]
hobot task send TASK_ID [--] PROMPT
hobot task abort TASK_ID
hobot task respond TASK_ID REQUEST_ID yes|no|cancel|VALUE
hobot task approvals TASK_ID [--details]
hobot task resume TASK_ID [-- PROMPT]
hobot task restart TASK_ID [--] PROMPT
hobot task rename TASK_ID NAME
hobot task model TASK_ID PROVIDER/MODEL
hobot task permissions TASK_ID review|ask|developer
hobot task sandbox TASK_ID review|workspace|system|off
hobot task network TASK_ID shared|model-only|offline
hobot task archive|unarchive TASK_ID
hobot task delete TASK_ID --yes
hobot task stop TASK_ID
```

以 `-` 开头的 Prompt 必须放在 `--` 后，避免被识别为命令参数。

## 22. TUI 命令与快捷键参考

### 22.1 会话与界面

| 命令 | 作用 |
|---|---|
| `/new` | 新建会话 |
| `/resume` | 选择并恢复会话 |
| `/tree` | 查看会话分支 |
| `/fork` | 从当前上下文创建分支 |
| `/compact` | 压缩长会话上下文 |
| `/copy` | 复制最近一条 Agent 回复 |
| `/btw <任务>` | 打开 Side Agent |
| `/detach` | 离开 persistent TUI，保留板端进程 |
| `/hotkeys` | 查看完整快捷键 |
| `/quit` | 退出 |

### 22.2 模型与配置

| 命令 | 作用 |
|---|---|
| `/model` | 选择模型 |
| `/settings` | 打开设置 |
| `/scoped-models` | 管理作用域模型 |
| `/login` | 使用 Pi Provider 登录 |
| `/cache` | 查看 Prompt cache usage |

### 22.3 RDK 与工程能力

| 命令 | 作用 |
|---|---|
| `/rdk` | 查看板卡专家摘要 |
| `/doctor` | 查看会话内诊断 |
| `/knowledge` | 查看 RDK 知识与来源 |
| `/system-prompt` | 查看实际系统 Prompt 组成 |
| `/init` | 初始化项目约定和质量门 |
| `/gate` | 管理质量门 |
| `/memory` | 管理持久记忆 |
| `/goal` | 管理持久目标 |
| `/hooks` | 管理工具 Hook |
| `/notifications` | 管理终端通知 |
| `/lsp` | 查看语言服务器能力 |
| `/permissions` | 查看和修改工具权限策略 |

### 22.4 快捷键

| 快捷键 | 作用 |
|---|---|
| `Escape` | 中断当前 turn；Side Agent 空闲时关闭 |
| `Ctrl+D` | 输入为空时退出；侧边区域中关闭 Side Agent |
| `Ctrl+T` | 显示或隐藏 thinking |
| `Ctrl+Shift+Right` | 焦点切到 Side Agent |
| `Ctrl+Shift+Left` | 焦点返回主 Agent |
| `Ctrl+PageUp/PageDown` | 键盘滚动侧边历史 |

运行中的版本和终端可能提供更多 Pi 快捷键，以 `/hotkeys` 的实时结果为准。

## 23. 术语表与相关文档

### 23.1 术语表

| 术语 | 说明 |
|---|---|
| **Pi** | Hobot Code 所基于并适配的 Agent 运行时；产品界面和角色为 Hobot Code |
| **BPU** | 地瓜机器人板卡上的 AI 加速单元 |
| **ION/Hbmem** | BPU 与多媒体等模块使用的板端物理内存体系 |
| **HBM** | OpenExplorer 工具链生成的板端模型产物；不要与内存术语混淆 |
| **Protocol** | Studio 与 agentd 的请求/响应兼容版本 |
| **Event schema** | 任务流式事件的数据结构版本 |
| **Qualification** | 对模型路由、运行时和 RDK 工作流的结构化验证证据 |
| **Support bundle** | 不含会话和项目内容的脱敏诊断文件 |

### 23.2 进一步阅读

- [配置说明](configuration.md)：环境变量、权限、记忆、目标、Hook、通知和 LSP 的高级配置。
- [agentd 协议](agentd-protocol.md)：后台任务、事件、恢复和 Studio RPC 的精确定义。
- [兼容矩阵](compatibility.md)：版本、事件 schema 和板卡验证基线。
- [模型能力契约](model-capabilities.md)：模型 reasoning、图片和代理能力的声明与校验。
- [缓存效率](cache-efficiency.md)：缓存统计口径和诊断方法。
- [安全说明](../SECURITY.md)：威胁模型、凭据、sandbox、Side Agent 和机器人安全边界。
- [发布与来源验证](releasing.md)：发行产物、校验值和 provenance。

遇到问题时，建议依次执行：确认版本一致 → `hobot doctor` → 查看任务状态与审批 → 重连/同步 → 生成 `hobot diagnose` 支持包。不要在公开 issue 中上传有效凭据、私有项目或未检查的诊断数据。
