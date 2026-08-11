# Hobot Code

[![CI](https://github.com/Kyrie-w8/hobot-code/actions/workflows/ci.yml/badge.svg)](https://github.com/Kyrie-w8/hobot-code/actions/workflows/ci.yml)

面向地瓜机器人 RDK 的终端开发 Agent。Hobot Code 直接使用 [Pi](https://github.com/earendil-works/pi) 的交互运行时，在保留其编辑器、流式输出、会话树、工具、扩展和 Skills 生态的同时，补充板卡识别、版本化知识、硬件安全策略和 Linux ARM64 部署能力。

Hobot Code 不维护另一套 TUI。交互行为来自固定版本的 Pi，板卡适配集中在可审计的扩展、Prompt、知识库和安装层中。

## 核心能力

- **原生终端体验**：流式 thinking、工具调用、会话恢复、分支、压缩和快捷键。
- **RDK 实机上下文**：按需读取型号、RDK OS、温度、内存、BPU 设备和工具状态。
- **版本化板卡知识**：按 X5 3.x、S100 4.x、S600 5.x 路由 27 个专业主题，并在每篇资料中保留官方来源与核对日期。
- **开放模型接入**：内置 D-Robotics Kimi 网关适配，也兼容 Pi 的 Provider、`models.json` 和 `/login`。
- **可组合扩展**：继续使用 Pi packages、extensions、MCP、Skills、Prompt templates 和 themes。
- **工程保障**：工具权限、质量门、Hook、资源受限 LSP、持久记忆和持久目标。
- **并行协作**：`/btw` 在右侧窗格启动独立、多轮、临时的侧边 Agent。

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

基础运行时自包含，使用内置 Agent 能力时，板端无需另外安装 Node.js、Bun、Go、Python 或容器。SSH 断线续跑功能需要 `tmux`；第三方 Pi package 可能需要系统中的 `git`、`npm` 或自定义 `npmCommand`，用户配置的 Hook 与 LSP 也需要对应外部命令。

## 快速开始

### 1. 一条命令安装

在 RDK X5、S100 或 S600 上执行：

```bash
curl -fsSL https://github.com/Kyrie-w8/hobot-code/releases/latest/download/hobot-install.sh | sh
```

板端需要先安装 `curl`。安装器只接受 Linux ARM64，并检查 device tree 中的 RDK 型号；它通过 HTTPS 下载版本化归档，严格核对 SHA256、归档根目录和文件类型，再调用事务安装器。普通用户会通过 `sudo` 安装程序，但配置、会话和状态仍属于发起安装的用户；root 直接执行时默认安装给 root。

安装指定版本：

```bash
curl -fsSL https://github.com/Kyrie-w8/hobot-code/releases/latest/download/hobot-install.sh \
  | sh -s -- --version 0.14.3
```

无法从板卡访问 GitHub 时，可从 [GitHub Releases](https://github.com/Kyrie-w8/hobot-code/releases) 下载版本化归档和同名 `.sha256`，传入板卡后离线安装：

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

目标用户必须已经存在并拥有可解析的 home 目录。root 默认会逐次确认 Shell、写入和编辑；如需让显式 `allow` 规则在 root 下生效，可执行 `/permissions root policy`。权限文件会在每次工具调用前重新读取，因此设置会立即同步到同一用户已打开的其他会话。破坏性命令、工作区外写入和关键系统路径仍受保护。

### 2. 配置模型

以安装目标用户编辑 `~/.config/hobot-code/hobot.env`：

```text
ANTHROPIC_BASE_URL=https://ai-api.d-robotics.cc
ANTHROPIC_AUTH_TOKEN=your-token
ANTHROPIC_MODEL=kimi-k3
API_TIMEOUT_MS=3000000
```

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
| `/settings` | 调整 Pi 交互设置 |
| `/new`、`/resume`、`/tree`、`/fork` | 管理会话与分支 |
| `/compact` | 手动压缩上下文 |
| `/rdk`、`/doctor` | 查看板卡摘要或完整诊断 |
| `/knowledge <问题>` | 检索当前板卡线路的专业知识与官方来源 |
| `/system-prompt`、`/system-prompt full` | 查看系统 Prompt 分层或展开完整内容 |
| `/permissions` | 查看或修改工具权限；`preset developer` 一键启用受保护的开发权限 |
| `/init`、`/gate` | 初始化并运行项目质量门 |
| `/memory`、`/goal` | 管理持久记忆与长期目标 |
| `/hooks`、`/notifications`、`/lsp` | 管理工程扩展能力 |
| `/btw <任务>` | 打开侧边 Agent |
| `/detach` | 退出持久会话界面并保持 Agent 在后台运行 |
| `/hotkeys` | 查看完整快捷键 |
| `/quit`、`/q`、`/exit` | 退出 |

`Escape` 中断当前模型或工具，`Ctrl+D` 在编辑区为空时退出，`Ctrl+T` 显示或隐藏 thinking。其余快捷键以 `/hotkeys` 为准。

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
- 内置工具的工作区外写入和识别出的破坏性 Shell 命令需要交互确认；root 默认额外确认 `bash`、`write`、`edit`，可通过 `/permissions root policy` 让显式规则生效。
- 默认权限允许模型检索记忆，但每次模型写入记忆都要求确认；用户可以修改该策略。
- 第三方扩展和 Skills 以当前用户权限运行，安装前必须审查来源与代码。
- `system_snapshot` 只能证明当前设备与工具状态，不能证明模型已经完成转换、量化或 BPU 验收。
- Hobot Code 只适合作为控制面工具，不应进入电机、CAN、GPIO、安全或急停的硬实时闭环。

威胁模型、密钥处理和漏洞报告方式见[安全说明](SECURITY.md)。

## 升级与回滚

安装器会在替换运行时前检查空间、备份已有安装，并拒绝覆盖正在运行的 Hobot Code。用户配置、会话、记忆和目标会保留；默认配置只在缺失时创建。

```bash
hobot update --check       # 只检查最新稳定版本
hobot update               # 下载、校验并升级
hobot update --version 0.14.3
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

`make check` 执行 Shell/JSON 校验、Node 测试、知识库与 Prompt 预算验证、品牌、文档链接和版本一致性检查，以及扩展源码语法与模块依赖检查。`make release` 还会校验完整发行包的文件集合与清单。构建缓存与开发覆盖项见[配置说明](docs/configuration.md#构建覆盖)。贡献前请阅读[贡献指南](CONTRIBUTING.md)。

## 文档

| 文档 | 内容 |
|---|---|
| [配置说明](docs/configuration.md) | 模型、权限、记忆、目标、Hook、通知和 LSP |
| [系统架构](docs/architecture.md) | 运行路径、适配层、数据边界与部署模型 |
| [用户目录布局](docs/user-directory-layout.md) | 配置、状态、迁移与安装目标用户 |
| [设计调研](docs/prime-agent-crush-review.md) | Prime Agent 与 Crush 的可借鉴设计 |
| [发布流程](docs/releasing.md) | 版本、GitHub Release、来源证明与实机检查 |
| [安全说明](SECURITY.md) | 权限边界、密钥、第三方代码与漏洞报告 |
| [贡献指南](CONTRIBUTING.md) | 本地验证、变更要求与提交检查表 |
| [变更记录](CHANGELOG.md) | 各版本行为变化 |

## 上游与许可证

Pi 的版本、提交和 Linux ARM64 SHA256 固定在 `pi-runtime/pi.lock`，`fd` 与 `ripgrep` 的版本、来源和校验值固定在 `pi-runtime/tools.lock`。发行包携带对应第三方许可证，仓库保留的许可证文本位于 `LICENSES/`。Hobot Code 自身采用 [MIT License](LICENSE)。
