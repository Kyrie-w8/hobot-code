# Hobot Code agentd 协议

`hobot-agentd` 是按 OS 用户运行的轻量常驻进程。它负责后台任务生命周期、事件持久化和客户端重连，实际 Agent 循环仍由固定版本的 Pi RPC worker 执行。TUI、命令行以及后续桌面客户端共享这一板端控制面；模型、工具、权限和 Skills 不在客户端重复实现。

## 传输与身份

协议版本 1 使用一行一个 JSON 对象的 JSONL，通过本机 Unix domain socket 传输。默认 socket 为 `${XDG_RUNTIME_DIR}/hobot-code/agentd.sock`；无有效 `XDG_RUNTIME_DIR` 时使用 `${HOBOT_CODE_STATE_DIR}/agentd/run/agentd.sock`，避免依赖部分 RDK 镜像中仅 root 可进入的 `/tmp`。目录权限为 `0700`，socket 权限为 `0600`。Linux 发行版还通过 `SO_PEERCRED` 拒绝其他 UID。

每个连接只提交一个请求。普通调用收到一个响应后结束；订阅调用先收到响应，再持续收到事件。请求最大 2 MiB，响应最大 8 MiB，Prompt 最大 256 KiB。未知版本、方法、字段或超限数据均拒绝。

计划调度是 `agentd` 的唯一权威，不由 Studio、CLI 或 cron 在客户端计时。计划状态保存在独立私有文件中，临时文件 fsync 后原子替换；读取拒绝符号链接、非当前用户所有、宽松权限、超限、未知字段、重复 ID 或无效状态组合。到期 occurrence 必须在推进 `nextRun` 并写入 durable claim 后才可派发；板端重启时已 claim 的轮次标为不确定且不自动重放，错过的 recurring occurrence 最多合并一次。

运行中的 Pi worker 不会获得主 socket。每轮启动时 `agentd` 只在短路径私有目录创建当前 task 专属 `0600` Unix socket，并以 `HOBOT_CODE_TASK_CONTROL_SOCKET` 与 `HOBOT_CODE_TASK_ID` 注入。该 socket 校验 peer UID，只允许 `schedule.*`，并强制目标为该普通主任务；sandbox 只读挂载该任务目录，不能看到 daemon socket 或其他任务控制目录。worker 退出后 socket 即关闭。外部 CLI、SDK 和 Studio 始终使用主 socket。

请求：

```json
{"protocol":1,"id":"client-1","method":"ping","params":{}}
```

成功响应：

```json
{"protocol":1,"id":"client-1","ok":true,"result":{}}
```

失败响应：

```json
{"protocol":1,"id":"client-1","ok":false,"error":{"code":"invalid_params","message":"..."}}
```

事件：

```json
{"protocol":1,"kind":"event","taskId":"...","sequence":12,"time":"2026-08-11T12:00:00Z","event":{"type":"message_update"}}
```

`id` 由客户端生成，用于关联响应；`sequence` 在单个任务内严格递增，用于断线后的增量重放。外层协议仍为 v1，可选的 `normalized` 字段使用独立的事件 schema v4：

```json
{"protocol":1,"kind":"event","taskId":"...","sequence":13,"time":"2026-08-11T12:00:01Z","event":{"type":"message_update"},"normalized":{"schema":4,"type":"assistant.text.delta","data":{"delta":"done"},"item":{"type":"agentMessage","status":"inProgress"}}}
```

`event` 保留经过板端安全处理的 Pi RPC 内容，或以 `hobot_` 开头的 agentd 内部生命周期事件，用于同版诊断和向后兼容；新客户端应优先消费 `normalized`。标准事件覆盖用户消息、Agent 状态、思考与正文增量、消息完成、工具生命周期、审批生命周期、排队、终态、重试、压缩和扩展错误。Pi 的 `auto_retry_start/end` 映射为 `retry_start/end`，只保留最多五次的 `attempt`、`maxAttempts`、退避时间和成功状态，不保留网关错误载荷。schema 4 保留 schema 3 的 `type`/`data`，并增加稳定的 `item` 类型和生命周期；Shell 映射为 `commandExecution`，其他工具映射为 `toolCall`。工具输入、输出预览最多各 12 KiB，客户端不得把它们视为完整日志；agentd 自行生成的用户事件和 Pi 回传的原始事件都会在订阅与落盘前移除结构化图片载荷，只保留名称、MIME 和省略标记。

支持 `tasks.failure.v1` 的服务会在 `task.failed` 和 `task.interrupted` 中返回稳定的 `code`、脱敏 `message` 与单一 `recovery`。恢复值只用于呈现 `resume`、`restart`、`check-model`、`diagnose` 或无动作，不授权客户端自动重放 Prompt、工具调用或审批。底层错误仅追加到任务私有且有界的 `worker.stderr.log`；任务元数据、Studio 与终端 attach 只展示稳定提示。

支持 `tasks.turn-evidence.v1` 的服务会在任务元数据的 `turnEvidence` 中保留最近 32 轮的恢复证据。每轮记录工具开始、完成、失败和未闭合数量，以及 Prompt 交付前后 Git 状态是否变化；不保存 Prompt、命令、输出、文件名或工作区路径。中断轮次只给出 `review-before-resume` 或 `review-before-restart`，客户端不得据此自动重放。工作区摘要不可用或不完整时，`workspaceChanged` 省略，不能解释为“没有变化”。

