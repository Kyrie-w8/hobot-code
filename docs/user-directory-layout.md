# Hobot Code 用户目录布局

Hobot Code 的程序文件由 root 统一安装，配置与可变状态按 OS 用户隔离。不同用户运行 `hobot` 时不会共享模型认证、会话、记忆或目标。

## 系统级文件

| 路径 | 内容 |
|---|---|
| `/usr/local/bin/hobot` | 启动器 |
| `/usr/local/sbin/hobot-rollback` | 需要 root 的回滚命令 |
| `/usr/local/lib/hobot-code` | 当前 Pi 运行时、`agentd`、扩展、Skills、知识和默认配置 |
| `/usr/local/lib/hobot-code/extensions/catalog.json` | 与产品版本绑定的内置扩展与 Skills 能力清单 |
| `/usr/local/lib/hobot-code-backups` | 安装前的运行时与迁移备份；默认保留最近 3 个、总量最多 768 MiB，并始终保留本次可回滚点 |

系统级程序通常为只读安装内容。运行时不应直接修改其中的知识、Prompt、扩展或能力清单；变更应在源码中验证后重新打包安装。清单只描述产品随包提供的能力，不是用户配置，也不授予执行权限。

## 用户配置

默认配置根目录为 `~/.config/hobot-code`：

| 路径 | 内容 |
|---|---|
| `hobot.env` | 模型端点、认证和环境覆盖 |
| `agent/settings.json` | Pi 交互设置 |
| `agent/models.json` | Pi 高级自管、本机或无密钥 Provider 与模型 |
| `agent/providers.json` | 不含密钥的 Hobot Code 受管 API Provider 元数据 |
| `agent/auth.json` | `/login` 创建的认证信息 |
| `agent/permissions.json` | 工具 allow/ask/deny 策略 |
| `agent/memory.json` | 记忆召回与容量设置 |
| `agent/goals.json` | 持久目标默认预算 |
| `agent/hooks.json` | PreToolUse/PostToolUse Hook |
| `agent/notifications.json` | SSH 终端通知 |
| `agent/lsp.json` | 语言服务器与资源上限 |

安装器以 `0600` 写入或迁移托管配置；启动器以 `0600` 创建 `hobot.env` 和缺失的默认 JSON，并以 `0700` 创建配置目录。启动器拒绝关键配置路径上的符号链接；`hobot.env` 和受管 `providers.json` 还必须属于当前用户，且不能向组或其他用户开放权限。不要把 `hobot.env`、`auth.json` 或含密钥的自定义配置提交到仓库；`providers.json` 只能包含元数据和 `HOBOT_CODE_PROVIDER_KEY_*` 引用，不能包含真实密钥。

## 用户状态

默认状态根目录为 `~/.local/state/hobot-code`：

| 路径 | 内容 |
|---|---|
| `sessions` | Pi JSONL 活动会话 |
| `memory/memory.db` | SQLite/FTS5 持久记忆与记忆审计 |
| `goals/goals.db` | 持久目标状态和事件 |
| `audit/hooks.jsonl` | 脱敏后的 Hook 审计 |
| `agentd/agentd.log`、`agentd/agentd.pid` | 当前用户后台服务日志与 PID |
| `agentd/run/agentd.sock` | 未提供 `XDG_RUNTIME_DIR` 时使用的当前用户私有 daemon socket |
| `agentd/model-qualification.json` | 精确模型的脱敏分层资格证据、构建绑定与过期/失效状态；不含密钥、Endpoint、Prompt 或模型正文 |
| `agentd/model-rdk-matrix.json` | 最多 64 个精确模型/工作流的脱敏 RDK 档案证据；规划能力、当前证据和失效证据严格分离 |
| `agentd/tasks/<task-id>/metadata.json` | 后台任务状态、Pi session 绑定、归档信息、有界审批和事件序号 |
| `agentd/tasks/<task-id>/events.jsonl` | 可按序号重放的 Pi RPC 事件 |
| `agentd/tasks/<task-id>/worker.stderr.log` | 有大小上限的 worker 诊断输出 |
| `legacy-sessions` | 从旧系统布局归档、不再作为活动会话加载的历史会话 |

