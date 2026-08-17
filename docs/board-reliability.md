# RDK 三板稳定性验证

Hobot Code 的发行标准同时要求本地自动化测试和 X5、S100、S600 实机证据。两者不能互相替代：本地测试证明代码契约，实机测试证明安装后的构建、RDK OS 和板端运行边界。

## 验证器的边界

`scripts/board-reliability.mjs` 默认只读。它通过既有 SSH stdio bridge 调用 `ping`、`system.snapshot` 和 `task.page`，检查：

- 产品版本、协议、event schema 与发行能力集，包括可操作的 `support.bundle.v2`；
- 源码提交、工作区清洁状态、构建时间、Pi 版本、Pi 能力兼容契约摘要与 `agentd` 二进制 SHA-256；
- 板型、RDK OS、可用内存、可用磁盘、温度和 BPU 遥测状态；
- 守护进程连续性、SSH/RPC 延迟、任务状态计数与三板构建一致性。

报告不保存板卡地址、主机名、任务 ID、任务名称、项目路径、Prompt、命令、输出、原始错误或凭据。远程字符串不会未经约束写入报告。报告采用 `0600` 权限、有界尺寸和原子替换；`--resume` 只读取当前用户拥有的私密普通文件。

它不调用模型，不运行用户命令，不修改工作区，也不能替代完整的交互、Pi 实机兼容场景、模型质量、外设或部署验收。

## 受管模型隔离验收

候选发行包根目录包含 `verify-model-egress-runtime.py`。该验收器先逐文件验证 `MANIFEST.sha256`，再只使用假密钥、本机 mock 网关和临时用户目录，真实执行包内 Pi 的 Anthropic Messages、OpenAI Chat Completions 与 OpenAI Responses 适配器，验证 `model-only` 的固定代理路由、流式事件闭环，以及 worker 进程树没有继承任何模型凭据：

```bash
python3 ./verify-model-egress-runtime.py \
  --package-root . \
  --output ../model-egress-acceptance.json \
  --rpc-output ../rpc-background-acceptance.json \
  --session-output ../session-recovery-acceptance.json \
  --extension-output ../extension-safety-acceptance.json \
  --tui-output ../tui-basics-acceptance.json \
  --readiness-output ../readiness-diagnostics-acceptance.json
```

输出报告为 `hobot.pi-board-compatibility/v1`，权限固定为 `0600`，并记录发行清单哈希；它不包含板卡地址、主机名、任务正文、模型输出、临时路径或凭据。X5、S100、S600 必须分别执行并留档。基础 `--output` 只覆盖 `model-egress-runtime`，可选参数各自产生独立场景报告，均不能替代下面的只读可靠性基线或其他交互场景。

提供 `--rpc-output` 时还会生成独立的 `rpc-background` 报告：真实包内 Pi 必须完成一次精确审批后的安全工作区写入，证明工具没有重复执行，再完成第二轮、图片输入、逐次新连接、主任务继续输入、两个多轮 Side Agent 及其扁平父任务关系。图片原始数据不得进入持久事件。

提供 `--session-output` 时会生成独立的 `session-recovery` 报告：真实包内 Pi 必须对私有持久会话完成有净 Token 缩减的语义压缩，在工具调用中被强制终止后精确恢复同一会话且不重放工具，并用历史编辑创建不含被替换轮次的新分支。

提供 `--extension-output` 时会生成独立的 `extension-safety` 报告：包内 RDK 扩展与三个 Skill 必须按只读清单被发现，真实扩展并行工具和关联审批必须完成；两个 Agent 竞争同一工作区时，后一个写入必须在执行前被写租约拒绝，前一个回合结束后租约必须释放。

提供 `--tui-output` 时，整个验收器必须以实际使用 Hobot Code 的普通用户运行，不能使用 root。它会通过真实 PTY 启动全屏 TUI，验证中文输入、结构化 thinking 展示、发送前编辑、`/detach` 后进程继续存在，以及重新连接后继续同一 Agent。测试只使用唯一的隔离 tmux 会话，结束后自动清理。

提供 `--readiness-output` 时还会生成独立的 `readiness-diagnostics` 报告：`diagnostics.inspect` 与 `hobot doctor --json` 必须只读且不发模型请求、不创建支持包；修复必须先因缺少显式确认而失败，再只能把验收器故意放宽的当前用户私有状态目录收紧到 `0700`。验收器同时证明工作区外哨兵文件、内容和权限不变，报告不泄露凭据、临时路径或哨兵内容。六类普通用户报告不能合并成一个模糊的“运行正常”。

