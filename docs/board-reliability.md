# RDK 三板稳定性验证

Hobot Code 的发行标准同时要求本地自动化测试和 X5、S100、S600 实机证据。两者不能互相替代：本地测试证明代码契约，实机测试证明安装后的构建、RDK OS 和板端运行边界。

## 验证器的边界

`scripts/board-reliability.mjs` 默认只读。它通过既有 SSH stdio bridge 调用 `ping`、`system.snapshot` 和 `task.page`，检查：

- 产品版本、协议、event schema 与发行能力集；
- 源码提交、工作区清洁状态、构建时间、Pi 版本与 `agentd` 二进制 SHA-256；
- 板型、RDK OS、可用内存、可用磁盘、温度和 BPU 遥测状态；
- 守护进程连续性、SSH/RPC 延迟、任务状态计数与三板构建一致性。

报告不保存板卡地址、主机名、任务 ID、任务名称、项目路径、Prompt、命令、输出、原始错误或凭据。远程字符串不会未经约束写入报告。报告采用 `0600` 权限、有界尺寸和原子替换；`--resume` 只读取当前用户拥有的私密普通文件。

它不调用模型，不运行用户命令，不修改工作区，也不能替代完整的交互、模型质量、外设或部署验收。

## 快速基线

在能同时 SSH 访问三块板的开发机上执行：

```bash
node scripts/board-reliability.mjs \
  --expected-version 0.26.0 \
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
  --expected-version 0.26.0 \
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