支持 `build.identity.v1` 的服务会在 `ping` 中返回 `build`。只有受信的 `BUILD_INFO.json` schema 3 与当前 `agentd` SHA-256、产品版本、运行架构和 Pi 兼容契约摘要全部一致时，`status` 才为 `verified`；缺失显示 `unavailable`，内容不可信或不匹配显示 `invalid`。`pi.compatibility.v1` 表示 `build.piCompatibilitySha256` 已绑定发行包中的 `hobot.pi-compatibility/v1` 契约。返回值不包含构建机路径、环境变量、测试内容或凭据。

## 方法

| 方法 | 参数 | 结果 |
|---|---|---|
| `ping` | `{}` | 版本、PID、协议版本、任务数、路径和可验证构建身份 |
| `capabilities` | `{}` | 协议范围、事件 schema、功能标识和资源上限 |
| `extensions.list` | `{taskId?}` | 版本化的能力清单，包括内置能力、私有用户 Provider/Hook/LSP、Pi extensions/Skills/prompts/themes/packages，以及可选的受信任务项目资源；返回脱敏状态、声明权限、适用板型、来源诊断和只读策略 |
| `system.snapshot` | `{}` | 板卡身份、RDK OS、负载、内存、磁盘、温度、逐核 BPU 负载与频率、Hbmem/DDR 和运行时工具的只读实时状态 |
| `diagnostics.inspect` | `{}` | 返回 `hobot.diagnostics/v1` 等价的只读 readiness 报告：总体状态、检查、固定建议和当前可用或受阻的白名单修复动作；不调用模型、不读取对话/项目内容、不生成文件 |
| `diagnostics.repair` | `{action, confirm}` | 仅接受服务端已声明可用的 `private-runtime-permissions`，且 `confirm` 必须为 `true`；通过不跟随符号链接的文件描述符将已知当前用户运行路径收紧为私有权限，然后返回新的完整报告 |
| `support.bundle` | `{includeContent?: boolean}` | 生成私有、脱敏的支持文件；v2 返回总体状态、检查计数和最多 16 条可执行 finding，另含 ID、板端路径、大小、SHA-256、排除项，并可选择返回不超过 4 MiB 的内容；客户端继续接受 v1 |
| `deployment.inspect` | `{path}` | 有界扫描工作区内的 ONNX、PyTorch、TFLite、HBM 等模型产物，并按当前板型标注兼容性 |
| `deployment.start` | `{cwd, artifactPath, goal?, profile?, name?, model?, permissionMode?}` | 创建绑定当前板型、RDK OS、产物和验收报告契约的持久 Agent 任务；已知工作负载可选择冻结验收档案 |
| `deployment.status` | `{taskId}` | 返回部署阶段、绑定信息和经板端重新校验的结构化报告 |
| `models.list` | `{}` | 板端当前可用模型的 `provider`/`id` 列表；显式受管 Provider 带 `managed: true`，不包含凭据 |
| `models.health` | `{model?, force?}` | 主动验证一个 D-Robotics 模型的真实流式路由；返回有界、脱敏、缓存的状态与延迟，不创建任务或会话 |
| `models.conformance` | `{model?, force?}` | 显式探测网关流式终态、结构化工具调用、匹配的工具结果续接及模型声明的图片输入；返回有界、脱敏、缓存的逐项结果及未测的 Agent/RDK 层级 |
| `models.runtime-probe` | `{model?}` | 在隔离的无会话和临时私有会话 Pi RPC 中验证工具、语义错误修复、thinking、关联审批、声明的图片输入、压缩和工具中断后的精确会话恢复；合成探针通过仅返回 `partial`，RDK 任务仍单独验收 |
| `models.rdk-probe` | `{model?, profile?}` | 在当前 ARM64 RDK 上执行一个注册且可运行的只读档案：仅开放板卡快照与版本知识检索，交叉验证工具证据和模型结构化结论，并返回绑定构建、Pi、专家 Prompt、完整 RDK 扩展包、知识包和目标板卡的非缓存报告 |
| `models.rdk-matrix` | `{model}` | 不调用模型，返回精确模型在当前 X5/S100/S600 上每个版本化 RDK 档案的可用性、未测/当前/失效证据、失效原因与明确未覆盖项 |
| `models.qualification` | `{model}` | 只读返回精确模型最近一次分层资格证据，不调用模型；配置/构建/板卡/RDK 资源漂移或短时结果过期时返回受影响层，客户端不得继续当作当前结论 |
| `workspace.list` | `{path?}` | 浏览当前用户可见的目录，只返回子目录 |
| `workspace.create` | `{parent, name}` | 在用户明确选定的父目录中创建私有工作目录 |
| `workspace.changes` | `{taskId}` | 读取任务绑定目录的 Git 状态与有界文本 Diff；返回 Git 是否可用、仓库状态、作用域、文件列表和截断标记 |
| `workspace.isolation` | `{path}` | 由板端检查目录是否为干净、有 `HEAD` 的 Git 项目，并返回 `shared`/`worktree` 建议 |
| `workspace.worktrees` | `{}` | 列出当前用户由 agentd 管理的隔离工作区及引用状态 |
| `workspace.writes` | `{}` | 列出当前用户正在修改工作区的 Agent 轮次 |
| `workspace.cleanup` | `{taskId}` | 显式清理无对话引用、无未提交内容、无新提交的受管 worktree |
| `daemon.shutdown` | `{force?: boolean}` | 请求服务停止；有活跃任务时必须显式 `force` |
| `schedule.create` | `{name?, taskId, prompt, at? | every?}` | 在已有普通主任务上创建一次性或固定间隔的持久计划；`at` 必须为带时区的未来 RFC3339，`every` 为 1 分钟至 30 天 |
| `schedule.list` | `{all?}` | 返回脱敏计划列表，永不包含 Prompt |
| `schedule.show` | `{id, details?}` | 返回一个计划；仅 `details:true` 返回 Prompt |
| `schedule.pause` / `schedule.resume` | `{id}` | 暂停或恢复未来触发；已完成的一次性计划不可恢复 |
| `schedule.run-now` | `{id}` | 请求一轮立即执行；忙碌任务只保留一个待执行 occurrence |
| `schedule.delete` | `{id}` | 删除未来计划，不中断已经开始的本轮 |
| `task.start` | `{name?, cwd, prompt, images?, approve?, model?, approvalModel?, permissionMode?, workspaceMode?, sandboxMode?, networkMode?}` | 新任务元数据；空 `approvalModel` 表示跟随任务模型，固定模型要求 `tasks.permissions.model.v1`；`permissionMode` 可为 `review`、`ask`、`auto-review` 或 `developer`。新版 `auto-review` 要求 `tasks.permissions.llm-review.v1`，与 OS sandbox 档位独立；`workspaceMode` 为 `shared` 或 `worktree`；`sandboxMode` 为 `review`、`workspace`、`system` 或 `off`；`networkMode` 为 `shared`、`model-only` 或 `offline`；无 worker 槽时返回 `queued` 任务 |
| `task.list` | `{}` | 未归档任务元数据，按创建时间倒序 |
| `task.page` | `{cursor?, limit?, includeArchived?}` | 有界任务分页与下一游标 |
| `task.get` | `{taskId}` | 单个任务元数据 |
| `task.rename` | `{taskId, name}` | 更新任务显示名称 |
| `task.archive` | `{taskId, archive}` | 归档或取消归档终态任务 |
| `task.delete` | `{taskId}` | 删除已归档的终态任务及本地日志 |
| `task.resume` | `{taskId, prompt?, images?}` | 重新打开已校验的 Pi session，可选发送新 Prompt 与图片 |
| `task.restart` | `{taskId, prompt, images?}` | 保留任务记录与工作目录，启动一个不继承旧上下文的新 session |
| `task.fork` | `{taskId, sequence?, prompt?, images?, name?, kind, model?, approvalModel?, permissionMode?, sandboxMode?, networkMode?}` | `side` 从最新稳定上下文创建独立任务，Prompt 可省略并在首条消息时启动；`edit` 从指定用户消息之前创建替换时间线且必须提供 Prompt；未指定审批模型时继承来源任务 |
| `task.model` | `{taskId, provider, modelId}` | 为 idle worker 切换模型，或为 queued/终态任务持久化下次启动使用的模型 |
| `task.permissions` | `{taskId, mode}` | 为 idle、queued 或终态任务设置独立的 `review`、`ask`、`auto-review` 或 `developer` 权限策略；客户端只有在服务声明 `tasks.permissions.llm-review.v1` 时才把 `auto-review` 显示为模型版 **Approve for me** |
| `task.approval-model` | `{taskId, model}` | 为 idle、queued 或终态任务设置独立审批模型；空字符串恢复跟随 Agent 模型，固定选择必须是当前板端模型出口可用的 `provider/model` |
| `task.sandbox` | `{taskId, mode}` | 为 queued 或终态任务设置板端 OS 隔离档位；运行中的 worker 不允许热切换 |
| `task.network` | `{taskId, mode}` | 为 queued 或终态任务设置 `shared`/`model-only`/`offline` 网络边界；后两者要求有效 Bubblewrap sandbox |
| `task.command` | `{taskId, command}` | 把一条 Pi RPC 命令发送给 worker |
| `task.approvals` | `{taskId}` | 有界待审批队列，包含活跃和已失效项 |
| `task.stop` | `{taskId}` | 终止 worker 进程组 |
| `task.events` | `{taskId, after?, limit?}` 或 `{taskId, direction:"before", before?, limit?}` | 向后读取 `sequence > after`，或在 `events.page.before.v1` 下读取严格早于 `before` 的最近一页；反向模式中 `before=0` 表示持久尾页；每页最多 1000 条并返回保留边界 |
| `task.subscribe` | `{taskId, after?, follow?}` | 先返回保留边界并重放 `sequence > after` 的事件，再按需跟随 |

