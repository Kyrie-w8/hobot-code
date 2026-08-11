# Hobot Code agentd 协议

`hobot-agentd` 是按 OS 用户运行的轻量常驻进程。它负责后台任务生命周期、事件持久化和客户端重连，实际 Agent 循环仍由固定版本的 Pi RPC worker 执行。TUI、命令行以及后续桌面客户端共享这一板端控制面；模型、工具、权限和 Skills 不在客户端重复实现。

## 传输与身份

协议版本 1 使用一行一个 JSON 对象的 JSONL，通过本机 Unix domain socket 传输。默认 socket 为 `${XDG_RUNTIME_DIR}/hobot-code/agentd.sock`；无有效 `XDG_RUNTIME_DIR` 时使用 `/tmp/hobot-code-agentd-<uid>/agentd.sock`。目录权限为 `0700`，socket 权限为 `0600`。Linux 发行版还通过 `SO_PEERCRED` 拒绝其他 UID。

每个连接只提交一个请求。普通调用收到一个响应后结束；订阅调用先收到响应，再持续收到事件。单个协议对象最大 2 MiB，Prompt 最大 256 KiB。未知版本、方法、字段或超限数据均拒绝。

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

`id` 由客户端生成，用于关联响应；`sequence` 在单个任务内严格递增，用于断线后的增量重放。

## 方法

| 方法 | 参数 | 结果 |
|---|---|---|
| `ping` | `{}` | 版本、PID、协议版本、任务数和路径 |
| `daemon.shutdown` | `{force?: boolean}` | 请求服务停止；有活跃任务时必须显式 `force` |
| `task.start` | `{name?, cwd, prompt, approve?}` | 新任务元数据 |
| `task.list` | `{}` | 全部任务元数据，按创建时间倒序 |
| `task.get` | `{taskId}` | 单个任务元数据 |
| `task.command` | `{taskId, command}` | 把一条 Pi RPC 命令发送给 worker |
| `task.stop` | `{taskId}` | 终止 worker 进程组 |
| `task.subscribe` | `{taskId, after?, follow?}` | 先重放 `sequence > after` 的事件，再按需跟随 |

`task.command` 当前支持 Pi RPC 的 `prompt`、`abort` 与 `extension_ui_response`。审批事件沿用 worker 的请求 ID；客户端必须把决定发回对应任务，不能在客户端绕过板端权限判定。

## 任务状态

```text
starting -> running -> idle -> running
                    -> waiting -> running
任何活动状态 -> stopping -> stopped
任何活动状态 -> failed
agentd 停止或重启时的活动状态 -> interrupted
```

`idle` 表示 worker 仍在等待下一轮输入，不是任务进程已经退出。`waiting` 表示 Agent 正在等待确认、选择或补充输入。`stopped`、`failed` 和 `interrupted` 为终态；v1 不在原任务上自动重新启动 worker。

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

客户端断开不会终止 daemon 或 worker。重新连接后使用最后收到的 `sequence` 继续订阅，即可先补齐持久事件再接收实时事件。daemon 自身停止、崩溃或板卡重启时，未完成任务标记为 `interrupted`，不会自动重放 Prompt、工具调用或审批；这是为了避免重复写文件、操作设备或执行其他不可逆副作用。

## 资源与安全边界

- 每个 OS 用户默认最多同时运行 2 个后台任务，可通过 `HOBOT_CODE_MAX_BACKGROUND_TASKS=1..8` 调整。
- worker 位于独立进程组；停止任务会先发送 `SIGTERM`，超时后发送 `SIGKILL`。
- `--approve` 只传递 Pi 的项目资源信任选项，不会关闭 Hobot Code 的工具权限和硬安全边界。
- daemon 继承启动器已验证的模型环境，但不会记录或通过协议返回认证 token。
- v1 只监听本机 socket，不开放 TCP。未来桌面端应通过 SSH 隧道连接受认证的板端网关，权限仍由板端判定。
