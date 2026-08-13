# Hobot Code agentd 协议

`hobot-agentd` 是按 OS 用户运行的轻量常驻进程。它负责后台任务生命周期、事件持久化和客户端重连，实际 Agent 循环仍由固定版本的 Pi RPC worker 执行。TUI、命令行以及后续桌面客户端共享这一板端控制面；模型、工具、权限和 Skills 不在客户端重复实现。

## 传输与身份

协议版本 1 使用一行一个 JSON 对象的 JSONL，通过本机 Unix domain socket 传输。默认 socket 为 `${XDG_RUNTIME_DIR}/hobot-code/agentd.sock`；无有效 `XDG_RUNTIME_DIR` 时使用 `/tmp/hobot-code-agentd-<uid>/agentd.sock`。目录权限为 `0700`，socket 权限为 `0600`。Linux 发行版还通过 `SO_PEERCRED` 拒绝其他 UID。

每个连接只提交一个请求。普通调用收到一个响应后结束；订阅调用先收到响应，再持续收到事件。请求最大 2 MiB，响应最大 8 MiB，Prompt 最大 256 KiB。未知版本、方法、字段或超限数据均拒绝。

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

`event` 保留原始 Pi RPC 内容或以 `hobot_` 开头的 agentd 内部生命周期事件，用于同版诊断和向后兼容；新客户端应优先消费 `normalized`。标准事件覆盖用户消息、Agent 状态、思考与正文增量、消息完成、工具生命周期、审批生命周期、排队、终态、重试、压缩和扩展错误。schema 4 保留 schema 3 的 `type`/`data`，并增加稳定的 `item` 类型和生命周期；Shell 映射为 `commandExecution`，其他工具映射为 `toolCall`。工具输入、输出预览最多各 12 KiB，客户端不得把它们视为完整日志；Prompt 图片仍只记录名称和 MIME，不持久化内容。

支持 `tasks.failure.v1` 的服务会在 `task.failed` 和 `task.interrupted` 中返回稳定的 `code`、脱敏 `message` 与单一 `recovery`。恢复值只用于呈现 `resume`、`restart`、`check-model`、`diagnose` 或无动作，不授权客户端自动重放 Prompt、工具调用或审批。底层错误仅追加到任务私有且有界的 `worker.stderr.log`；任务元数据、Studio 与终端 attach 只展示稳定提示。

支持 `tasks.turn-evidence.v1` 的服务会在任务元数据的 `turnEvidence` 中保留最近 32 轮的恢复证据。每轮记录工具开始、完成、失败和未闭合数量，以及 Prompt 交付前后 Git 状态是否变化；不保存 Prompt、命令、输出、文件名或工作区路径。中断轮次只给出 `review-before-resume` 或 `review-before-restart`，客户端不得据此自动重放。工作区摘要不可用或不完整时，`workspaceChanged` 省略，不能解释为“没有变化”。

支持 `build.identity.v1` 的服务会在 `ping` 中返回 `build`。只有受信的 `BUILD_INFO.json` schema 2 与当前 `agentd` SHA-256、产品版本和运行架构全部一致时，`status` 才为 `verified`；缺失显示 `unavailable`，内容不可信或不匹配显示 `invalid`。返回值不包含构建机路径、环境变量或凭据。

## 方法

