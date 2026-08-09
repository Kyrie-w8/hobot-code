# RDK 系统识别与诊断

先识别板型和系统软件，再选择命令、示例和工具链。`/sys/firmware/devicetree/base/model`
是板型的实时证据，`/etc/version` 是 RDK OS 的首选版本来源。不要只依赖 Ubuntu 版本：
X5 与 S100 都可能显示 Ubuntu 22.04，但其 BPU、驱动和多媒体栈并不相同。

`rdkos_info` 用于收集软硬件版本、已加载驱动、RDK 软件包、近期日志以及 CPU/BPU/内存
状态。它适合作为支持工单和环境对比的证据，但输出可能较长；先运行 `system_snapshot`，
需要完整报告时再单独调用，并对输出做截断或保存到文件。

推荐的只读诊断顺序：

1. 读取板型、`/etc/version`、`/etc/os-release` 和 `uname -a`。
2. 检查剩余内存、负载、磁盘空间、温度以及 BPU 设备节点。
3. 检查目标运行库、命令和软件包的实际版本。
4. 用最小模型或官方示例做限时 smoke test，并保留命令、退出码和日志。

`rdkos_info` 或设备节点存在，只表示相关组件可见，不等于摄像头已连接、模型与当前 BPU
兼容、驱动链路已工作或性能达到规格值。文档说明和实机证据必须分开陈述。

板卡版本可能包含 `Beta`、`RC` 或厂商后缀。匹配资料时保留完整字符串；若知识结果的
`versionMatch` 为 `false`，只把它当邻近版本线索，不直接执行写系统、刷机或升级步骤。

只读基线通常包括板型、`/etc/version`、`/etc/os-release`、`uname -a`、`df -hT`、`df -ih`、
`/proc/meminfo`、`systemctl --failed` 和本次启动的 warning/error 日志。内存问题要区分
`MemAvailable`、CMA/连续内存、进程 RSS 和媒体共享内存池；普通 free 很低可能只是页缓存，
而 BPU/媒体分配失败也可能发生在总内存仍充足时。

温度诊断记录所有 thermal zone 的类型和值，并与 CPU/BPU 负载、频率、供电告警对齐。
服务故障按单元状态、本次启动日志、依赖设备/文件、最小前台启动的顺序定位；不要先反复重启
或删除状态目录，以免覆盖首次故障证据。