终端 `hobot deploy inspect/start/status` 是上述部署方法的薄客户端，不另设权限或状态体系。`start` 返回普通持久任务 ID，可继续使用 `hobot task attach/stop`；SSH 断开不会终止任务。

`extensions.list` 每次调用都会重新检查 Provider、Hook、LSP 和 Pi 固定资源目录，不要求重启 agentd。全局范围只读取当前用户的 `settings.json`、`extensions`、`skills`、`prompts`、`themes` 和 `~/.agents/skills`；目录遍历限制条目数和深度，不跟随符号链接，并拒绝不属于当前用户或可由组/其他用户写入的资源。私有配置还必须只允许当前用户读写。来源无效时按来源 fail closed；安全目录中的单个不安全条目被跳过并返回 `partial`。

项目资源不能通过客户端路径查询。客户端只能传入已存在的 `taskId`，agentd 再从任务私有元数据解析实际运行目录；任务创建时未批准项目资源则只返回 `untrusted`，不会读取 `.pi/settings.json`、`.pi/extensions`、`.pi/skills`、`.pi/prompts`、`.pi/themes` 或从任务目录到 Git 根的 `.agents/skills`。工作目录、`.pi` 或 `.agents` 是符号链接、可被其他用户写入或不满足所有权检查时 fail closed。