| 方法 | 参数 | 结果 |
|---|---|---|
| `ping` | `{}` | 版本、PID、协议版本、任务数、路径和可验证构建身份 |
| `capabilities` | `{}` | 协议范围、事件 schema、功能标识和资源上限 |
| `extensions.list` | `{}` | 版本化的能力清单，包括内置扩展与 Skills、私有用户 Provider/Hook/LSP 的脱敏状态、声明权限、适用板型、来源诊断和只读策略 |
| `system.snapshot` | `{}` | 板卡身份、RDK OS、负载、内存、磁盘、温度、逐核 BPU 负载与频率、Hbmem/DDR 和运行时工具的只读实时状态 |
| `support.bundle` | `{includeContent?: boolean}` | 生成私有、脱敏的 schema-v1 支持文件；返回 ID、板端路径、大小、SHA-256、排除项，并可选择返回不超过 4 MiB 的内容 |
| `deployment.inspect` | `{path}` | 有界扫描工作区内的 ONNX、PyTorch、TFLite、HBM 等模型产物，并按当前板型标注兼容性 |
| `deployment.start` | `{cwd, artifactPath, goal?, profile?, name?, model?, permissionMode?}` | 创建绑定当前板型、RDK OS、产物和验收报告契约的持久 Agent 任务；已知工作负载可选择冻结验收档案 |
| `deployment.status` | `{taskId}` | 返回部署阶段、绑定信息和经板端重新校验的结构化报告 |
| `models.list` | `{}` | 板端当前可用模型的 `provider`/`id` 列表，不包含凭据 |
| `models.health` | `{model?, force?}` | 主动验证一个 D-Robotics 模型的真实流式路由；返回有界、脱敏、缓存的状态与延迟，不创建任务或会话 |
| `models.conformance` | `{model?, force?}` | 显式验收流式终态、结构化工具调用、匹配的工具结果续接及模型声明的图片输入；返回有界、脱敏、缓存的逐项结果 |
| `workspace.list` | `{path?}` | 浏览当前用户可见的目录，只返回子目录 |
| `workspace.create` | `{parent, name}` | 在用户明确选定的父目录中创建私有工作目录 |
| `workspace.changes` | `{taskId}` | 读取任务绑定目录的 Git 状态与有界文本 Diff；返回 Git 是否可用、仓库状态、作用域、文件列表和截断标记 |
| `workspace.isolation` | `{path}` | 由板端检查目录是否为干净、有 `HEAD` 的 Git 项目，并返回 `shared`/`worktree` 建议 |
| `workspace.worktrees` | `{}` | 列出当前用户由 agentd 管理的隔离工作区及引用状态 |
| `workspace.writes` | `{}` | 列出当前用户正在修改工作区的 Agent 轮次 |
| `workspace.cleanup` | `{taskId}` | 显式清理无对话引用、无未提交内容、无新提交的受管 worktree |
| `daemon.shutdown` | `{force?: boolean}` | 请求服务停止；有活跃任务时必须显式 `force` |
| `task.start` | `{name?, cwd, prompt, images?, approve?, model?, permissionMode?, workspaceMode?, sandboxMode?}` | 新任务元数据；`workspaceMode` 为 `shared` 或 `worktree`；`sandboxMode` 为 `review`、`workspace`、`system` 或 `off`；无 worker 槽时返回 `queued` 任务 |
| `task.list` | `{}` | 未归档任务元数据，按创建时间倒序 |
| `task.page` | `{cursor?, limit?, includeArchived?}` | 有界任务分页与下一游标 |
| `task.get` | `{taskId}` | 单个任务元数据 |
| `task.rename` | `{taskId, name}` | 更新任务显示名称 |
| `task.archive` | `{taskId, archive}` | 归档或取消归档终态任务 |
| `task.delete` | `{taskId}` | 删除已归档的终态任务及本地日志 |
| `task.resume` | `{taskId, prompt?, images?}` | 重新打开已校验的 Pi session，可选发送新 Prompt 与图片 |
| `task.restart` | `{taskId, prompt, images?}` | 保留任务记录与工作目录，启动一个不继承旧上下文的新 session |
| `task.fork` | `{taskId, sequence?, prompt?, images?, name?, kind, model?, permissionMode?, sandboxMode?}` | `side` 从最新稳定上下文创建独立任务，Prompt 可省略并在首条消息时启动；`edit` 从指定用户消息之前创建替换时间线且必须提供 Prompt |
| `task.model` | `{taskId, provider, modelId}` | 为 idle worker 切换模型，或为 queued/终态任务持久化下次启动使用的模型 |
| `task.permissions` | `{taskId, mode}` | 为 idle、queued 或终态任务设置独立的 `review`、`ask` 或 `developer` 权限策略 |
| `task.sandbox` | `{taskId, mode}` | 为 queued 或终态任务设置板端 OS 隔离档位；运行中的 worker 不允许热切换 |
| `task.command` | `{taskId, command}` | 把一条 Pi RPC 命令发送给 worker |
| `task.approvals` | `{taskId}` | 有界待审批队列，包含活跃和已失效项 |
| `task.stop` | `{taskId}` | 终止 worker 进程组 |
| `task.events` | `{taskId, after?, limit?}` | 按序号读取最多 1000 条持久事件 |
| `task.subscribe` | `{taskId, after?, follow?}` | 先重放 `sequence > after` 的事件，再按需跟随 |

