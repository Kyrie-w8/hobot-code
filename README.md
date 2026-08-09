# Aster

Aster 是运行在地瓜机器人 RDK X5、RDK S100 和 RDK S600 上的终端 Agent。
0.5 开始直接使用 Pi 的官方交互运行时，因此编辑器、流式显示、thinking、工具调用、
会话树、分支、压缩、Skills、扩展和快捷键与 Pi 保持一致。Aster 不重写 TUI，只增加
地瓜板卡所需的模型协议、硬件探测、安全策略和 ARM64 安装层。

## 设计边界

- Pi 0.84.1 官方 Linux ARM64 二进制按 SHA256 固定并原样装入发行包。
- `package.json` 使用 Pi 官方 `piConfig` 机制将产品名改为 `aster`，配置目录改为 `.aster`。
- `extensions/rdk.ts` 注册 D-Robotics Kimi Provider、`system_snapshot` 工具、`/doctor`、
  `/rdk`、`/exit` 和 `/q`，并对板端危险命令增加确认。
- Pi 自带的 provider、`models.json`、extensions、packages、Skills、prompt templates 和
  themes 均可继续使用。
- 板端无需安装 Node、Bun、Go、Python 或容器。

Pi 上游版本和来源记录在 `pi-runtime/pi.lock`，许可证位于 `LICENSES/`。

## 构建 ARM64 包

构建机需要 `curl`、`tar` 和 SHA256 工具。发行脚本会下载并校验 Pi、fd 和 ripgrep
的官方 ARM64 产物：

```bash
make release VERSION=0.5.0
```

输出：

```text
dist/aster-0.5.0-linux-arm64.tar.gz
```

## 安装

```bash
scp dist/aster-0.5.0-linux-arm64.tar.gz root@RDK_IP:/tmp/
ssh root@RDK_IP
cd /tmp
tar -xzf aster-0.5.0-linux-arm64.tar.gz
cd aster-0.5.0-linux-arm64
./install.sh
```

安装器会保留 `/etc/aster/aster.env`，将现有 Aster 命令和运行时备份到
`/usr/local/lib/aster-backups/`，并将旧 Go 版第一次备份为 `aster-legacy`。

## 模型配置

默认接入 D-Robotics Kimi 网关：

```bash
chmod 600 /etc/aster/aster.env
vi /etc/aster/aster.env
```

```text
ANTHROPIC_BASE_URL=https://ai-api.d-robotics.cc
ANTHROPIC_AUTH_TOKEN=your-token
ANTHROPIC_MODEL=kimi-k3
```

密钥只保存在 root 可读的环境文件中。Kimi 网关当前只返回完整的 Anthropic 响应，
Aster Provider 会将 thinking、文本、工具调用和 usage 转换为 Pi 原生事件，所以界面和
会话行为不需要特殊分支。

运行时也保留 Pi 的其他模型接入方式：

- 在 `/model` 中选择已配置厂商模型。
- 编辑 `/etc/aster/agent/models.json` 添加 Ollama、vLLM、LM Studio 或兼容网关。
- 使用 `/login <provider>` 配置 Pi 支持的登录型 Provider。
- 使用 `aster install <package>` 安装 Pi 扩展包。

详细字段见 [配置说明](docs/configuration.md)。

## 使用

启动只需要：

```bash
aster
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
/quit           退出；Aster 另外提供 /q 和 /exit
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
aster -p "检查这个项目并给出结论"
aster --mode json "输出 JSON 事件流"
aster --continue
aster --resume
```

## RDK 适配

`/rdk` 显示简要板卡状态，`/doctor` 显示完整诊断。模型需要实时硬件信息时会调用
`system_snapshot`，读取板卡型号、CPU、内存、负载、温度、BPU 设备节点和 RDK 工具。
实时硬件详情不会自动加入每次系统 Prompt。

会话保存在 `/var/lib/aster/pi-sessions`，与 0.4 的 SQLite 数据分离。全局配置位于
`/etc/aster/agent`，板端扩展和 Skills 位于 `/usr/local/lib/aster`。

## 回滚

```bash
aster-rollback
```

该命令恢复最近一次安装前的 Aster 命令和运行时。旧数据库、Pi JSONL 会话、模型密钥
和用户配置均不会被删除。

## 安全边界

- 对工作目录外的写入和高风险 Shell 命令要求交互确认；非交互模式默认拒绝。
- `/proc`、`/sys`、`/dev` 下的直接文件写入被文件工具阻止。
- `system_snapshot` 只证明当前节点和工具存在，不证明任意模型已完成 BPU 转换。
- Aster 是控制面 Agent，不应进入电机、CAN、GPIO、安全或急停的硬实时闭环。

旧 Go 0.4 源码暂时保留用于回滚和迁移参考，`make legacy-release` 仍可构建它。
