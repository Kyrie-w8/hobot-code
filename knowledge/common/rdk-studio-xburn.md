# RDK Studio 与 XBurn 工具链

> 资料核对日期：2026-08-10。桌面工具用于连接、开发和烧录，但不能替代镜像版本、硬件状态与恢复路径核对。

RDK Studio 是面向机器人开发的桌面工作台，官方资料将对话、项目工作区、设备连接、远程开发、烧录、本地
模型和板端 Agent 组织在同一应用中。使用前确认宿主系统、Studio 版本、目标板支持范围和连接方式；远程
工作区中的命令仍在目标板权限下执行，不能把 GUI 操作误认为无风险。

XBurn 是 PC 端板级烧录和备份工具，当前官方概述列出 RDK S100、S600、X5 和 X5 Module。具体板卡支持的
DFU、Fastboot、串口、USB 或网络路径以对应手册为准。开始烧录前选择正确 product、连接方式、download
mode、存储介质和 firmware type，并保存工具日志。高级功能如全盘擦除、指定分区、分区备份/恢复具有更高
风险，必须先解析准确目标。

Studio 或 XBurn 发现不了设备时，从供电、线缆、USB 枚举、驱动、板卡启动模式和权限逐层检查；不要反复
切换拨码并带电插拔。远程开发失败与烧录链路失败要分开诊断：SSH 正常不证明 DFU/Fastboot 正常，ADB
正常也不证明目标存储和镜像选择正确。

完成后核对烧录日志、首次启动串口、`/etc/version`、启动介质和关键设备。批量烧录需要固定主机、USB hub、
线缆、电源、镜像 SHA256 和工位流程，并抽检恢复；工具显示 100% 不能替代板端启动与功能验收。

## 官方来源

- [RDK Studio 用户手册](https://developer.d-robotics.cc/rdk_studio_doc/category/1-product-intro)
- [XBurn 用户手册](https://developer.d-robotics.cc/xburn_doc/overview)
- [D-Robotics 资料中心](https://developer.d-robotics.cc/rdk_doc_center/)
- [RDK S600 系统烧录](https://developer.d-robotics.cc/rdk_s_doc/en/Quick_start/install_os/rdk_s600/burn)