终端 `hobot deploy inspect/start/status` 是上述部署方法的薄客户端，不另设权限或状态体系。`start` 返回普通持久任务 ID，可继续使用 `hobot task attach/stop`；SSH 断开不会终止任务。

`extensions.list` 每次调用都会重新检查 Provider、Hook 和 LSP 配置，不要求重启 agentd。用户配置必须是当前用户拥有、仅当前用户可读写的普通文件，符号链接、过大文件、无效结构和不安全权限会按来源 fail closed。返回值只包含名称、能力类别、数量、可用状态和声明权限，不包含模型端点、token、Hook 命令、LSP 参数或本地路径。该方法不扫描未知目录、不安装 package、不加载代码，也不改变任何权限；Pi package 与 MCP 的统一目录要等其稳定、可验证的本地契约后再接入。

`workspace.changes` 不接受客户端路径，只能解析服务端任务元数据中已经持久化的工作目录。服务只调用受信的系统 Git，移除 `GIT_*` 环境变量，禁用 hooks、fsmonitor、external diff、textconv、pager 和 submodule 内容；状态最多返回 200 个文件，文本 Diff 最多 512 KiB，未跟踪文件和二进制内容不会通过该方法返回。路径和 Diff 中的控制字符会被替换。该结果是共享工作区的当前快照，不证明某个文件由当前 Agent 修改，也不提供写入、暂存、提交或回滚能力。

`workspace.isolation` 和 `task.start workspaceMode=worktree` 在板端重新执行相同检查，客户端预检不是授权。只有非 bare、已有 `HEAD`、整个仓库无跟踪和未跟踪改动时才会创建 detached worktree，运行目录放在当前用户的 `0700` agentd 状态下。主任务、Side Agent 和编辑分支共享同一个受管 worktree；不同根任务可以隔离。删除对话只删除会话和日志，不删除代码。`workspace.cleanup` 会核对私有 manifest、Git common-dir/git-dir、工作区干净状态和创建时的基线提交；任一对话仍引用、存在未提交文件或新提交时均 fail closed。

`workspaces.delivery.v1` 提供 `workspace.delivery {taskId}` 预检与 `workspace.apply {taskId, expectedDigest}` 显式交付。预检只生成有界、支持二进制文件的 Git 快照摘要；应用必须回传该 SHA-256，板端会锁住隔离工作区和原项目后重新生成并比对，防止客户端审阅后内容变化。应用前还会确认原项目仍位于创建时提交且完全干净、隔离工作区没有新提交、两侧没有正在运行、排队或等待审批的 Agent。明确应用时，板端条件式停止仍在等待下一条消息的 idle Agent，把同一份快照应用为原项目的 staged changes，并记录摘要；不会创建 commit 或 push。应用后若隔离工作区又发生变化，清理会拒绝。ignored artifacts 不进入交付快照，清理前仍需单独保存或删除。

`workspaces.write-leases.v1` 表示运行时会为 `bash`、`write`、`edit`、质量门和 MCP 等可修改工作区的工具取得按物理目录判定的私有写租约，并持有到本轮 Agent 完成。路径相同、父子目录重叠或属于同一 Git 根目录的另一个 Agent 写入会被拒绝；只读工具不受影响。并行工具批次中的写调用也必须串行。租约包含 PID 活性检查，崩溃进程的记录可自动回收；`workspace.writes` 与系统快照提供当前占用，支持包只保留占用事实并清除任务、进程和路径身份。

运行时还会在模型开始工作时记录有界的文件元数据指纹，并在每个写工具前复核、在工具完成后刷新。由另一个 SSH、IDE、脚本或非 Hobot Code 进程造成的源码变化会阻止下一次写入，要求 Agent 重新读取后进入新的模型步骤。指纹排除 `.git`、依赖、缓存和生成目录，只使用路径、大小、权限和时间元数据，不读取或传输源码内容；超大目录会在固定条目数后截断并向用户说明保护已降级，跨 Agent 写租约仍保持生效。