返回值不包含模型端点、token、Hook 命令、LSP 参数、设置中的绝对路径或 package URL。`discovered` 只表示在受控目录发现了候选资源，`declared` 只表示设置中存在声明；两者都不证明 Pi 已加载、执行或成功安装。该方法不安装 package、不加载代码、不启停资源，也不改变权限。Pi 没有可由 agentd 独立验证的通用 MCP 核心注册表；由 extension/package 提供的 MCP 能力只按其真实宿主展示，不根据名称猜测独立 MCP 状态。

`workspace.changes` 不接受客户端路径，只能解析服务端任务元数据中已经持久化的工作目录。服务只调用受信的系统 Git，移除 `GIT_*` 环境变量，禁用 hooks、fsmonitor、external diff、textconv、pager 和 submodule 内容；状态最多返回 200 个文件，文本 Diff 最多 512 KiB，未跟踪文件和二进制内容不会通过该方法返回。路径和 Diff 中的控制字符会被替换。该结果是共享工作区的当前快照，不证明某个文件由当前 Agent 修改，也不提供写入、暂存、提交或回滚能力。

`workspace.isolation` 和 `task.start workspaceMode=worktree` 在板端重新执行相同检查，客户端预检不是授权。只有非 bare、已有 `HEAD`、整个仓库无跟踪和未跟踪改动时才会创建 detached worktree，运行目录放在当前用户的 `0700` agentd 状态下。主任务、Side Agent 和编辑分支共享同一个受管 worktree；不同根任务可以隔离。删除对话只删除会话和日志，不删除代码。`workspace.cleanup` 会核对私有 manifest、Git common-dir/git-dir、工作区干净状态和创建时的基线提交；任一对话仍引用、存在未提交文件或新提交时均 fail closed。

`workspaces.delivery.v1` 提供 `workspace.delivery {taskId}` 预检与 `workspace.apply {taskId, expectedDigest}` 显式交付。预检只生成有界、支持二进制文件的 Git 快照摘要；应用必须回传该 SHA-256，板端会锁住隔离工作区和原项目后重新生成并比对，防止客户端审阅后内容变化。应用前还会确认原项目仍位于创建时提交且完全干净、隔离工作区没有新提交、两侧没有正在运行、排队或等待审批的 Agent。明确应用时，板端条件式停止仍在等待下一条消息的 idle Agent，把同一份快照应用为原项目的 staged changes，并记录摘要；不会创建 commit 或 push。应用后若隔离工作区又发生变化，清理会拒绝。ignored artifacts 不进入交付快照，清理前仍需单独保存或删除。

`workspaces.write-leases.v1` 表示运行时会为 `bash`、`write`、`edit`、质量门和 MCP 等可修改工作区的工具取得按物理目录判定的私有写租约，并持有到本轮 Agent 完成。路径相同、父子目录重叠或属于同一 Git 根目录的另一个 Agent 写入会被拒绝；只读工具不受影响。并行工具批次中的写调用也必须串行。租约包含 PID 活性检查，崩溃进程的记录可自动回收；`workspace.writes` 与系统快照提供当前占用，支持包只保留占用事实并清除任务、进程和路径身份。

运行时还会在模型开始工作时记录有界的文件元数据指纹，并在每个写工具前复核、在工具完成后刷新。由另一个 SSH、IDE、脚本或非 Hobot Code 进程造成的源码变化会阻止下一次写入，要求 Agent 重新读取后进入新的模型步骤。指纹排除 `.git`、依赖、缓存和生成目录，只使用路径、大小、权限和时间元数据，不读取或传输源码内容；超大目录会在固定条目数后截断并向用户说明保护已降级，跨 Agent 写租约仍保持生效。

终端 `hobot model check drobotics/kimi-k3` 使用 `models.health`。探测发送无工具的最小请求，优先验证 SSE，并只在成功响应不完整或端点明确不支持流式格式时进行一次缓冲回退。每次响应最多读取 256 KiB，整体约 12 秒超时，同一模型结果缓存 5 分钟；`--force` 跳过现有缓存。返回值只有 `available/unavailable`、认证/限流/路由/超时/网络/网关/协议类别、首包与总耗时，不包含网关响应正文、请求 ID、Prompt 或凭据。不可用结果同样缓存，避免故障期间重复冲击网关。该探测可能产生极少量模型 token，只由用户主动触发，不在 Studio 连接时自动运行。

终端 `hobot model probe drobotics/kimi-k3` 使用 `models.conformance`；旧的 `verify` 只作为兼容别名。它依次执行工具调用、对应工具结果续接和（仅对声明支持图片的模型）有效的 32x32 PNG 输入。每一步都先验证流式终态；若网关返回成功但流不完整，则使用有界缓冲回退继续探测。返回的 `scope` 固定为 `gateway-protocol`，`agentRuntimeStatus` 和 `rdkTaskStatus` 在未执行对应套件时必须为 `not-tested`。`verified`、`compatible` 和 `failed` 仅是为协议兼容保留的探测状态，不是整体模型资格。报告只保留逐项状态、脱敏说明、耗时和次数，原始请求、响应、模型文本与凭据均不落盘。结果按精确模型缓存 1 小时，`--force` 可重测；它消耗少量模型 token，不会在连接板卡时自动执行。证据分层见[模型适配等级](model-adaptation-levels.md)。

