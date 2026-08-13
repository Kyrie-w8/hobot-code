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

`BUILD_INFO.json` schema 2 记录源码提交、清洁状态、构建时间、目标架构、Pi 来源和实际 `agentd` SHA-256。包校验和板端运行时都会拒绝元数据与二进制不一致的构建；产品版本相同不再被当作构建相同的充分证据。

macOS 发布环境需要配置以下 GitHub Secrets：

- `MACOS_CERTIFICATE_BASE64`：Developer ID Application 证书及私钥导出的 `.p12` 的 Base64。
- `MACOS_CERTIFICATE_PASSWORD`：上述 `.p12` 的导出密码。
- `MACOS_SIGNING_IDENTITY`：完整签名身份，例如 `Developer ID Application: Example Corp (TEAMID)`。
- `APPLE_NOTARY_KEY_BASE64`：App Store Connect API 私钥 `.p8` 的 Base64。
- `APPLE_NOTARY_KEY_ID` 与 `APPLE_NOTARY_ISSUER_ID`：对应 API Key ID 和 Issuer ID。

仓库的 `production` environment 应启用 required reviewers，限制仅受保护 tag 可部署。证书与公证私钥只写入 runner 临时目录和临时 keychain，构建结束后无论成功或失败都会删除。

至少在 X5、S100、S600 中受影响的板卡上验证安装、启动、模型连接、工具审批、会话恢复与卸载保留数据。涉及 `agentd` 时还要验证任务启动、多轮输入、事件重放、SSH 断线、重新连接与进程回收。无法完成的板卡验证必须写入发布说明，不能以本机构建通过代替实机结果。

每次候选发行还应运行[三板稳定性验证](board-reliability.md)，保存脱敏的快速基线。重启和 24 小时续测只在专用或已确认空闲的板卡上进行。

Studio 的 `testdata/handshakes` 保留脱敏的当前版、前一 minor 和最低 schema 握手样本。`studio-check` 必须重放这些样本，验证 SDK 解码、Studio 降级语义和用户警告代码；升级时不得只修改版本比较而忽略真实字段契约。

## 创建正式发行

提交版本变更并推送 `main` 后创建 annotated tag：

```bash
version=$(sed -n '1p' VERSION)
git tag -a "v$version" -m "Hobot Code $version"
git push origin "v$version"
```

`.github/workflows/release.yml` 会重新运行完整检查和构建，确认 tag 与源码版本一致，然后发布：

- `hobot-code-<version>-linux-arm64.tar.gz`
- `hobot-code-<version>-linux-arm64.tar.gz.sha256`
- `hobot-install.sh`
- `hobot-code-version.txt`
- `hobot-code-<version>-macos-arm64.dmg`
- `hobot-code-<version>-macos-arm64.dmg.sha256`

工作流在 Ubuntu 和 macOS ARM64 独立构建，再由发布 job 聚合不可变产物。GitHub OIDC 为板端归档、桌面 DMG、安装脚本和版本文件生成 build provenance attestation。发布失败时修复源码并发布新版本；不要替换已被用户下载的同版本产物。

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
