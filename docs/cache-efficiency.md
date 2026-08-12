# Prompt 缓存效率审计

本文记录 Hobot Code 对模型 Prompt 缓存的实现边界、实机测量方法与当前结果。它不是模型厂商的性能承诺；网关路由、账号缓存、缓存淘汰和模型版本变化都可能改变结果。

## 结论

Hobot Code 在本次审计前已经能解析 Anthropic-compatible usage 中的 `cache_read_input_tokens` 与 `cache_creation_input_tokens`，并把它们映射为 Pi 的 `cacheRead` 与 `cacheWrite`。因此基础多轮会话可以使用上游自动前缀缓存，但产品此前没有显示命中率，也无法判断模型、系统 Prompt 或工具契约是否在相邻请求间变化。

0.24.0 增加 `/cache [status|reset]`：它汇总当前进程内网关实际返回的 token，并对系统 Prompt 和有序工具契约做 SHA-256 指纹。哈希只用于诊断稳定性，不保存 Prompt、工具说明、会话正文或凭据，也不等同于上游内部缓存键。

指标定义为：

```text
totalInput = input + cacheRead + cacheWrite
hitRate   = cacheRead / totalInput
```

输出 token 不属于 Prompt 缓存分母。聚合命中率包含首次请求；评估进入稳定状态后的效率时应同时报告热轮命中率，不能只挑最好的一轮。

## 参考原则

