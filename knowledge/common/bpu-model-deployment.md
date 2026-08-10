# BPU 模型转换、部署与验证

> 资料核对日期：2026-08-10。模型、工具链和 Runtime 的兼容性以目标芯片与当前官方版本说明为准。

RDK 的 BPU 模型不是跨芯片通用产物。开始转换前同时锁定板卡、BPU 架构、RDK OS、
板端推理库、转换工具链或 Docker 镜像、原始模型格式以及输入输出约定。X5 与 S100/S600
不能因为都能运行 `hrt_model_exec` 就复用同一个转换结果。

可靠流程分为五个阶段：

1. 在训练框架中导出并检查 ONNX，固定输入尺寸、数据布局、归一化和输出节点语义。
2. 使用与目标 BPU 和 RDK OS 对应的官方工具链完成校准与量化，保存完整配置和日志。
3. 比较浮点模型、校准模型与量化模型的逐输出误差。官方 Model Zoo 的排障建议以输出
   节点余弦相似度 0.999 为推荐目标，0.99 为最低参考线；任务指标仍需单独评测。
4. 将产物放到板端，用匹配的运行库执行最小推理，记录延迟、内存、温度和错误日志。
5. 最后接入预处理、后处理、摄像头或 ROS 链路，验证端到端任务结果。

遇到转换失败时，支持信息至少包含：目标板卡及 BPU 架构、`horizon_nn` 版本、Python
版本、工具链镜像版本、原始 ONNX、转换配置、完整日志和可复现输入。不要通过随机替换
算子或降低校准阈值来掩盖数值问题。

模型能加载不代表部署完成。验收至少区分：转换成功、板端单次 smoke test、持续性能与
温度测试、真实数据精度回归、完整应用链路。每一层都要保留可复现证据。

## 版本与产物清单

当前官方版本路由中，S100 RDK OS 4.0.5 与 S600 RDK OS 5.1.0 都有 OE 3.7.0 工具链入口，
但转换时仍必须选择正确的目标 BPU；工具链版本相同不代表模型、驱动或系统包可以互换。
X5 使用独立的 X 系列目标和运行时，RDK OS 3.5.0 起 Python 示例统一转向 `hbm_runtime`。

每个可交付模型至少保存板型、完整 RDK OS、BPU target、工具链/镜像版本、板端 Runtime 包、
原始模型和转换配置 SHA256、校准数据标识、输出模型 SHA256。缺少这些字段时，不应把某次
成功加载推广为可重复部署。

定位精度问题按导出、预处理、量化、Runtime、后处理五层比较固定输入。性能报告同时给出
warmup、样本数、BPU 时间、预处理、后处理、端到端 P50/P95、CPU/内存、核心调度、温度和
降频状态；单个 FPS 数字不能证明完整 pipeline 达标。

## 官方来源

- [Model Zoo 用户手册](https://developer.d-robotics.cc/model_zoo_doc/model_zoo_intro)
- [RDK Model Zoo（X 系列）](https://github.com/D-Robotics/rdk_model_zoo)
- [RDK Model Zoo S（以仓库当前 README 的板卡范围为准）](https://github.com/D-Robotics/rdk_model_zoo_s)
- [RDK S AI 模型与工具链 FAQ](https://developer.d-robotics.cc/rdk_s_doc/en/FAQ/toolchain)
- [RDK S 算法工具链](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/toolchain_development/overview)
