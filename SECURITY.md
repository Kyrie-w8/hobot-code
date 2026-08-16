# Hobot Code 安全说明

Hobot Code 可以读取项目、执行命令并调用板端设备。前台 TUI 应被视为拥有当前 OS 用户权限的开发工具；agentd 后台任务默认增加 Linux 进程级隔离，但仍不是用于运行任意不可信代码的完整虚拟机边界。

## 威胁模型

Hobot Code 的安全控制用于降低误操作和模型生成危险调用的概率，不能防御已经取得当前用户权限的恶意扩展、Skill、Hook、语言服务器或 Shell 命令。

以下内容属于信任边界：

- 当前用户选择的模型服务，以及发送给该服务的系统 Prompt、对话和工具结果。
- 用户安装或项目加载的 Pi 扩展、packages、Skills、Prompt 和 themes。
- 全局 Hook 与用户配置的语言服务器命令。
- 用户确认执行的 Shell 命令和设备操作。

只在可信项目中启用项目资源，并在安装第三方代码前审查来源。

## 内置防护

- 工具权限支持 `allow`、`ask` 和 `deny`，并在工具暴露和调用阶段进行检查。
- 内置 `write`、`edit` 禁止修改 `/boot`、`/dev`、`/etc`、`/proc`、`/sys`、`/usr` 和 `/var/lib`。
- 内置工具写入工作区外，以及识别出的破坏性 Shell 和外联客户端，要求交互确认；虚拟 `network` 权限可设为 `allow`、`ask` 或 `deny`。
- root 默认逐次确认 `bash`、`write`、`edit`；显式切换到 `policy` 后普通操作遵守 allow/ask/deny，破坏性命令、工作区外写入和关键系统路径仍受硬边界保护。
- 工具确认、Hook 审计和记忆存储会尽力识别或隐藏常见 token、私钥与 secret。
- Provider 响应、Hook 输出、LSP 进程和侧边 Agent 并发均有资源上限。

前台 TUI 和后台 worker 的整个进程树默认由 bubblewrap 约束，而不只约束 Shell：宿主根文件系统与任务控制目录只读，Linux capabilities 被丢弃。`review` 不开放工作区写入，`workspace` 仅增加当前工作区写入，`system` 再开放 BPU、ION/Hbmem、DMA heap、video 和 media 设备，`off` 明确关闭隔离。`shared` 下的 `network` 规则只能启发式识别常见客户端；`model-only` 使用独立 Linux 网络命名空间，仅允许内置 D-Robotics Provider 及配置了凭据的 Hobot 受管 Anthropic Messages、OpenAI Chat Completions、OpenAI Responses Provider，通过私有 Unix Socket 访问 agentd 固定的模型网关；`offline` 连模型 Socket 也不可见。Google Generative AI、Pi 登录和自管模型仍需 `shared`。`model-only` 限制任意互联网出口，但能访问 Socket 的 Agent 进程仍可把上下文发送给模型服务，也不是同用户宿主进程之间的隔离。只读宿主文件和其他同用户数据仍可能被读取。高价值环境仍应使用独立账号、文件权限、容器、网络出口控制或系统级强制访问控制。

## 密钥与数据

模型凭据默认保存在 `~/.config/hobot-code/hobot.env`。启动器只接受非符号链接、属于当前用户且未向组或其他用户开放的文件；不满足条件时拒绝启动。内容按纯 `KEY=VALUE` 数据解析，不执行变量替换、命令替换或其他 Shell 语法，并拒绝危险的进程注入变量。配置根、状态根和会话目录会在运行入口统一收紧为 `0700`。

Capabilities 清单是库存视图，不是隔离边界。它只扫描 Pi 的固定用户目录，以及由已信任任务绑定的项目 `.pi`/`.agents` 目录；扫描有所有权、符号链接、写权限、深度和数量限制，且不会返回配置中的端点、命令、绝对路径或 package URL。`declared`/`discovered` 不表示代码已审查或已加载。第三方 extension、package、Skill、Prompt 和 theme 一旦由 Pi 加载，仍可影响模型上下文或以当前用户权限执行；root 用户必须特别谨慎。

OpenExplorer LLM Skill Pack 是用户提供的外部第三方内容。Hobot Code 对其执行有界只读结构检查，并只自动加载客户目录登记项，但不会由此证明 Skill 指令、脚本、依赖、许可证或模型结果可信。主机侧步骤使用 S600 自己的 OpenSSH 配置直连用户选择的构建机；私钥不会传给模型或 Studio。`openexplorer_remote_run` 在每次执行前验证目标为 x86_64，CUDA 工作流还检查 `nvidia-smi`，但远端命令仍拥有该 SSH 用户的权限并可能修改构建机数据，因此保持逐次审批。建议使用专用账号、专用密钥、`authorized_keys restrict`、独立工作目录和最小文件权限。

内置 D-Robotics token 和 `HOBOT_CODE_PROVIDER_KEY_*` 受管 Provider 密钥启动后都会从长期进程环境移除：`shared` 下非沙箱进程之间使用版本化匿名 FD，bubblewrap 内使用读取后删除的 tmpfs 一次性文件；`model-only` 下所有受支持 Provider 密钥只留在 agentd，worker 不获得任何模型密钥。后台 worker 使用任务私有的 Pi 配置快照处理设置锁，全局配置保持只读；`auth.json` 只允许进入 `shared` 快照，并会从 `model-only` 与 `offline` 快照中删除。模型代理在 daemon 启动时冻结 Provider、模型和协议路由白名单，固定 origin、路径、方法与认证头，禁止重定向，限制并流式转发数据，日志不记录请求正文、回复正文、模型名或凭据。Provider 或密钥配置变化后必须重启 daemon，配置指纹不一致时模型相关操作会失败关闭。`hobot provider add` 和 `rotate` 从控制终端隐藏读取密钥，Studio 只通过固定短时 SSH 标准输入传递。该边界不能防御同用户宿主进程、管理员或调试器；任何能访问私有 Socket 的同用户进程都能消耗已配置 Provider 的额度。Pi 登录、自管 `models.json` 和 Google Generative AI 尚未接入模型代理，凭据与网络必须单独审计。`hobot.env` 不要提交到版本控制。