终端 `hobot model check drobotics/kimi-k3` 使用 `models.health`。探测发送无工具的最小请求，优先验证 SSE，并只在成功响应不完整或端点明确不支持流式格式时进行一次缓冲回退。每次响应最多读取 256 KiB，整体约 12 秒超时，同一模型结果缓存 5 分钟；`--force` 跳过现有缓存。返回值只有 `available/unavailable`、认证/限流/路由/超时/网络/网关/协议类别、首包与总耗时，不包含网关响应正文、请求 ID、Prompt 或凭据。不可用结果同样缓存，避免故障期间重复冲击网关。该探测可能产生极少量模型 token，只由用户主动触发，不在 Studio 连接时自动运行。

终端 `hobot model verify drobotics/kimi-k3` 使用 `models.conformance`。它依次执行工具调用、对应工具结果续接和（仅对声明支持图片的模型）有效的 32x32 PNG 输入。每一步都先验证流式终态；若网关返回成功但流不完整，则使用与运行时相同的有界缓冲回退继续验收。`verified` 表示原生流式闭环完整，`compatible` 表示 Agent 闭环可用但流式项已明确降级，`failed` 表示必要能力未通过。报告只保留逐项状态、脱敏说明、耗时和次数，原始请求、响应、模型文本与凭据均不落盘。结果按精确模型缓存 1 小时，`--force` 可重测；它消耗少量模型 token，绝不会在连接板卡时自动执行。通过只代表 Agent 协议闭环可用，不代表长上下文、推理质量、额度或 RDK 任务质量已达标。

后台 worker 默认在 bubblewrap 中运行。`review` 只读工作区且只看到最小设备，`workspace` 允许当前工作区写入，`system` 另外开放 BPU、ION/Hbmem、DMA heap、video 和 media 设备，`off` 明确关闭 OS 隔离。前三档均使用只读宿主根文件系统、丢弃 Linux capabilities，并让任务控制目录保持只读；模型和开发服务所需网络仍与宿主共享。板端缺少或无法运行 bubblewrap 时 fail closed，只有显式选择 `off` 才能继续。

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

目录和文件分别使用 `0700` 与 `0600`。元数据使用临时文件加原子重命名更新；恢复时拒绝符号链接、异常所有者、宽松权限、超限文件和无效任务 ID。事件日志每个任务默认最多 16 MiB，可通过 `HOBOT_CODE_MAX_EVENT_MIB=1..64` 调整；worker stderr 与 agentd 故障细节合计最多保留 1 MiB。达到上限后仍持续排空进程管道，避免 worker 因反压卡死。支持文件最多 4 MiB、只保留最近 5 份；每次生成都使用原子私有写入。

客户端断开不会终止 daemon 或 worker。重新连接后使用最后收到的 `sequence` 继续订阅，即可先补齐持久事件再接收实时事件。尚未启动的 `queued` 请求在 daemon 或板卡重启后继续排队；Prompt 在入队时只生成一次持久用户事件，恢复执行不会重复显示。已经启动的任务在 daemon 停止、崩溃或板卡重启时标记为 `interrupted`，其未完成审批标记为非活跃。`task.resume` 会先验证 session 是当前用户所有、权限私有、大小有界且物理路径位于配置的 session 目录内，然后使用上游运行时的 `--session` 续接。它不会自动重放已经开始的 Prompt、工具调用或审批；这是为了避免重复写文件、操作设备或执行其他不可逆副作用。`task.restart` 会清除任务的旧 session 绑定，并在同一工作目录中启动新 worker；事件日志与任务 ID 保留，但旧会话上下文不会注入新 worker。`task.fork` 不修改源 session 文件：它根据 session 树的 `parentId` 链物化私有分支文件。`side` 取最新已稳定叶节点并作为独立 Agent 展示；省略 Prompt 时只创建私有分支，首条消息才启动 worker。`edit` 要求指定 `sequence` 和替换 Prompt，停止被替代的空闲 worker，继承该条 `user.message` 之前的可见历史，并把修改后的 Prompt 作为同一会话的新时间线。旧时间线仍以内部任务记录保留，但 Studio 会折叠它，不会将编辑操作展示成 Side Agent。

## SSH 标准输入桥接

Mac 等远程客户端通过 `ssh <board> hobot bridge --stdio` 连接。bridge 从标准输入读取一行请求，转发到当前 OS 用户的 Unix socket，并把完整响应或订阅流写到标准输出。长时 `task.subscribe` 会占用该 bridge，因此客户端应为控制请求和每个实时订阅分别建立 SSH 进程。bridge 不监听 TCP、不返回模型 token，也不改变板端 UID 和权限边界。

