# 系统镜像、安装、升级与恢复

> 资料核对日期：2026-08-10。刷机步骤必须按板型、硬件版本和目标存储介质重新核对，不能跨系列照搬。

开始前记录板型、载板版本、当前 `/etc/version`、启动介质、分区布局、网络配置和业务数据位置。
下载镜像后保存来源 URL、文件名、版本与 SHA256。先备份业务数据和关键配置，再确认稳定供电、散热、
串口调试手段和可用的恢复主机。升级说明中没有明确支持的跨版本跳跃，不应自行推断可行。

X5 使用 X 系列镜像与 RDK OS 3.x 路线。系统升级和 miniboot 更新是两个不同层次；执行
`rdk-miniboot-update` 前必须核对发布说明、当前和目标 miniboot、供电及回滚条件。官方警告的
miniboot 降级限制不能通过替换文件或强制写入规避。

S600 使用 XBurn。空白板或系统损坏时进入 DFU+Fastboot，正常系统更新可使用 Fastboot。官方默认
提供 UFS 镜像；NVMe 启动需要选择正确的 SW8 启动位并用 `RDK_DISK_MEDIUM="nvme"` 自行构建对应
镜像。`miniboot_flash` 位于 Norflash，`miniboot_ufs`/`miniboot_nvme` 与实际启动介质对应，选择错误
可能导致无法启动。分区备份完成后还要验证文件可读、大小合理和恢复路径，不能只看工具提示成功。

烧录后的验收包括：首次启动完成、板型和完整版本正确、根文件系统位于预期介质、网络和 SSH 正常、
BPU/媒体/MCU 设备与关键服务可用、官方最小示例运行通过。保留串口日志和烧录日志；出现黑屏时先看
串口与启动介质，不要连续重复烧录覆盖首个故障证据。

## 官方来源

- [RDK X 系列镜像下载](https://developer.d-robotics.cc/rdk_x_doc/Quick_start/download)
- [RDK X 系列版本说明](https://developer.d-robotics.cc/rdk_x_doc/en/Release_Note/release_note)
- [RDK S600 系统烧录](https://developer.d-robotics.cc/rdk_s_doc/en/Quick_start/install_os/rdk_s600/burn)
- [RDK S Build System](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/rdk_gen)