[InfoQ 文章](https://mp.weixin.qq.com/s/Nmfg5eF6rC7HY3e-zT3CFg)介绍的 Pi/DeepSeek 案例报告了接近 10 亿输入 token、99.93% 命中率，并将高命中归因于稳定的前缀、只追加不改写历史，以及对动态日期、工具顺序和摘要改写的控制。该数值来自文章转述的特定用户、模型和运行方式，不能直接当作 Hobot Code 或 D-Robotics 网关基线。

[DeepSeek 官方缓存文档](https://api-docs.deepseek.com/guides/kv_cache)确认其官方服务默认启用缓存，后续请求只有完整复用已持久化的前缀单元才会命中；缓存属于尽力而为，并不保证 100%。[官方发布说明](https://api-docs.deepseek.com/news/news0802/)还说明缓存按 64 token 单元工作，长期未使用的条目会自动清理。[Anthropic 兼容说明](https://api-docs.deepseek.com/guides/anthropic_api)则明确 `cache_control` 会被忽略，缓存应由服务自动完成。这些规则来自 DeepSeek 官方端点；D-Robotics 是独立网关和路由层，必须以它实际返回的 usage 为准。

## 代码审视

已经具备：

- Provider 的流式与缓冲响应路径都会验证并读取缓存 usage。
- 会话消息按正常多轮顺序追加，后续请求能复用之前的 system、tools 和 history 前缀。
- D-Robotics 模型配置、专家 Prompt 和板卡快照在一次进程内通常保持稳定。

本次补齐：

- `/cache` 展示累计和最近一次命中率、cache read/write、未缓存输入及前缀稳定性。
- 指纹覆盖模型路由、系统 Prompt 和有序工具契约；模型切换、工具增删、工具重排或 Prompt 改动都会被诊断为变化。
- RDK 专家 Prompt 在 `session_start` 渲染一次并冻结到会话结束，避免运行中模板文件或板卡探测结果变化破坏前缀；重载或新会话会重新读取。
- 汇总覆盖当前进程完整请求历史，最近明细最多保留 32 条，避免长期会话无界增长。
- `tests/cache-metrics.test.mjs` 覆盖公式、冷热聚合、Prompt 变化、工具顺序变化和模型切换；Provider 回归夹具也加载指标模块。

仍需关注：

- `before_agent_start` 每个用户回合都会重新拼装质量门状态、按当前问题召回的记忆和持久目标进度。这些内容如果变化，会改变 system 前缀。它们当前有明确产品语义，不能为了命中率直接删除；后续应评估改成会话消息或稳定状态引用。
- 权限变化会改变活跃工具集合，这是必要的安全行为。命中率不能优先于工具不可见和 fail-closed 权限判定。
- Pi 自动压缩会改写较早历史。当前尚未对摘要确定性和摘要缓存做 Hobot Code 层保证，压缩后的首轮应预期出现缓存下降。
- 上游只提供 token 结果，不公开缓存键、TTL 或账号隔离细节；客户端指纹只能定位自身前缀变化。

## S100 实机结果

测量时间：2026-08-12（Asia/Shanghai）
板卡：RDK S100 `10.112.10.98`
板端版本：Hobot Code 0.23.7
Provider / 模型：D-Robotics / `kimi-k3`
参数：thinking off，默认扩展与工具契约，固定工作目录，独立 session ID，连续追加 15 个短用户回合

0.24.0 没有改变请求语义，只新增成功响应后的观测，因此 0.23.7 的网关 usage 可作为改动前基线。每轮均从 JSON 事件的 assistant `message_end.usage` 读取，不使用本地 token 估算。

| 轮次 | 未缓存 input | cacheRead | cacheWrite | 命中率 |
|---:|---:|---:|---:|---:|
| 1 | 2,589 | 512 | 0 | 16.51% |
| 2 | 137 | 3,072 | 0 | 95.73% |
| 3 | 222 | 3,072 | 0 | 93.26% |
| 4 | 302 | 3,072 | 0 | 91.05% |
| 5 | 148 | 3,328 | 0 | 95.74% |
| 6 | 229 | 3,328 | 0 | 93.56% |
| 7 | 305 | 3,328 | 0 | 91.60% |
| 8 | 123 | 3,584 | 0 | 96.68% |
| 9 | 201 | 3,584 | 0 | 94.69% |
| 10 | 259 | 3,584 | 0 | 93.26% |
| 11 | 102 | 3,840 | 0 | 97.41% |
| 12 | 164 | 3,840 | 0 | 95.90% |
| 13 | 222 | 3,840 | 0 | 94.53% |
| 14 | 294 | 3,840 | 0 | 92.89% |
| 15 | 122 | 4,096 | 0 | 97.11% |

汇总：

- 早期热轮（2-5）：12,544 / 13,353 tokens 命中，**93.94%**。
- 长会话热轮（6-15）：36,864 / 38,885 tokens 命中，**94.80%**。
- 全部热轮（2-15）：49,408 / 52,238 tokens 命中，**94.58%**。
- 全部轮次（含首次）：49,920 / 55,339 tokens 命中，**90.21%**。
- 热轮单轮范围为 **91.05%-97.41%**，没有一轮达到 99%。
- 首轮已有 512 tokens 命中，说明“新 session”不等于上游绝对冷缓存；相同账号下的公共系统前缀可能已经存在。
- 本次没有返回 `cacheWrite`。这只说明该兼容网关没有通过当前 Anthropic usage 字段报告写入 token，不能推断它没有建立缓存。

这组结果证明 Hobot Code 的追加式多轮链路能稳定获得高比例缓存；在本次默认产品路径上，可复现的热轮汇总是 **94.58%**，不能声称达到文章中的 99%+。差距可能来自每轮新增消息与输出、动态系统上下文、模型与网关缓存粒度，以及文章案例不同的模型和 workload。

## 长稳定前缀上限

为了区分产品默认上下文与网关能力，同日在同一 S100/Kimi K3 上执行了第二组对照：关闭工具、Skills 与项目上下文，保留 RDK 扩展，通过 `--append-system-prompt` 注入约 50K token 的唯一固定文本，并在同一 session 连续追加 3 个短回合。唯一文本使首轮不可能复用此前的测试前缀。

| 轮次 | 未缓存 input | cacheRead | cacheWrite | 命中率 |
|---:|---:|---:|---:|---:|
| 1 | 50,837 | 0 | 0 | 0.00% |
| 2 | 242 | 50,688 | 0 | 99.52% |
| 3 | 308 | 50,688 | 0 | 99.40% |

两个热轮合计 101,376 / 101,926 input tokens 命中，命中率为 **99.46%**。这证明 D-Robotics/Kimi K3 网关与 Hobot Code Anthropic-compatible 请求链路能够在长且严格稳定的前缀下达到 99% 级别。它是诊断上限，不是日常开发承诺：默认工具契约、短会话新增内容、记忆召回、目标状态、质量门和压缩都会降低比例。

## DeepSeek V4 Flash 协议对照与优化

测量时间：2026-08-12 至 2026-08-13（Asia/Shanghai）

板卡：同一 RDK S100

Provider / 模型：D-Robotics / `deepseek-v4-flash`

第一轮审计使用 D-Robotics Anthropic-compatible `/v1/messages`。默认产品上下文前 11 个有效回合合计 **39,765 input tokens**，全部为 `cacheRead=0`、`cacheWrite=0`；第 12-15 回合返回空 content 和零 usage。约 52K token 长稳定前缀只有首轮正常，两个重复回合仍为空。2026-08-13 增加的独立原始 API 对照也得到相同方向：8K 自动缓存、8K 显式 `cache_control` 和 32K 自动缓存各重复三轮，所有有效回合仍为 `cacheRead=0`。完全相同请求的 input/output token 还会变化，说明该兼容路由的缓存和 usage 行为不适合作为产品路径。

随后在相同模型、账号和网关上测试 OpenAI-compatible `/v1/chat/completions`。该路径同时通过 `prompt_tokens_details.cached_tokens` 与 `cache_read_input_tokens` 暴露缓存。8K 对照第二轮命中 6,400 / 6,471 tokens（98.90%），但第三轮回落为 0，短前缀稳定性不足。32K 第一组结果如下：

| 轮次 | prompt tokens | cached tokens | 命中率 | 耗时 | 结果 |
|---:|---:|---:|---:|---:|---|
| 1 | 25,803 | 0 | 0.00% | 1.28 s | 正常 |
| 2 | 25,803 | 25,600 | 99.21% | 1.28 s | 正常 |
| 3 | 25,803 | 25,600 | 99.21% | 1.13 s | 正常 |

在参数兼容对照已经预热同一全新 32K 前缀后，继续使用 `chat_template_kwargs.enable_thinking=false` 做五轮稳定性复验。其中 4/5 轮命中；命中轮均为 23,040 / 23,290 tokens，单轮 **98.93%**。第 4 轮完全相同的请求回落到 0 命中，并重新出现 24 个 reasoning tokens；这说明 D-Robotics 后端可能在不同路由实例间漂移，客户端不能保证每次命中。五轮详情：

| 轮次 | cached / prompt | 命中率 | 耗时 | reasoning tokens |
|---:|---:|---:|---:|---:|
| 1 | 23,040 / 23,290 | 98.93% | 1.01 s | 0 |
| 2 | 23,040 / 23,290 | 98.93% | 7.09 s | 0 |
| 3 | 23,040 / 23,290 | 98.93% | 5.88 s | 0 |
| 4 | 0 / 23,290 | 0.00% | 10.95 s | 24 |
| 5 | 23,040 / 23,290 | 98.93% | 0.93 s | 0 |

## DeepSeek 完整产品路径

基于上述证据，0.24.0 只把 DeepSeek V4 Flash 和 Pro 切换到 OpenAI-compatible 流式实现；Kimi K3、Qwen 3.8 Max 和 GLM 5.2 保持原 Anthropic SSE 路径。DeepSeek thinking off 映射到已验证的 `chat_template_kwargs.enable_thinking=false`，模型仍保持文本输入限制。

先执行更接近日常开发的六轮测试：加载完整 RDK 扩展，保留 `read`、`grep`、`find`、`ls` 四个只读工具及其真实工具契约，不额外注入长文本，只连续追加短用户消息。六轮全部正常、`reasoning=0`，系统 Prompt 与工具契约指纹在 5 次转换中均保持稳定。

| 轮次 | uncached input | cacheRead | 命中率 |
|---:|---:|---:|---:|
| 1 | 1,746 | 0 | 0.00% |
| 2 | 739 | 1,024 | 58.08% |
| 3 | 756 | 1,024 | 57.53% |
| 4 | 261 | 1,536 | 85.48% |
| 5 | 278 | 1,536 | 84.67% |
| 6 | 39 | 1,792 | 97.87% |

全部热轮合计 6,912 / 8,985 input tokens 命中，命中率为 **76.93%**；含首次冷轮的聚合命中率为 **64.41%**。短会话的前两轮仍有较多新增 Prompt 和历史，因此不能用长前缀的 99% 上限替代日常基线；随着会话增长，第六轮已达到 97.87%。

发布前在 S100 上加载完整当前 RDK 扩展、完整 Hobot Code 系统 Prompt 和约 32K token 唯一稳定前缀，在同一 Pi RPC 进程连续提交两轮短消息。首轮冷请求为 24,820 uncached input；第二轮为 24,576 cache-read + 254 uncached input，热轮命中率为 **98.98%**。两个回复均正常完成且 `reasoning=0`。`/cache` 同时显示：

```text
Cache: 2 request(s) | model deepseek-v4-flash
Hit rate: 49.5% aggregate | 99.0% latest
Input: 49.6k total | 24.6k read | 0 write | 25.1k uncached
Prefix stability: 0 route/contract change(s) across 1 transition(s)
```

此外，独立 Pi 流式回归验证了纯文本响应；工具回归让 DeepSeek 生成 `read` tool call、接收 Pi 的 tool result，再输出文件中的 `RDK_TOOL_OK`，两次模型调用均 `reasoning=0`。因此此次优化覆盖真实 Agent 所需的流式文本、工具调用、多轮历史、thinking-off 和 usage，不只是原始 HTTP 请求成功。

产品可对外强调的已验证亮点是：**Hobot Code 具备网关实测的缓存可观测性和前缀稳定性诊断；D-Robotics/Kimi K3 默认热轮 94.58%、长稳定前缀 99.46%；DeepSeek V4 Flash 经协议优化后，完整 Hobot Code 热轮达到 98.98%。** DeepSeek 的五轮独立复验有 4 轮命中，仍必须把路由漂移披露为当前服务边界，不能把最佳单轮描述为 SLA。

## 复测规范

发布前缓存回归应至少执行 5 个连续回合，并记录以下两组数据：

1. 默认产品配置：保留 RDK 扩展、权限和工具契约，用于反映真实用户路径。
2. 稳定前缀上限：关闭工具、Skills 和项目上下文，用于区分产品动态内容与上游缓存能力。

每组必须固定模型、工作目录、系统配置和 session，Prompt 只在尾部追加；同时报告首轮、逐热轮、热轮聚合和含首轮聚合。换模型、修改权限、压缩上下文或重载扩展后应重新建立基线。真实网关的缓存命中存在成本且受服务状态影响，不应放入每次 CI；CI 只验证指标公式、协议映射和指纹变化。
