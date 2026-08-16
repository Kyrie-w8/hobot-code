# Hobot Code 用户手册

本手册面向使用 RDK X5、RDK S100 或 RDK S600 进行日常开发、模型部署和板端调试的用户，适用于 Hobot Code 0.27.x。终端版和 Mac 桌面版使用同一个板端服务、任务、权限和安全边界。

## 1. 先了解三个概念

Hobot Code 的任务设置由三个互相独立的维度组成：

| 设置 | 回答的问题 | 执行位置 |
|---|---|---|
| **Approvals** | 哪些工具操作需要先询问用户？ | 板端权限策略 |
| **Board access** | 即使用户同意，Agent 最多能访问哪些文件、设备和系统特权？ | 板端 Linux OS sandbox |
| **Network** | Agent 工具和模型能够访问哪些网络？ | 板端网络命名空间与模型代理 |

可以简单理解为：Approvals 管“是否需要问”，Board access 管“最多能碰到哪里”，Network 管“能连接到哪里”。Studio 只负责展示和提交选择，最终判定始终在板端执行。

## 2. 安装

### 2.1 安装板端程序

在 RDK 上运行：

```bash
curl -fsSL https://github.com/bryant-w/hobot-code/releases/latest/download/hobot-install.sh | sh
```

安装完成后执行：

```bash
hobot setup
hobot doctor
```

`hobot setup` 配置模型，`hobot doctor` 进行只读检查。建议在继续前确认诊断结果为 **Healthy**。

### 2.2 安装 Mac 应用

