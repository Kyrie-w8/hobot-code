# Hobot Code 发布流程

Hobot Code 以 GitHub Release 作为公开发行源。`VERSION`、`CHANGELOG.md` 的首个版本标题和 `pi-runtime/package.json` 必须一致；发行工作流还要求 Git tag 精确等于 `v<VERSION>`。

## 发布前检查

在干净工作区执行：

```bash
make check
make release
make studio-macos-release  # 仅在 macOS ARM64 构建机上
```

`make release` 先以 `CGO_ENABLED=0` 从当前源码交叉编译 Go `agentd`，再生成版本化 Linux ARM64 归档及 SHA256 文件。包校验器要求 daemon 是小端 ARM64 ELF。归档包含 `BUILD_INFO.json` 和覆盖包内普通文件的 `MANIFEST.sha256`，并拒绝符号链接、特殊文件、路径逃逸和未登记内容。`make studio-macos-release` 可在本地生成开发用 DMG；GitHub 的 `production` 发布环境强制使用 Developer ID Application 签名、hardened runtime 和可信时间戳，依次公证并 staple App 与 DMG，最后执行 Gatekeeper 校验。缺少任一凭据时正式发布直接失败。

`BUILD_INFO.json` schema 3 记录源码提交、清洁状态、构建时间、目标架构、Pi 来源、Pi 能力兼容契约 SHA-256 和实际 `agentd` SHA-256。包校验和板端运行时都会拒绝元数据与二进制、锁文件或契约不一致的构建；产品版本相同不再被当作构建相同的充分证据。

`make pi-check` 会先验证 `pi-runtime/compatibility.json` 中每项 Pi 依赖都有 Hobot Code 回归测试。打包后再对实际解压的 Pi 文档契约执行第二次校验。两者仍不能替代 X5、S100 和 S600 的 `hobot.pi-board-compatibility/v1` 实机场景报告，完整边界见 [Pi 上游兼容契约](pi-compatibility.md)。

候选包解压后，在三块板上分别以实际部署普通用户执行 `python3 ./verify-model-egress-runtime.py --package-root . --output ../model-egress-acceptance.json --rpc-output ../rpc-background-acceptance.json --session-output ../session-recovery-acceptance.json --extension-output ../extension-safety-acceptance.json --tui-output ../tui-basics-acceptance.json --readiness-output ../readiness-diagnostics-acceptance.json`，不能使用 root。报告必须写在候选包外，避免改变待验证目录。该命令分别生成 `providers.models` 的 `model-egress-runtime` 报告，后台任务、审批、第二轮、图片、重连和 Side Agent 的 `rpc-background` 报告，上下文压缩、中断恢复和历史编辑分支的 `session-recovery` 报告，包内资源、扩展并行工具、权限 Hook 和工作区写租约的 `extension-safety` 报告，真实 PTY 下中文输入、thinking、编辑、脱离与重新连接的 `tui-basics` 报告，以及只读诊断、显式确认、受限权限修复与隐私边界的 `readiness-diagnostics` 报告；所有报告必须与候选包的 `manifestSha256`、`agentdSha256` 和 `piCompatibilitySha256` 一致。

同一候选包还必须在三块板上分别完成 root 级安装生命周期验收。该测试只写入临时隔离树，不替换当前安装，不读取实际用户任务数据，也不自动安装系统依赖：

```bash
sudo python3 ./verify-install-lifecycle.py \
  --package-root . \
  --output ../install-lifecycle-acceptance.json \
  --user <existing-non-root-user>
```

它验证首次安装、普通用户启动、升级保留数据、换入后故障恢复、回滚和卸载保留数据，并要求真实系统安装路径的测试前后指纹完全一致。`install-lifecycle` 是 Hobot Code 产品发布场景，不声明成 Pi 上游 capability；仍使用同一严格单板报告与三板矩阵格式。

如果板端 `/tmp` 仅允许 root 进入，应先创建归属该普通用户、权限为 `0700` 的私有临时目录，并为整个命令设置 `TMPDIR`。不得通过改宽 `/tmp` 权限或改用 root 绕过普通用户验收。

每个场景的三板报告应放入独立私有目录，并先用 `make board-acceptance-check REPORT_DIR=<dir> SCENARIO=<scenario>` 验证子矩阵。正式发布再将全部场景报告放入一个权限为 `0700` 的私有目录，生成规定名称的脱敏聚合矩阵：

```bash
make board-acceptance-check \
  REPORT_DIR=<private-report-dir> \
  REPORT=hobot-code-<version>-board-acceptance.json
```

只有完整矩阵返回 `pass` 和退出码 `0` 才满足 Pi 板端兼容门槛。单板报告始终留在维护者私有存储中，不能上传到 GitHub。聚合矩阵经过严格字段白名单，只保留板型、RDK OS、时间、构建指纹和结果；它不包含地址、主机名、Prompt、命令、模型输出、路径或凭据，是唯一允许上传的板端验收文件。

macOS 发布环境需要配置以下 GitHub Secrets：

- `MACOS_CERTIFICATE_BASE64`：Developer ID Application 证书及私钥导出的 `.p12` 的 Base64。
- `MACOS_CERTIFICATE_PASSWORD`：上述 `.p12` 的导出密码。
- `MACOS_SIGNING_IDENTITY`：完整签名身份，例如 `Developer ID Application: Example Corp (TEAMID)`。
- `APPLE_NOTARY_KEY_BASE64`：App Store Connect API 私钥 `.p8` 的 Base64。
- `APPLE_NOTARY_KEY_ID` 与 `APPLE_NOTARY_ISSUER_ID`：对应 API Key ID 和 Issuer ID。

