# 视频编解码、显示与端到端多媒体 Pipeline

> 资料核对日期：2026-08-10。产品峰值是模块能力参考，不是任意组合 pipeline 的保证值。

多媒体工程先写清 pipeline 拓扑和每条边的格式：Camera/VIN、ISP、PYM/GDC、颜色转换、编码/解码、显示、
网络或 AI。每个节点记录分辨率、像素格式、stride、buffer 数、时间戳、内存类型与所有权。NV12、RGB/BGR、
JPEG、H.264/H.265 的转换位置不明确，常导致隐式 CPU 拷贝、颜色错误或吞吐下降。

S100/S600 从当前镜像 `/app/multimedia_samples` 的最小 Camera、codec、display 或 GPU 样例开始，再用
Camera/vnode/vflow 组合模块。S100 官方 codec 文档给出 VPU/JPU 的规格和限制，但这些数字不能直接用于
S600 或复杂多路组合。X5 使用自身 RDK OS 随附 API 和样例；不要跨系列复制结构体、配置文件或内存池参数。

编码验收包含 profile/level、GOP、码率模式、实际码率、关键帧、时间戳和解码兼容性；显示验收包含刷新率、
颜色、裁剪、旋转和断连恢复。多路场景逐路增加负载并记录资源，不要直接从单路峰值乘算。出现卡住时检查
buffer 是否归还、生产/消费速率、阻塞调用、EOS 和异常退出清理。

端到端测试至少统计首帧时间、稳态 FPS、丢帧、延迟 P50/P95/P99、CPU/内存、媒体共享池、温度和持续
运行。保存一段可复现输入码流和解码后帧摘要，确保性能优化没有改变图像内容或时间顺序。

## 官方来源

- [RDK S 多媒体样例总览](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/multimedia_development/multimedia_application/overview)
- [RDK S100 Codec](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/multimedia_development/S100/codec)
- [RDK S100 Display](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/multimedia_development/S100/display)
- [RDK X5 多媒体与 AI Python API（归档英文页）](https://developer.d-robotics.cc/rdk_doc/en/Basic_Application/multi_media_sp_dev_api/RDK_X5/pydev_multimedia_api_x5/ai-python-api/)
- [RDK X 系列当前用户手册](https://developer.d-robotics.cc/rdk_x_doc/RDK)