部分 RDK 镜像的 `/tmp` 为 `0700 root:root`。此时应在普通用户可进入的位置创建归属该用户、权限为 `0700` 的临时目录，并通过 `TMPDIR` 传给整个验收命令；不要为了测试放宽系统 `/tmp` 权限。

## 安装生命周期验收

候选包还包含必须以 root 单独运行的 `verify-install-lifecycle.py`。它不覆盖上面的普通用户 Pi/TUI 场景，而是在 root 拥有、不可被组或其他用户写入的临时树中执行首次安装、同版本升级、换入运行时后的故障注入、回滚和非 purge 卸载：

```bash
sudo python3 ./verify-install-lifecycle.py \
  --package-root . \
  --output ../install-lifecycle-acceptance.json \
  --user <existing-non-root-user>
```

测试用户必须是板上已经存在的非 root 账户；未指定时验收器只从有限的常见账户中选择，不会创建或删除系统用户。安装器、回滚器、卸载器和启动器只有同时设置内部测试开关与安全隔离根时才接受路径重定向，测试用户 HOME 也必须位于隔离树内。测试模式不会安装缺失的系统依赖。

验收器逐文件验证候选清单，确认普通用户可以进入启动器，验证配置、状态和备份在升级、失败恢复、回滚、卸载中的保留语义，并比较真实 `/usr/local`、`/etc/hobot-code` 和 `/var/lib/hobot-code` 测试前后的元数据指纹。临时树始终清理，报告不保留用户名、路径或命令。该报告是独立的 `install-lifecycle` 发布场景，X5、S100、S600 均必须通过。

把同一候选构建、同一场景的三板报告单独放入权限为 `0700` 的目录后执行：

```bash
make board-acceptance-check \
  REPORT_DIR=artifacts/model-egress-candidate \
  SCENARIO=model-egress-runtime \
  REPORT=artifacts/model-egress-matrix.json
```

发布前去掉 `SCENARIO`，此时必须覆盖兼容契约声明的全部场景。校验器会拒绝缺板、重复报告、混用构建、不同包清单、Pi 契约不一致、公开权限、符号链接和未知字段。缺少场景明确返回 `incomplete` 与非零退出码，不能被解释成“受限但可发布”。聚合报告同样以 `0600` 生成，只保留板型、RDK OS、时间、构建指纹和结果。私有单板报告不得上传；只有严格生成并命名为 `hobot-code-<version>-board-acceptance.json` 的聚合矩阵可以附到 Draft Release，随后由受保护的 Promote Release 工作流重新校验。

## 快速基线

在能同时 SSH 访问三块板的开发机上执行：

```bash
node scripts/board-reliability.mjs \
  --expected-version 0.28.0 \
  --board x5=root@192.0.2.10 \
  --board s100=root@192.0.2.11 \
  --board s600=root@192.0.2.12 \
  --samples 3 \
  --output artifacts/rdk-three-board-baseline.json
```

返回码 `0` 表示全部检查通过，`1` 表示至少一项发行检查失败，`2` 表示命令或报告本身不可用。`warn` 不会伪装成完全通过；典型情况是尚未实现网络 namespace 隔离、RDK OS 不在精确验证列表或资源证据不完整。

## 24 小时断点续测

289 个样本、相邻样本间隔 5 分钟，覆盖完整 24 小时：

```bash
node scripts/board-reliability.mjs \
  --expected-version 0.28.0 \
  --board x5=root@192.0.2.10 \
  --board s100=root@192.0.2.11 \
  --board s600=root@192.0.2.12 \
  --samples 289 --interval 5m \
  --output artifacts/rdk-three-board-24h.json
```

开发机中断后使用原命令加 `--resume`。记录的是每块板的尝试次数，不会因为某块板曾经失联而对其他板重复采样。

## 守护进程恢复

`--restart-idle` 是唯一会改变板端运行状态的选项，必须显式传入。验证器只在 daemon 活动数、排队数与任务分页中的活动状态全部为零时重启；否则跳过并报告 `warn`。重启后要求 PID 和启动时间改变、任务状态计数保持一致。

正式发行前应先完成默认只读基线，再只对专用空闲板执行重启验证。不应为了通过检查停止用户的运行中 Agent。
