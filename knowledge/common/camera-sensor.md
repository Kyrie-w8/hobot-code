# Camera、Sensor、MIPI 与图像链路

> 资料核对日期：2026-08-10。摄像头能力由 sensor、载板、连接器、MIPI lane、时钟、驱动和媒体配置共同决定。

接入 camera 前记录 sensor 精确型号、模组版本、I2C 地址、MIPI lane 数与速率、参考时钟、供电、复位脚、
输出格式和分辨率。官方列出的 IMX219、OV5647、IMX477 等支持项只适用于相应板卡和配置，不代表任意同芯片
模组即插即用。FPC 方向错误可能损坏硬件，接线和更换模组必须断电。

排查从物理层向上：供电/时钟/复位，I2C 探测，sensor 驱动 probe 与内核日志，MIPI 接收错误，VIN/ISP，
PYM/GDC，再到编码、显示或 ROS。先运行当前镜像自带的单路官方样例，固定一种分辨率与像素格式；不要在
自定义 ROS 节点里同时排查 sensor、ISP、编码和网络。

S100/S600 多媒体框架用 Camera、vnode、vflow 组织 VIN、ISP、PYM、GDC 等节点，配置通常包含 sensor
参数与内存池。复制另一板卡或另一系统版本的 JSON/C 配置时，逐项核对 sensor index、host、lane、MCLK、
输出 buffer 数和连续内存。X5 的 API 与 S 系列抽象不同，应从各自系统自带样例建立基线。

图像“有画面”仍可能存在 Bayer 顺序、色彩空间、stride、曝光、时间戳或丢帧错误。验收保存原始帧、像素
格式、stride、帧率、曝光/增益、时间戳单调性、MIPI/ISP 错误计数和长时间丢帧统计；AI 精度异常时先导出
模型输入前的实际帧，与训练预处理逐像素比较。

## 官方来源

- [RDK X5 硬件与 MIPI 接口](https://developer.d-robotics.cc/rdk_x_doc/Quick_start/hardware_introduction/rdk_x5)
- [RDK S 多媒体样例总览](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/multimedia_development/multimedia_application/overview)
- [RDK S Camsys 子系统](https://developer.d-robotics.cc/rdk_s_doc/Advanced_development/multimedia_development/multimedia_development/camsys)
- [RDK S100/S600 认证配件](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/hardware_development/accessory)