## 资源与安全边界

`system.snapshot` 只读取固定的 procfs、sysfs 与 debugfs 节点，并在存在时调用固定路径的官方 `hrt_ucp_monitor` 采集 DDR 带宽。该命令有 2.5 秒超时、256 KiB 输出上限和 5 秒缓存，失败时不影响其他快照字段。BPU 负载来自每个核心的 `ratio` 节点，频率来自对应 devfreq；`bpuTelemetry.status` 区分可用、未检测到设备、RDK OS 未暴露指标和读取失败，客户端不得用 CPU 指标代替。

明确使用 `hrt_model_exec` 等 BPU 工具、`/dev/videoN` 相机设备或地瓜 `vflow/vnode` 媒体管线的 Shell 调用，会在执行前获取当前 OS 用户范围内的硬件资源租约。冲突调用在启动前被拒绝，并返回占用任务、PID 和获取时间；工具结束、失败或 session 关闭时释放。多资源调用按稳定顺序原子抢占，失败会回滚已取得租约；进程崩溃留下的租约会在下一次获取时回收。租约记录位于 `~/.local/state/hobot-code/hardware-leases`，只保存资源名、任务 ID、PID、工作目录与时间，不保存完整命令、模型输入或凭据。`system.snapshot.hardwareLeases` 只返回所有者匹配、权限私有、格式有效且 PID 仍存活的有界摘要。

支持包只保留租约的资源名和获取时间，任务 ID、PID 与工作目录会被清空，避免诊断文件泄露项目路径或进程身份。

`support.bundle` 只组合固定的系统快照、守护进程上限、私有路径权限检查、固定 RDK 工具可用性和任务元数据统计，不读取事件、session、worker stderr、daemon 原始日志、环境变量或工作区。主机名、状态目录和设备绝对路径会被替换；任务标题、工作目录、session 标识、部署路径和原始错误不会进入结果，任务关联与错误仅使用每份文件独立随机密钥生成的截断 HMAC-SHA-256 指纹和错误类别，密钥不持久化。`includeContent` 只决定是否把同一份文件内容通过现有受 UID 保护的协议返回，不改变收集范围或权限判定。

Hbmem 容量与当前分配优先来自 `/sys/kernel/debug/ion/heaps/all_heap_info`，`accelerator.source=ion-debugfs` 表示数值来自内核 heap 账本。服务仅把汇总表中仍有同名 `/proc/<pid>/status` 的记录归属到活跃应用，返回其 Hbmem 与 RSS；其余驱动、固件、已退出进程或无法安全归属的分配计入对应 pool 的 `systemBytes`。PID、heap、客户端与进程数均有上限，且进程名不匹配时拒绝归属，避免 PID 复用产生错误结果。S600 的某些 `carveout` 驱动会把设备地址窗口而非可用物理内存作为 heap 容量；此时服务保留精确分配量与进程归属，但不返回虚假的容量百分比。debugfs 不可读时才回退到 `hrt_ucp_monitor-estimate`，该来源的 pool 与进程信息只能视为近似值。各 Hbmem pool 是共享 DDR 中用途不同的保留区，不会相加成虚构“显存总量”；`carveout` 也可能由 BPU、编解码和系统组件共同使用。

为兼容旧版板端，`aiMemory` 仍保留 BPU client allocation、ION 活跃分配、ION orphaned、CMA 和 DMA-BUF 诊断字段；不同或可能重叠的指标不会相加。大于实机物理内存的异常 heap 容量不会上报。每个调试文件最多读取 2 MiB，heap 最多返回 32 项，heap 客户端最多解析 256 项，活跃进程最多返回 32 项，BPU 核心最多返回 16 项。

