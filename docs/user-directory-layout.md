# Hobot Code 用户目录布局

Hobot Code 的程序文件仍安装在 `/usr/local/lib/hobot-code`，命令位于 `/usr/local/bin/hobot`。
所有用户配置、认证和可变数据按当前用户隔离：

| 路径 | 内容 |
|---|---|
| `~/.config/hobot-code/hobot.env` | 模型端点与认证环境变量，权限 `0600` |
| `~/.config/hobot-code/agent` | Pi 设置、模型、权限、Hook、通知和 LSP 配置 |
| `~/.local/state/hobot-code/sessions` | Pi JSONL 会话 |
| `~/.local/state/hobot-code/memory` | 持久化记忆数据库 |
| `~/.local/state/hobot-code/goals` | 持久目标数据库 |
| `~/.local/state/hobot-code/audit` | Hook 审计记录 |

启动器遵循 `XDG_CONFIG_HOME` 与 `XDG_STATE_HOME`。托管环境还可以通过
`HOBOT_CODE_CONFIG_DIR`、`HOBOT_CODE_STATE_DIR`、`HOBOT_CODING_AGENT_DIR` 和
`HOBOT_CODING_AGENT_SESSION_DIR` 覆盖路径。

从旧系统布局升级时，安装器先将 `/etc/hobot-code` 和 `/var/lib/hobot-code` 保存到本次运行时
备份，再把配置、记忆、目标和审计迁移到安装用户的目录；旧会话不会迁移，新会话目录从空状态
开始。迁移完成后旧目录会被删除。若检测到仍在运行的 Hobot Code 进程，安装器会先退出，避免
清理正在写入的会话。
