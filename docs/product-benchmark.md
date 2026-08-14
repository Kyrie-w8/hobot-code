# 产品基准与升级路线

本文以 2026-08-14 的 Hobot Code `0.27.0` 源码、发行锁定的 Pi `0.84.1`，以及当日可查的官方产品资料为基准。对比的目的不是复制界面，而是明确哪些能力来自上游、哪些是 Hobot Code 的产品价值、哪些差距会阻碍公开交付。

## 产品定位

Hobot Code 应定位为 **运行在 RDK 上、能持续完成真实开发与部署工作的本地工程 Agent**，而不是又一个通用聊天客户端。

- Pi 提供小而强的 Agent 内核、终端体验和开放扩展机制。
- Codex 提供成熟控制面的参考：项目、并行任务、审阅、隔离、恢复和扩展管理形成统一产品。
- Hobot Code 的差异化必须来自 RDK：实机证据、板型知识、BPU 与多媒体状态、部署验收、弱网重连、板端权限和现场支持。

## 能力对照

| 领域 | 原生 Pi | Hobot Code 当前状态 | Codex 参考 | 判断 |
|---|---|---|---|---|
| Agent 内核与 TUI | 完整编辑器、工具、会话树、压缩和消息队列 | 直接使用固定版本 Pi，不复制内核 | CLI、IDE、桌面和云端共享成熟 Agent 协议 | 与 Pi 同源，不应算作独立领先 |
| 模型与 Provider | 多 Provider、OAuth、自动目录刷新、自定义 Provider 和 llama.cpp | 保留 Pi 能力；增加 D-Robotics 网关、受管 API Provider、安全配置和分层资格证据，Studio 可管理并展示精确模型状态 | 模型目录、能力、登录、输入模态和执行策略由统一协议协商 | 密钥与验证闭环已形成，OAuth/目录刷新/本地模型仍未产品化 |
| 扩展 | Extensions、Skills、Prompt templates、themes 和 packages，带项目信任、`pi config` 启停和 npm/git 更新 | 兼容 Pi 扩展；Studio 已按全局/任务上下文展示来源、作用域、信任、状态和权限，并增加 RDK 知识、Hook、LSP、记忆和目标 | Skills、Plugins、MCP、hooks、apps 有发现、认证、启停和逐工具策略 | TUI 继承 Pi 生命周期能力，但 Studio 仍主要只读，且缺可验证加载、签名与隔离 |
| 会话与分支 | JSONL 树、分支、fork 和 compaction | 增加持久任务、Side Agent、编辑历史、断线续读、显式恢复和有界回合证据 | 分页 thread/item、fork、resume、archive、delete、compact 和 rollback | 主要语义与基础恢复证据已具备；大量历史分页、独立验证项和后台终端仍落后 |
| 后台任务 | 默认是前台进程，可由用户自行结合终端复用工具 | `agentd` 托管、持久 FIFO、重连补流、板卡重启后的保守恢复 | 并行长任务、后台终端、计划任务和多环境 | 这是 Hobot Code 相对 Pi 的核心增量；仍缺计划任务和完整进程控制面 |
| 代码审阅 | 可由命令或扩展实现，不是默认产品控制面 | Studio 已有任务绑定的 Changes、二进制安全交付预检和经摘要确认的 staged 回写 | 内置 Git review、跨仓库项目、逐 hunk 操作和 reviewer | 隔离任务已有可靠交付；共享目录归因、选择性暂存/回滚和专用 reviewer 仍缺失 |
| 工作区隔离 | 默认共享当前目录；可由扩展增加 sandbox | 干净 Git 项目可选每任务 worktree；所有 Agent 的工作区写入按轮次互斥；前台 TUI 与后台 worker 共用板端 bubblewrap 档位；支持共享网络、仅模型出口和完全离线三档 | worktree 与 sandbox 是一等能力 | Git、前后台文件/设备/网络边界已闭环；仅模型出口覆盖 D-Robotics 和受管 Anthropic/OpenAI，尚不覆盖登录、自管与 Google |
| 权限与安全 | 默认继承启动用户权限；项目资源有信任门，权限弹窗与 sandbox 由扩展或容器提供 | 板端权限策略、路径保护、危险 Shell、项目信任、Hook、硬件租约，以及文件/设备/capability 隔离 | 命名权限档案、文件与域名网络规则、细粒度审批、自动审查和受管策略 | 已明显强于 Pi 默认值；仍缺域名级出站、策略组合、自动审查与组织基线 |
| RDK 专业能力 | 无板卡语义 | X5/S100/S600 识别、27 个官方来源主题、硬件快照、部署与支持包 | 通用软件开发，不提供 RDK 专用闭环 | Hobot Code 的主要不可替代价值 |
| 桌面体验 | 无官方板卡桌面控制面 | Mac Studio 已支持项目、任务、Side Agent、审批、图片、硬件监控和 Changes | 项目、多目录、review、worktree、终端、Skills、automations 和文件预览 | 可用原型已经形成，但工作流完整度和细节成熟度仍有明显差距 |
| 诊断与运营 | 通用日志、可选 telemetry 契约和社区排查 | 一键脱敏支持包、兼容性协商、分层模型资格、升级与回滚 | 反馈、日志、状态数据库、配置约束和多端发布体系 | 单机支持较好，缺少可选崩溃反馈、发布通道和规模化运维证据 |