终端 `hobot model rdk-probe [--profile ID] PROVIDER/MODEL` 使用 `models.rdk-probe`，且不缓存；内置 D-Robotics 和显式 Hobot 受管模型可用。服务先确认当前主机是 X5、S100 或 S600 的 ARM64 RDK，再在私有临时目录启动无会话 Pi RPC，只复制所选 Provider 的严格校验元数据、显式加载产品 RDK 扩展并仅开放 `system_snapshot` 与 `rdk_docs_search`。验证器要求唯一、顺序且关联的两次工具调用，工具快照必须与 agentd 独立采集的板型、RDK OS 和架构一致，知识结果必须匹配打包知识版本并引用清单内的 D-Robotics 官方 URL，最终模型 JSON 必须严格复现这些证据。报告不包含模型正文、工具正文、思考或凭据；只保留检查状态、官方来源 URL 和可追溯摘要。只有干净、可验证、Linux ARM64 的发布构建才能设置 `releaseEligible: true`。

档案注册表目前包含板卡诊断、模型部署规划、多媒体规划、硬件安全规划四个可运行的只读档案，以及明确为 `planned` 的隔离工作区编码档案。后三个可运行规划档案只证明模型能基于实时板型和版本化官方知识形成受约束方案，不证明转换、量化、板端推理、精度、性能、摄像头、编解码、GPIO、CAN、固件或电源操作已经执行。`models.rdk-matrix` 与 `hobot model profiles` 从私有 `model-rdk-matrix.json` 读取每个精确模型/档案的最近证据；构建、Pi、板型、RDK OS、Prompt、扩展或知识包变化会保留旧结果但标记为 `stale`。规划中档案不能携带结果，客户端也不得将规划证据提升为执行资格。

后台 worker 默认在 bubblewrap 中运行。`review` 只读工作区且只看到最小设备，`workspace` 允许当前工作区写入，`system` 另外开放 BPU、ION/Hbmem、DMA heap、video 和 media 设备，`off` 明确关闭 OS 隔离。前三档均使用只读宿主根文件系统、丢弃 Linux capabilities，并让任务控制目录保持只读。`shared` 使用宿主网络；`model-only` 与 `offline` 都通过 `--unshare-net` 创建独立网络命名空间，前者只重新挂载 agentd 的私有模型 Unix Socket，后者隐藏该 Socket。`model-only` 不向 worker 传递任何模型密钥，只允许启动快照中具有凭据和适配器的 D-Robotics、Anthropic Messages、OpenAI Chat Completions、OpenAI Responses Provider 及其精确模型。Google Generative AI、Pi 登录、自管模型、缺失密钥、未声明模型和配置漂移均 fail closed，绝不退回 `shared`。

`task.command` 当前支持 Pi RPC 的 `prompt`、`abort`、`set_model` 与 `extension_ui_response`。`prompt` 可携带最多 4 个 `ImageContent` 项；每项包含 `type: "image"`、base64 `data`、受支持的 `mimeType`，以及仅用于显示的可选 `name`。板端会校验数量、MIME、base64 和总大小，事件日志只记录附件名称与 MIME 摘要，不持久化图片数据。客户端应使用 `task.model` 切换模型：活动 worker 只在 `idle` 时接受，`stopped`、`failed` 和 `interrupted` 任务会将选择写入元数据并在下次 Resume/Restart 生效。`task.permissions` 为每个任务写入私有策略文件；`review` 禁止变更，`ask` 确认变更，`developer` 放行日常 Shell 与工作区编辑，但破坏性命令、受保护路径、持久状态与未知/MCP 工具仍由板端保护或确认。审批事件沿用 worker 的请求 ID；客户端只能回复当前活跃 ID。审批队列最多保留 16 项，文本、选项数和超时均有上限。权限结果始终在板端 worker 内判定，客户端无法绕过。

## 任务状态

```text
queued -> starting
starting -> running -> idle -> running
                    -> waiting -> running
任何活动状态 -> stopping -> stopped
任何活动状态 -> failed
agentd 停止或重启时的已启动状态 -> interrupted
```

`queued` 表示请求和 Prompt 已在板端以私有文件原子落盘，但尚未启动 worker。队列按入队时间 FIFO 调度；worker 槽释放或现有 Agent 完成本轮进入 `idle` 后，服务会挂起最久未使用的 idle worker并启动队首。调度器会在启动 worker 前先把队列项原子标记为 `launching`：若服务在交接窗口崩溃，该任务恢复为 `interrupted` 并要求用户显式 Resume/Restart，绝不猜测并重放可能已经产生副作用的工作。`task.stop` 可取消尚未执行的队列项。`idle` 表示 worker 仍在等待下一轮输入，不是任务进程已经退出。`waiting` 表示 Agent 正在等待确认、选择或补充输入。`stopped`、`failed` 和 `interrupted` 为终态。它们不会自动重启 worker；具有安全 session 绑定的未归档任务可通过 `task.resume` 续接上下文，没有可用 session 或需要明确丢弃上下文时可通过 `task.restart` 启动新会话。空白 Side Agent 以 `stopped` 和 `awaitingPrompt: true` 持久化，不启动 worker、不占活跃任务槽；客户端发送首条消息时通过 `task.resume` 从私有分支 session 启动。`agentd` 只有在 worker 返回的 session 文件已经真实创建、位于配置的 session 目录内且通过私有文件检查后才持久化绑定；启动恢复时会清除已经失效的绑定，避免客户端展示一个必然失败的 Resume。

