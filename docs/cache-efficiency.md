# Prompt 缓存效率

Hobot Code 直接使用模型网关返回的 usage 统计 Prompt 缓存，不使用字符数或本地 token 估算替代。`/cache` 在当前进程内展示累计与最近一轮命中率，并诊断模型、系统 Prompt 和工具契约是否保持稳定。

## 指标口径

```text
totalInput = input + cacheRead + cacheWrite
hitRate   = cacheRead / totalInput
```

`input` 表示未命中的输入 token，`cacheRead` 表示从缓存读取的输入 token，`cacheWrite` 表示本轮写入缓存的输入 token。输出 token 不进入分母。

首次请求用于建立前缀，通常是冷请求；热轮结果只统计后续连续请求。聚合结果按 token 总量计算，不对单轮百分比做算术平均。

## 实机结果

测量环境：

| 项目 | 配置 |
|---|---|
| 板卡 | RDK S100 |
| 网关 | D-Robotics |
| 日期 | 2026-08-12 至 2026-08-13，Asia/Shanghai |
| 会话 | 同一 Hobot Code 进程连续追加消息 |
| 数据来源 | Assistant 完成事件中的网关 usage |
| 前缀检查 | 模型、系统 Prompt、工具契约 SHA-256 指纹 |

### 产品路径基线

日常基线加载完整 RDK 扩展并保留稳定的真实工具契约，不额外注入长文本。GLM 5.2 与 DeepSeek V4 Flash 使用 `read`、`grep`、`find`、`ls` 四个只读工具；Kimi K3 使用默认产品工具契约。请求只在会话尾部追加，模型、工作目录、权限和工具顺序保持不变。

| 模型 | 热轮 | cacheRead / totalInput | 热轮命中率 | 单轮范围 |
|---|---:|---:|---:|---:|
| Kimi K3 | 14 | 49,408 / 52,238 | **94.58%** | 91.05%-97.41% |
| GLM 5.2 | 5 | 7,680 / 8,350 | **91.98%** | 88.33%-95.40% |
| DeepSeek V4 Flash | 5 | 6,912 / 8,985 | **76.93%** | 57.53%-97.87% |

GLM 5.2 的独立第二组六轮会话全部返回 cache-read usage，单轮命中率为 90.78%-96.66%，验证了结果的可重复性。DeepSeek V4 Flash 的命中比例随短会话前缀增长而提升，第六轮达到 97.87%。

### 稳定长前缀

稳定前缀测试使用每组唯一的固定文本，保持同一模型与系统契约，只在末尾追加短消息。它用于测量协议和网关在理想稳定前缀下的能力上限，不代表日常会话的固定比例。

| 模型 | 稳定前缀规模 | 热轮 | cacheRead / totalInput | 热轮命中率 |
|---|---:|---:|---:|---:|
| Kimi K3 | 约 50K token | 2 | 101,376 / 101,926 | **99.46%** |
| GLM 5.2 | 约 33K token | 2 | 67,072 / 67,233 | **99.76%** |
| DeepSeek V4 Flash | 约 25K token | 1 | 24,576 / 24,830 | **98.98%** |

Kimi K3 和 GLM 5.2 在 D-Robotics Anthropic-compatible 路径上均达到 99%+；DeepSeek V4 Flash 在 OpenAI-compatible 路径上达到 98.98%。DeepSeek 的独立五轮稳定性验证中有四轮返回约 98.93% 命中、一轮未命中，因此该模型仍应按网关实际 usage 观察，而不能把单轮结果当作保证。

## 产品行为

- `/cache` 展示请求数、累计与最近一轮命中率、未缓存输入、缓存读取和缓存写入。
- 相邻请求的模型、系统 Prompt 或有序工具契约发生变化时，前缀稳定性会明确标记变化。
- 指标状态只保存 SHA-256 指纹和 token 计数，不保存 Prompt、工具说明、会话正文或凭据。
- 当前进程累计值覆盖完整请求历史；最近明细最多保留 32 条，避免长会话无界增长。
- Kimi K3、Qwen 3.8 Max 和 GLM 5.2 使用 Anthropic-compatible 路径；DeepSeek V4 Flash 和 Pro 使用 OpenAI-compatible 路径。两种路径统一映射为 `input`、`cacheRead` 和 `cacheWrite`。

## 适用边界

缓存由模型网关负责，命中率会受公共前缀、账号缓存、路由实例、缓存淘汰和模型版本影响。以下操作会自然降低下一轮命中率：

- 切换模型或 Provider；
- 修改系统 Prompt、权限或工具集合；
- 动态记忆、目标或质量门改变系统上下文；
- 自动压缩改写较早历史；
- 新会话前缀尚未建立或上游缓存已淘汰。

部分兼容网关不会返回 cache-read 字段。此时 Hobot Code 显示 `0%` 只表示没有收到可计量的缓存 usage，不能证明上游没有内部缓存。实机结果是指定环境与时间下的可复现基线，不是模型或网关的服务等级承诺。

## 参考资料

- [DeepSeek Context Caching](https://api-docs.deepseek.com/guides/kv_cache)
- [DeepSeek Anthropic API](https://api-docs.deepseek.com/guides/anthropic_api)
- [高缓存命中率实践案例](https://mp.weixin.qq.com/s/Nmfg5eF6rC7HY3e-zT3CFg)