## 与原生 Pi 的真实差距

Hobot Code 当前锁定的 Pi `0.84.1` 与 2026-08-14 上游主包版本一致，因此 TUI 编辑、消息队列、会话树、自动压缩、模型选择和扩展 API 不存在版本代差。差距主要出现在产品包装，而不是 Agent 内核：

1. **Provider 目录与登录态仍不如 Pi 完整。** Pi 有广泛的内置 Provider、自动模型目录刷新、OAuth 登录、认证预检和 llama.cpp 本地模型入口；Hobot Code 已增加不把密钥写进 JSON 的受管 API Provider、安全增删与原地轮换、明确来源标记、隔离凭据传递以及 Studio 图形化管理，但仍缺 OAuth 登录态、目录刷新和本地模型管理的统一界面。
2. **Studio 尚未接住 Pi 已有的资源生命周期。** Pi 已能通过项目信任决定是否加载项目设置/资源，通过 `pi config` 启停 package 资源，并支持 npm/git 安装、固定版本、更新和模型目录刷新。Hobot Code 已用只读目录汇总内置能力、用户 Provider/Hook/LSP、Pi packages、Skills、Prompt templates、themes 和受信任务资源，并明确区分声明、发现与加载；但 Studio 还不能安全启停、安装或更新，也不能由运行时证明实际加载结果。供应链签名、故障隔离和统一回滚仍缺失；Pi 本身没有内置 MCP，Hobot Code 也不能凭配置名称虚构 MCP 已可用。
3. **上游升级已有不可绕过的三板门禁。** 版本、归档哈希和十项能力兼容契约已经进入发布门禁；随包验收器可执行覆盖 `model-egress-runtime`、`rpc-background`、`session-recovery`、`extension-safety` 与 `tui-basics`，包括后台任务、审批、图片、Side Agent、compaction、中断恢复、历史编辑、包内资源、扩展批处理、权限 Hook、工作区写租约，以及普通用户真实 PTY 下的中文输入、thinking、编辑与脱离重连。Tag 工作流只创建 Draft；受保护的独立晋级流程会把同一候选归档、精确干净提交和 X5/S100/S600 完整矩阵做哈希绑定，只有新鲜证据全部通过才公开。源码测试或单板通过不能代替。
4. **模型资格已有深度，执行覆盖仍不足。** Hobot Code 已区分路由、网关协议、隔离 Agent runtime，并把四个只读 RDK 档案按精确模型/板型/工作流独立持久化和失效；其中三个只验证知识约束的规划，不能替代转换、推理、媒体或硬件执行。下一步不是再加一个“可用”标签，而是让所有公开模型在三种板型上完成真实 RDK 执行工作流套件。

