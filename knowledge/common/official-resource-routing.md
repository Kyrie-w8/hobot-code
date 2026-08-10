# 官方资料中心与版本路由

> 资料核对日期：2026-08-10。检索专业结论时优先使用当前资料中心；旧版手册仅用于对应历史系统的版本证据。

D-Robotics 资料已按 RDK 用户手册、芯片 SDK、TROS、Model Zoo、应用案例、产品与配件、软件和算法工具链
重新组织。Agent 回答板端问题前先确定资料类别，不应只在单一 RDK 手册中搜索所有问题：板卡使用查 X/S
用户手册，芯片底层接口查 SDK，机器人中间件查 TROS，模型接口与案例查 Model Zoo，转换算子与量化查
算法工具链，刷机工具行为查 XBurn。

部分 2026-06-10 前的聚合页面明确标记为归档。归档页面仍可解释旧 RDK OS、旧工具链和旧样例，但不能用来
断言最新支持范围。当前资料中心或当前手册与归档页冲突时，以当前资料和实机版本为准；当前资料尚未上架时，
应明确说明只能使用归档资料，并通过板端包、头文件、样例和最小测试补充证据。

来源可信度顺序是：当前官方产品/版本/用户手册，当前官方 SDK/工具链/TROS/Model Zoo 手册，D-Robotics
官方 GitHub 仓库，归档官方手册。社区文章可以帮助发现线索，但不进入本地专业知识清单，也不能覆盖官方
版本边界。任何来源都记录核对日期，不把搜索结果摘要当作正文证据。

面对 S100/S600 共用页面或 `s100-s600` 包名，仍需检查章节、模型目录和 release note 是否明确列出目标
板卡。面对 X5 RDK 用户手册与 X5 芯片 SDK，要区分量产/EVB 芯片开发和 RDK X5 开发板封装，接口和烧录
流程不能无条件互换。

## 官方来源

- [D-Robotics 资料中心](https://developer.d-robotics.cc/rdk_doc_center/)
- [RDK X 系列用户手册](https://developer.d-robotics.cc/rdk_x_doc/RDK)
- [RDK S 系列用户手册](https://developer.d-robotics.cc/rdk_s_doc/RDK)
- [D-Robotics 官方 GitHub](https://github.com/D-Robotics)