脱敏和敏感数据检测是纵深防御，不保证识别所有密钥格式。不要在 Prompt、会话、持久记忆、Hook 输出或 issue 中放入不必要的凭据。怀疑凭据泄漏时，应立即在对应模型服务撤销并轮换，而不是只删除本地记录。

Pi 会话、Hobot Code 记忆、目标和审计默认保存在当前用户目录。模型请求会把完成任务所需的上下文发送给所选 Provider；使用外部 Provider 前应确认其数据处理政策符合你的要求。

## 侧边 Agent 边界

`/btw` 使用独立进程和临时会话。关闭后，其对话与临时 Prompt 不写回主会话并会被删除，但这不是系统隔离：侧边 Agent 与主 Agent 共享 OS 用户、工作区、环境、进程命名空间、服务和设备视图。它已经写入的文件、启动的进程或执行的硬件操作会保留。

侧边 Agent 禁止写入 Hobot Code 持久记忆和修改持久目标状态；该限制不阻止它通过获准的其他工具产生外部副作用。

## 后台任务边界

`agentd` 按 OS 用户运行，只监听权限为 `0600` 的本机 Unix socket，所在目录权限为 `0700`；Linux 发行版还核对连接方 UID。worker 的 bubblewrap 边界不能替代跨用户隔离：已经取得同一宿主用户权限的其他进程仍可以访问该用户的任务状态，也可以直接绕过 daemon 运行程序。

后台 worker 继续使用与 TUI 相同的板端权限判定。客户端断线不会终止任务；daemon 停止、崩溃或板卡重启后，活动任务标记为 `interrupted`，不会自动重放 Prompt、审批或工具调用，避免重复产生外部副作用。任务事件、元数据和诊断日志具有权限与大小上限，但其中仍可能包含用户 Prompt、模型回复、工具参数或输出，应按会话数据保护。

`hobot diagnose` 生成的支持文件采用独立的最小化数据模型，不复制上述任务事件与原始日志。它排除对话、Prompt、工具输入输出、环境、凭据和项目内容，并替换主机名、路径、任务 ID 与原始错误；恢复建议来自固定分类规则，不拼接原始故障文本。板端和 Studio 写出的文件均为当前用户私有。Studio 使用的 SDK 会对 schema、枚举、文本边界、文件名、绝对路径、大小、SHA-256、内嵌内容和 manifest 一致性执行失败关闭校验，不能把畸形板端响应保存为支持文件。该边界由自动化泄漏回归测试覆盖，但任何自动脱敏都不能替代分享前的人工检查和组织数据政策。

模型资格证据保存在私有且有大小上限的 `agentd/model-qualification.json` 中，只记录规范化后的检查状态、耗时、官方资料 URL 和版本/资源摘要，不记录凭据、Endpoint、Prompt、思考内容、原始模型回复或原始工具输出。文件会校验所有者、权限、普通文件类型和结构，配置、构建、Pi、板卡或 RDK 资源变化后旧证据不会继续被当作当前结论。它仍属于本机状态，不应替代发布构建、测试环境和公开资格报告的独立签名与审计。

## 机器人系统边界

`system_snapshot` 和知识检索用于辅助诊断，不构成部署验收或安全认证。节点存在、工具可用或温度可读，都不能单独证明模型转换正确、BPU 输出正确或整机满足实时性要求。

Hobot Code 不应进入电机、CAN、GPIO、安全或急停的硬实时闭环。影响人员、机械设备或不可恢复数据的动作必须由独立的限位、急停、权限隔离和人工确认保护。

## 更新与回滚

安全修复以最新稳定 Release 为准。发布新包后应尽快使用 `hobot update` 升级，并在部署前完成板端验证。回滚要求 root 权限和完整的前一版本备份；首次安装通常没有可用回滚点。

一键安装器只通过 HTTPS 获取 GitHub Release，要求 SHA256 文件恰好包含目标归档的一条记录，并在解压前拒绝路径逃逸、非规范路径、链接与特殊文件。发布工作流还为公开产物生成 GitHub build provenance attestation；验证方法见[发布流程](docs/releasing.md#验证发行来源)。

Pi、`fd` 与 `ripgrep` 发行产物按锁文件中的 SHA256 校验；`agentd` 从对应 tag 的仓库源码交叉编译并由包校验器确认目标架构。完整性校验和来源证明不能替代对上游代码、许可证或供应链的评估。

## 报告安全问题

请不要在公开 issue 中披露尚未修复的漏洞、有效凭据或可直接复现的设备破坏步骤。优先使用 GitHub 的[私有安全漏洞报告](https://github.com/bryant-w/hobot-code/security/advisories/new)；若仓库尚未启用该入口，只创建不含漏洞细节的公开 issue，请求维护者提供私下沟通方式。报告应包含：

- 受影响版本或提交。
- 板卡型号与 RDK OS 版本。
- 影响、前置条件和最小复现步骤。
- 已知缓解措施，以及是否包含敏感数据。

项目目前不承诺固定响应时限。若报告中包含真实 token，请先撤销并轮换，再提供脱敏信息。
