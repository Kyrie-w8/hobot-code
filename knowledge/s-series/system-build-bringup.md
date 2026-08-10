# S100/S600 系统构建与板级 Bring-up

> 资料核对日期：2026-08-10。适用于自定义镜像、载板或底层启动调试；量产前需完成硬件评审和完整回归。

RDK S Build System 用于构建系统镜像和组件。构建前锁定 BSP/manifest 提交、目标 product、Ubuntu 配置、
启动介质、交叉工具链和补丁集；构建结果保存 manifest、日志、镜像 SHA256 与分区清单。S100 常见 Ubuntu
22 配置，S600 5.x 使用 Ubuntu 24 配置，不能只修改产品字符串混用 rootfs 或 boot 产物。

S600 默认发布 UFS 镜像，NVMe 启动需设置 `RDK_DISK_MEDIUM="nvme"` 并匹配 SW8 启动位。Norflash、
UFS/NVMe miniboot 与完整磁盘镜像属于不同区域，更新顺序和组合按官方烧录说明执行。构建成功后先在可恢复
开发板验证，保留串口、DFU/Fastboot 和已知可启动镜像。

自定义载板 bring-up 从电源时序、时钟、reset、boot strap、board ID 与串口开始，再推进 DDR/存储、网口、
USB、PCIe、camera、显示和其他外设。DTS/U-Boot 改动一次只引入一组可验证变化；先证明启动链，再启用复杂
设备。PMIC、board ID 和 pinmux 错误可能表现为软件驱动问题，必须结合原理图和测量结果判断。

交付门槛包括冷/热启动、断电恢复、系统升级和回滚、存储与网络压力、所有外设、BPU/媒体/MCU、thermal、
watchdog、长时间运行和版本可追溯性。Agent 可辅助生成补丁和测试，但不得自主决定 PMIC、电压、启动熔丝或
量产烧录参数。

## 官方来源

- [RDK S Build System](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/rdk_gen)
- [RDK S600 Board Bring-up](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/hardware_development/rdk_s600_board_bringup)
- [RDK S600 System Flashing](https://developer.d-robotics.cc/rdk_s_doc/en/Quick_start/install_os/rdk_s600/burn)
- [RDK S Hardware Development Accessories](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/hardware_development/accessory)
