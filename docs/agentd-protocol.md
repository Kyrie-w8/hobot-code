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

`id` 由客户端生成，用于关联响应；`sequence` 在单个任务内严格递增，用于断线后的增量重放。外层协议仍为 v1，可选的 `normalized` 字段使用独立的事件 schema v3：

```json
{"protocol":1,"kind":"event","taskId":"...","sequence":13,"time":"2026-08-11T12:00:01Z","event":{"type":"message_update"},"normalized":{"schema":3,"type":"assistant.text.delta","data":{"delta":"done"}}}
```

`event` 保留原始 Pi RPC 内容或以 `hobot_` 开头的 agentd 内部生命周期事件，用于同版诊断和向后兼容；新客户端应优先消费 `normalized`。标准事件覆盖用户消息、Agent 状态、思考与正文增量、消息完成、工具生命周期、审批生命周期、重试、压缩和扩展错误。schema 3 增加了持久化的 `user.message`，使客户端能在断线重连后重建完整对话轮次。标准工具事件不复制 Shell 命令或完整工具输出。

## 方法

| 方法 | 参数 | 结果 |
|---|---|---|
| `ping` | `{}` | 版本、PID、协议版本、任务数和路径 |
| `capabilities` | `{}` | 协议范围、事件 schema、功能标识和资源上限 |
| `system.snapshot` | `{}` | 板卡身份、RDK OS、负载、内存、磁盘、温度、逐核 BPU 负载与频率、Hbmem/DDR 和运行时工具的只读实时状态 |
| `support.bundle` | `{includeContent?: boolean}` | 生成私有、脱敏的 schema-v1 支持文件；返回 ID、板端路径、大小、SHA-256、排除项，并可选择返回不超过 4 MiB 的内容 |
| `deployment.inspect` | `{path}` | 有界扫描工作区内的 ONNX、PyTorch、TFLite、HBM 等模型产物，并按当前板型标注兼容性 |
| `deployment.start` | `{cwd, artifactPath, goal?, profile?, name?, model?, permissionMode?}` | 创建绑定当前板型、RDK OS、产物和验收报告契约的持久 Agent 任务；已知工作负载可选择冻结验收档案 |
| `deployment.status` | `{taskId}` | 返回部署阶段、绑定信息和经板端重新校验的结构化报告 |
| `models.list` | `{}` | 板端当前可用模型的 `provider`/`id` 列表，不包含凭据 |
| `workspace.list` | `{path?}` | 浏览当前用户可见的目录，只返回子目录 |
| `workspace.create` | `{parent, name}` | 在用户明确选定的父目录中创建私有工作目录 |
| `daemon.shutdown` | `{force?: boolean}` | 请求服务停止；有活跃任务时必须显式 `force` |
| `task.start` | `{name?, cwd, prompt, images?, approve?, model?, permissionMode?}` | 新任务元数据；省略名称时从首条 Prompt 生成 Unicode 标题 |
| `task.list` | `{}` | 未归档任务元数据，按创建时间倒序 |
| `task.page` | `{cursor?, limit?, includeArchived?}` | 有界任务分页与下一游标 |
| `task.get` | `{taskId}` | 单个任务元数据 |
| `task.rename` | `{taskId, name}` | 更新任务显示名称 |
| `task.archive` | `{taskId, archive}` | 归档或取消归档终态任务 |
| `task.delete` | `{taskId}` | 删除已归档的终态任务及本地日志 |
| `task.resume` | `{taskId, prompt?, images?}` | 重新打开已校验的 Pi session，可选发送新 Prompt 与图片 |
| `task.restart` | `{taskId, prompt, images?}` | 保留任务记录与工作目录，启动一个不继承旧上下文的新 session |
| `task.fork` | `{taskId, sequence?, prompt, images?, name?, kind, model?, permissionMode?}` | `side` 从最新稳定上下文创建独立任务；`edit` 从指定用户消息之前创建替换时间线 |
| `task.model` | `{taskId, provider, modelId}` | 为 idle worker 切换模型，或为终态任务持久化下次 Resume 使用的模型 |
| `task.permissions` | `{taskId, mode}` | 为 idle 或终态任务设置独立的 `review`、`ask` 或 `developer` 权限策略 |
| `task.command` | `{taskId, command}` | 把一条 Pi RPC 命令发送给 worker |
| `task.approvals` | `{taskId}` | 有界待审批队列，包含活跃和已失效项 |
| `task.stop` | `{taskId}` | 终止 worker 进程组 |
| `task.events` | `{taskId, after?, limit?}` | 按序号读取最多 1000 条持久事件 |
| `task.subscribe` | `{taskId, after?, follow?}` | 先重放 `sequence > after` 的事件，再按需跟随 |

终端 `hobot deploy inspect/start/status` 是上述部署方法的薄客户端，不另设权限或状态体系。`start` 返回普通持久任务 ID，可继续使用 `hobot task attach/stop`；SSH 断开不会终止任务。

