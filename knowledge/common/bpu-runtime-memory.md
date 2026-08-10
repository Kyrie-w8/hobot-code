# BPU Runtime、HBM 产物与内存管理

> 资料核对日期：2026-08-10。Runtime API、模型格式和内存接口必须以当前板端软件包为准。

BPU 部署的核心契约是：模型产物的目标架构与板端驱动/Runtime 兼容，输入输出 tensor 的 shape、layout、
dtype、stride 与量化参数一致，内存生命周期覆盖异步任务。模型能被加载只证明文件头和部分兼容性，不证明
输入正确、所有输出可用或长时间稳定。

X5 RDK OS 3.5.0 起 Python 推理示例使用 `hbm_runtime`，典型接口围绕 `HB_HBMRuntime`，官方基础模型位于
`/opt/hobot/model/x5/basic/`。旧系统或旧示例可能使用不同接口，先检查 `/etc/version`、Python 模块位置
和实际安装包，不要用 `pip install` 随机覆盖厂商 Runtime。

S100/S600 的媒体与 BPU 可能使用共享/连续物理内存。普通 `MemAvailable` 充足时，仍可能因 CMA、heap、
对齐、缓存同步或 buffer 生命周期失败。零拷贝并非“没有内存管理”：生产者与消费者必须约定所有权、格式、
同步和释放顺序；CPU 与设备之间还要遵守对应 API 的 cache flush/invalidate 规则。

验证时先用固定输入查询模型元数据，再运行同步单次、循环、并发和异常退出测试。记录模型 SHA256、Runtime、
BPU 核心选择、warmup、任务数、每层/整体延迟、输入输出摘要、RSS/CMA/共享池、温度和错误日志。遇到段错误
优先检查 tensor 大小、stride、输出数量、释放时序和前后处理越界，而不是直接重装系统。

## 官方来源

- [RDK X5 hbm_runtime Python API（归档英文页）](https://developer.d-robotics.cc/rdk_doc/en/Basic_Application/multi_media_sp_dev_api/RDK_X5/pydev_multimedia_api_x5/ai-python-api/)
- [RDK X 系列当前用户手册](https://developer.d-robotics.cc/rdk_x_doc/RDK)
- [RDK S AI 模型与工具链 FAQ](https://developer.d-robotics.cc/rdk_s_doc/en/FAQ/toolchain)
- [RDK S100/S600 HBMEM 驱动入口](https://developer.d-robotics.cc/rdk_s_doc/en/driver_development_s100_s600)
- [RDK Model Zoo](https://github.com/D-Robotics/rdk_model_zoo)