Hobot Code 相对 Pi 的明确增量是板端 `agentd`、Side Agent、D-Robotics Provider、RDK 实机证据、工具权限、硬件与工作区租约、支持包、Studio 和模型部署入口。其余继承能力必须明确标注为 Pi 能力，避免错误评估自身成熟度。

## 与 Codex 的真实差距

Codex 的领先并不只是界面更精致，而是统一控制协议把会话、回合、项目、执行、权限和扩展都做成可查询、可分页、可恢复的产品对象：

1. **安全执行层。** Codex 已把只读/工作区/全权限和自定义命名档案统一到文件路径、工作区根、域名、Unix socket 与本地监听规则，并能对 sandbox、规则、MCP、Skill 和权限请求分别决定是否提示或自动审查。Hobot Code 的前台 TUI 与后台 worker 已统一用 bubblewrap 约束文件、设备和 Linux capability；`offline` 可完全断网，`model-only` 还能在工具无通用网络的情况下，通过 agentd 私有 Unix Socket 保留固定 D-Robotics、Anthropic Messages、OpenAI Chat Completions 和 OpenAI Responses 模型出口。但该代理尚未覆盖 Pi 登录、自管和 Google Generative AI，也仍缺通用域名规则、策略组合、自动审查和组织级受管策略。
2. **会话与执行协议。** Codex 提供分页 thread/turn/item、steer、interrupt、compact、fork、后台终端和状态通知；Hobot Code 已有任务、schema-4 item 和有界恢复证据，但大历史分页、独立验证项、终端进程和非 Git 副作用证据仍较粗。
3. **代码工作流。** Codex 把 review、worktree、Diff、逐 hunk 操作和项目多目录放在同一工作流中；Hobot Code 目前适合“检查并完整交付一个隔离任务”，还不适合复杂人工挑选和跨仓库修改。
4. **扩展控制面。** Codex 能启停 Skills，管理 hooks、MCP OAuth、服务启动要求、工具 allow/deny 与逐工具审批，并对 app/plugin 做独立策略。Hobot Code 已有任务上下文、来源、信任和诊断明确的只读版本化目录，但 Studio 的启停、真实加载状态、更新、认证和供应链契约仍不完整。
5. **规模化管理。** Codex 有 SQLite 可恢复作业状态、受管配置、可选反馈/分析/OTel、自动化任务和多端产品面；Hobot Code 仍是单用户、单板优先，尚未形成组织策略、多板队列和可选的规模化可靠性数据。
6. **模型与产品协同。** Codex 可以针对自己的模型、工具协议和交互面一起优化；Hobot Code 面向异构模型，必须用公开可复核的适配等级和评测门槛弥补这种天然不确定性。

## 当前优势

1. **RDK 现场问题可以被验证，而不是只靠 Prompt 猜测。** Agent 能读取板型、RDK OS、BPU、温度、内存、存储、运行时工具和部署产物，并区分资料与实时证据。
2. **任务真正留在板端。** SSH、Mac 休眠或 VPN 短暂中断不终止 `agentd` 中的工作；客户端按事件序号补齐输出。
3. **安全判断留在执行侧。** Studio 只发请求，权限、路径和硬件资源边界始终由 RDK 上的 worker 判定。
4. **Pi 生态可以继续使用。** Hobot Code 通过扩展和配置适配，而不是维护一套分叉的 Agent 内核。

## 主要差距

### P0：公开 Beta 前必须完成

