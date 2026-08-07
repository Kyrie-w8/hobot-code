# Aster

Aster 是一个面向嵌入式 Linux 的 Agentic Shell。它参考 Claude Code、OpenCode
的交互方式，但针对 RDK X5、RDK S100、RDK S600 这类 ARM64 板卡设计：一个
静态 Go 二进制即可运行，板端不需要 Go、Python、容器或第三方运行库。

## 当前能力

- 可取消的交互式终端会话、流式文本与 reasoning summary、会话恢复和轨迹导出。
- OpenAI Responses、OpenAI-compatible、Anthropic、Gemini 和离线 mock。
- 有界 Agent 工具循环，支持 JSON Schema、超时、输出上限、白名单和人工审批。
- 内置板卡探测、目录读取、文件读写和 Shell 工具。
- `SKILL.md` 技能加载、进程隔离插件、MCP stdio 客户端。
- 本地 HTTP/SSE 服务，可作为板端 Agent 网关由其他设备调用或取消运行中的会话。
- `linux/arm64` 静态构建、安装包和 systemd 服务。

## 本地构建

```bash
make test
make build

./dist/aster \
  --config config/aster.example.json \
  --board config/boards/x5.json \
  doctor --json
```

默认使用离线 mock。进入交互界面：

```bash
./dist/aster --config config/aster.example.json chat
```

接入本地 llama.cpp、vLLM、Ollama 兼容网关或其他 OpenAI-compatible 服务：

```bash
export MODEL_API_KEY=""
./dist/aster \
  --config config/aster.example.json \
  --provider config/providers/openai-compatible.json \
  --board config/boards/s600.json \
  chat
```

先修改 provider overlay 中的 `model` 和 `base_url`。其他厂商示例在
`config/providers/`，密钥只从环境变量读取。

## ARM64 发布与安装

```bash
make release VERSION=0.2.0
scp dist/aster-0.2.0-linux-arm64.tar.gz root@10.112.10.106:/tmp/
ssh root@10.112.10.106
cd /tmp && tar -xzf aster-0.2.0-linux-arm64.tar.gz
cd aster-0.2.0-linux-arm64 && ./install.sh
aster --config /etc/aster/config.json doctor --json
```

使用 `./install.sh --enable-service` 会同时启用仅监听
`127.0.0.1:7337` 的 systemd 服务。安装器不会覆盖已有
`/etc/aster/config.json` 或 `/etc/aster/aster.env`。

## 终端命令

```text
/new            新会话
/session        当前会话 ID
/sessions       会话列表
/resume ID      恢复历史会话
/models         当前 Provider 和模型
/thinking       展开或折叠模型返回的 reasoning summary
/details        展开或折叠工具参数与结果
/tools          查看当前模型可见工具
/skills         查看已发现 Skills
/doctor         查看板卡、模型和扩展状态
/export [ID]    导出当前或指定会话
/clear          清屏
/q              退出，也支持 /quit 和 /exit
```

空闲时 `Ctrl-C` 退出；模型或工具运行时第一次 `Ctrl-C` 取消当前轮，第二次
强制退出。运行期间输入的新消息会排队到下一轮。reasoning summary 只展示模型
明确返回的内容，不生成或猜测隐藏思维链。

## HTTP 和 SSE

`serve` 模式提供以下本机接口：

```text
POST /v1/chat                         同步运行
POST /v1/chat/events                  SSE 流式事件
GET  /v1/sessions                     会话列表
GET  /v1/sessions/{id}                导出会话
POST /v1/sessions/{id}/cancel         取消运行中的会话
```

事件包括 `turn.started`、`provider.started`、`reasoning.delta`、`text.delta`、
`tool.requested`、`tool.completed`、`turn.completed`、`turn.cancelled` 和
`turn.failed`。同一会话串行执行，不同会话可以并行。

命令行子命令包括 `chat`、`run`、`doctor`、`tools`、`skills`、
`sessions`、`export` 和 `serve`。

## 扩展

Skills 放在 `skills/<name>/SKILL.md`，通过配置中的
`agent.enabled_skills` 启用。插件 manifest 示例位于
`examples/plugins/uptime/`。MCP server 直接加入配置：

```json
{
  "mcp_servers": [
    {
      "name": "robot",
      "command": "/usr/local/bin/robot-mcp",
      "args": [],
      "protocol_version": "2025-06-18",
      "enabled": true
    }
  ]
}
```

MCP 工具会以 `<server>__<tool>` 暴露，并默认要求人工批准。

## 安全边界

- 默认写文件、Shell、插件和 MCP 工具需要终端批准。
- `--yes` 仅适合明确受控的自动化环境。
- 文件和 Shell 工作目录被限制在 `security.workspace_root`。
- HTTP 服务没有交互审批能力，因此默认拒绝需要批准的工具。
- Aster 是控制面 Agent，不应进入电机、CAN、GPIO 等硬实时闭环。
- `hrt_model_exec` 存在不代表任意 LLM 已转换并可在 BPU 上运行；本地模型仍需独立验证精度、内存、延迟和温度。

详细设计见 [架构](docs/architecture.md) 和 [配置说明](docs/configuration.md)。
