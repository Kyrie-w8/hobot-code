# X5 芯片 SDK 与底层开发

> 资料核对日期：2026-08-10。适用于 X5 芯片/EVB/模组底层开发；RDK X5 用户应先判断是否真的需要进入 SDK 层。

X5 芯片用户手册覆盖交付包、芯片与系统软件架构、EVB 和 X5 MD、连接与烧录、示例、内核/驱动、多媒体
和算法等底层内容。它与 RDK X5 开发板手册互补：RDK 手册面向套件使用，SDK 手册面向芯片方案和更深的
BSP/硬件开发。执行命令前核对硬件是 RDK X5、X5 EVB 1_B、EVB V2P0 还是 X5 MD。

SDK 手册列出的接口和拨码、供电、启动介质、IO 电平、摄像头、CAN FD、40-pin 与烧录方式按具体硬件章节
选择，不能在不同 EVB/模组间复制。烧录支持 GUI、命令行和手动协议等多种路径，分区擦除、备份/恢复与
指定分区写入都应由明确的目标和恢复镜像驱动，而不是排障试错。

示例覆盖 VIN、ISP、VSE、OSD、GDC、codec、VOT 等多媒体模块。底层开发先原样编译和运行匹配版本的
sample，再修改一项配置；保存输入、配置、固件、库和日志。`/app` 空间、串口日志、sensor 探测和多媒体
buffer 问题应分别诊断。

从 SDK 集成回 RDK 应记录 SDK 版本、BSP 提交、内核/设备树、用户态库和 RDK OS 的对应关系。未经官方
兼容说明，不把 SDK 交付包中的内核模块或共享库覆盖到另一 RDK OS；最小功能通过后还要完成升级、恢复、
压力、温度和异常路径回归。

## 官方来源

- [X5 芯片用户手册 1.1.2](https://developer.d-robotics.cc/x5_sdk_doc/)
- [RDK X 系列用户手册](https://developer.d-robotics.cc/rdk_x_doc/RDK)
- [RDK X5 产品页](https://developer.d-robotics.cc/en/rdkx5)