仓库的 `production` environment 必须启用 required reviewers，限制仅受保护 tag 可部署。Reviewer 在晋级时要确认验收矩阵来自目标 Draft 的精确 ARM64 归档，且私有单板报告未上传。证书与公证私钥只写入 runner 临时目录和临时 keychain，构建结束后无论成功或失败都会删除。

X5、S100、S600 必须验证安装生命周期、启动、模型连接、工具审批、会话恢复与卸载保留数据。涉及 `agentd` 时还要验证任务启动、多轮输入、事件重放、SSH 断线、重新连接与进程回收。无法完成任一板卡或场景时，候选矩阵保持 `incomplete`，不能以本机构建通过或发布说明代替实机结果。

每次候选发行还应运行[三板稳定性验证](board-reliability.md)，保存脱敏的快速基线。重启和 24 小时续测只在专用或已确认空闲的板卡上进行。

Studio 的 `testdata/handshakes` 保留脱敏的当前版、前一 minor 和最低 schema 握手样本。`studio-check` 必须重放这些样本，验证 SDK 解码、Studio 降级语义和用户警告代码；升级时不得只修改版本比较而忽略真实字段契约。

## 创建正式发行

提交版本变更并推送 `main` 后创建 annotated tag：

```bash
version=$(sed -n '1p' VERSION)
git tag -a "v$version" -m "Hobot Code $version"
git push origin "v$version"
```

`.github/workflows/release.yml` 会重新运行完整检查和构建，确认 tag 与源码版本一致，然后创建仅维护者可见的 Draft Release：

- `hobot-code-<version>-linux-arm64.tar.gz`
- `hobot-code-<version>-linux-arm64.tar.gz.sha256`
- `hobot-install.sh`
- `hobot-code-version.txt`
- `hobot-code-<version>-macos-arm64.dmg`
- `hobot-code-<version>-macos-arm64.dmg.sha256`

工作流在 Ubuntu 和 macOS ARM64 独立构建，再由 Draft job 聚合不可变产物。GitHub OIDC 为板端归档、桌面 DMG、安装脚本和版本文件生成 build provenance attestation。该工作流没有公开 Release 的代码路径；构建成功也不能绕过实机验收。

下载 Draft 中的精确 ARM64 归档，在 X5、S100、S600 上完成全部场景后，把脱敏聚合矩阵上传到同一个 Draft：

```bash
version=$(sed -n '1p' VERSION)
gh release upload "v$version" "hobot-code-$version-board-acceptance.json"
gh workflow run promote-release.yml -f version="$version"
```

受 `production` environment reviewer 保护的 **Promote Release** 工作流是唯一允许公开 Draft 的入口。它只允许从默认分支运行受审校验代码，解析精确 annotated tag 的提交身份，不执行候选 tag 中的脚本；同时要求 Release 仍为 Draft，验证固定资产清单、SHA256 和原构建 provenance，安全解压并完整校验 ARM64 包，然后要求：

- `BUILD_INFO.json` 来自该 tag 的精确提交，且 `dirty=false`；
- 聚合矩阵覆盖契约声明的全部场景，以及 X5、S100、S600；
- 矩阵中的 `agentd`、包清单和 Pi 契约哈希与下载归档逐项相同；
- 每条实机证据都在候选构建后产生、没有未来时间，并且不超过七天；允许的时钟偏差最多五分钟。

晋级流程会生成 `hobot-code-<version>-release-evidence.json`，把归档、构建身份、Pi 契约和矩阵摘要确定性绑定，并为该证据生成 GitHub provenance attestation。只有最终八项资产精确匹配后才公开 Release。失败时保留 Draft，修复源码后发布新版本；不要替换已经公开或已经验收的同版本产物。

维护者也可以在本地运行与晋级流程相同的核心校验。`EVIDENCE` 必须是尚不存在的新文件；本地产物只用于预检，不代替工作流生成并签名的公开证据：

```bash
make release-candidate-check \
  PACKAGE_ROOT=<extracted-package> \
  ARCHIVE=hobot-code-<version>-linux-arm64.tar.gz \
  MATRIX=hobot-code-<version>-board-acceptance.json \
  EXPECTED_COMMIT=$(git rev-parse "v<version>^{commit}") \
  EVIDENCE=release-evidence.preflight.json
```

发布完成后还应下载 DMG 并重新执行 `xcrun stapler validate` 和 `spctl --assess --type open --context context:primary-signature`。未经 Developer ID 签名和 Apple 公证的本地开发 DMG 不得上传为公开 Release。

## 验证发行来源

下载归档后可以使用 GitHub CLI 验证工作流身份签发的 provenance：

```bash
gh attestation verify hobot-code-<version>-linux-arm64.tar.gz \
  --repo bryant-w/hobot-code
```

板端一键安装器还会通过 HTTPS 下载归档及同版本 SHA256，要求校验文件只包含目标归档的一条记录，并在解压前检查所有归档路径和文件类型。

## 版本策略

遵循 SemVer：修复使用 patch，向后兼容的新能力使用 minor，不兼容的配置、协议或命令变化使用 major。预发布版本使用 `-beta.N` 或 `-rc.N`；GitHub 的 latest 入口不会选择 prerelease，因此一键安装默认只取得最新稳定版本，预发布版必须显式指定：

```bash
curl -fsSL https://github.com/bryant-w/hobot-code/releases/latest/download/hobot-install.sh \
  | sh -s -- --version <prerelease-version>
```
