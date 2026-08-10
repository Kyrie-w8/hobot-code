# TogetheROS.Bot 与多媒体开发路由

> 资料核对日期：2026-08-10。TROS、ROS 发行版与 RDK OS 强绑定，先识别实机再选择文档线。

TogetheROS.Bot（TROS）用于在 RDK 上搭建机器人原型，但 ROS 发行版、包名和底层媒体
接口随 RDK 系列与系统版本变化。先确认 `/etc/version`、ROS 环境和已安装软件包，再
选择 X 系列或 S 系列文档；不要把 X5 示例中的库名直接搬到 S100/S600。

相机问题按物理链路到应用层排查：供电与线序、sensor/解串器配置、设备节点与内核日志、
单路官方采集示例、像素格式和分辨率、编码或 ROS 发布节点。先证明单模块工作，再连接
完整 pipeline，避免在 ROS 节点中同时调试传感器、ISP、编码和网络传输。

S100/S600 的高级多媒体栈使用 Camera、vnode 和 vflow 抽象 VIN、ISP、PYM、GDC 等模块；
板端官方 C/C++ 示例位于 `/app/multimedia_samples` 时，应优先从同版本示例建立基线。
S100 的 Python 环境也可能预装 `hobot_vio.libsrcampy`，但安装状态必须实测确认。

多媒体性能验收要记录输入格式、分辨率、帧率、pipeline 拓扑、零拷贝情况、CPU/BPU/GPU
占用、丢帧、端到端延迟与温度。产品页的峰值编解码能力不是任意组合下的实测保证。

当前官方 TROS 路由中，X5/S100 主要对应 Ubuntu 22.04 + ROS 2 Humble，S600 对应
Ubuntu 24.04 + ROS 2 Jazzy。先检查实际存在的 `/opt/tros/humble/setup.bash` 或
`/opt/tros/jazzy/setup.bash`，再启动节点；不能把 source 错误当成包未安装。

`hobot_codec` 的典型相机链路以 NV12 共享内存图像为输入，再输出 JPEG/H264/H265 或 ROS
图像 topic。输入模式、像素格式、topic 和 QoS 任一不一致都可能表现为无数据。先在
`/app/multimedia_samples` 用同一 sensor 建立非 ROS 基线，再接 TROS，可以快速区分底层
Camera/VIN/ISP 问题和 ROS 配置问题。

持续验收至少统计时间戳单调性、丢帧、码率、延迟 P50/P95、CPU、内存和温度。浏览器偶尔
显示一帧只说明链路曾经打通，不代表生产稳定。

## 官方来源

- [TogetheROS.Bot 用户手册](https://developer.d-robotics.cc/tros_doc/tros)
- [TROS 版本说明](https://developer.d-robotics.cc/tros_doc/en/quick_start/changelog)
- [RDK S TROS/ROS FAQ](https://developer.d-robotics.cc/rdk_s_doc/en/FAQ/tros_ros)
- [RDK S 多媒体样例总览](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/multimedia_development/multimedia_application/overview)
- [RDK S Camsys 子系统](https://developer.d-robotics.cc/rdk_s_doc/Advanced_development/multimedia_development/multimedia_development/camsys)
