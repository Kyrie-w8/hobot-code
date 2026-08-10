# 性能、温度、内存与稳定性调试

> 资料核对日期：2026-08-10。性能结论必须来自目标板、固定工作负载和可复现统计口径。

先定义要测量的边界：纯 BPU 推理、预处理+推理+后处理、相机到结果、编码到网络或完整机器人回路。每次记录
板型与完整系统版本、模型/程序 SHA256、输入、分辨率、线程、CPU affinity、BPU 核心、频率策略、warmup、
样本数、环境温度和供电。只有平均 FPS 的报告无法解释尾延迟、掉帧和热降频。

建议同时采集端到端延迟 P50/P95/P99、吞吐、CPU/BPU/GPU/VDSP 利用率、频率、RSS、`MemAvailable`、
CMA/共享媒体池、磁盘 I/O、网络、所有 thermal zone 和内核错误。`hrt_model_exec` 等板端工具适合建立模型
基线，但它的推理时间不包括业务前后处理。短时峰值不能代表持续性能，至少覆盖达到热稳态后的时间窗口。

故障定位保留第一现场：命令、退出码、stderr、`dmesg`/journal、本次启动服务日志、core/ramdump（若系统
已配置）和输入样本。OOM、连续内存失败、段错误、watchdog、I/O timeout、MIPI error 和 thermal throttle
应分开判断。不要先清日志、重启服务或重复刷机。

优化一次只改变一个变量，并保留前后对比。优先消除不必要拷贝、重复颜色转换、错误 tensor layout、串行等待、
无界缓存和过大的日志；再考虑线程、核心绑定和频率策略。频率或电源策略写操作需要明确散热、功耗和恢复方案，
不得把压测配置留在生产系统中。

## 官方来源

- [RDK S AI Toolchain FAQ 与 hrt 工具](https://developer.d-robotics.cc/rdk_s_doc/en/FAQ/toolchain)
- [RDK S100/S600 Linux 开发与日志指南](https://developer.d-robotics.cc/rdk_s_doc/en/linux_development)
- [RDK S100/S600 驱动开发指南](https://developer.d-robotics.cc/rdk_s_doc/en/driver_development_s100_s600)
- [RDK Model Zoo](https://github.com/D-Robotics/rdk_model_zoo)