数据库与状态文件默认使用 `0600`，目录默认使用 `0700`。运行时也会创建并收紧配置根、Agent 配置根、状态根和会话根，防止宽松的预建目录削弱用户隔离。`legacy-sessions` 是归档语义，不是文件系统只读目录；其文件仍由所属用户控制。

`/btw` 的会话和 Prompt 使用按用户隔离的临时目录，关闭时删除。并发租约位于 `/tmp/hobot-code-side-agents-<uid>`，因此默认上限在同一 UID 的进程间共享，而不是跨用户共享。

## XDG 与路径覆盖

启动器遵循：

```text
XDG_CONFIG_HOME   默认 $HOME/.config
XDG_STATE_HOME    默认 $HOME/.local/state
```

托管环境还可以使用以下绝对路径覆盖：

```text
HOBOT_CODE_CONFIG_DIR
HOBOT_CODE_STATE_DIR
HOBOT_CODE_AGENTD_SOCKET
HOBOT_CODING_AGENT_DIR
HOBOT_CODING_AGENT_SESSION_DIR
HOBOT_CODE_MANAGED_PROVIDER_CONFIG
```

启动器拒绝相对路径，避免配置或状态随当前工作目录漂移。`XDG_CONFIG_HOME`、`XDG_STATE_HOME` 和 `HOBOT_CODE_CONFIG_DIR` 决定 `hobot.env` 自身的位置，必须在调用 `hobot` 前设置，不能写入该文件；状态、Agent 和会话目录覆盖会在读取 `hobot.env` 后解析。其他单项配置与数据库覆盖见[配置说明](configuration.md#路径与开发覆盖)。

## 安装目标用户

安装脚本必须由 root 执行，但需要确定由哪个用户接收默认配置和旧布局数据：

| 调用方式 | 默认目标用户 |
|---|---|
| 普通用户执行 `sudo ./install.sh` | `SUDO_USER` |
| root 直接执行 `./install.sh` | `root` |
| 设置 `HOBOT_CODE_INSTALL_USER=<user>` | 显式指定的用户 |

目标用户必须存在；home 必须是绝对路径、由该用户拥有，且路径不能经过符号链接。特殊部署可以同时设置 `HOBOT_CODE_INSTALL_HOME`，但目录必须已经存在并满足相同约束。

如果直接以 root 安装，旧 `/etc/hobot-code` 和 `/var/lib/hobot-code` 数据会迁移到 root 的用户目录。之后切换到普通用户运行会得到该用户自己的全新配置和状态，不会自动看到 root 的会话或记忆。因此应在首次安装前确定实际的日常运行用户。

无论安装目标是谁，其他 OS 用户首次运行 `hobot` 时也会获得独立的缺省配置；不会自动复制安装目标用户的密钥或状态。

## 旧布局迁移

从旧系统布局升级时，安装器按以下顺序处理：

1. 检查是否仍有 Hobot Code 进程运行；存在时直接退出。
2. 将旧 `/etc/hobot-code`、`/var/lib/hobot-code` 和目标用户配置写入本次备份。
3. 只把目标目录中缺失的配置、记忆、目标和审计数据迁入用户目录。
4. 将旧会话复制到 `legacy-sessions`，新的活动会话目录从当前用户状态继续管理。
5. 成功完成运行时切换后删除旧系统目录。

迁移标记防止同一批旧数据被重复导入。安装过程失败时会尝试恢复安装前的运行时和已经触及的目标用户配置。

回滚只恢复具有完整备份的旧命令与运行时，并可能恢复备份中的旧系统目录；它不会删除或回退当前用户的配置、活动会话、记忆和目标数据库。
