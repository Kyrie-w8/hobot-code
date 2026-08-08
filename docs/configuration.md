# Aster 配置

配置按三层合并，后者覆盖前者：

```text
--config 基础配置 -> --provider 模型配置 -> --board 板卡配置
```

安装后可将这三个路径保存到 `/etc/aster/launcher.json`：

```bash
aster \
  --config /etc/aster/config.json \
  --provider /etc/aster/providers/drobotics-kimi.json \
  --board /etc/aster/boards/s600.json \
  configure
```

之后 `aster`、`aster doctor` 和 `aster serve` 自动使用相同配置。非 root 用户写入
`~/.config/aster/launcher.json`。优先级从高到低为：显式参数、`ASTER_*`
环境变量、用户 launcher、系统 launcher、程序默认值。

示例：

```bash
aster \
  --config /etc/aster/config.json \
  --provider /etc/aster/providers/openai-compatible.json \
  --board /etc/aster/boards/s600.json \
  chat
```

## 关键字段

| 字段 | 作用 |
|---|---|
| `env_file` | 可选的 root-only 环境文件；进程环境变量优先 |
| `agent.system_prompt_file` | 系统 Prompt 文件 |
| `agent.max_steps` | 单轮最大模型/工具循环，范围 1-64 |
| `agent.enabled_skills` | 懒加载的 Skill 名称 |
| `provider.type` | `openai-responses`、`openai-compatible`、`anthropic`、`gemini` 或 `mock` |
| `provider.api_key_env` | 密钥所在环境变量名 |
| `security.workspace_root` | 文件和 Shell 工具的根目录 |
| `security.allowed_tools` | 可暴露工具 glob |
| `security.denied_tools` | 优先级更高的拒绝 glob |
| `security.approval_tools` | 每次调用均需批准的工具 glob |
| `plugins[].manifest` | 进程插件 manifest |
| `mcp_servers[]` | stdio MCP server 启动参数 |
| `session.dir` | JSONL 会话目录 |
| `server.listen` | HTTP 监听地址，默认仅本机 |

JSON 字符串支持 `${ENV_NAME}` 环境变量展开。不要把真实密钥写入配置文件。
`env_file` 必须设置为 `0600` 等不允许 group/other 访问的权限，内容使用普通
`KEY=VALUE` 格式；Aster 只从中读取 provider 所声明的密钥变量。

## Provider

OpenAI-compatible 允许空 API key，适合无鉴权的本机 llama.cpp 服务。其他三个
厂商适配要求 API key。`provider.settings` 中的生成参数会按协议筛选或传给对应接口。

## 插件

插件 manifest 声明 `command`、`args`、`env` 和工具 schema。工具调用时，Aster
向插件 stdin 写入一条 JSON-RPC `tools/call` 请求，插件从 stdout 返回响应。命令的
相对路径以 manifest 所在目录解析。

## systemd

默认服务读取 launcher 选择的配置和 `/etc/aster/aster.env`，工作目录为
`/var/lib/aster/workspace`。修改配置后执行：

```bash
systemctl restart aster
systemctl status aster
```

若需要直接访问硬件设备，应为具体插件配置最小权限；不要因为服务以 root 启动就把
所有设备操作默认暴露给模型。