1. **完成安全执行闭环。** `review`、`workspace`、`system` 三档 bubblewrap 与 `shared`、`model-only`、`offline` 三档网络已覆盖前台 TUI 和后台 worker；`model-only` 已代理内置 D-Robotics 与受管 Anthropic Messages、OpenAI Chat Completions、OpenAI Responses，其他协议会失败关闭而不会静默退回共享网络。受管 Anthropic/OpenAI 的包清单、原生适配器、代理路由、完整事件、凭据隔离，以及普通用户 TUI 基础交互已有三板自动验收器。下一步补齐安装用户、工作目录、硬件设备、网络泄漏、长流式响应、额度滥用和断线矩阵，再根据上游是否支持安全注入传输决定是否扩展 Google 等协议。
2. **完成模型资格矩阵。** 路由、协议、thinking、单/并行工具、非法参数修复、审批、图片、长会话压缩和中断恢复已有有界测试；诊断、部署规划、多媒体规划和硬件安全规划已有独立的三板只读证据槽位，工作区编码明确标为规划中。公开 Beta 前还要对每个公开模型在 X5、S100、S600 上补齐真实代码修改、转换部署、多媒体执行和硬件安全工作流；只有干净发行构建上的完整执行档案才可标为相应资格。
3. **任务级恢复证据。** 最近 32 轮的工具终态、未闭合工具数和脱敏 Git 工作区摘要已由板端持久化，CLI 摘要、Studio 恢复卡和支持包可以区分“完成、部分证据、需审查”，且不会自动重放。下一步仍需把测试/验证结果变成独立证据项，并覆盖非 Git 副作用与后台终端。
4. **扩展三板稳定性矩阵。** 五类 Pi 能力场景与诊断安全、安装生命周期两个产品场景已经成为强制发布门禁；事件空间耗尽后继续滚动持久化及断点过期提示已有本地契约测试。仍需在 X5、S100、S600 上补齐 24 小时任务、SSH/VPN 抖动、daemon 重启、板卡重启、磁盘紧张、滚动日志压力、审批和模型故障，并发布同样可复核的脱敏结果。
5. **桌面发行闭环。** 完成 macOS 签名、公证、可验证更新、崩溃恢复和失败反馈入口；安装失败、协议不兼容和板端缺少依赖都必须给出单一可执行动作。
6. **协议兼容测试。** 为 agentd、SDK 和 Studio 建立录制的跨版本契约样本，至少覆盖当前版、前一小版本和缺失可选能力的板端。

### P1：形成成熟产品体验

1. **Provider 控制面。** 板端已有 `hobot provider` 安全增删与原地轮换、脱敏状态和受管密钥边界；Studio 已能图形化新增、轮换和删除，并由板端返回来源、能力、模型限制和共享密钥影响范围。下一步增加 OAuth 登录态、远端目录刷新、Provider 健康和“已验证/实验性”筛选，不能退回靠硬编码名单扩展。
2. **扩展控制面。** `hobot.extensions/v1` 已在 CLI、agentd、SDK 与 Studio 统一展示内置能力、Pi resources、packages、hooks、LSP 与 Provider 的来源、作用域、声明/发现状态、信任和权限，并把项目扫描绑定到受信任务。下一步增加由 Pi 运行时证明的加载状态、显式启停、版本更新、签名和故障隔离；安装和更新继续由板端执行并接受权限约束。
3. **真正的代码工作流。** 在现有隔离交付上增加按轮次变更、验证结果、冲突提示、逐 hunk 暂存和选择性回滚；提交和推送保持用户显式操作。
4. **板端终端。** 提供任务关联的 PTY、后台进程列表、输入/终止和断线重连，不把任意 Shell 权限搬到 Mac 客户端。
5. **文件与文档输入。** 在现有图片协议上增加受限文件附件；PDF/Office 先在客户端或板端受控解析为有来源的文本/图片块，限制类型、大小、页数和解压边界。
6. **任务编排。** 增加计划任务、完成后动作、依赖关系和资源预约，但仍以板端持久状态和硬件租约为真相来源。

### P2：规模化交付

1. 多板卡任务视图、批量诊断和型号/系统版本分组。
2. 团队权限、受管策略、Provider/Skill 白名单和审计导出。
3. 可选的内网中继或企业控制面，默认仍不暴露板端公网端口。
4. 匿名且可关闭的可靠性指标、版本采用率和失败类别统计，用真实数据驱动优先级。
5. 经过审核的 RDK Skills/插件目录、版本兼容标记和供应链签名。

## 演进原则

