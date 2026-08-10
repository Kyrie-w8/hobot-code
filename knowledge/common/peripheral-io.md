# 40-pin、GPIO、I2C、SPI、UART 与 PWM

> 资料核对日期：2026-08-10。针脚定义、电平与复用关系必须按具体板型和载板版本核对后再接线。

外设开发先画出“物理针脚、SoC 控制器、pinctrl 功能、Linux 设备、用户态接口”五层映射。读取当前
overlay、设备树、pinctrl 和设备节点作为实机证据；`/dev/spidev*` 或 `/dev/tty*` 存在不代表针脚已经
复用到连接器，也不代表电气连接安全。GPIO 编号、控制器编号和物理 pin 不是同一个概念。

X5 40-pin 信号为 3.3V 电平，官方样例位于 `/app/40pin_samples`，常用复用由 `srpi-config` 设置并在
重启后生效。UART1 使用物理 pin 8/10；SPI1 和 CAN 等功能可能共享针脚或 overlay，启用一个功能前先检查
冲突。严禁把 5V 信号直接接到 3.3V GPIO，也不要从信号针脚给大负载供电。

S100/S600 的 UART、I2C、SPI、GPIO 分布跨 Acore、MCU 和不同电源域。S100 的 UART2 默认可能未启用，
且与 I2C5 受 DIP/复用配置影响。S600 驱动文档列出更多 UART 控制器，但数量不等于都被当前载板引出。
涉及 MCU 域的 CAN、UART 或 I2C 时，要先确认所有权和 IPC/CANHAL 路径，不能让 Linux 与 MCU 同时驱动
同一控制器。

调试顺序是：断电检查接线和共地；低速单设备测试；核对波形、电平和上拉；再提高频率或接入业务。
I2C 地址扫描可能改变某些器件状态，SPI 无通用枚举，PWM/GPIO 写操作可能驱动执行器，均应在隔离负载后
执行。交付时保存 pin map、overlay/DTS、总线参数、线缆长度、外设型号和示波器或逻辑分析仪证据。

## 官方来源

- [RDK X5 40-pin 定义](https://developer.d-robotics.cc/rdk_x_doc/Basic_Application/01_40pin_user_sample/40pin_define)
- [RDK X5 UART 样例](https://developer.d-robotics.cc/rdk_x_doc/en/Basic_Application/01_40pin_user_sample/uart)
- [RDK X config.txt 与 overlays](https://developer.d-robotics.cc/rdk_x_doc/en/System_configuration/config_txt)
- [RDK S100/S600 驱动开发指南](https://developer.d-robotics.cc/rdk_s_doc/en/driver_development_s100_s600)
- [RDK S100 UART 使用](https://developer.d-robotics.cc/rdk_s_doc/en/Basic_Application/03_40pin_user_guide/s100/uart)
