# S100/S600 VDSP 与 GPU 加速

> 资料核对日期：2026-08-10。VDSP/GPU 能力和可用 SDK 取决于板型、系统与发布包，不可仅按芯片规格推断。

S600 有两个 VDSP 核，官方说明 VDSP1 仅适用于 S600/双核平台。VDSP 由 remoteproc 管理固件，Acore 与
VDSP 通过 RPMSG 服务通信；同一 service 不支持并发 send/recv 的限制必须纳入线程模型。开发前核对 Xtensa
工具、固件、内存布局、服务名和驱动版本，不要把 S600 VDSP1 配置用于 S100。

VDSP 适合数据并行的视觉/信号处理内核，但传输、对齐和同步可能抵消计算收益。先在 CPU 建立正确性基线，
再测量 H2D/D2H 或共享内存、单核/双核、队列深度与端到端延迟。固件异常时保存 remoteproc 状态、RPMSG、
VDSP 日志和输入；重启 VDSP 前确认不会破坏正在使用共享 buffer 的业务。

S 系列 Mali GPU 可用于图形和部分通用计算/多媒体流程，具体 API 与样例以当前镜像为准。先运行官方 GPU
样例并检查设备、驱动和用户态库一致，再接入业务。不要把桌面 Linux 的 Mesa/OpenCL 包直接覆盖板端厂商
驱动；用户态与内核驱动不匹配可能导致初始化失败或不稳定。

加速验收同时报告正确性、传输开销、P50/P95、吞吐、CPU 占用、内存、功耗和温度。多加速器 pipeline
明确每个 buffer 的生产者、消费者、格式、cache 同步和超时，避免用无界重试掩盖跨核死锁。

## 官方来源

- [RDK S VDSP Development Guide](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/vdsp_development)
- [RDK S Multimedia Samples](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/multimedia_development/multimedia_application/overview)
- [RDK S600 产品页](https://developer.d-robotics.cc/rdks600)