支持空白 Side Agent 的服务声明 `tasks.fork.deferred-prompt.v1`。客户端不得仅凭 `tasks.fork` 推断 Prompt 可省略；连接旧服务时应提示升级，而不是发送必然失败的创建请求。

支持 `tasks.collaboration.v1` 的服务在任务元数据中提供 `sourceTaskId`、`currentActivity`，并在能力响应中提供 `maximumSideTasks`。`parentTaskId` 保持 Side Agent 在同一主任务下的扁平展示关系，`sourceTaskId` 记录实际派生来源。`currentActivity` 只允许 `thinking`、`responding`、`waiting for approval` 和经过严格工具名校验的 `using <tool>`；它不包含 Prompt、回答正文、隐藏思考、命令参数或工具输出。默认每个主任务最多有两个正在运行或等待首条消息的 Side Agent，可通过 `HOBOT_CODE_MAX_SIDE_AGENTS=1..8` 调整。

每个后台 worker 根据不可由客户端覆盖的任务元数据收到 `main` 或 `side` 身份。Side worker 禁止写持久记忆和目标状态。扩展在每轮开始时从私有任务元数据重新构造同一任务族的有界协作快照；共享工作区中主任务处于 `starting`、`running` 或 `waiting` 时，Side Agent 的写工具会在执行前拒绝。协作元数据缺失、过宽、超限、链接、格式错误或关系无效时不向 Prompt 注入内容，Side Agent 的工作区写入 fail closed。独立工作区仍按通常租约和权限执行。

## 持久化与恢复

状态位于 `~/.local/state/hobot-code/agentd`：

```text
agentd.pid
agentd.log
support/hobot-code-support-<UTC>-<id>.json
tasks/<task-id>/metadata.json
tasks/<task-id>/events.jsonl
tasks/<task-id>/worker.stderr.log
tasks/<task-id>/queue.json                # queued 及短暂 starting 交接状态
```

目录和文件分别使用 `0700` 与 `0600`。元数据使用临时文件加原子重命名更新；恢复时拒绝符号链接、异常所有者、宽松权限、超限文件和无效任务 ID。事件日志每个任务默认最多 16 MiB，可通过 `HOBOT_CODE_MAX_EVENT_MIB=1..64` 调整。达到上限后，agentd 以原子方式滚动为连续的最新事件窗口，并继续持久化后续事件，不会因为旧历史占满空间而只向在线客户端发送。worker stderr 与 agentd 故障细节合计最多保留 1 MiB；支持文件最多 4 MiB、只保留最近 5 份；每次生成都使用原子私有写入。

支持 `events.retention.v1` 的 `task.events` 结果和 `task.subscribe` 首个确认会返回 `retainedFrom`、`retainedThrough`、`latestSequence`、`historyTruncated` 与 `cursorExpired`。`retainedFrom..retainedThrough` 是当前可重放的连续持久区间；`cursorExpired` 表示客户端断点早于该区间，应从 `retainedFrom` 恢复；`latestSequence > retainedThrough` 则表示近期事件存在真实持久化缺口，而不只是旧历史正常滚动。旧客户端可以忽略这些新增字段。

支持 `events.page.before.v1` 时，反向页保持事件按 sequence 升序，并额外返回 `nextBefore` 与 `hasEarlier`。客户端只能把 `nextBefore` 用作下一次 `before` 游标，不能用数组位置代替；`after` 与反向模式不能组合。每个反向请求都会重新验证完整日志 envelope、连续 sequence、单条记录和总响应大小。分页期间如果保留窗口已经越过客户端游标，结果设置 `cursorExpired`，客户端必须明确显示历史边界，不能静默当作已经读到第一条记录。

客户端断开不会终止 daemon 或 worker。重新连接后使用最后收到的 `sequence` 继续订阅，即可先补齐仍保留的持久事件再接收实时事件；若断点已经过期，CLI 和 Studio 会明确提示实际重放起点。尚未启动的 `queued` 请求在 daemon 或板卡重启后继续排队；Prompt 在入队时只生成一次持久用户事件，恢复执行不会重复显示。已经启动的任务在 daemon 停止、崩溃或板卡重启时标记为 `interrupted`，其未完成审批标记为非活跃。`task.resume` 会先验证 session 是当前用户所有、权限私有、大小有界且物理路径位于配置的 session 目录内，然后使用上游运行时的 `--session` 续接。它不会自动重放已经开始的 Prompt、工具调用或审批；这是为了避免重复写文件、操作设备或执行其他不可逆副作用。`task.restart` 会清除任务的旧 session 绑定，并在同一工作目录中启动新 worker；事件日志与任务 ID 保留，但旧会话上下文不会注入新 worker。`task.fork` 不修改源 session 文件：它根据 session 树的 `parentId` 链物化私有分支文件。`side` 取最新已稳定叶节点并作为独立 Agent 展示；省略 Prompt 时只创建私有分支，首条消息才启动 worker。`edit` 要求指定 `sequence` 和替换 Prompt，停止被替代的空闲 worker，继承该条 `user.message` 之前的可见历史，并把修改后的 Prompt 作为同一会话的新时间线。旧时间线仍以内部任务记录保留，但 Studio 会折叠它，不会将编辑操作展示成 Side Agent。

