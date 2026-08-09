# Hobot Code

Hobot Code 是运行在地瓜机器人 RDK X5、RDK S100 和 RDK S600 上的终端开发 Agent。
0.5 开始直接使用 Pi 的官方交互运行时，因此编辑器、流式显示、thinking、工具调用、
会话树、分支、压缩、Skills、扩展和快捷键与 Pi 保持一致。Hobot Code 不重写 TUI，只增加
地瓜板卡所需的模型协议、硬件探测、安全策略和 ARM64 安装层。

## 设计边界

- Pi 0.84.1 官方 Linux ARM64 二进制按 SHA256 固定并原样装入发行包。
- `package.json` 使用 Pi 官方 `piConfig` 机制将主命令改为 `hobot`，项目配置目录改为 `.hobot`。
- `extensions/rdk/index.ts` 注册 D-Robotics Kimi Provider、实时 `system_snapshot`、版本感知的
  `rdk_docs_search`、完整 RDK 专家角色以及板端命令。
- `prompts/rdk-expert.md` 定义证据优先级、平台路由、工程流程、BPU/多媒体/TROS 能力、
  安全边界和交付规范，并在每轮注入实机板型与 RDK OS。
- Pi 自带的 provider、`models.json`、extensions、packages、Skills、prompt templates 和
  themes 均可继续使用。
- 板端无需安装 Node、Bun、Go、Python 或容器。

Pi 上游版本和来源记录在 `pi-runtime/pi.lock`，许可证位于 `LICENSES/`。

## 构建 ARM64 包

构建机需要 `curl`、`tar` 和 SHA256 工具。发行脚本会下载并校验 Pi、fd 和 ripgrep
的官方 ARM64 产物：

```bash
make release VERSION=0.9.0
```

输出：

```text
dist/hobot-code-0.9.0-linux-arm64.tar.gz
```

在不能稳定访问 GitHub Release 的构建机上，可通过 `HOBOT_CODE_PI_CACHE_DIR` 复用 Pi
归档，并通过 `HOBOT_CODE_TOOL_BUNDLE_DIR` 提供已经解压的 `fd`、`rg` 及对应许可证。
脚本仍会按 `pi-runtime/pi.lock` 和 `pi-runtime/tools.lock` 校验每个文件：

```bash
HOBOT_CODE_PI_CACHE_DIR=/path/to/pi-cache \
HOBOT_CODE_TOOL_BUNDLE_DIR=/path/to/tool-bundle \
make release VERSION=0.9.0
```

## 安装

```bash
scp dist/hobot-code-0.9.0-linux-arm64.tar.gz root@RDK_IP:/tmp/
ssh root@RDK_IP
cd /tmp
tar -xzf hobot-code-0.9.0-linux-arm64.tar.gz
cd hobot-code-0.9.0-linux-arm64
./install.sh
```

安装器会保留 `/etc/hobot-code/hobot.env` 和用户配置，并把当前运行时备份到
`/usr/local/lib/hobot-code-backups/`。

## 模型配置

默认接入 D-Robotics Kimi 网关：

```bash
chmod 600 /etc/hobot-code/hobot.env
vi /etc/hobot-code/hobot.env
```

```text
ANTHROPIC_BASE_URL=https://ai-api.d-robotics.cc
ANTHROPIC_AUTH_TOKEN=your-token
ANTHROPIC_MODEL=kimi-k3
```

密钥只保存在 root 可读的环境文件中。Kimi 网关当前只返回完整的 Anthropic 响应，
Hobot Code Provider 会将 thinking、文本、工具调用和 usage 转换为 Pi 原生事件，所以界面和
会话行为不需要特殊分支。

运行时也保留 Pi 的其他模型接入方式：

- 在 `/model` 中选择已配置厂商模型。
- 编辑 `/etc/hobot-code/agent/models.json` 添加 Ollama、vLLM、LM Studio 或兼容网关。
- 使用 `/login <provider>` 配置 Pi 支持的登录型 Provider。
- 使用 `hobot install <package>` 安装 Pi 扩展包。

详细字段见 [配置说明](docs/configuration.md)。

## 使用

启动只需要：

```bash
hobot
```

常用 Pi 交互保持不变：