从 [GitHub Releases](https://github.com/bryant-w/hobot-code/releases) 下载 `hobot-code-<version>-macos-arm64.dmg`，将 **Hobot Code** 拖入 Applications。

首次添加板卡时填写：

- 名称，例如 `RDK S600`
- IP 地址
- SSH 用户，例如 `root`
- SSH 端口，默认 `22`
- 可选的 SSH 私钥路径

Hobot Code 使用 macOS 自带的 OpenSSH 和 `known_hosts`，不在 Mac 应用中保存 SSH 密码或板端模型密钥。

## 3. 开始一个任务

### 3.1 选择项目

在 Studio 左侧点击 **New conversation**，然后选择板端已有目录或创建新目录。

- **Shared project**：任务直接使用原目录。适合普通目录或已经存在本地改动的项目。
- **Isolated worktree**：为干净且已有提交的 Git 项目创建独立 worktree。适合多个任务并行开发，完成后可在 **Changes** 中审阅并应用回原项目。

删除对话不会删除项目文件。隔离 worktree 也不会随对话删除，必须在确认没有未保存改动后显式清理。

### 3.2 发送消息

- `Enter`：发送
- `Shift+Enter`：换行
- 发送后，发送按钮会在原位置变成停止按钮
- 编辑历史消息会回到该消息之前的上下文，并从修改后的消息创建新时间线

模型菜单位于输入框底部。任务正在执行时不能切换模型；任务显示 **Ready** 或已经停止时可以切换。

## 4. Approvals

Approvals 控制工具调用是否需要用户确认，但不会扩大 Board access 或 Network 的边界。

| 选项 | 行为 | 建议场景 |
|---|---|---|
| **Review only** | 禁止修改项目和系统状态 | 代码审查、日志分析、方案讨论 |
| **Ask for changes** | 修改前询问用户 | 初次使用、不熟悉的项目、高风险任务 |
| **Developer** | 普通读取、构建、测试和工作区编辑尽量不打断；高风险操作仍然询问 | 受信项目的日常开发 |

Developer 不是“允许所有操作”。以下行为仍可能要求确认或被拒绝：

- 删除、覆盖、破坏性 Git 或文件系统命令
- 修改工作区之外或受保护系统路径
- 安装软件包、修改服务、内核或网络配置
- 终止进程或操作板卡硬件
- 持久记忆写入、目标完成、未知工具和 MCP 工具

审批对话可能提供：

- **Allow once**：只允许这一次
- **Allow for this task**：在当前任务内记住完全相同且非危险的调用
- **Deny**：拒绝

危险操作不会提供长期记忆授权。板卡以 root 连接时，Developer 仍然保留硬安全检查。

## 5. Board access

Board access 使用板端 Bubblewrap 创建 OS sandbox。前三档都有 sandbox，只有 **No sandbox** 会关闭它。

| 选项 | 项目写入 | RDK 硬件设备 | Linux capabilities | 适用场景 |
|---|---:|---:|---:|---|
| **Read only** | 否 | 最小设备 | 丢弃 | 只读检查与审查 |
| **Workspace** | 当前项目 | 不开放 | 丢弃 | 普通编码、构建和测试 |
| **Board hardware** | 当前项目 | BPU、ION/Hbmem、DMA heap、摄像头、VPU、ISP、DRI 等白名单设备 | 丢弃 | 模型推理、Camera、多媒体和 BPU 调试 |
| **No sandbox** | 当前板端用户可访问的全部位置 | 当前板端用户可访问的全部设备 | 不额外丢弃 | 明确的系统维护任务 |

`Board hardware` 不是完整 root 权限。它仍然保持宿主根文件系统只读，只在 Workspace 边界上额外开放已识别的 RDK 设备。

`No sandbox` 会让工具直接继承板端用户权限。使用 `root` SSH 连接时，这基本等同整板 root 访问，应只用于无法在受限档位完成的系统维护任务。

### 5.1 为什么 Board access 突然不能修改

Board access 在 worker 启动时决定文件挂载、设备映射和 Linux namespace，不能对已经存在的 worker 热切换。

**Ready 只表示 Agent 正在等待下一条消息，不代表 worker 已停止。** 因此已有对话显示 Ready 时：

- Approvals 可以修改，因为权限策略能够动态加载。
- Board access 和 Network 会锁定，因为它们属于进程级边界。

需要更改时：

1. 在 **Task settings** 中点击 **Stop Agent**。停止只会结束空闲 worker，不会删除对话或已保存的 session。
2. 在同一个面板修改 Board access 或 Network。
3. 选择 **Resume** 继续已有会话；如果没有可恢复会话，则选择 **New session**。

也可以新建对话，在发送第一条消息之前完成设置。

## 6. Network

| 选项 | 模型访问 | 工具访问互联网 | 说明 |
|---|---:|---:|---|
| **Network** | 允许 | 允许 | 使用板端宿主网络；`curl`、Git、SSH 和包管理器等仍受 Approvals 检查 |
| **Model only** | 仅允许受支持的已配置模型 | 禁止 | 工具位于独立网络命名空间，模型请求经 agentd 私有 Unix Socket 代理 |
| **Offline** | 仅本地模型 | 禁止 | 完全断网，模型代理也不可见 |

Model only 只有在所选模型声明支持板端安全代理时才可选择。目前覆盖内置 D-Robotics，以及配置了板端凭据的 Hobot 受管 Anthropic Messages、OpenAI Chat Completions 和 OpenAI Responses Provider。Pi 登录、Google Generative AI 和自管 `models.json` 需要 Network。

受限网络依赖 OS sandbox，因此 No sandbox 只能与 Network 组合。

## 7. 推荐组合

| 目标 | Approvals | Board access | Network |
|---|---|---|---|
| 只读分析代码或日志 | Review only | Read only | Model only |
| 日常开发 | Developer | Workspace | Model only |
| 需要 Git、下载依赖或网络排障 | Developer | Workspace | Network |
| BPU 模型部署和推理 | Developer | Board hardware | Model only 或 Network |
| 摄像头、编解码和多媒体调试 | Ask for changes 或 Developer | Board hardware | 按任务需要选择 |
| 系统安装和板级维护 | Ask for changes | No sandbox | Network |

对于普通开发，优先使用 **Developer + Workspace + Model only**。只有任务确实需要下载、远程 Git 或网络诊断时才切换到 Network。

## 8. Side Agent

点击任务标题栏的 **Side Agent**，可以从主任务最近一次稳定上下文创建独立多轮对话。

- Side Agent 与主任务共享上下文快照、项目、模型和安全边界。
- 两者对话独立，Side Agent 的回答不会写回主对话。
- 两者可能共享同一实际项目，因此写操作受工作区写租约保护。
- 每个主任务同时只能打开一个 Side Agent；每个板端用户的常驻 worker 数量也有上限。
- 删除 Side Agent 只删除会话记录，不会回滚已经产生的文件、进程或硬件副作用。

## 9. 断线和后台运行

### 9.1 Studio 后台任务

Studio 创建的任务由板端 `agentd` 托管。关闭 Mac 应用、Mac 休眠、VPN 短暂断开或 SSH 断开不会终止板端任务。重新连接后，Studio 会从已保存的事件序号继续显示输出。

板卡重启、断电、进程崩溃或内存不足仍可能中断任务。此时 Hobot Code 会显示恢复证据，由用户选择 Resume 或 New session；不会自动重放可能带副作用的工具调用。

### 9.2 终端持久会话

需要保留完整 TUI 时运行：

```bash
hobot persistent
```

主动离开但保持任务运行，输入 `/detach`。重新 SSH 登录后再次运行 `hobot persistent` 即可返回。

## 10. 图片、文件和链接

- 支持 JPEG、PNG、WebP 和 GIF 图片。
- 每条消息最多 4 张，Mac 会在本地进行有界压缩。
- 只有模型明确声明图片输入能力时才开放附件按钮。
- PDF、Word 和其他通用文档附件当前尚未开放。
- 回复中的 HTTP/HTTPS 链接会交给 Mac 默认浏览器打开。

图片通过现有 SSH/RPC 通道发送，不会创建公开上传地址。事件日志只保存附件名称和 MIME 摘要。

## 11. 诊断、版本和更新

### 11.1 常用检查

```bash
hobot --version
hobot doctor
hobot daemon status
hobot task list
```

Studio 右上角提供：

- **Version and updates**：查看 Studio 与板端版本
- **Capabilities**：查看模型、工具、Skills 和扩展能力
- **Board readiness**：运行只读板端诊断
- **Save private support bundle**：生成并保存脱敏支持包
- **Sync board now**：重新同步板卡和任务状态
- **Board monitor**：查看 BPU、温度、内存和任务边界

如果 Studio 与板端版本不一致，优先从版本页面更新板端，再重新连接。升级不会在有活动任务时强制终止 Agent。

## 12. 常见问题

### Board access 或 Network 是灰色的

当前对话的 worker 仍然存在。Ready 也属于活动会话。停止任务后再修改并 Resume，或者新建对话并在首条消息前设置。

### Model only 是灰色的

所选模型不支持板端模型代理、缺少板端受管凭据，或者 Board access 选择了 No sandbox。切换为支持的 D-Robotics/受管模型并启用 Read only、Workspace 或 Board hardware。

### 设置 Developer 后仍然出现审批

Developer 只放行普通开发操作。危险命令、系统路径、外联、软件安装、服务修改、进程终止、硬件写入、MCP 和未知工具仍可能询问。

### 显示任务数量达到上限

如果所有 worker 都正在运行或等待审批，新任务会排队。先处理审批、停止不再需要的任务，或等待当前任务进入 Ready。服务会优先挂起最久未使用的 Ready worker，而不会中断正在工作的 Agent。

### 连接断开后任务是否继续

Studio/agentd 任务以及 `hobot persistent` 任务都会在普通 SSH 或 VPN 断开后继续。普通 `hobot` 会随终端连接结束，不具备该保证。

### Board diagnostics format incompatible

先确认 Studio 和板端版本一致，重新选择板卡并点击 **Sync board now**。然后在板端运行 `hobot doctor --json`。如果 CLI 正常而 Studio 仍显示旧错误，重新连接该板卡；必要时保存支持包。

## 13. 终端速查

```bash
hobot                              # 启动 TUI
hobot persistent                   # 启动或返回持久 TUI
hobot setup                        # 配置模型
hobot doctor                       # 只读诊断
hobot diagnose                     # 生成私有支持包
hobot task list                    # 查看后台任务
hobot task attach <task-id>        # 跟随任务并处理审批
hobot task stop <task-id>          # 停止任务 worker
hobot task resume <task-id>        # 恢复已有会话
hobot task sandbox <task-id> workspace
hobot task network <task-id> model-only
hobot task permissions <task-id> developer
```

更多配置项见[配置说明](configuration.md)，任务状态和恢复规则见[agentd 协议](agentd-protocol.md)，版本兼容边界见[兼容矩阵](compatibility.md)，安全范围和漏洞报告见[安全说明](../SECURITY.md)。
