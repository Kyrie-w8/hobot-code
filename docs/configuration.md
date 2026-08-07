# Aster 配置

配置按三层合并，后者覆盖前者：

```text
--config 基础配置 -> --provider 模型配置 -> --board 板卡配置
```

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

## Provider

OpenAI-compatible 允许空 API key，适合无鉴权的本机 llama.cpp 服务。其他三个
厂商适配要求 API key。`provider.settings` 中的生成参数会按协议筛选或传给对应接口。

## 插件

插件 manifest 声明 `command`、`args`、`env` 和工具 schema。工具调用时，Aster
向插件 stdin 写入一条 JSON-RPC `tools/call` 请求，插件从 stdout 返回响应。命令的
相对路径以 manifest 所在目录解析。

## systemd

默认服务读取 `/etc/aster/config.json` 和 `/etc/aster/aster.env`，工作目录为
`/var/lib/aster/workspace`。修改配置后执行：

```bash
systemctl restart aster
systemctl status aster
```

若需要直接访问硬件设备，应为具体插件配置最小权限；不要因为服务以 root 启动就把
所有设备操作默认暴露给模型。