```text
/model          选择模型
/settings       打开设置
/new            新会话
/resume         恢复会话
/tree           浏览和切换会话分支
/fork           从历史消息分支
/compact        手动压缩上下文
/reload         重载扩展、Skills、Prompt 和主题
/hotkeys        查看完整快捷键
/system-prompt  查看当前生效的 Pi + RDK 专家系统 Prompt
/permissions    查看或修改工具权限
/init           生成项目 AGENTS.md 和质量门配置
/gate           查看、配置或运行质量门
/quit           退出；Hobot Code 另外提供 /q 和 /exit
```

```text
Escape          中断当前模型或工具
Ctrl+C          清空编辑区
Ctrl+D          编辑区为空时退出
Ctrl+T          显示/隐藏 thinking
Ctrl+O          展开/折叠工具输出
Shift+Tab       切换 thinking 等级
Ctrl+L          打开模型选择器
Ctrl+P          切换下一个已启用模型
Alt+Enter       排队 follow-up 消息
```

非交互调用同样沿用 Pi：

```bash
hobot -p "检查这个项目并给出结论"
hobot --mode json "输出 JSON 事件流"
hobot --continue
hobot --resume
```

## 权限与质量门

全局权限策略位于 `/etc/hobot-code/agent/permissions.json`，按顺序匹配工具名，支持
`allow`、`ask` 和 `deny`。`deny` 工具不会进入模型上下文；`ask` 在 TUI 中显示经过密钥
脱敏的工具、风险和目标，在非交互模式下默认拒绝：

```text
/permissions status
/permissions set bash ask
/permissions set mcp:* deny
/permissions default ask
/permissions reload
```

RDK 安全底线优先于通用策略：`/proc`、`/sys`、`/dev` 直接写入始终拒绝，工作区外写入和
破坏性 Shell 命令始终需要交互确认。

在项目根目录运行 `/init` 会创建 `AGENTS.md` 和 `.hobot/quality-gates.json`，自动识别
Make、Node、Go、Rust 或 pytest 验证命令，已有文件保持不变。质量门配置和最近结果随会话
持久化，通过结果绑定工作区指纹；门禁之后再次修改文件会变为 `stale`：

```text
/gate status
/gate set make check
/gate set ["npm run check","npm test"]
/gate timeout 300
/gate run
```

配置质量门后，Agent 只有在最终修改之后取得当前 `passed` 结果才能声明完成；否则 Hobot Code
会在回复中标记该完成声明不被接受。

## RDK 适配

`/rdk` 显示简要板卡状态，`/doctor` 显示完整诊断。模型需要实时硬件信息时会调用
`system_snapshot`，读取板卡型号、完整 RDK OS 版本、CPU、内存、负载、温度、BPU 设备
节点和 RDK 工具。实时硬件详情不会自动加入每次系统 Prompt。

板卡专业知识位于 `/usr/local/lib/hobot-code/knowledge`。Hobot Code 根据 device tree 中的型号和
`/etc/version` 自动选择 X5 3.x、S100 4.x 或 S600 5.x 资料，通过 `rdk_docs_search` 按需
返回短摘要、适用版本和地瓜官方来源，不把整套文档塞进上下文。用户也可直接运行：

```text
/knowledge BPU 模型如何转换和验证
/knowledge S600 的 VDSP 有几个核心
```

知识结果是版本化文档，实机状态由 `system_snapshot` 证明；两者不一致时 Hobot Code 会明确
提示版本不匹配。知识清单与来源索引见 `knowledge/manifest.json`。

完整专家角色位于 `prompts/rdk-expert.md`。它不会替代 Pi 的编码工具规则，而是追加完整
的地瓜平台工程约束；每次模型调用都会动态填充板卡、RDK OS、文档线、主机名和架构。

会话保存在 `/var/lib/hobot-code/sessions`。全局配置位于 `/etc/hobot-code/agent`，
板端扩展和 Skills 位于 `/usr/local/lib/hobot-code`。

## 回滚

```bash
hobot-rollback
```

该命令恢复最近一次安装前的 Hobot Code 命令和运行时；会话、模型密钥和用户配置不会被删除。

## 安全边界

- 对工作目录外的写入和高风险 Shell 命令要求交互确认；非交互模式默认拒绝。
- `/proc`、`/sys`、`/dev` 下的直接文件写入被文件工具阻止。
- `system_snapshot` 只证明当前节点和工具存在，不证明任意模型已完成 BPU 转换。
- Hobot Code 是控制面 Agent，不应进入电机、CAN、GPIO、安全或急停的硬实时闭环。

Hobot Code 仅提供 `hobot` 和 `hobot-rollback`，不携带其他历史命令或运行时。
