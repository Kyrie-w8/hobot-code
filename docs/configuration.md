# Hobot Code 0.8 配置

## 路径

| 路径 | 作用 |
|---|---|
| `/etc/hobot-code/hobot.env` | root-only 模型密钥和端点 |
| `/etc/hobot-code/agent/settings.json` | Pi/Hobot Code 全局设置 |
| `/etc/hobot-code/agent/models.json` | 自定义 Provider 和模型 |
| `/etc/hobot-code/agent/auth.json` | `/login` 保存的认证信息 |
| `/etc/hobot-code/agent/bin` | 固定版本的 fd 和 ripgrep |
| `/var/lib/hobot-code/sessions` | Pi JSONL 会话 |
| `/usr/local/lib/hobot-code/extensions` | RDK 扩展 |
| `/usr/local/lib/hobot-code/skills` | 板端 Skills |
| `/usr/local/lib/hobot-code/knowledge` | 版本化 RDK 板卡知识与官方来源索引 |
| `/usr/local/lib/hobot-code/prompts/rdk-expert.md` | 动态渲染的地瓜开发专家角色模板 |

启动器设置 `HOBOT_CODING_AGENT_DIR=/etc/hobot-code/agent`。项目目录使用 `.hobot/`
放置局部 settings、extensions、skills、prompts 和 themes；Pi 的 project trust 机制会在
首次加载项目资源前询问。

## D-Robotics Kimi

`/etc/hobot-code/hobot.env`：

```text
ANTHROPIC_BASE_URL=https://ai-api.d-robotics.cc
ANTHROPIC_AUTH_TOKEN=your-token
ANTHROPIC_MODEL=kimi-k3
```

文件权限必须是 `0600`。Hobot Code 不把 token 写入 `models.json`、会话或日志。默认设置选择
`drobotics/kimi-k3`，thinking 等级为 `max`，Provider 请求超时为 3000000 ms。

## 添加模型

Pi 支持在 `models.json` 中定义 Anthropic Messages、OpenAI Chat Completions、OpenAI
Responses 和 Google Generative AI 兼容服务。例如本机 Ollama：

```json
{
  "providers": {
    "ollama": {
      "baseUrl": "http://127.0.0.1:11434/v1",
      "api": "openai-completions",
      "apiKey": "ollama",
      "models": [
        {
          "id": "qwen2.5-coder:7b",
          "contextWindow": 32768,
          "maxTokens": 4096
        }
      ]
    }
  }
}
```

保存后打开 `/model`；文件会重新读取，不需要重启。API key 可以写成 `$ENV_NAME` 引用，
不要把真实密钥直接写进 JSON。

## 交互设置

推荐直接使用 `/settings`、`/model`、`/scoped-models` 和 `/hotkeys`。默认 settings
启用自动压缩、三次 Agent 级重试、1M Kimi 上下文、可见 thinking 和 regular TUI。

扩展、Skills 和 Prompt 可用 Pi 原生命令管理：

```bash
hobot install <npm-or-git-source>
hobot list
hobot config
hobot update --extensions
```

第三方扩展拥有当前用户的完整系统权限，安装前必须审查源码。在 root 板端尤其不要加载
来源不明的 package 或 Skill。

## 环境覆盖

可按进程覆盖：

```bash
HOBOT_CODING_AGENT_DIR=/tmp/hobot-agent hobot
HOBOT_CODING_AGENT_SESSION_DIR=/tmp/hobot-sessions hobot
```

`PI_SKIP_VERSION_CHECK=1` 默认开启，避免 Hobot Code 被 Pi 的自更新提示误导。Hobot Code 的 Pi
运行时升级必须修改 `pi-runtime/pi.lock`、重新构建并完成板端回归。

知识目录可在开发和测试时按进程覆盖：

```bash
HOBOT_CODE_RDK_KNOWLEDGE_DIR=/path/to/knowledge hobot
HOBOT_CODE_RDK_EXPERT_PROMPT=/path/to/rdk-expert.md hobot
```

生产环境建议使用安装包内的只读知识目录。更新知识时修改 `knowledge/manifest.json` 的
`knowledgeVersion` 和 `updatedAt`，运行 `make pi-check` 后重新打包；不要直接在板端堆放
没有版本和来源的零散说明。

专家模板中的 `BOARD_NAME`、`BOARD_ID`、`RDK_OS_VERSION`、`DOCUMENTATION_TRACK`、
`HOSTNAME` 和 `ARCHITECTURE` 占位符由扩展动态替换。修改模板后运行 `make pi-check`，
可在板端用 `/system-prompt` 检查最终内容。
