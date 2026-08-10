# 官方 Model Zoo 使用与模型验收

> 资料核对日期：2026-08-10。仓库名称不等于全系列支持；必须阅读当前 README 和具体模型目录的板卡说明。

RDK 官方 Model Zoo 是转换、前后处理和板端部署的参考实现，不是所有版本的统一二进制仓库。X 系列优先查看
`D-Robotics/rdk_model_zoo`，当前 README 建议 RDK OS 3.5.0 及以上。S 系列仓库
`D-Robotics/rdk_model_zoo_s` 当前明确列出的主要对象是 S100/S100P Nash 平台；S600 用户必须逐模型确认
是否有明确适配、目标架构和对应发布资源，不能看到仓库名中的 `_s` 就直接运行。

采用样例前记录仓库 commit、模型目录 README、下载资产 URL 与 SHA256、目标板卡、系统、工具链和 Runtime。
先原样运行官方预转换模型与固定测试图，建立环境基线；再替换为自有 ONNX/产物，并同步修改输入尺寸、类别、
anchors、归一化、输出节点和后处理。只替换模型文件不是有效迁移方法。

性能比较必须使用同一输入、预处理、线程、BPU 核心、warmup 和统计口径。区分模型推理延迟、前后处理延迟、
整条 camera pipeline 的端到端延迟和吞吐。官方表格或产品峰值只作为参考，正式结论来自目标板上的重复实测。

模型验收至少包括：官方样例基线、自有固定输入输出对齐、真实数据集任务指标、持续运行、异常输入、资源回收、
温度和降频。对 Model Zoo 做本地修改时保留最小补丁，不要把下载模型、业务数据或密钥提交到源码仓库。

## 官方来源

- [Model Zoo 用户手册](https://developer.d-robotics.cc/model_zoo_doc/model_zoo_intro)
- [RDK Model Zoo](https://github.com/D-Robotics/rdk_model_zoo)
- [RDK Model Zoo S](https://github.com/D-Robotics/rdk_model_zoo_s)
- [RDK S AI 模型与工具链 FAQ](https://developer.d-robotics.cc/rdk_s_doc/en/FAQ/toolchain)
- [D-Robotics 官方 GitHub](https://github.com/D-Robotics)
