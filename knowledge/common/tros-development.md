# TogetheROS.Bot、ROS 2 与机器人应用

> 资料核对日期：2026-08-10。TROS 包、ROS 发行版和 RDK OS 版本需成套使用。

TogetheROS.Bot（TROS）建立在 ROS 2 之上，增加 RDK sensor、BPU 推理、图像处理、编解码、渲染与应用
示例。它不是另一套与 ROS 无关的通信框架。迁移普通 ROS 2 包时仍需处理 ROS 发行版、消息接口、QoS、
RMW、依赖和交叉编译；接入 RDK 加速组件时还要遵守共享内存图像和硬件模块的格式契约。

当前官方版本线中，X5/S100 项目常见 Ubuntu 22.04 + ROS 2 Humble，S600 在 RDK OS 5.1.0 线使用
Ubuntu 24.04 + ROS 2 Jazzy；TROS Jazzy 2.5.0 首次加入 S600 适配，2.5.4 对齐 S600 5.1.0。
运行前检查 `/opt/tros/<distro>/setup.bash` 和已安装包版本，不要只凭项目 README 假定发行版。

图像链路优先采用官方消息与零拷贝机制，但 publisher、subscriber、编码器和推理节点必须在 encoding、
尺寸、stride、时间戳、共享内存开关和 QoS 上一致。无数据时先验证底层 Camera/VIN/ISP 官方样例，再检查
topic 类型、QoS、domain ID 和组件参数；不要通过无界队列掩盖下游阻塞。

机器人应用验收应覆盖启动依赖、设备缺失、节点退出、重连、时间同步、消息延迟 P50/P95、丢帧、CPU/BPU、
内存和温度。模型输出不得直接承担急停或硬实时运动控制；TROS 负责感知和编排，安全闭环应放在确定性控制器
和 MCU 中，并有独立 watchdog 与限幅。

## 官方来源

- [TogetheROS.Bot 用户手册](https://developer.d-robotics.cc/tros_doc/tros)
- [TROS 版本说明](https://developer.d-robotics.cc/tros_doc/en/quick_start/changelog)
- [RDK S TROS/ROS FAQ](https://developer.d-robotics.cc/rdk_s_doc/en/FAQ/tros_ros)
- [RDK X 机器人开发](https://developer.d-robotics.cc/rdk_x_doc/en/Robot_development)
- [D-Robotics TROS GitHub](https://github.com/D-Robotics)
