# Hobot Code 0.10 架构

## 运行路径

```mermaid
flowchart LR
  U["Terminal user"] --> T["Pi TUI and editor"]
  T --> S["Pi session, tree, compaction"]
  S --> A["Pi agent and tool loop"]
  A --> P["Provider registry"]
  P --> K["Hobot Code D-Robotics Kimi adapter"]
  P --> V["Pi built-in and models.json providers"]
  A --> B["Pi built-in coding tools"]
  A --> R["Hobot Code RDK extension"]
  R --> H["Board, BPU, thermal status"]
  R --> D["Versioned local RDK knowledge"]
  R --> E["Dynamic RDK expert role"]
  R --> M["Scoped SQLite and FTS5 memory"]
  A --> X["Pi extensions, packages, Skills"]
```

交互路径没有 Hobot Code 自建 TUI。`runtime/hobot` 是固定版本的 Pi 官方 Bun standalone
二进制；它读取同目录的 Hobot Code `package.json`，由 Pi 自己生成标题、帮助、配置路径、
会话 UI 和快捷键。这使交互升级可以跟随明确的 Pi 上游版本，而无需维护两套编辑器。

## 产品适配层

`extensions/rdk/index.ts` 是唯一必须加载的产品扩展，包含七类适配：

1. D-Robotics Provider：使用 Bearer token 调用 Anthropic-compatible 网关。网关不发送
   完整 SSE 结束事件，因此适配器使用完整响应并生成 Pi 原生 thinking、text、tool call
   和 done 事件。
2. 硬件工具：从 device tree、`/etc/version`、sysfs、procfs 和 RDK 工具位置读取实时状态。
3. 专家角色：把实机信息渲染进完整的 RDK 工程角色，规定证据、工作流、验收和安全边界。
4. 知识路由：按 X5 3.x、S100 4.x、S600 5.x 检索本地知识包，返回版本匹配状态和官方来源。
5. 安全钩子：阻止虚拟设备文件写入，并确认工作区外写入和破坏性 Shell 命令。
6. 板卡 UX：在 Pi 原生 footer status 中显示本机摘要，并增加 `/rdk`、`/doctor`、
   `/knowledge`、`/system-prompt` 和退出别名。
7. 持久化记忆：使用 Bun 内置 SQLite/FTS5 按 user、project、board、session 隔离，
   支持过期、去重、检索、召回、审计和用户删除。

扩展不替换 Pi 的 `read`、`bash`、`edit`、`write`、`grep`、`find`、`ls`，也不修改
InteractiveMode、SessionManager、TUI 组件或消息队列。

## 数据和隐私

Pi JSONL 会话存放在 `/var/lib/hobot-code/sessions`。终端展示与执行状态仍由 Pi 会话模型统一
管理。Hobot Code 只另外维护 `/var/lib/hobot-code/memory/memory.db` 作为结构化长期记忆，不复制会话消息。

RDK footer 在本地读取状态。系统 Prompt 加入完整但不携带手册正文的专家角色；完整硬件
详情与知识正文分别只在模型调用 `system_snapshot`、`rdk_docs_search` 时进入上下文。
记忆在写入前执行敏感数据检查，数据库和目录分别为 `0600` 和 `0700`。自动召回有条数上限，
只将当前作用域内的相关条目加入模型上下文。

## 控制面

`extensions/rdk/control-plane.mjs` 提供无第三方依赖、可单测的权限、初始化、脱敏和
工作区指纹逻辑，`extensions/rdk/index.ts` 只负责接入 Pi 生命周期。会话启动时 deny 工具从 active
tools 中移除，`tool_call` 再执行 fail-closed 检查；ask 工具必须在交互 TUI 中确认。

质量门配置来自项目 `.hobot/quality-gates.json`，会话覆盖与结果通过 Pi custom entry 保存，
不引入第二套数据库。结果只对运行后取得的工作区指纹有效；后续写入、编辑、Shell 或 MCP
调用会保守地将结果标记为 stale。质量门与修改工具出现在同一并行批次时，门禁调用会被拒绝。

## 部署与回滚

发行包包含 Pi、fd、ripgrep 的官方 ARM64 二进制及许可证，并锁定版本和 SHA256。
安装目录为 `/usr/local/lib/hobot-code`，启动器为 `/usr/local/bin/hobot`。安装前的命令和
运行时放入 `/usr/local/lib/hobot-code-backups/<UTC timestamp>`，`hobot-rollback` 可恢复。
