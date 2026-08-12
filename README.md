# Hobot Code

[![CI](https://github.com/bryant-w/hobot-code/actions/workflows/ci.yml/badge.svg)](https://github.com/bryant-w/hobot-code/actions/workflows/ci.yml)

面向地瓜机器人 RDK 的开发 Agent。Hobot Code 在板端提供基于 [Pi](https://github.com/earendil-works/pi) 的原生 TUI 和常驻任务服务，在 Mac 上提供同名桌面应用；两个入口共享同一套模型、工具、审批、Skills、会话和 RDK 专业知识。

Hobot Code 不维护 Pi 的叉路 TUI。终端交互来自固定版本的 Pi，桌面端通过 SSH Bridge 操作板端常驻任务；权限判定、工具执行和模型凭据始终留在 RDK 上。

## 核心能力

- **原生终端体验**：流式 thinking、工具调用、会话恢复、分支、压缩和快捷键。
- **RDK 实机上下文**：按需读取型号、RDK OS、温度、内存、BPU 设备和工具状态。
- **版本化板卡知识**：按 X5 3.x、S100 4.x、S600 5.x 路由 27 个专业主题，并在每篇资料中保留官方来源与核对日期。
- **开放模型接入**：内置 D-Robotics Kimi K3、Qwen 3.8 Max、GLM 5.2、DeepSeek V4 Flash 和 DeepSeek V4 Pro 网关适配，也兼容 Pi 的 Provider、`models.json` 和 `/login`。
- **可组合扩展**：继续使用 Pi packages、extensions、MCP、Skills、Prompt templates 和 themes。
- **工程保障**：工具权限、质量门、Hook、资源受限 LSP、持久记忆和持久目标。
- **并行协作**：`/btw` 在右侧窗格启动独立、多轮、临时的侧边 Agent。
- **后台任务**：板端 `agentd` 托管无界面 Agent，支持 SSH 断开后继续运行、事件重放和多轮续接。
- **Mac 桌面端**：统一查看板卡、主/后台任务、thinking、工具时间线和待审批操作。
- **一键技术支持**：`hobot diagnose` 生成私有、脱敏、可校验的板端诊断文件，Studio 可通过现有 SSH 连接保存到 Mac。

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

安装指定版本：

```bash
curl -fsSL https://github.com/bryant-w/hobot-code/releases/latest/download/hobot-install.sh \
  | sh -s -- --version 0.24.0
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

### Mac 桌面应用

从 [GitHub Releases](https://github.com/bryant-w/hobot-code/releases) 下载 `hobot-code-<version>-macos-arm64.dmg`，打开后将 **Hobot Code** 拖入 Applications。首次启动时添加板卡名称、IP、SSH 用户与可选私钥路径；应用使用 macOS 系统 OpenSSH 和 `known_hosts`，不保存 SSH 密码或板端模型密钥。

桌面应用连接时会协商协议、event schema、功能能力、产品版本、板型与 RDK OS。最低要求是 event schema 2、任务生命周期/分页能力和 `hobot bridge --stdio`；不满足硬条件时拒绝半兼容连接，可降级能力则在板卡详情中明确提示。升级到 schema 3 后会额外持久化用户消息，并将 thinking、工具调用与最终回答组织成稳定的对话轮次。0.22.4 及之后的板端还会向详情栏提供只读硬件快照，并对过热、低内存、低磁盘、BPU 或验证工具缺失给出就地提示；0.23.0 增加逐核 BPU、ION/Hbmem 与结构化模型部署能力，0.24.0 增加一键诊断与支持包下载。退出桌面应用、Mac 休眠或 VPN 短暂断开只会中断界面连接，`agentd` 托管的任务仍在板端继续；重新连接后会按事件序号重放缺失输出。详细边界见[兼容矩阵](docs/compatibility.md)。

消息输入框使用 `Enter` 发送，`Shift+Enter` 换行；发送后同一按钮原位切换为停止，中文输入法确认候选词时不会误触发发送。编辑历史用户消息会保留该消息之前的上下文，用修改后的内容替换原消息，并在同一主对话中隐藏后续旧时间线，不会创建可见的 Side Agent。左侧项目可以折叠，每个项目可创建多个对话；新对话会从首条指令生成可修改标题。对话和 Side Agent 作为项目子项展示，每一项的 `…` 菜单可删除单个对话或移除项目中的全部对话，但不会删除板端工作目录。任务标题右侧的 **Side Agent** 会从当前已稳定上下文创建独立多轮分支，多个 Side Agent 始终作为主对话的同级分支显示。输入框底部只展示 D-Robotics 模型，并可在任务 Ready 或停止后切换；停止后的选择会在下次 Resume 生效。终态任务有安全 session 时显示 Resume，没有 session 时显示 New session，并在同一任务记录中明确启动全新会话。回复中的 HTTP/HTTPS 链接会交给 Mac 默认浏览器打开。

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

`inspect` 只扫描候选，不运行模型；`start` 创建可断线续跑的持久任务；`status` 读取经过板端复核的验收状态。未声明目标的编译产物会显示为待验证，明确属于另一块板或 march 的产物会被拒绝。

### 一键诊断与技术支持包

遇到启动、连接、任务恢复、BPU 状态或资源异常时，在板端执行：

```bash
hobot diagnose
```

命令会启动或连接当前用户的 `agentd`，完成固定诊断，并在私有状态目录中生成 `0600` 的 `hobot-code-support-*.json`。文件包含板型和 RDK OS、负载、内存、磁盘、温度、BPU/Hbmem 状态、固定 RDK 工具是否可用、守护进程版本与资源上限、健康检查，以及脱敏后的任务状态统计；只保留最近 5 份。使用 `hobot diagnose --json` 可获得便于自动化处理的路径、大小和 SHA-256。

支持文件不包含对话或 session 内容、系统或用户 Prompt、工具输入与输出、环境变量、凭据、项目文件、工作区内容或原始日志。主机名和本地路径会被替换，任务 ID 与错误原文只保留不可逆短指纹和错误类别。Mac 应用标题栏的下载按钮会通过现有 SSH Bridge 在板端生成文件、校验 SHA-256，再由系统保存对话框写入本机；不会开放新的网络端口。发送给技术支持前仍应查看文件中的 `manifest` 和内容，确认符合所在组织的数据政策。

### 2. 配置模型

以安装目标用户编辑 `~/.config/hobot-code/hobot.env`：

```text
ANTHROPIC_BASE_URL=https://ai-api.d-robotics.cc
ANTHROPIC_AUTH_TOKEN=your-token
ANTHROPIC_MODEL=kimi-k3
API_TIMEOUT_MS=3000000
```

内置可选模型为 `drobotics/kimi-k3`、`drobotics/qwen3.8-max`、`drobotics/glm-5.2`、`drobotics/deepseek-v4-flash` 和 `drobotics/deepseek-v4-pro`。`ANTHROPIC_MODEL` 只决定默认模型；终端可通过 `/model`，桌面端可通过输入框底部的模型菜单切换。

配置完成后可先验证模型的真实流式路由：

```bash
hobot model check drobotics/kimi-k3
```

该命令发送无工具的最小文本请求，不创建 Agent 任务或会话；只返回可用状态、脱敏错误类别、首包和总耗时。同一模型结果缓存 5 分钟，使用 `--force` 强制重测。Studio 在模型菜单旁提供相同的 **Check** 控件，但连接板卡时不会自动调用模型或产生费用。健康通过只证明此刻的最小文本链路可用，不代表图像、长上下文、工具调用和生产配额均已验证。

安装器默认以 `0600` 创建该文件。启动器按纯 `KEY=VALUE` 数据解析，不执行 Shell 语法，并拒绝符号链接、非当前用户所有或向组/其他用户开放的凭据文件。D-Robotics Provider 优先请求 Anthropic SSE 流；若端点明确不支持流式格式或返回普通 JSON，则回退到有大小上限的缓冲响应。

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

缓存命中率按网关实际 usage 计算，不用字符数或本地估算替代。D-Robotics 端点实测中，Kimi K3 默认产品路径热轮为 **94.58%**，约 50K token 严格稳定前缀达到 **99.46%**。DeepSeek V4 Flash 的同端点路由目前没有返回缓存命中，并暴露了重复长前缀空响应/慢响应问题；Hobot Code 会拒绝空成功响应，不把异常数据包装成性能结果。完整方法和逐轮数据见[缓存效率审计](docs/cache-efficiency.md)。

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

```bash
hobot task start --name build -- "检查项目、修复问题并运行测试"
hobot task list
hobot task attach <task-id>                 # 重放历史并持续查看输出
hobot task send <task-id> "继续处理下一项"  # 同一 Agent 多轮续接
hobot task abort <task-id>                  # 中断当前一轮，保留 worker
hobot task respond <task-id> <request-id> yes
hobot task approvals <task-id>              # 查看待处理或已失效的审批
hobot task resume <task-id> ["继续任务"]     # 从已落盘的 Pi 对话显式恢复
hobot task restart <task-id> "重新开始"      # 保留任务记录，创建全新会话
hobot task rename <task-id> build-rdk
hobot task archive <task-id>
hobot task list --all
hobot task delete <task-id> --yes            # 必须先停止并归档
hobot task stop <task-id>
```

首次执行 `hobot task` 会自动启动当前用户的 `agentd`；也可用 `hobot daemon start|status|stop|restart` 管理。命令行退出或 SSH 断开不影响后台 Agent。每个用户默认最多保留两个常驻 worker；创建或恢复对话时，服务会自动挂起最久未使用的 Ready worker并保留其 session，只有所有槽位都正在工作或等待审批时才拒绝新任务。板卡重启、daemon 崩溃或强制停止会把未完成任务标记为 `interrupted`。此时 `resume` 只会重新打开同一 Pi 对话，不会自动重放 Prompt、审批或可能带副作用的工具调用。协议与恢复边界见 [agentd 协议](docs/agentd-protocol.md)。

Hobot Code 桌面端通过 SSH 调用 `hobot bridge --stdio`，使用同一套板端任务、审批和权限判定。该桥接不监听 TCP，不会向 Mac 端返回模型凭据。桌面端按工作目录组织项目和任务；新建任务时可浏览板端目录、新建文件夹，或选择不绑定项目的默认工作区，无需手输路径。

输入框底部可切换板端已配置的 D-Robotics 模型和当前任务权限模式。发送后客户端会立即显示用户消息、当前阶段和已等待时间，发送按钮在原位置变为停止，而不是等到首个模型 token 才反馈。从历史用户消息编辑时，板端会在该消息之前的会话节点创建新分支，原任务和审计记录保留不变。侧边任务使用相同的安全分支机制继承主任务已稳定的上下文，两者可独立多轮继续，且都受每用户并发上限约束。

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

其他模型可通过 `/model`、`/login <provider>` 或 `~/.config/hobot-code/agent/models.json` 配置；第三方扩展包使用 `hobot install <package>` 安装。完整字段见[配置说明](docs/configuration.md)。

## 侧边 Agent

`/btw <任务>` 在全屏 TUI 中将终端等分为主 Agent 和右侧 Agent，后者运行在独立的 Pi RPC 子进程中。打开后键盘焦点保留在主 Agent；点击任一半屏即可切换到对应输入，也可按 `Ctrl+Shift+Right` 进入右侧、按 `Ctrl+Shift+Left` 返回主输入。鼠标滚轮和触控板会滚动指针所在的半屏；侧边输出的自动跟随会在用户向上滚动时暂停，避免阅读历史时被新输出拉回底部。窄终端或非全屏模式会自动回退到非抢占焦点的右侧浮层。

侧边 Agent 支持多轮对话，并从主会话取得一次性上下文快照，同时继承当前模型、thinking 等级、有效系统 Prompt、工具集合和项目信任状态。若主 Agent 正在工作，快照严格截止到本轮开始前记录的稳定会话叶节点；当前未完成任务不会复制到侧边会话，也不会被误当成侧边任务继续执行。

侧边 Agent 与主 Agent 具有相同的工作目录、用户权限、环境、进程命名空间、服务和设备视图，因此文件、进程或硬件副作用会保留。它的对话记录不会写回主会话；关闭后临时会话与 Prompt 会被删除。它不会独立重新扫描 Skills，并禁止写入持久记忆或修改持久目标状态。

每个主会话同时只能打开一个侧边 Agent。同一 **OS 用户** 的所有 Hobot Code 进程默认合计最多运行两个，可通过 `HOBOT_CODE_MAX_SIDE_AGENTS=1..8` 调整；该限制不是跨用户的整板全局配额。异常退出留下的陈旧租约会在后续打开时回收。工具审批会在侧栏按顺序显示，可按 `Y`/`N` 处理；无人处理时会在两分钟后自动拒绝，侧边任务不会无限等待。

在侧边窗格中按 `Enter` 继续追问，`Escape` 中断当前一轮或在空闲时关闭，`Ctrl+D` 随时关闭。键盘滚动可用 `Ctrl+PageUp` / `Ctrl+PageDown` 或在输入为空时使用上下方向键。

## 安全模型

Hobot Code 是具备当前用户权限的开发 Agent，不是安全沙箱：

- 内置 `write`、`edit` 禁止直接修改 `/boot`、`/dev`、`/etc`、`/proc`、`/sys`、`/usr` 和 `/var/lib`。
- 内置工具的工作区外写入和识别出的高风险 Shell 命令需要交互确认；root 下 Ask 模式逐次审批变更工具，Developer 模式按实际风险审批，但不能绕过受保护路径和破坏性操作边界。
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
hobot update --version 0.24.0
```

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
| [配置说明](docs/configuration.md) | 模型、权限、记忆、目标、Hook、通知和 LSP |
| [系统架构](docs/architecture.md) | 运行路径、适配层、数据边界与部署模型 |
| [agentd 协议](docs/agentd-protocol.md) | 后台任务协议、状态机、重连与安全边界 |
| [模型能力契约](docs/model-capabilities.md) | 模型默认选择、推理与图像输入能力协商 |
| [兼容矩阵](docs/compatibility.md) | Studio、agentd、板型与 RDK OS 的支持边界 |
| [用户目录布局](docs/user-directory-layout.md) | 配置、状态、迁移与安装目标用户 |
| [设计调研](docs/prime-agent-crush-review.md) | Prime Agent 与 Crush 的可借鉴设计 |
| [发布流程](docs/releasing.md) | 版本、GitHub Release、来源证明与实机检查 |
| [安全说明](SECURITY.md) | 权限边界、密钥、第三方代码与漏洞报告 |
| [贡献指南](CONTRIBUTING.md) | 本地验证、变更要求与提交检查表 |
| [变更记录](CHANGELOG.md) | 各版本行为变化 |

## 上游与许可证

Pi 的版本、提交和 Linux ARM64 SHA256 固定在 `pi-runtime/pi.lock`，`fd` 与 `ripgrep` 的版本、来源和校验值固定在 `pi-runtime/tools.lock`。发行包携带对应第三方许可证，仓库保留的许可证文本位于 `LICENSES/`。Hobot Code 自身采用 [MIT License](LICENSE)。