- **先完成工作流，再增加入口。** 每项功能都要覆盖启动、进行中、失败、恢复、取消和诊断。
- **把模型能力当成协商结果。** 不假设所有模型都支持 thinking、图片、长上下文或相同工具语义。
- **板端是权限与状态真相。** Mac 端不得持有可绕过板端判断的工具执行路径。
- **共享目录不做虚假归因。** 只有建立 worktree 或检查点证据后，才能把某项变化归属到具体任务。
- **上游固定与同步并存。** 发行继续锁定 Pi 版本和哈希，同时为每次上游升级维护兼容清单、回归结果和回滚依据。
- **以用户任务衡量价值。** 核心指标应是首次成功时间、无需人工干预完成率、断线恢复率、无效审批率、失败可诊断率和三板任务成功率，而不是命令或配置项数量。

## 推荐实施顺序

| 阶段 | 交付物 | 可验收结果 |
|---|---|---|
| 1. 安全基线 | 已实现三档 sandbox、三档网络边界、D-Robotics/受管 Anthropic/OpenAI 固定出口，以及受管 Provider 三板验收器；继续完成普通用户安装和文件、设备、网络攻击矩阵 | 受限 Agent 无法绕过文件/设备策略或取得通用网络；模型仅能访问声明的固定出口；所有降级状态可见 |
| 2. 模型适配 | 已完成四层资格骨架、四个只读 RDK 档案和版本化矩阵；继续增加三板执行档案 | 每个公开模型有固定版本、板型和资源摘要报告；完整执行档案通过后才标相应资格 |
| 3. 恢复证据 | 有界 turn ledger、工具终态、工作区摘要 | 基础闭环已实现；强杀矩阵需继续验证非 Git 副作用和绝不静默重放 |
| 4. 三板可靠性 | 五类 Pi 能力场景与诊断安全、安装生命周期共七个场景已由 Draft 晋级门禁强制执行；继续补齐断网和 24 小时测试 | 发布页附带经过哈希绑定与 attestation 的矩阵证据，关键场景无数据丢失或静默重复执行 |
| 5. 工作流与扩展 | 任务上下文只读目录已建立；继续实现逐 hunk 审阅，以及扩展加载证明、启停、签名和更新 | 用户无需编辑 JSON 即可理解来源、能力与风险；板端可证明加载状态并安全变更生命周期 |
| 6. 发行与规模化 | macOS 签名公证、更新通道、可选反馈、多板视图 | 新用户一条命令装板端、一个安装包装 Studio，失败能自助恢复 |

## 参考资料

- [Pi Documentation](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/index.md)
- [Pi coding agent README](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/README.md)
- [Pi packages](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/packages.md)
- [Pi stream options and custom transport contract](https://github.com/earendil-works/pi/blob/v0.84.1/packages/ai/src/types.ts)
- [Pi Google Generative AI adapter](https://github.com/earendil-works/pi/blob/v0.84.1/packages/ai/src/api/google-generative-ai.ts)
- [OpenAI: Codex App Server](https://learn.chatgpt.com/docs/app-server#api-overview)
- [OpenAI: Codex configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference#configtoml)
- [OpenAI: Agent approvals and security](https://learn.chatgpt.com/docs/agent-approvals-security)
- [OpenAI: Subagents](https://learn.chatgpt.com/docs/agent-configuration/subagents)
- [OpenAI: Remote connections](https://learn.chatgpt.com/docs/remote-connections)
- [OpenAI: Plugins](https://learn.chatgpt.com/docs/plugins)
- [OpenAI: Projects and chats](https://learn.chatgpt.com/docs/projects#use-local-projects-for-folders-and-codebases)
- [OpenAI: Codex app launch](https://learn.chatgpt.com/docs/whats-new#the-codex-app-launches-on-macos)
- [OpenAI: Developer commands](https://learn.chatgpt.com/docs/developer-commands#built-in-slash-commands)
- [OpenAI: Customization order](https://learn.chatgpt.com/docs/customization/overview#next-step)
- [OpenAI: Managed configuration](https://learn.chatgpt.com/docs/enterprise/managed-configuration#admin-enforced-requirements-requirementstoml)
