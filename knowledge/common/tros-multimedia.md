# TogetheROS.Bot 与多媒体开发路由

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
