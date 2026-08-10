# OpenExplorer 算法工具链与量化工程

> 资料核对日期：2026-08-10。工具链版本相同不代表目标 BPU、Runtime、模型仓库和系统包可以互换。

转换项目必须锁定目标板卡和 BPU 架构、RDK OS、板端 `libdnn`/Runtime、OpenExplorer/OE 版本、Docker
镜像摘要、Python 与 `horizon_nn` 版本。S 系列官方 OE 3.7.0 页面标注对应 S100 系统 4.0.5，并提供
`s100-s600` 工具包；对 S600 的具体模型和接口支持仍应按 5.x 发布说明与模型目录验证，不能只依据包名。

推荐流程是：检查 ONNX 合法性和固定输入；准备有代表性的校准集；明确 layout、颜色空间、归一化、均值方差
和输出节点；运行 checker；完成 PTQ/QAT 编译；比较浮点、校准和量化输出；最后在板端匹配 Runtime 上验证。
动态 shape、自定义算子、输出头解码和 CPU fallback 都应在转换阶段显式记录。

精度下降按预处理、ONNX、校准数据、量化敏感层、Runtime 输入、后处理逐层定位。YOLO 类模型尤其检查输出
特征图维度、NHWC/NCHW、类别数、anchors、sigmoid/解码位置、letterbox 逆变换与 NMS。把自定义 `.bin`/
`.hbm` 直接替换进板端样例而不修改前后处理，可能产生错误结果或越界崩溃。

支持工单至少包含板卡和完整系统版本、目标架构、工具链与容器、原始模型、yaml、校准样本、完整转换日志、
板端代码、Runtime 版本、固定复现输入、期望与实际输出。不要在知识库或项目配置中保存官方页面出现的临时
仓库口令；凭据应通过受控密钥渠道获得并定期轮换。

## 官方来源

- [RDK S 算法工具链 3.7.0](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/toolchain_development/overview)
- [RDK S AI 模型与工具链 FAQ](https://developer.d-robotics.cc/rdk_s_doc/en/FAQ/toolchain)
- [OpenExplorer 在线手册](https://toolchain.d-robotics.cc/)
- [RDK X Toolchain Development](https://developer.d-robotics.cc/rdk_x_doc/en/Advanced_development/toolchain_development/overview)