## SSH 标准输入桥接

Mac 等远程客户端通过 `ssh <board> hobot bridge --stdio` 连接。bridge 从标准输入读取一行请求，转发到当前 OS 用户的 Unix socket，并把完整响应或订阅流写到标准输出。长时 `task.subscribe` 会占用该 bridge，因此客户端应为控制请求和每个实时订阅分别建立 SSH 进程。bridge 不监听 TCP、不返回模型 token，也不改变板端 UID 和权限边界。

## 资源与安全边界

`system.snapshot` 只读取固定的 procfs、sysfs 与 debugfs 节点，并在存在时调用固定路径的官方 `hrt_ucp_monitor` 采集 DDR 带宽。该命令有 2.5 秒超时、256 KiB 输出上限和 5 秒缓存，失败时不影响其他快照字段。BPU 负载来自每个核心的 `ratio` 节点，频率来自对应 devfreq；`bpuTelemetry.status` 区分可用、未检测到设备、RDK OS 未暴露指标和读取失败，客户端不得用 CPU 指标代替。

明确使用 `hrt_model_exec` 等 BPU 工具、`/dev/videoN` 相机设备或地瓜 `vflow/vnode` 媒体管线的 Shell 调用，会在执行前获取当前 OS 用户范围内的硬件资源租约。冲突调用在启动前被拒绝，并返回占用任务、PID 和获取时间；工具结束、失败或 session 关闭时释放。多资源调用按稳定顺序原子抢占，失败会回滚已取得租约；进程崩溃留下的租约会在下一次获取时回收。租约记录位于 `~/.local/state/hobot-code/hardware-leases`，只保存资源名、任务 ID、PID、工作目录与时间，不保存完整命令、模型输入或凭据。`system.snapshot.hardwareLeases` 只返回所有者匹配、权限私有、格式有效且 PID 仍存活的有界摘要。

支持包只保留租约的资源名和获取时间，任务 ID、PID 与工作目录会被清空，避免诊断文件泄露项目路径或进程身份。

`support.bundle` 只组合固定的系统快照、守护进程上限、私有路径权限检查、固定 RDK 工具可用性和任务元数据统计，不读取事件、session、worker stderr、daemon 原始日志、环境变量或工作区。主机名、状态目录和设备绝对路径会被替换；任务标题、工作目录、session 标识、部署路径和原始错误不会进入结果，任务关联与错误仅使用每份文件独立随机密钥生成的截断 HMAC-SHA-256 指纹和错误类别，密钥不持久化。`includeContent` 只决定是否把同一份文件内容通过现有受 UID 保护的协议返回，不改变收集范围或权限判定。

`diagnostics.inspect` 复用支持包的固定检查，但不落盘，也不返回系统快照或任务摘要。客户端配置指纹存在时，它会明确标记运行中服务是否仍使用当前配置；模型配置只返回“可用/缺失”的固定状态，不返回 Provider URL、凭据类型或内容。`restart-daemon` 是 `executor: client` 的建议动作，不由 RPC 直接执行；客户端必须再次确认，并调用既有固定命令 `hobot daemon restart`，而服务端在存在 active 或 queued 任务时仍会拒绝关闭。`diagnostics.repair` 不创建路径、不修改所有权、不跟随链接，也不处理未知路径、凭据、配置、依赖、RDK OS 或资源状态。

`support.bundle.v2` 使用 `healthy`、`attention` 和 `action-required` 三种总体状态。单项检查分为 `pass`、`info`、`warn` 和 `fail`：未安装可选工具、未配置未使用的模型专用出口等只属于 `info`；构建身份、板型/RDK 主线、私有路径、沙箱、模型出口、内存、磁盘、温度和任务生命周期异常才产生有界 finding。finding 只能使用固定 code、severity、scope 和恢复建议，不包含任务 ID、项目路径或原始错误。最近 24 小时的失败按认证、模型、网络、存储、资源、中断和其他类别聚合，不逐任务回传。v1 能力与解码保持兼容，但旧结果没有服务端分级和建议，客户端只能根据计数保守展示。

Hbmem 容量与当前分配优先来自 `/sys/kernel/debug/ion/heaps/all_heap_info`，`accelerator.source=ion-debugfs` 表示数值来自内核 heap 账本。服务仅把汇总表中仍有同名 `/proc/<pid>/status` 的记录归属到活跃应用，返回其 Hbmem 与 RSS；其余驱动、固件、已退出进程或无法安全归属的分配计入对应 pool 的 `systemBytes`。PID、heap、客户端与进程数均有上限，且进程名不匹配时拒绝归属，避免 PID 复用产生错误结果。S600 的某些 `carveout` 驱动会把设备地址窗口而非可用物理内存作为 heap 容量；此时服务保留精确分配量与进程归属，但不返回虚假的容量百分比。debugfs 不可读时才回退到 `hrt_ucp_monitor-estimate`，该来源的 pool 与进程信息只能视为近似值。各 Hbmem pool 是共享 DDR 中用途不同的保留区，不会相加成虚构“显存总量”；`carveout` 也可能由 BPU、编解码和系统组件共同使用。

