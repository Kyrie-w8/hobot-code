# 开发环境、交叉编译与依赖管理

> 资料核对日期：2026-08-10。开发主机、交叉工具链、板端系统和目标架构必须作为一个版本集合管理。

先判断任务应在板端原生编译还是在 x86_64 工作站交叉编译。小型应用和验证程序可在板端完成；模型转换、
大型 C++ 工程和系统镜像构建通常更适合容器化工作站。任何构建产物都记录源代码提交、编译器、sysroot、
CMake 选项、目标架构、依赖版本和构建镜像摘要，避免“同名二进制”掩盖 ABI 差异。

交叉编译时从目标 RDK OS 对应文档取得编译器和 sysroot，不用宿主机的随机 AArch64 工具链替代。链接前
检查目标库的 ELF 架构、SONAME 和动态依赖；部署后用板端实际 loader 验证。内核模块还要求目标内核的
准确版本、配置与 headers，一般发行版的 `linux-headers-arm64` 不能代替 RDK 内核 headers。

Python 环境要区分系统包、板端厂商包与业务虚拟环境。BPU、多媒体等厂商 Python 模块常依赖系统库和
特定 Python ABI，不能只复制 site-packages。创建 venv/conda 前记录系统 Python 与厂商包可见性；若需要
继承系统包，明确使用方式并验证导入路径。不要为解决单个依赖冲突批量升级系统 Python、NumPy 或厂商 wheel。

构建或安装前检查磁盘、inode、内存与活动业务。优先把源码、缓存和中间产物放在业务工作目录；不要让
编译器、包管理器或容器默认写满根分区。最小验收应包含启动、动态链接、一个真实输入和退出码，而不只是
“编译成功”。

## 官方来源

- [RDK X Toolchain Development](https://developer.d-robotics.cc/rdk_x_doc/en/Advanced_development/toolchain_development/overview)
- [RDK S Linux Development Guide](https://developer.d-robotics.cc/rdk_s_doc/en/linux_development)
- [RDK S Build System](https://developer.d-robotics.cc/rdk_s_doc/en/Advanced_development/rdk_gen)
- [RDK S AI Toolchain FAQ](https://developer.d-robotics.cc/rdk_s_doc/en/FAQ/toolchain)