`task.command` 当前支持 Pi RPC 的 `prompt`、`abort`、`set_model` 与 `extension_ui_response`。`prompt` 可携带最多 4 个 `ImageContent` 项；每项包含 `type: "image"`、base64 `data`、受支持的 `mimeType`，以及仅用于显示的可选 `name`。板端会校验数量、MIME、base64 和总大小，事件日志只记录附件名称与 MIME 摘要，不持久化图片数据。客户端应使用 `task.model` 切换模型：活动 worker 只在 `idle` 时接受，`stopped`、`failed` 和 `interrupted` 任务会将选择写入元数据并在下次 Resume/Restart 生效。`task.permissions` 为每个任务写入私有策略文件；`review` 禁止变更，`ask` 确认变更，`developer` 放行日常 Shell 与工作区编辑，但破坏性命令、受保护路径、持久状态与未知/MCP 工具仍由板端保护或确认。审批事件沿用 worker 的请求 ID；客户端只能回复当前活跃 ID。审批队列最多保留 16 项，文本、选项数和超时均有上限。权限结果始终在板端 worker 内判定，客户端无法绕过。

## 任务状态

```text
starting -> running -> idle -> running
                    -> waiting -> running
任何活动状态 -> stopping -> stopped
任何活动状态 -> failed
agentd 停止或重启时的活动状态 -> interrupted
```

`idle` 表示 worker 仍在等待下一轮输入，不是任务进程已经退出。`waiting` 表示 Agent 正在等待确认、选择或补充输入。`stopped`、`failed` 和 `interrupted` 为终态。它们不会自动重启 worker；具有安全 session 绑定的未归档任务可通过 `task.resume` 续接上下文，没有可用 session 或需要明确丢弃上下文时可通过 `task.restart` 启动新会话。`agentd` 只有在 worker 返回的 session 文件已经真实创建、位于配置的 session 目录内且通过私有文件检查后才持久化绑定；启动恢复时会清除已经失效的绑定，避免客户端展示一个必然失败的 Resume。

## 持久化与恢复

状态位于 `~/.local/state/hobot-code/agentd`：

```text
agentd.pid
agentd.log
support/hobot-code-support-<UTC>-<id>.json
tasks/<task-id>/metadata.json
tasks/<task-id>/events.jsonl
tasks/<task-id>/worker.stderr.log
```

目录和文件分别使用 `0700` 与 `0600`。元数据使用临时文件加原子重命名更新；恢复时拒绝符号链接、异常所有者、宽松权限、超限文件和无效任务 ID。事件日志每个任务默认最多 16 MiB，可通过 `HOBOT_CODE_MAX_EVENT_MIB=1..64` 调整；worker stderr 最多保留 1 MiB。达到上限后仍持续排空进程管道，避免 worker 因反压卡死。支持文件最多 4 MiB、只保留最近 5 份；每次生成都使用原子私有写入。

客户端断开不会终止 daemon 或 worker。重新连接后使用最后收到的 `sequence` 继续订阅，即可先补齐持久事件再接收实时事件。daemon 自身停止、崩溃或板卡重启时，未完成任务标记为 `interrupted`，其未完成审批标记为非活跃。`task.resume` 会先验证 session 是当前用户所有、权限私有、大小有界且物理路径位于配置的 session 目录内，然后使用上游运行时的 `--session` 续接。它不会自动重放 Prompt、工具调用或审批；这是为了避免重复写文件、操作设备或执行其他不可逆副作用。`task.restart` 会清除任务的旧 session 绑定，并在同一工作目录中启动新 worker；事件日志与任务 ID 保留，但旧会话上下文不会注入新 worker。`task.fork` 不修改源 session 文件：它根据 session 树的 `parentId` 链物化私有分支文件。`side` 取最新已稳定叶节点并作为独立 Agent 展示；`edit` 要求指定 `sequence`，停止被替代的空闲 worker，继承该条 `user.message` 之前的可见历史，并把修改后的 Prompt 作为同一会话的新时间线。旧时间线仍以内部任务记录保留，但 Studio 会折叠它，不会将编辑操作展示成 Side Agent。

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

- 每个 OS 用户默认最多保留 2 个后台 worker，可通过 `HOBOT_CODE_MAX_BACKGROUND_TASKS=1..8` 调整。创建、分支、Resume 或 Restart 需要空位时，会原子挂起最久未使用的 `idle` worker并保留 session；`running`、`waiting`、`starting` 和 `stopping` 任务绝不会被自动回收。所有槽位都在工作时才返回并发上限错误。
- 默认最多保留 100 个任务，可通过 `HOBOT_CODE_MAX_RETAINED_TASKS=10..1000` 调整。达到上限后拒绝新任务，不会静默删除旧任务。
- worker 位于独立进程组；停止任务会先发送 `SIGTERM`，超时后发送 `SIGKILL`。
- `--approve` 只传递 Pi 的项目资源信任选项，不会关闭 Hobot Code 的工具权限和硬安全边界。
- daemon 继承启动器已验证的模型环境，但不会记录或通过协议返回认证 token。
- v1 只监听本机 socket，不开放 TCP。桌面端使用 SSH stdio bridge，权限仍由板端判定。
