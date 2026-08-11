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
| `daemon.shutdown` | `{force?: boolean}` | 请求服务停止；有活跃任务时必须显式 `force` |
| `task.start` | `{name?, cwd, prompt, approve?}` | 新任务元数据 |
| `task.list` | `{}` | 未归档任务元数据，按创建时间倒序 |
| `task.page` | `{cursor?, limit?, includeArchived?}` | 有界任务分页与下一游标 |
| `task.get` | `{taskId}` | 单个任务元数据 |
| `task.rename` | `{taskId, name}` | 更新任务显示名称 |
| `task.archive` | `{taskId, archive}` | 归档或取消归档终态任务 |
| `task.delete` | `{taskId}` | 删除已归档的终态任务及本地日志 |
| `task.resume` | `{taskId, prompt?}` | 重新打开已校验的 Pi session，可选发送新 Prompt |
| `task.restart` | `{taskId, prompt}` | 保留任务记录与工作目录，启动一个不继承旧上下文的新 session |
| `task.command` | `{taskId, command}` | 把一条 Pi RPC 命令发送给 worker |
| `task.approvals` | `{taskId}` | 有界待审批队列，包含活跃和已失效项 |
| `task.stop` | `{taskId}` | 终止 worker 进程组 |
| `task.events` | `{taskId, after?, limit?}` | 按序号读取最多 1000 条持久事件 |
| `task.subscribe` | `{taskId, after?, follow?}` | 先重放 `sequence > after` 的事件，再按需跟随 |

`task.command` 当前支持 Pi RPC 的 `prompt`、`abort` 与 `extension_ui_response`。审批事件沿用 worker 的请求 ID；客户端只能回复当前活跃 ID。审批队列最多保留 16 项，文本、选项数和超时均有上限。权限结果仍在板端 worker 内判定，客户端无法绕过。

## 任务状态

```text
starting -> running -> idle -> running
                    -> waiting -> running
任何活动状态 -> stopping -> stopped
任何活动状态 -> failed
agentd 停止或重启时的活动状态 -> interrupted
```

`idle` 表示 worker 仍在等待下一轮输入，不是任务进程已经退出。`waiting` 表示 Agent 正在等待确认、选择或补充输入。`stopped`、`failed` 和 `interrupted` 为终态。它们不会自动重启 worker；具有安全 session 绑定的未归档任务可通过 `task.resume` 续接上下文，没有可用 session 或需要明确丢弃上下文时可通过 `task.restart` 启动新会话。

## 持久化与恢复

状态位于 `~/.local/state/hobot-code/agentd`：

```text
agentd.pid
agentd.log
tasks/<task-id>/metadata.json
tasks/<task-id>/events.jsonl
tasks/<task-id>/worker.stderr.log
```

目录和文件分别使用 `0700` 与 `0600`。元数据使用临时文件加原子重命名更新；恢复时拒绝符号链接、异常所有者、宽松权限、超限文件和无效任务 ID。事件日志每个任务最多 64 MiB，worker stderr 最多保留 1 MiB；达到上限后仍持续排空进程管道，避免 worker 因反压卡死。

客户端断开不会终止 daemon 或 worker。重新连接后使用最后收到的 `sequence` 继续订阅，即可先补齐持久事件再接收实时事件。daemon 自身停止、崩溃或板卡重启时，未完成任务标记为 `interrupted`，其未完成审批标记为非活跃。`task.resume` 会先验证 session 是当前用户所有、权限私有、大小有界且物理路径位于配置的 session 目录内，然后使用上游运行时的 `--session` 续接。它不会自动重放 Prompt、工具调用或审批；这是为了避免重复写文件、操作设备或执行其他不可逆副作用。`task.restart` 会清除任务的旧 session 绑定，并在同一工作目录中启动新 worker；事件日志与任务 ID 保留，但旧会话上下文不会注入新 worker。

## SSH 标准输入桥接

Mac 等远程客户端通过 `ssh <board> hobot bridge --stdio` 连接。bridge 从标准输入读取一行请求，转发到当前 OS 用户的 Unix socket，并把完整响应或订阅流写到标准输出。长时 `task.subscribe` 会占用该 bridge，因此客户端应为控制请求和每个实时订阅分别建立 SSH 进程。bridge 不监听 TCP、不返回模型 token，也不改变板端 UID 和权限边界。

## 资源与安全边界

- 每个 OS 用户默认最多同时运行 2 个后台任务，可通过 `HOBOT_CODE_MAX_BACKGROUND_TASKS=1..8` 调整。
- 默认最多保留 200 个任务，可通过 `HOBOT_CODE_MAX_RETAINED_TASKS=10..1000` 调整。达到上限后拒绝新任务，不会静默删除旧任务。
- worker 位于独立进程组；停止任务会先发送 `SIGTERM`，超时后发送 `SIGKILL`。
- `--approve` 只传递 Pi 的项目资源信任选项，不会关闭 Hobot Code 的工具权限和硬安全边界。
- daemon 继承启动器已验证的模型环境，但不会记录或通过协议返回认证 token。
- v1 只监听本机 socket，不开放 TCP。桌面端使用 SSH stdio bridge，权限仍由板端判定。
