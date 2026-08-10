# S100/S600 MCU、实时域与 IPC

> 资料核对日期：2026-08-10。适用于 S100/S600；MCU 固件、Acore 驱动和 IPC 配置必须版本匹配。

S 系列包含 Cortex-R52+ MCU 域，用于确定性任务和与 Linux Acore 协作。硬实时控制不应由 LLM Agent 或
普通 Linux 进程直接承担；Agent 只负责生成、构建、部署、诊断和测试，最终控制逻辑必须经过周期、最坏执行
时间、watchdog、故障降级和硬件安全验证。

开始前确认 MCU cluster/core、固件版本、启动状态和外设所有权。官方 sysfs 入口可查看 alive、taskcounter、
version、cpuload 等状态，但路径与语义以当前版本手册为准。taskcounter 变化只能说明对应任务仍在运行，不能
单独证明时序、数据正确或安全闭环有效。

S 系列 IPC 基于共享内存和 mailbox，可提供多通道传输；Acore 与 VDSP 还使用 RPMSG。通信双方必须统一
channel、buffer 数量、对齐、消息结构版本、缓存同步、超时和重连策略。4.0.4 到 4.0.5 等版本演进存在接口
重构时，禁止只替换一侧库或头文件。大数据宜使用共享 buffer 传句柄/描述符，控制消息保持有界。

验收覆盖双向序号、CRC、超时、乱序/丢包策略、对端重启、队列满、异常长度、长时间压力和 CPU/MCU 负载。
刷 MCU 固件属于高风险写操作：先备份当前镜像与配置、核对板型和 cluster、保证供电，并准备串口与恢复流程。

## 官方来源

- [RDK S MCU Quick Start](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/mcu_development/basic_information)
- [RDK S MCU IPC](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/mcu_development/mcu_ipc)
- [RDK S Linux IPC Driver](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/linux_development/driver_development_super/driver_ipc)
- [RDK S MCU Port User Manual](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/mcu_development/mcu_port/user_manual)
