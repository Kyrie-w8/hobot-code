# S100/S600 Linux 驱动、存储与网络

> 资料核对日期：2026-08-10。驱动配置必须匹配当前内核、设备树、载板和启动介质。

S 系列驱动范围包括 UART、I2C、GPIO、pinctrl、IPC、SPI、thermal、音频、时间同步、PCIe、Wi-Fi、
HBMEM、Ethernet、RTC、watchdog、UFS、USB gadget 和 Bluetooth。修改前先保存 `/etc/version`、内核
版本、设备树/overlay、相关模块、设备节点和首个错误日志；不要因单个设备失败直接替换整个内核或 rootfs。

内核模块必须使用匹配 headers、config 和 toolchain 构建。DTS 改动先做最小 overlay 或独立分支，检查
pinctrl、电源、时钟、reset、IOMMU/内存和中断。驱动 probe 成功后仍要验证数据路径、并发、挂起恢复和
异常拔插；日志中的 warning 不能仅因功能“看起来正常”而忽略。

S100/S600 集成 UFS Host，官方驱动页说明硬件最高 UFS 3.1、软件接口最高 3.0、HS-G4、双 lane；实际
Developer Kit 容量与启动布局按实机确认。S600 还可从 NVMe 启动，但需要对应镜像和启动位。存储压测不得
占满系统盘，要监控温度、I/O error、文件系统与掉电恢复。

S600 `eth0` 默认可用于 DHCP；配置 EtherCAT 时，`eth0` 的普通 Ethernet 与 EtherCAT 互斥，远程维护应
提前迁移到 `eth1` 或串口，避免切换后失联。网络性能测试记录 MTU、offload、IRQ/CPU、丢包和交换设备，
不要在 SSH 唯一路径上直接应用未验证的持久网络配置。

## 官方来源

- [RDK S100/S600 Driver Development Guide](https://developer.d-robotics.cc/rdk_s_doc/en/driver_development_s100_s600)
- [RDK S Linux Development Guide](https://developer.d-robotics.cc/rdk_s_doc/en/linux_development)
- [RDK S600 EtherCAT](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/linux_development/driver_development_super/driver_ethernet/ethercat)
- [RDK S Build System](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/rdk_gen)
