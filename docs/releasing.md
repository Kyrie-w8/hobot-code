# Hobot Code 发布流程

Hobot Code 以 GitHub Release 作为公开发行源。`VERSION`、`CHANGELOG.md` 的首个版本标题和 `pi-runtime/package.json` 必须一致；发行工作流还要求 Git tag 精确等于 `v<VERSION>`。

## 发布前检查

在干净工作区执行：

```bash
make check
make release
```

`make release` 生成版本化 Linux ARM64 归档及 SHA256 文件。归档包含 `BUILD_INFO.json` 和覆盖包内普通文件的 `MANIFEST.sha256`，并拒绝符号链接、特殊文件、路径逃逸和未登记内容。

至少在 X5、S100、S600 中受影响的板卡上验证安装、启动、模型连接、工具审批、会话恢复与卸载保留数据。无法完成的板卡验证必须写入发布说明，不能以本机构建通过代替实机结果。

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

工作流使用 GitHub OIDC 为归档、安装器和版本文件生成 build provenance attestation。发布失败时修复源码并发布新版本；不要替换已被用户下载的同版本产物。

## 验证发行来源

下载归档后可以使用 GitHub CLI 验证工作流身份签发的 provenance：

```bash
gh attestation verify hobot-code-<version>-linux-arm64.tar.gz \
  --repo Kyrie-w8/hobot-code
```

板端一键安装器还会通过 HTTPS 下载归档及同版本 SHA256，要求校验文件只包含目标归档的一条记录，并在解压前检查所有归档路径和文件类型。

## 版本策略

遵循 SemVer：修复使用 patch，向后兼容的新能力使用 minor，不兼容的配置、协议或命令变化使用 major。预发布版本使用 `-beta.N` 或 `-rc.N`；GitHub 的 latest 入口不会选择 prerelease，因此一键安装默认只取得最新稳定版本，预发布版必须显式指定：

```bash
(
  installer=$(mktemp) &&
  trap 'rm -f "$installer"' EXIT &&
  wget -q -T 20 -t 4 -O "$installer" https://github.com/Kyrie-w8/hobot-code/releases/latest/download/hobot-install.sh &&
  sh "$installer" --version <prerelease-version>
)
```