为兼容旧版板端，`aiMemory` 仍保留 BPU client allocation、ION 活跃分配、ION orphaned、CMA 和 DMA-BUF 诊断字段；不同或可能重叠的指标不会相加。大于实机物理内存的异常 heap 容量不会上报。每个调试文件最多读取 2 MiB，heap 最多返回 32 项，heap 客户端最多解析 256 项，活跃进程最多返回 32 项，BPU 核心最多返回 16 项。

界面术语遵循地瓜官方资料中的 BPU `ratio`/负载、BPU 运行频率和 ION/Hbmem：X 系列将 ION 描述为供 BPU 与图像、视频模块使用的物理内存；S100/S600 的 Hbmem 基于 ION，并区分 `cma_reserved`、`carveout`、`cma`、`ion_uncache` 等 heap。来源：[hrut_somstatus](https://developer.d-robotics.cc/rdk_x_doc/en/Appendix/rdk-command-manual/cmd_hrut_somstatus)、[S100/S600 CPU-BPU-DDR 测试](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/linux_development/hardware_unit_test/bpu_cpu_ddr_stress)、[X 系列 ION 配置](https://developer.d-robotics.cc/rdk_x_doc/System_configuration/srpi-config)、[S100/S600 Hbmem](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/linux_development/driver_development_super/driver_hbmem/s100_hbmem_hardware)。

`deployment.inspect` 不执行模型或工具，只在所选工作区内检查最多 4096 个目录项、返回最多 256 个候选、进入最多 4 层目录，不进入隐藏目录、不跟随符号链接。文件扩展名、板型名和 march 标识只能作为候选提示，不能证明兼容性；运行时 `model_infer_input`、`model_infer_output` 等转储文件会被排除。当前保守路由识别 X5/Bayes、S100/Nash-E/Nash-M、S600/Nash-P；没有明确标识的编译产物保持“待验证”，不会伪装为匹配。`deployment.start` 会把当前实机板型、RDK OS 和可选验收 `profile` 冻结到任务元数据；Agent 写出的报告仅是候选结果，`deployment.status` 会重新检查 schema、板型、源模型绑定、绝对产物路径、工作区边界、普通文件和 SHA-256。新建任务使用报告 schema v2，只有冻结数据集上的全部数值精度指标、最低样本数、模型与端到端延迟分布、吞吐，以及基线/峰值/结束三阶段的 BPU、温度、系统内存和 CMA/ION/Hbmem 证据均满足档案限制时，才允许显示为通过。历史 schema v1 任务仍可读取，但不能伪装成 v2 完整验收。部署任务必须使用 `ask` 或 `developer` 权限，以便在审批后写入验收报告；`review` 会在启动前被拒绝。

- 每个 OS 用户默认最多保留 2 个后台 worker，可通过 `HOBOT_CODE_MAX_BACKGROUND_TASKS=1..8` 调整。创建、分支、Resume 或 Restart 需要空位时，会原子挂起最久未使用的 `idle` worker并保留 session；`running`、`waiting`、`starting` 和 `stopping` 任务绝不会被自动回收。所有槽位都在工作时，新请求进入持久 FIFO 队列，不再返回并发上限错误。
- 默认最多保留 100 个任务，可通过 `HOBOT_CODE_MAX_RETAINED_TASKS=10..1000` 调整。达到上限后拒绝新任务，不会静默删除旧任务。
- worker 位于独立进程组；停止任务会先发送 `SIGTERM`，超时后发送 `SIGKILL`。
- `hobot task start --workspace shared|worktree`、`--model PROVIDER/MODEL`、`--approval-model follow|PROVIDER/MODEL`、`--permissions review|ask|auto-review|developer`、`--sandbox review|workspace|system|off` 和 `--network shared|model-only|offline` 可在创建时固定工作区、Agent/审批模型、工具权限、OS 与网络隔离。`--trust-project` 只传递 Pi 的项目资源信任选项，不会关闭 Hobot Code 的工具权限和硬安全边界；旧 `--approve` 仅作为兼容别名保留。
- 交互式终端中的 `hobot task attach` 可原地处理 confirm、select、input 和多行 editor 审批；`Ctrl+C` 只退出附着界面，板端 Agent 保持运行。首次附着显示全部仍保留的持久事件，之后从当前用户私有的最后已显示序号继续；断点每两秒和退出时原子、跨进程单调写入，多个终端不会让进度倒退，过期时明确提示实际重放起点，损坏时 fail closed 并提示显式使用 `--replay-all`。非交互输出不会代替用户答复，而会打印可复制的 `hobot task respond` 命令。
- `hobot task show` 与 `hobot task approvals` 默认输出适合日常排障的脱敏摘要，不包含 session 文件/标识、审批正文或部署产物与报告绝对路径；本机用户只有显式传入 `--details` 才会获得协议中的完整记录。
- 启动器为当前私有 `hobot.env`、`settings.json`、`models.json` 和受管 `providers.json` 生成不显示在响应中的组合配置指纹。支持 `configuration.fingerprint.v1` 的客户端在模型相关操作前只获得“一致/已变化”结果；变化时必须显式执行 `hobot daemon restart`，任务查看、停止和审批仍然可用。
- daemon 继承启动器已验证的模型环境，但不会记录或通过协议返回认证 token。
- v1 只监听本机 socket，不开放 TCP。桌面端使用 SSH stdio bridge，权限仍由板端判定。
