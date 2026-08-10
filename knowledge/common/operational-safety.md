# 板端操作安全边界

> 资料核对日期：2026-08-10。本文是 Agent 的操作约束，不替代产品安全手册或系统烧录说明。

Hobot Code 是控制面 Agent，不是硬实时控制器。模型输出不得直接进入电机、急停、CAN、GPIO、
PWM 或安全闭环；实时控制必须由确定性程序、MCU 和独立安全机制承担。

以下操作需要明确目标、备份、回滚方案和交互确认：刷写或降级 miniboot/系统镜像，修改
启动配置与分区，写 `/sys` 或设备节点，更改供电和频率策略，停用关键服务，重启或关机。
只读 sysfs 数据可以用于诊断，但不要把旧版本文档中的 sysfs 写命令直接用于新系统。

执行重负载编译、模型转换或持续推理前，检查可用内存、磁盘、温度和现有业务进程。板端
内存紧张时优先在工作站转换模型，只把运行产物和最小依赖部署到 RDK。

X5 官方要求 5V/5A 供电，并警告不要使用电脑 USB 口供电。反复重启、USB 外设掉线和
推理负载下异常通常应先排除供电与散热，而不是直接修改驱动或重刷系统。

系统级安装可以由 root 完成，但日常 Agent 应优先以普通用户运行。确实需要 root 时，默认对
Shell、文件修改和插件调用逐次确认，并禁止直接写 `/boot`、`/dev`、`/etc`、`/proc`、
`/sys`、`/usr` 和关键运行状态目录。第三方 Skill、Hook、MCP、LSP 进程不得继承模型密钥。

任何升级先检查活动进程、剩余空间和 inode，分阶段校验新运行时后再原子切换；失败必须自动
恢复旧命令和运行时。仅有“备份目录存在”不等于可回滚，仍需验证备份完整性和旧版本自检。

## 官方来源

- [RDK X5 硬件介绍与供电要求](https://developer.d-robotics.cc/rdk_x_doc/Quick_start/hardware_introduction/rdk_x5)
- [RDK S100 硬件 FAQ](https://developer.d-robotics.cc/rdk_s_doc/en/Quick_start/hardware_introduction/rdk_s100/FAQ)
- [RDK S600 系统烧录与分区备份](https://developer.d-robotics.cc/rdk_s_doc/en/Quick_start/install_os/rdk_s600/burn)
