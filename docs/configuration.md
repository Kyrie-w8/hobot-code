# Aster 0.5 配置

## 路径

| 路径 | 作用 |
|---|---|
| `/etc/aster/aster.env` | root-only 模型密钥和端点 |
| `/etc/aster/agent/settings.json` | Pi/Aster 全局设置 |
| `/etc/aster/agent/models.json` | 自定义 Provider 和模型 |
| `/etc/aster/agent/auth.json` | `/login` 保存的认证信息 |
| `/etc/aster/agent/bin` | 固定版本的 fd 和 ripgrep |
| `/var/lib/aster/pi-sessions` | Pi JSONL 会话 |
| `/usr/local/lib/aster/extensions` | RDK 扩展 |
| `/usr/local/lib/aster/skills` | 板端 Skills |

启动器设置 `ASTER_CODING_AGENT_DIR=/etc/aster/agent`。项目目录可以使用 `.aster/`
放置局部 settings、extensions、skills、prompts 和 themes；Pi 的 project trust 机制会在
首次加载项目资源前询问。

## D-Robotics Kimi

`/etc/aster/aster.env`：

```text
ANTHROPIC_BASE_URL=https://ai-api.d-robotics.cc
ANTHROPIC_AUTH_TOKEN=your-token
ANTHROPIC_MODEL=kimi-k3
```

文件权限必须是 `0600`。Aster 不把 token 写入 `models.json`、会话或日志。默认设置选择
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
aster install <npm-or-git-source>
aster list
aster config
aster update --extensions
```

第三方扩展拥有当前用户的完整系统权限，安装前必须审查源码。在 root 板端尤其不要加载
来源不明的 package 或 Skill。

## 环境覆盖

可按进程覆盖：

```bash
ASTER_CODING_AGENT_DIR=/tmp/aster-agent aster
ASTER_CODING_AGENT_SESSION_DIR=/tmp/aster-sessions aster
```

`PI_SKIP_VERSION_CHECK=1` 默认开启，避免 Aster 被 Pi 的自更新提示误导。Aster 的 Pi
运行时升级必须修改 `pi-runtime/pi.lock`、重新构建并完成板端回归。
