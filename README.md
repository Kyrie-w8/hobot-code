# Hobot Code

[![CI](https://github.com/bryant-w/hobot-code/actions/workflows/ci.yml/badge.svg)](https://github.com/bryant-w/hobot-code/actions/workflows/ci.yml)

面向地瓜机器人 RDK 的开发 Agent。Hobot Code 在板端提供原生 TUI 和常驻任务服务，在 Mac 上提供同名桌面应用；两个入口共享模型、工具、审批、Skills、会话和 RDK 专业知识。权限判定、工具执行和模型凭据始终留在 RDK 上。

第一次使用请从[完整用户手册](docs/user-guide.md)开始；该文档系统覆盖安装配置、Studio 与 TUI、项目任务、模型接入、权限安全、Side Agent、RDK 能力、诊断更新和故障恢复。

## 核心能力

- **RDK 开发专家**：识别 X5、S100、S600 与 RDK OS，按需读取 BPU、温度、内存、存储和工具链状态，并从 27 个带官方来源的专业主题中检索板型匹配的知识。
- **模型部署闭环**：发现 ONNX、HBM 等模型产物，生成板型方案并启动可断线续跑的部署任务；验收报告统一记录精度、延迟、资源、环境和产物 SHA-256。
- **双端一致体验**：板端 TUI 与 Mac Studio 连接同一 `agentd`、会话和权限控制面，支持流式 thinking、工具时间线、审批、图片输入和断线重连。
- **持久与并行任务**：`hobot persistent` 保留完整终端，后台任务在 SSH 断开后继续运行；Side Agent 继承稳定上下文并独立多轮工作，不污染主对话。
- **开放模型与扩展**：内置 D-Robotics Kimi K3、Qwen 3.8 Max、GLM 5.2、DeepSeek V4 Flash 和 Pro，同时兼容 Pi Provider、MCP、packages、extensions、Skills、Prompt templates 和 themes。
- **缓存效率可观测**：直接读取网关 usage，展示命中率、冷热输入和前缀稳定性；S100 实机验证中，GLM 5.2 与 Kimi K3 的稳定长前缀均达到 **99%+** 热轮命中率。
- **工程上下文与质量门**：项目初始化、持久记忆、持久目标、Hook 和资源受限 LSP 共同保留开发约定；完成状态与工作区指纹和验证命令绑定。
- **板端安全边界**：前台 TUI 与后台任务默认使用板端 OS 文件、设备和 capability 隔离，并叠加权限策略、项目信任、危险 Shell 与外联客户端检测、模型凭据隔离和硬件租约；桌面客户端无法绕过。
- **诊断与可支持性**：`hobot doctor` 只读检查首启配置、板型、隔离、资源和任务状态；只有显式执行 `hobot diagnose` 才生成私有、脱敏、带校验值的支持包。

### 实机验证

以下结果读取 D-Robotics 网关返回的真实缓存 usage，不使用字符数或本地 token 估算。日常基线保留完整 RDK 扩展与稳定的真实工具契约；稳定前缀结果用于衡量模型链路上限。

| 模型 | 日常多轮热请求 | 稳定长前缀热请求 |
|---|---:|---:|
| Kimi K3 | **94.58%** | **99.46%** |
| GLM 5.2 | **91.98%** | **99.76%** |
| DeepSeek V4 Flash | **95.10%** | **99.68%** |

数据来自 RDK S100 实机连续多轮请求。命中率受会话长度、工具变化、上下文压缩和网关路由影响，不是服务等级承诺；测量口径与适用边界见[缓存效率](docs/cache-efficiency.md)。

## 支持平台

| 板卡 | 文档线路 | 运行架构 |
|---|---|---|
| RDK X5 | RDK OS 3.x | Linux ARM64 |
| RDK S100 | RDK OS 4.x | Linux ARM64 |
| RDK S600 | RDK OS 5.x | Linux ARM64 |

启动时会根据 device tree 与 `/etc/version` 选择资料线路。文档描述能力边界，`system_snapshot` 提供当前实机证据；两者不一致时，以实机状态为准。

## RDK 专业知识

知识库不是塞进系统 Prompt 的静态长文本，而是由 `rdk_docs_search` 按板型、RDK OS 和问题检索。当前覆盖：

| 领域 | 内容 |
|---|---|
| 板卡与系统 | X5、S100、S600 硬件边界，镜像、烧录、升级、恢复、系统构建与 bring-up |
| AI 部署 | OpenExplorer/OE、PTQ/QAT、ONNX、BPU Runtime、HBM/HBMEM、Model Zoo、LLM/VLM/VLA 与验收 |
| 视觉与多媒体 | Camera、Sensor、MIPI、VIN/ISP/PYM/GDC、编解码、显示和端到端 pipeline |
| 机器人与实时域 | TROS/ROS 2、Humble/Jazzy、MCU、IPC/RPMSG、CAN 与安全控制边界 |
| 系统工程 | 交叉编译、40-pin、GPIO/I2C/SPI/UART/PWM、VDSP/GPU、驱动、存储、网络和性能调试 |

每篇资料在正文末尾就地列出 D-Robotics 官方文档或官方 GitHub 来源。发布校验会拒绝未登记文档、缺失核对日期、来源不足、正文未引用来源、非官方域名和疑似凭据；资料中的版本说明仍不能替代当前板端的实时检查。

基础运行时自包含，使用内置 Agent 能力时，板端无需另外安装 Node.js、Bun、Go、Python 或容器。完整 TUI 的断线续跑需要 `tmux`；无界面后台任务由随包安装的 `agentd` 托管，不依赖 `tmux`。第三方 Pi package 可能需要系统中的 `git`、`npm` 或自定义 `npmCommand`，用户配置的 Hook 与 LSP 也需要对应外部命令。

## 快速开始

### 1. 一条命令安装

在 RDK X5、S100 或 S600 上执行：

```bash
curl -fsSL https://github.com/bryant-w/hobot-code/releases/latest/download/hobot-install.sh | sh
```

板端需要先安装 `curl`。安装器只接受 Linux ARM64，并检查 device tree 中的 RDK 型号；它通过 HTTPS 下载版本化归档，严格核对 SHA256、归档根目录和文件类型，再调用事务安装器。普通用户会通过 `sudo` 安装程序，但配置、会话和状态仍属于发起安装的用户；root 直接执行时默认安装给 root。

安装完成后先运行 `hobot setup` 配置模型，再在项目目录执行 `hobot`。如果尚未配置可用 Provider，直接运行 `hobot` 会在创建空白会话前给出配置指引；`hobot doctor` 可随时做零副作用检查，显式 `hobot tui` 仍可用于不依赖模型的界面检查。

`latest` 始终安装已经公开发布的稳定版本；`main` 分支可能包含尚未发布的下一版功能。安装指定的已发布版本：

```bash
curl -fsSL https://github.com/bryant-w/hobot-code/releases/latest/download/hobot-install.sh \
  | sh -s -- --version <version>
```

无法从板卡访问 GitHub 时，可从 [GitHub Releases](https://github.com/bryant-w/hobot-code/releases) 下载版本化归档和同名 `.sha256`，传入板卡后离线安装：

```bash
cd /tmp
set -- hobot-code-*-linux-arm64.tar.gz
[ "$#" -eq 1 ] && [ -f "$1" ] || { echo "请确保 /tmp 中只有一个 Hobot Code 发行包" >&2; exit 1; }
package=$1
sha256sum -c "$package.sha256"
tar -xzf "$package"
cd "${package%.tar.gz}"
sudo ./install.sh  # root 直接登录时使用 ./install.sh
```

安装后可用 `hobot version`（也支持 `hobot --version`、`hobot -v`）只读查看板端版本；这些命令不会启动 Agent 对话。

### Mac 桌面应用

新任务会由板端检查工作区。干净且已有提交的 Git 项目在 Studio 中默认使用私有的独立 worktree；普通目录或存在未提交、未跟踪内容时保持共享，避免新 worktree 静默缺少本地内容。Side Agent 和编辑历史分支继承主任务的同一工作区。共享模式的 Changes 不会归因给单个 Agent，隔离模式在标题中明确标记 **Isolated**。

从 [GitHub Releases](https://github.com/bryant-w/hobot-code/releases) 下载 `hobot-code-<version>-macos-arm64.dmg`，打开后将 **Hobot Code** 拖入 Applications。安装后可点击右上角版本号分别检查 Studio 与板端更新；Studio 只接受固定官方 Release 中版本号、ARM64 DMG 和 SHA-256 资产完整匹配的稳定版本，板端更新仍受活动任务检查、事务安装和失败回滚保护。首次启动时添加板卡名称、IP、SSH 用户与可选私钥路径；应用使用 macOS 系统 OpenSSH 和 `known_hosts`，不保存 SSH 密码或板端模型密钥。

桌面应用连接时会协商协议、event schema、功能能力、产品版本、构建身份、板型与 RDK OS。最低要求是 event schema 2、任务生命周期/分页能力和 `hobot bridge --stdio`；不满足硬条件时拒绝半兼容连接，可降级能力则在板卡详情中明确提示。schema 3 持久化用户消息并将 thinking、工具调用与最终回答组织成稳定轮次；schema 4 增加结构化 item、工具预览和可恢复的任务排队。0.22.4 及之后的板端还会向详情栏提供只读硬件快照，并对过热、低内存、低磁盘、BPU 或验证工具缺失给出就地提示；0.23.0 增加逐核 BPU、ION/Hbmem 与结构化模型部署能力，0.24.0 增加一键诊断与支持包下载。退出桌面应用、Mac 休眠或 VPN 短暂断开只会中断界面连接，`agentd` 托管的任务仍在板端继续；重新连接后会按事件序号重放缺失输出。详细边界见[兼容矩阵](docs/compatibility.md)。

消息输入框使用 `Enter` 发送，`Shift+Enter` 换行；发送后同一按钮原位切换为停止，中文输入法确认候选词时不会误触发发送。网络中断时草稿保留但禁止误发送，按钮原位切换为重连。编辑历史用户消息会保留该消息之前的上下文，用修改后的内容替换原消息，并在同一主对话中隐藏后续旧时间线，不会创建可见的 Side Agent；原消息含图片时会明确要求重新附加或确认移除。左侧项目可以折叠，每个项目可创建多个对话；新对话先浏览板端目录、选择现有工作区或创建文件夹，再从首条指令生成可修改标题。后台任务完成、失败或等待审批时会显示未读标记和应用内提醒。对话和 Side Agent 作为项目子项展示，每一项的 `…` 菜单可删除单个对话或移除项目中的全部对话，但不会删除板端工作目录。任务标题右侧的 **Side Agent** 会从当前已稳定上下文创建独立多轮分支，多个 Side Agent 始终作为主对话的同级分支显示。任务开始后，标题栏的 **Changes** 可读取绑定工作区的 Git 文件状态与有界文本 Diff；未跟踪文件只列名称、不传内容。由于主任务、Side Agent 和人工操作可以共享目录，该视图只称为当前工作区快照，不会把所有改动错误归因给当前 Agent。输入框底部展示内置 D-Robotics 和显式受管模型，并可在任务 Ready 或停止后切换；停止后的选择会在下次 Resume 生效。终态任务有安全 session 时显示 Resume，没有 session 时显示 New session，并在同一任务记录中明确启动全新会话。标题栏版本入口显示 Studio、板端服务和兼容性；回复中的 HTTP/HTTPS 链接会交给 Mac 默认浏览器打开。

Studio 添加板卡时会先验证 SSH、协议、能力与实际板型，成功后才保存；错误地址不会污染板卡列表。已保存板卡可直接编辑或删除，删除连接不会停止板端任务。版本页分别提供 Studio 下载和板端事务升级；两侧都有新版时可以先升级板端并重连，再打开 Studio 下载。执行板端升级时仍由板端拒绝活跃 Agent、Studio bridge、并发安装和隐式降级，桌面端不能绕过这些保护。

输入框底部的权限菜单为当前任务独立选择三档板端策略：**Review only** 禁止变更，**Ask for changes** 在变更前确认，**Developer** 放行非 root 会话的日常 Shell 与工作区编辑。板卡以 root 连接时，变更工具仍要求确认。审批时可选择仅放行一次、仅在当前任务记住这一次完全相同的工具调用，或拒绝；危险 Shell 不提供记忆授权。工作区外写入和受保护系统路径仍需要单独处理。权限模式切换仅允许在 Ready 或任务停止后进行。目标用户必须已经存在并拥有可解析的 home 目录。

桌面端在当前模型明确声明支持图像输入时，允许在新任务和后续消息中附加 JPEG、PNG、WebP 或 GIF 图片；模型能力未知时按纯文本处理。大图会在 Mac 本地缩放压缩，每条消息最多 4 张、编码前合计不超过 1 MiB；图片通过既有 SSH/RPC 通道直接写入板端会话，不创建公开上传地址，事件日志只保留文件名和 MIME 摘要。Studio 和 agentd 会分别校验同一套[模型能力契约](docs/model-capabilities.md)，客户端不能绕过。PDF、Word 等文档附件尚未开放，也不会被静默当作纯文本发送。

新任务页提供板卡诊断、模型部署、Camera pipeline、TROS 和 BPU 性能验证入口。板卡诊断等通用入口只会预填一条可编辑的专业任务。0.23.0 及之后的板端上，**Deploy model** 会打开部署向导：它在当前项目内有界扫描模型候选，结合实机板型标注转换需求或疑似不匹配，用户选择产物和目标后才创建持久任务。任务仍走板端审批；0.23.1 要求 schema-v2 报告包含量化前后数值精度、模型与端到端延迟分布、资源采样和温度/内存限制，只有这些证据、板型、产物路径和 SHA-256 均经守护进程复核后，Studio 才显示为 Verified deployment。RDK X5 的 RegNet-X-400MF 目标闭环与固定验收命令见[部署说明](docs/regnet-x5-deployment.md)；[RT-IGEV 说明](docs/rt-igev-x5-deployment.md)保留了复杂立体模型未达到实时阈值时的方案筛选边界。

终端使用同一套板端部署协议：

```bash
cd /path/to/project
hobot deploy inspect
hobot deploy start --goal deploy-and-validate model.onnx
hobot task attach <task-id>
hobot deploy status <task-id>
```

空闲或已停止的后台任务可直接调整下一轮使用的模型和权限，无需转到 Studio：

```bash
hobot task model <task-id> drobotics/kimi-k3
hobot task permissions <task-id> review|ask|developer
hobot task sandbox <task-id> review|workspace|system|off
hobot task network <task-id> shared|model-only|offline
```

`inspect` 只扫描候选，不运行模型；`start` 创建可断线续跑的持久任务；`status` 读取经过板端复核的验收状态。未声明目标的编译产物会显示为待验证，明确属于另一块板或 march 的产物会被拒绝。

### 只读体检与技术支持包

安装后或遇到启动、连接、任务恢复、BPU 状态和资源异常时，先执行只读体检：

```bash
hobot doctor
```

命令会启动或连接当前用户的 `agentd`，但不会调用模型、读取对话或项目内容，也不会创建支持文件。它返回 **Healthy**、**Attention** 或 **Action required**，并检查当前配置是否已被服务加载、是否存在可用模型、发布身份、板型、私有运行目录、隔离、资源和任务生命周期。Studio 标题栏的 Board readiness 使用同一协议。

体检只会提供两个严格白名单动作：把当前用户拥有的已知运行目录或日志收紧为 `0700`/`0600`，以及在没有运行中或排队任务时重启 `agentd` 以加载新配置。两者都必须由用户明确确认；符号链接、错误所有者、错误文件类型、凭据、配置内容、系统版本、依赖和资源问题永不自动修改。

```bash
hobot doctor --repair private-runtime-permissions --yes
hobot doctor --repair restart-daemon --yes
```

需要把证据交给技术支持时，再显式生成支持包：

```bash
hobot diagnose
```

`diagnose` 复用相同检查，并写入私有状态目录中的 `0600` 文件 `hobot-code-support-*.json`。文件包含板型和 RDK OS、负载、内存、磁盘、温度、BPU/Hbmem 状态、固定 RDK 工具是否可用、守护进程版本与资源上限、健康检查、最多 16 条可执行建议，以及脱敏后的任务状态统计；只保留最近 5 份。使用 `hobot doctor --json` 获取不落盘的结构化体检，使用 `hobot diagnose --json` 获取支持包路径、大小和 SHA-256。

支持文件不包含对话或 session 内容、系统或用户 Prompt、工具输入与输出、环境变量、凭据、项目文件、工作区内容或原始日志。主机名和本地路径会被替换，任务 ID 与错误原文只保留不可逆短指纹和错误类别；面向用户的建议来自固定规则，不拼接远端原始错误。Mac 应用标题栏的下载按钮会通过现有 SSH Bridge 在板端生成文件，严格校验文件名、大小、SHA-256、schema 和清单一致性，再由系统保存对话框写入本机；不会开放新的网络端口。发送给技术支持前仍应查看文件中的 `manifest` 和内容，确认符合所在组织的数据政策。

### 2. 配置模型

首次安装后运行安全配置向导：

```bash
hobot setup
```

向导会让你选择内置模型和网关，并在读取 API token 时关闭终端回显。凭据只写入当前板卡用户的私有配置文件，不会出现在命令行历史或输出中。配置后可按提示执行一次最小模型连通性检查；若后台服务正在运行，向导会明确提示重启，不会中断正在执行的任务。

自动化环境可从标准输入传入 token：

```bash
printf '%s\n' "$DROBOTICS_TOKEN" | hobot setup --token-stdin --model kimi-k3 --check
```

需要高级配置时，也可以安装目标用户身份手动编辑 `~/.config/hobot-code/hobot.env`：

```text
ANTHROPIC_BASE_URL=https://ai-api.d-robotics.cc
ANTHROPIC_AUTH_TOKEN=your-token
ANTHROPIC_MODEL=kimi-k3
API_TIMEOUT_MS=3000000
```

内置可选模型为 `drobotics/kimi-k3`、`drobotics/qwen3.8-max`、`drobotics/glm-5.2`、`drobotics/deepseek/deepseek-v4-flash` 和 `drobotics/deepseek-v4-pro`。`ANTHROPIC_MODEL` 只决定默认模型；终端可通过 `/model`，桌面端可通过输入框底部的模型菜单切换。

配置完成后可先验证模型的真实流式路由：

```bash
hobot model check drobotics/kimi-k3
hobot model probe drobotics/kimi-k3
hobot model runtime-probe drobotics/kimi-k3
hobot model rdk-probe drobotics/kimi-k3
hobot model profiles drobotics/kimi-k3
hobot model rdk-probe --profile read-only-model-deployment-planning-v1 drobotics/kimi-k3
hobot model status drobotics/kimi-k3
```

Studio 把这四层检查收在模型选择旁的 **Readiness** 面板中，并在 RDK 层按诊断、部署规划、多媒体规划、硬件安全规划和工作区编码分别展示。每项均需用户主动运行，分别说明覆盖范围、耗时和证据等级；规划档案通过不会被显示成转换、推理、媒体链路或硬件操作已经执行。

已完成的检查会以脱敏结构保存在板端私有状态中，Studio 重连或服务重启后可恢复。打开面板与 `model status` 都只读本地证据，不调用模型、不消耗 token；配置、产品、Pi、板型、RDK OS、专家 Prompt、扩展或知识包变化后，受影响的旧证据会显示为需重测而不会继续计入当前结论。

`check` 发送无工具的最小文本请求，不创建 Agent 任务或会话；只返回可用状态、脱敏错误类别、首包和总耗时。同一模型结果缓存 5 分钟。`probe` 进一步探测网关流式终态、结构化工具调用、工具结果续接，以及模型声明的图片输入，结果缓存 1 小时。Studio 显示 **Protocol OK**、**Fallback** 或 **Protocol failed**，并始终标明 Agent runtime 和 RDK tasks 是否真正验收。两者都只由用户显式触发，使用 `--force` 强制重测；旧的 `model verify` 仅作为兼容别名保留。完整的[模型适配等级](docs/model-adaptation-levels.md)不允许将网关连通或协议探测误宣称为 RDK 任务资格。

`runtime-probe` 先在不持久会话中验证单工具闭环、同一轮并行工具、语义参数报错后的自主修复、与最终文本分离的结构化 thinking、关联的只读审批，以及固定无隐私图像输入。随后它在临时私有会话中验证上下文压缩后的语义保留，并在工具执行中强制终止 Pi 进程，重启后要求恢复同一会话、保留中断前的上下文且不重放已中断工具。整个过程不加载用户 Skills、扩展、项目上下文、Shell 或文件工具，临时会话在结束后删除。探针不保留思考正文；不声明 reasoning 或 image 的模型会对应显示 `not-applicable`。通过仍只返回 `partial`，因为这是合成的 Agent runtime 证据，RDK 真实开发任务仍需独立验收。

`rdk-probe` 只能在识别出的 ARM64 RDK X5、S100 或 S600 上运行。它通过完整 RDK 专家 Prompt，要求模型按顺序调用一次只读板卡快照和一次当前板型/系统版本的官方资料检索，再严格合成结构化结论。Agentd 会独立比对实时板卡证据、工具事件、知识版本和最终回答，并绑定产品、agentd、Pi、完整 RDK 扩展、专家 Prompt 与知识包摘要。用户 Skills、项目上下文、Shell、文件工具和会话持久化全部关闭；原始 Prompt、思考、工具正文和模型回答不进入报告。默认档案是 `read-only-rdk-diagnostic-v1`；`--profile` 可选择另外三个只读、知识约束的规划档案。`hobot model profiles` 只读显示每项在当前模型和板卡上的未测、当前或失效证据。工作区编码档案目前明确标为 `planned`，所有规划档案都不证明真实转换、板端推理、媒体执行或硬件写入。脏构建可以用于本地功能验证，但不能生成公开资格证据。

带日期和环境边界的实测结果见[模型协议验证记录](docs/model-conformance-report.md)。该记录不是永久白名单；网关、模型版本或路由变化后应重新验证。

内置 Pi 运行时不仅锁定版本和归档哈希；发行还会依据[机器可读的 Pi 兼容契约](docs/pi-compatibility.md)验证 TUI、RPC、会话分支、压缩、扩展、资源发现、Provider、并行工具、thinking 和图片语义。源码测试、包内上游契约和三板实机场景是三层独立证据，不会互相替代。

安装器默认以 `0600` 创建该文件。启动器按纯 `KEY=VALUE` 数据解析，不执行 Shell 语法，并拒绝符号链接、非当前用户所有或向组/其他用户开放的凭据文件。Kimi K3、Qwen 3.8 Max 和 GLM 5.2 使用 Anthropic-compatible 路径；DeepSeek V4 Flash 和 Pro 使用 OpenAI-compatible 路径。两种路径都保留统一的 usage、工具调用、超时和安全语义。

### 3. 启动

```bash
hobot
```

需要在 SSH 断开后继续运行时，使用持久会话启动：

```bash
hobot persistent
```

这会创建或进入默认的 `main` 会话。重新连接板卡后再次执行 `hobot persistent`，或执行 `hobot persistent attach main`，即可回到原界面。该能力依赖板卡上的 `tmux`；若尚未安装，执行 `sudo apt-get install tmux`。完整命令见[断线续跑](#断线续跑)。

## 日常使用

| 命令 | 用途 |
|---|---|
| `/model` | 选择已配置模型 |
| `hobot model check <provider/model>` | 在任务外主动验证模型流式路由与延迟 |
| `hobot model probe <provider/model>` | 探测网关的流式、工具、续接和声明输入协议 |
| `hobot model runtime-probe <provider/model>` | 在隔离 Pi RPC 中验证工具、thinking、审批、图片、压缩和中断恢复 |
| `hobot model rdk-probe <provider/model>` | 在当前 RDK 上验证只读诊断、版本知识检索与证据合成 |
| `hobot model rdk-probe --profile <id> <provider/model>` | 运行一个有界、只读的 RDK 工作流档案 |
| `hobot model profiles <provider/model>` | 不调用模型，查看每个 RDK 工作流的可用性、证据和失效原因 |
| `hobot model status <provider/model>` | 不调用模型，读取板端保存的分层资格与过期/失效状态 |
| `/settings` | 调整 Pi 交互设置 |
| `/new`、`/resume`、`/tree`、`/fork` | 管理会话与分支 |
| `/compact` | 手动压缩上下文 |
| `/rdk`、`/doctor` | 查看板卡摘要或完整诊断 |
| `/knowledge <问题>` | 检索当前板卡线路的专业知识与官方来源 |
| `/system-prompt`、`/system-prompt full` | 查看系统 Prompt 分层或展开完整内容 |
| `/cache`、`/cache reset` | 查看当前进程的模型缓存命中率、输入 token 构成和前缀稳定性 |
| `/permissions` | 查看或修改工具权限；`preset developer` 一键启用受保护的开发权限 |
| `/init`、`/gate` | 初始化并运行项目质量门 |
| `/memory`、`/goal` | 管理持久记忆与长期目标 |
| `/hooks`、`/notifications`、`/lsp` | 管理工程扩展能力 |
| `/btw <任务>` | 打开侧边 Agent |
| `/detach` | 退出持久会话界面并保持 Agent 在后台运行 |
| `/hotkeys` | 查看完整快捷键 |
| `/quit`、`/q`、`/exit` | 退出 |

`Escape` 中断当前模型或工具，`Ctrl+D` 在编辑区为空时退出，`Ctrl+T` 显示或隐藏 thinking。其余快捷键以 `/hotkeys` 为准。

`/cache` 直接展示当前进程的网关缓存 usage、最近一轮与累计命中率，以及模型、系统 Prompt 和工具契约是否保持稳定。实机基线与测量方法见[缓存效率](docs/cache-efficiency.md)。

全屏模式中可用鼠标主键拖选对话文本，松开后会通过终端剪贴板协议复制到本地电脑；执行 `/copy` 可复制最近一条 Agent 回复。`hobot persistent` 会转发该协议。若本地终端禁用了远程剪贴板写入，可使用 `Shift` 加鼠标拖选走终端自身的复制路径，或在终端设置中允许 OSC 52。

## 断线续跑

Hobot Code 可以由 `tmux` 托管完整 TUI 和子进程。SSH 或网络连接断开后，主 Agent、侧边 Agent、工具调用以及其前台子进程会继续在板卡上运行；重新连接只需附着原会话：

```bash
hobot persistent                           # 创建或重连默认 main 会话
hobot persistent start main                # 创建；已存在时直接重连
hobot persistent start debug -- --resume   # 以 Hobot 参数启动命名会话
hobot persistent list                      # 列出当前用户的 Hobot Code 会话
hobot persistent attach main               # 重连
hobot persistent stop main                 # 终止会话及受其终端托管的进程
```

主动离开但保持任务运行时，直接执行 `/detach`。也可使用 tmux 原生快捷键：按 `Ctrl+B`，松开后按 `D`。持久会话运行在按 OS 用户隔离的 `hobot-code` 专用 `tmux` 服务中，随包配置会启用鼠标、剪贴板转发、扩展按键和 256 色支持，不会读取或修改用户普通 `tmux` 服务的会话与设置。若当前已经位于其他 `tmux` 客户端中，需要先分离再运行 `hobot persistent`。它只能承受客户端断线：板卡重启、断电、内存不足杀进程或程序崩溃仍会终止实时任务；此后可使用 `hobot --resume` 恢复已落盘的对话，但不会自动重放中断的工具调用。

不需要保留完整 TUI 时，可把独立任务交给板端常驻服务：

发行版安装器会安装或验证 `bubblewrap`。后台 Agent 默认使用 OS 级写入、设备和 capabilities 隔离。`shared` 允许模型与工具访问板卡网络；`model-only` 将 worker 放入独立网络命名空间，只允许内置 D-Robotics Provider，以及配置了板端密钥的 Hobot 受管 Anthropic Messages、OpenAI Chat Completions、OpenAI Responses Provider，通过 agentd 私有 Unix Socket 访问各自固定模型网关，工具没有通用网络；`offline` 同时切断远程模型与工具，只适合本地模型。Studio 按模型的板端能力标记自动选择 `model-only`。Google Generative AI、Pi 登录和自管 `models.json` 目前需要 `shared`。只有明确的系统维护任务才应使用 `--sandbox off`；该模式不能与受限网络档位组合。

```bash
hobot workspace inspect .                    # 检查是否适合隔离运行
hobot task start --name build --workspace worktree --model drobotics/kimi-k3 --permissions developer --sandbox workspace -- "检查项目、修复问题并运行测试"
hobot task start --name secure --model drobotics/kimi-k3 --sandbox workspace --network model-only -- "检查项目并修复问题"
hobot task start --name audit --model local/model --sandbox review --network offline -- "离线审查当前项目"
hobot task list
hobot task attach <task-id>                 # 首次显示仍保留的历史，之后从上次断点继续并原地处理审批
hobot task attach <task-id> --replay-all    # 从当前滚动保留窗口的起点回放
hobot task send <task-id> "继续处理下一项"  # 同一 Agent 多轮续接
hobot task abort <task-id>                  # 中断当前一轮，保留 worker
hobot task respond <task-id> <request-id> yes
hobot task approvals <task-id>              # 默认只显示脱敏摘要；--details 显示完整记录
hobot task resume <task-id> ["继续任务"]     # 从已落盘的 Pi 对话显式恢复
hobot task restart <task-id> "重新开始"      # 保留任务记录，创建全新会话
hobot task rename <task-id> build-rdk
hobot task archive <task-id>
hobot task list --all
hobot task delete <task-id> --yes            # 必须先停止并归档
hobot task stop <task-id>
hobot workspace list                        # 对话删除后仍保留隔离代码
hobot workspace writes                      # 查看当前正在修改工作区的 Agent
hobot workspace delivery <task-id>          # 预检隔离改动能否安全回写
hobot workspace apply <task-id> --yes        # 停止空闲 Agent，并以 staged changes 回写原项目
hobot workspace cleanup <task-id> --yes     # 仅清理无改动、无新提交且无对话引用的 worktree
```

首次执行 `hobot task` 会自动启动当前用户的 `agentd`；也可用 `hobot daemon start|status|stop|restart` 管理。命令行退出或 SSH 断开不影响后台 Agent。每个用户默认最多保留两个常驻 worker；创建或恢复对话时，服务会自动挂起最久未使用的 Ready worker并保留其 session。若所有槽位都正在工作或等待审批，新请求会在板端私有持久队列中等待，而不是报错；停止 queued 任务即可取消。板卡重启后尚未执行的队列项继续排队，已经开始的任务标记为 `interrupted`。板端还会为最近 32 轮保存脱敏恢复证据：工具是否全部结束、是否存在未闭合调用，以及 Git 工作区状态是否变化；不写入 Prompt、命令、输出、文件名或路径。终端和 Studio 据此显示完成、未知与需审查状态，并只建议 Resume、New session、Check model 或 Save diagnostics 中适用的一项；恢复仍需用户明确发送，不会自动重放已开始的 Prompt、审批或可能带副作用的工具调用。协议与恢复边界见 [agentd 协议](docs/agentd-protocol.md)。

多个 Hobot Code Agent 可以并行阅读不同或相同项目，但同一实际工作区的 `bash`、`write`、`edit`、质量门和 MCP 调用按 Agent 轮次互斥；冲突调用会立即指出正在占用的任务，而不会静默覆盖文件。每次写入前还会比较模型开始工作时及上次工具完成后的有界元数据快照，发现另一个 SSH、IDE 或脚本改过源码时要求 Agent 重新读取。大型项目的检查会有界降级并明确提示，不扫描 `.git`、构建输出、依赖树或模型数据内容。进程退出或崩溃后的陈旧租约自动回收。独立 worktree 具有不同实际路径，因此不同根任务仍可并行修改；同一根任务的 Side Agent 和编辑分支共享 worktree，会遵守同一写入互斥。

隔离任务完成后，可在 Studio 的 Changes 中先复核再选择 **Apply to project**。板端会重新验证原项目未变化、隔离工作区没有新提交且所有 Agent 已结束当前轮次；随后锁住两侧工作区、停止空闲 Agent，并仅在 SHA-256 仍和用户审阅的快照一致时，把文本、新文件、删除和二进制变化作为 staged changes 放回原项目，留给用户最终审阅。它不会自动 commit 或 push，ignored artifacts 也不会静默交付。

Hobot Code 桌面端通过 SSH 调用 `hobot bridge --stdio`，使用同一套板端任务、审批和权限判定。该桥接不监听 TCP，不会向 Mac 端返回模型凭据。桌面端按用户选定的项目目录组织任务，即使实际运行在私有 worktree 中也不会把内部状态路径暴露为新项目。新建任务时可浏览板端目录、新建文件夹，或选择不绑定项目的默认工作区，无需手输路径。删除对话不会删除代码；隔离工作区只能通过独立命令显式清理。

输入框底部可切换板端已配置的内置 D-Robotics 模型、显式受管模型和当前任务权限模式；Pi 登录产生的其他模型不会无差别挤进 Studio 列表。发送后客户端会立即显示用户消息、当前阶段和已等待时间，发送按钮在原位置变为停止，而不是等到首个模型 token 才反馈。从历史用户消息编辑时，板端会在该消息之前的会话节点创建新分支，原任务和审计记录保留不变。侧边任务使用相同的安全分支机制继承主任务已稳定的上下文，两者可独立多轮继续，且都受每用户并发上限约束。

脚本化调用沿用 Pi：

```bash
hobot -p "检查这个项目并给出结论"
hobot --mode json "输出 JSON 事件流"
```

恢复交互会话：

```bash
hobot --continue
hobot --resume
```

其他模型有三种接入方式：常见 API Key 服务用 `hobot provider add` 安全接入和保存密钥；Pi 原生 OAuth/登录服务使用 `/login <provider>`；本机或高级自管服务继续使用 `agent/models.json`。`hobot provider list` 只显示脱敏状态，`hobot provider rotate PROVIDER` 可原地轮换密钥，删除时必须显式执行 `hobot provider remove PROVIDER --yes`。Studio 也提供同一套板端受控操作。第三方扩展包使用 `hobot install <package>` 安装。完整字段和安全边界见[配置说明](docs/configuration.md)。

查看当前版本随板端安装的内置扩展与 Skills：

```bash
hobot extensions
hobot extensions --task TASK_ID
hobot extensions --json
```

该目录是只读能力清单，展示来源、作用域、运行时、声明权限和适用板型，也会以脱敏方式汇总当前用户的 Provider、Hook、LSP、Pi extensions、Skills、Prompt templates、themes 与 package 声明。`--task` 只接受现有任务 ID，并且只有该任务已信任项目资源时才读取其 `.pi`/`.agents` 目录；Studio 会自动使用当前任务上下文。目录不会加载代码，也不会授予工具权限；`Declared`/`Discovered` 不等于已经加载。第三方 package 仍由 Pi 的安装机制管理，执行与最终审批始终留在板端。

## 侧边 Agent

`/btw <任务>` 在全屏 TUI 中将终端等分为主 Agent 和右侧 Agent，后者运行在独立的 Pi RPC 子进程中。打开后键盘焦点保留在主 Agent；点击任一半屏即可切换到对应输入，也可按 `Ctrl+Shift+Right` 进入右侧、按 `Ctrl+Shift+Left` 返回主输入。鼠标滚轮和触控板会滚动指针所在的半屏；侧边输出的自动跟随会在用户向上滚动时暂停，避免阅读历史时被新输出拉回底部。窄终端或非全屏模式会自动回退到非抢占焦点的右侧浮层。

侧边 Agent 支持多轮对话，并从主会话取得一次性上下文快照，同时继承当前模型、thinking 等级、有效系统 Prompt、工具集合和项目信任状态。若主 Agent 正在工作，快照严格截止到本轮开始前记录的稳定会话叶节点；当前未完成任务不会复制到侧边会话，也不会被误当成侧边任务继续执行。

侧边 Agent 与主 Agent 具有相同的工作目录、用户权限、环境、进程命名空间、服务和设备视图，因此文件、进程或硬件副作用会保留。它的对话记录不会写回主会话；关闭后临时会话与 Prompt 会被删除。它不会独立重新扫描 Skills，并禁止写入持久记忆或修改持久目标状态。

每个主会话同时只能打开一个侧边 Agent。同一 **OS 用户** 的所有 Hobot Code 进程默认合计最多运行两个，可通过 `HOBOT_CODE_MAX_SIDE_AGENTS=1..8` 调整；该限制不是跨用户的整板全局配额。异常退出留下的陈旧租约会在后续打开时回收。工具审批会在侧栏按顺序显示，可按 `Y`/`N` 处理；无人处理时会在两分钟后自动拒绝，侧边任务不会无限等待。

在侧边窗格中按 `Enter` 继续追问，`Escape` 中断当前一轮或在空闲时关闭，`Ctrl+D` 随时关闭。键盘滚动可用 `Ctrl+PageUp` / `Ctrl+PageDown` 或在输入为空时使用上下方向键。

## 安全模型

Hobot Code 使用板端 OS sandbox 限制默认 Agent 的文件、设备和 capability，但它不是用来执行任意不可信代码的完整虚拟机：

- 内置 `write`、`edit` 禁止直接修改 `/boot`、`/dev`、`/etc`、`/proc`、`/sys`、`/usr` 和 `/var/lib`。
- 内置工具的工作区外写入、识别出的高风险 Shell 和外联客户端需要交互确认；root 下 Ask 模式逐次审批变更工具，Developer 模式按实际风险审批，但不能绕过受保护路径和破坏性操作边界。
- `shared` 网络下，`/permissions set network allow|ask|deny` 只控制可识别的外联命令，是降低误操作的启发式策略。内置 D-Robotics 模型和受支持的 Hobot 受管 Anthropic/OpenAI 模型可用 `--network model-only` 保留固定模型出口并强制切断工具通用网络；本地模型可用 `offline` 完全断网。Google、Pi 登录和自管 Provider 当前仍需 `shared`。
- 默认权限允许模型检索记忆，但每次模型写入记忆都要求确认；用户可以修改该策略。
- 第三方扩展和 Skills 以当前用户权限运行，安装前必须审查来源与代码。
- `system_snapshot` 只能证明当前设备与工具状态；文件名和 march 也只能用于候选筛选。模型完成状态必须由部署报告、实际产物摘要、正确性与性能证据共同证明。
- Hobot Code 只适合作为控制面工具，不应进入电机、CAN、GPIO、安全或急停的硬实时闭环。

威胁模型、密钥处理和漏洞报告方式见[安全说明](SECURITY.md)。

## 升级与回滚

安装器会在替换运行时前检查空间、备份已有安装，并拒绝覆盖正在运行的 Hobot Code。用户配置、会话、记忆和目标会保留；默认配置只在缺失时创建。

```bash
hobot update --check       # 只检查最新稳定版本
hobot update               # 下载、校验并升级
hobot update --version <version>
```

在板端无法访问 GitHub 时，`hobot update --check` 会在 10 秒内结束并保留当前版本，不会因重试卡住终端或显示底层传输噪声。需要诊断网络问题时，可临时使用 `HOBOT_CODE_DEBUG=1 hobot update --check`。

更新器按 SemVer 比较版本，默认拒绝任何降级，包括发布源的 `latest` 元数据意外落后于板端版本时。确需回退到指定的已验证发行版，应优先使用下方的事务回滚；没有可用备份时，才显式执行 `hobot update --version <version> --allow-downgrade`。

公开候选版本还会在 X5、S100、S600 上分别经过隔离的安装生命周期验收，覆盖首次安装、普通用户启动、升级保留数据、换入后失败恢复、回滚和卸载保留数据。验收使用临时系统树，并要求板端既有安装路径在测试前后保持不变。

`hobot update --extensions` 仍用于更新 Pi 扩展，不会触发 Hobot Code 自身升级。正常卸载保留用户配置、会话、记忆、目标和安装备份；彻底清理必须显式确认：

```bash
hobot uninstall
hobot uninstall --purge    # 永久删除当前安装用户的数据与备份
```

回滚必须以 root 权限执行，并且只在存在完整的前一版本备份时可用，因此首次安装后通常没有可回滚版本：

```bash
sudo /usr/local/sbin/hobot-rollback
```

回滚恢复命令和运行时，不删除当前用户的配置、会话、记忆或目标。成功恢复的备份会写入 `.hobot-restored` 标记并拒绝再次使用，避免同一备份被反复回滚。

## 开发与验证

```bash
make check
make release
```

`make check` 执行 Shell/JSON 校验、Node 测试、Go race/vet、知识库与 Prompt 预算验证、品牌、文档链接和版本一致性检查，以及扩展源码语法与模块依赖检查。`make release` 还会交叉编译并校验 ARM64 `agentd`、完整发行包文件集合与清单。构建缓存与开发覆盖项见[配置说明](docs/configuration.md#构建覆盖)。贡献前请阅读[贡献指南](CONTRIBUTING.md)。

## 文档

| 文档 | 内容 |
|---|---|
| [完整用户手册](docs/user-guide.md) | 从安装配置到 Studio/TUI、项目任务、模型、权限、RDK 能力、运维和命令参考 |
| [配置说明](docs/configuration.md) | 模型、权限、记忆、目标、Hook、通知和 LSP |
| [系统架构](docs/architecture.md) | 运行路径、适配层、数据边界与部署模型 |
| [agentd 协议](docs/agentd-protocol.md) | 后台任务协议、状态机、重连与安全边界 |
| [模型能力契约](docs/model-capabilities.md) | 模型默认选择、推理与图像输入能力协商 |
| [模型适配等级](docs/model-adaptation-levels.md) | 连通、网关协议、Agent runtime 与 RDK 任务资格的证据边界 |
| [兼容矩阵](docs/compatibility.md) | Studio、agentd、板型与 RDK OS 的支持边界 |
| [三板稳定性验证](docs/board-reliability.md) | X5、S100、S600 的只读基线、断点续测和空闲恢复验证 |
| [产品基准与升级路线](docs/product-benchmark.md) | 与原生 Pi、Codex 的能力边界、差距和公开交付优先级 |
| [用户目录布局](docs/user-directory-layout.md) | 配置、状态、迁移与安装目标用户 |
| [缓存效率](docs/cache-efficiency.md) | 网关 usage、实机基线与适用边界 |
| [发布流程](docs/releasing.md) | 版本、GitHub Release、来源证明与实机检查 |
| [安全说明](SECURITY.md) | 权限边界、密钥、第三方代码与漏洞报告 |
| [贡献指南](CONTRIBUTING.md) | 本地验证、变更要求与提交检查表 |
| [变更记录](CHANGELOG.md) | 各版本行为变化 |

## 上游与许可证

Pi 的版本、提交和 Linux ARM64 SHA256 固定在 `pi-runtime/pi.lock`，`fd` 与 `ripgrep` 的版本、来源和校验值固定在 `pi-runtime/tools.lock`。发行包携带对应第三方许可证，仓库保留的许可证文本位于 `LICENSES/`。Hobot Code 自身采用 [MIT License](LICENSE)。
