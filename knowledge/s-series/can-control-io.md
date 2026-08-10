# S100/S600 CAN 与控制类外设

> 资料核对日期：2026-08-10。CAN、PWM、GPIO 等控制输出必须在隔离负载和独立安全机制下测试。

S 系列 CAN 控制器位于 MCU 域，Acore 访问通常涉及 IPC 与 CANHAL 转发。开始前确认板卡原理图、收发器、
通道、终端电阻、MCU 固件、bitrate/data bitrate、classic CAN 或 CAN FD、ID 类型和总线拓扑。Linux 侧
看到接口不代表物理收发器、终端或 MCU 路径已经正确配置。

CAN 总线两端通常需要与线缆阻抗匹配的终端，支线长度与波特率会影响信号完整性。断电测量总线电阻并检查
CAN_H/CAN_L/地线；先用低风险节点和监听模式验证，再发送受限帧。不要在真实车辆或机器人在线时运行总线
扫描、模糊测试或未知控制帧。

MCU UART/I2C/CAN 的所有权必须唯一。若 Acore 通过 IPC 请求 MCU 操作外设，协议要包含版本、命令白名单、
范围校验、序号、超时和明确错误码。PWM、GPIO 与 CAN 控制命令必须在 MCU 或安全控制器中实施限幅、
watchdog、失联回落和急停，不能依赖 Agent 对自然语言的理解作为安全条件。

验收保存 bus load、错误帧、error passive/bus-off、恢复次数、端到端时延和长时间统计。故障注入应在台架上
覆盖断线、短时拥塞、对端复位、IPC timeout 和非法命令，确认系统进入定义好的安全状态。

## 官方来源

- [RDK S MCU CAN](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/mcu_development/mcu_can)
- [RDK S MCU UART](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/mcu_development/mcu_uart)
- [RDK S MCU I2C](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/mcu_development/mcu_i2c)
- [RDK S MCU IPC](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/mcu_development/mcu_ipc)