界面术语遵循地瓜官方资料中的 BPU `ratio`/负载、BPU 运行频率和 ION/Hbmem：X 系列将 ION 描述为供 BPU 与图像、视频模块使用的物理内存；S100/S600 的 Hbmem 基于 ION，并区分 `cma_reserved`、`carveout`、`cma`、`ion_uncache` 等 heap。来源：[hrut_somstatus](https://developer.d-robotics.cc/rdk_x_doc/en/Appendix/rdk-command-manual/cmd_hrut_somstatus)、[S100/S600 CPU-BPU-DDR 测试](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/linux_development/hardware_unit_test/bpu_cpu_ddr_stress)、[X 系列 ION 配置](https://developer.d-robotics.cc/rdk_x_doc/System_configuration/srpi-config)、[S100/S600 Hbmem](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/linux_development/driver_development_super/driver_hbmem/s100_hbmem_hardware)。

`deployment.inspect` 不执行模型或工具，只在所选工作区内检查最多 4096 个目录项、返回最多 256 个候选、进入最多 4 层目录，不进入隐藏目录、不跟随符号链接。文件扩展名、板型名和 march 标识只能作为候选提示，不能证明兼容性；运行时 `model_infer_input`、`model_infer_output` 等转储文件会被排除。当前保守路由识别 X5/Bayes、S100/Nash-E/Nash-M、S600/Nash-P；没有明确标识的编译产物保持“待验证”，不会伪装为匹配。`deployment.start` 会把当前实机板型、RDK OS 和可选验收 `profile` 冻结到任务元数据；Agent 写出的报告仅是候选结果，`deployment.status` 会重新检查 schema、板型、源模型绑定、绝对产物路径、工作区边界、普通文件和 SHA-256。新建任务使用报告 schema v2，只有冻结数据集上的全部数值精度指标、最低样本数、模型与端到端延迟分布、吞吐，以及基线/峰值/结束三阶段的 BPU、温度、系统内存和 CMA/ION/Hbmem 证据均满足档案限制时，才允许显示为通过。历史 schema v1 任务仍可读取，但不能伪装成 v2 完整验收。部署任务必须使用 `ask` 或 `developer` 权限，以便在审批后写入验收报告；`review` 会在启动前被拒绝。

- 每个 OS 用户默认最多保留 2 个后台 worker，可通过 `HOBOT_CODE_MAX_BACKGROUND_TASKS=1..8` 调整。创建、分支、Resume 或 Restart 需要空位时，会原子挂起最久未使用的 `idle` worker并保留 session；`running`、`waiting`、`starting` 和 `stopping` 任务绝不会被自动回收。所有槽位都在工作时，新请求进入持久 FIFO 队列，不再返回并发上限错误。
- 默认最多保留 100 个任务，可通过 `HOBOT_CODE_MAX_RETAINED_TASKS=10..1000` 调整。达到上限后拒绝新任务，不会静默删除旧任务。
- worker 位于独立进程组；停止任务会先发送 `SIGTERM`，超时后发送 `SIGKILL`。
- `hobot task start --workspace shared|worktree`、`--model PROVIDER/MODEL`、`--permissions review|ask|developer` 和 `--sandbox review|workspace|system|off` 可在创建时固定工作区、模型、工具权限与 OS 隔离。`--trust-project` 只传递 Pi 的项目资源信任选项，不会关闭 Hobot Code 的工具权限和硬安全边界；旧 `--approve` 仅作为兼容别名保留。
- 交互式终端中的 `hobot task attach` 可原地处理 confirm、select、input 和多行 editor 审批；`Ctrl+C` 只退出附着界面，板端 Agent 保持运行。首次附着显示全部持久事件，之后从当前用户私有的最后已显示序号继续；断点每两秒和退出时原子、跨进程单调写入，多个终端不会让进度倒退，损坏时 fail closed 并提示显式使用 `--replay-all`。非交互输出不会代替用户答复，而会打印可复制的 `hobot task respond` 命令。
- `hobot task show` 与 `hobot task approvals` 默认输出适合日常排障的脱敏摘要，不包含 session 文件/标识、审批正文或部署产物与报告绝对路径；本机用户只有显式传入 `--details` 才会获得协议中的完整记录。
- 启动器为当前私有 `hobot.env`、`settings.json` 和 `models.json` 生成不显示在响应中的组合配置指纹。支持 `configuration.fingerprint.v1` 的客户端在模型相关操作前只获得“一致/已变化”结果；变化时必须显式执行 `hobot daemon restart`，任务查看、停止和审批仍然可用。
- daemon 继承启动器已验证的模型环境，但不会记录或通过协议返回认证 token。
- v1 只监听本机 socket，不开放 TCP。桌面端使用 SSH stdio bridge，权限仍由板端判定。
