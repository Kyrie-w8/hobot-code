# S100/S600 端侧 LLM、VLM 与 VLA

> 资料核对日期：2026-08-10。大模型支持范围必须按板卡、工具链版本、模型变体、量化位宽和官方案例逐项确认。

S100/S100P 官方 `D-Robotics_LLM_S100` 1.0.0 工具链列出 DeepSeek-R1-Distill-Qwen 1.5B/7B、
InternLM2 1.8B、Qwen2.5 1.5B/7B（含部分 Instruct）以及 Qwen2.5-Omni 3B。不同模型支持的量化、
单轮/多轮、PPL、离线/在线功能不完全相同，不能因同属一个 SDK 就假定接口一致。官方 benchmark 使用
S100P、单条 prompt 和特定 context/位宽；TTFT、TPS 与内存只能作为复现实验基线。

S600 产品和官方案例面向更大的 VLM/VLA 工作负载。官方列出 Pi0/0.5、Qwen3-VL-8B、Whisper 等适配方向，
并提供 Pi0 VLA 板端案例；具体可下载模型、输入输出、BPU 核数和量化产物以案例页面为准。产品页“支持”
不等于任意上游 checkpoint 可直接加载，自训练模型仍需正式转换、量化精度和真实任务闭环验证。

LLM/VLM 性能报告同时记录模型精确版本、tokenizer、prompt token、生成 token、context 上限、KV cache、
量化、采样参数、TTFT、prefill、decode TPS、峰值/稳态内存和温度。多轮会话会持续增长 KV cache；Agent
必须设置上下文、输出、并发和超时上限，并在取消、断连和模型错误后释放资源。

VLA 输出是高风险控制建议。官方案例中的模拟器推理和延迟统计不等于真实机器人任务成功率，更不等于安全
认证。真实部署要单独验证观测/动作归一化、相机与关节标定、控制频率、动作限幅、延迟抖动、失联回落、
watchdog 与急停；模型输出必须经过确定性安全控制层，不能由 Hobot Code 直接写入执行器。

模型下载与转换保存许可证、来源 URL、commit、原始/量化 SHA256、工具链、目标架构和校准集。包含云端 API
的多模态案例还要明确数据出板范围、凭据隔离、限流、超时和离线降级。

## 官方来源

- [RDK S100 LLM 工具链 1.0.0](https://developer.d-robotics.cc/rdk_s_doc/Advanced_development/toolchain_development/LLM_Toolchain/s100_LLM_Toolchain)
- [RDK S600 产品页与大模型能力](https://developer.d-robotics.cc/rdks600)
- [RDK S600 Pi0 VLA 官方案例](https://developer.d-robotics.cc/case_doc/en/advanced/vla)
- [RDK S 系列用户手册](https://developer.d-robotics.cc/rdk_s_doc/RDK)
