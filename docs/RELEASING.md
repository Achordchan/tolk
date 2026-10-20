# 发布流程

分两件事：**Tolk 自己的发布**（以后每个版本），和**桥接**（一次性，让老用户升级到 Tolk）。

## 为什么要分开

老版本的程序里写死了旧的公钥，只认旧私钥的签名；旧私钥只在旧仓库（`dazuo-win`、`bagayalu-translate`）自己的 Secrets 里，Tolk 仓库读不到，也不应该读。所以：

- Tolk 自己的发布用 **Tolk 自己的新密钥**签名（`tolk-release keygen` 生成，私钥放 Tolk 仓库的 Secrets）；
- 桥接由旧仓库里的 `tolk-bridge` 工作流完成：它取 Tolk 的构建，用旧仓库自己的 Secrets 重新签名并发布成旧仓库的 Release。

## 一、Tolk 自己的发布

在 Tolk 仓库 Actions 里手动运行 **Release**，填版本号。它会：

1. 校验 `version.go`、`mygo.config.ts`、`CHANGELOG.md` 三处版本一致，建草稿 Release；
2. Windows：测试（含真 PowerShell 的更新替换脚本）→ 构建 → 更新包签名 → Inno Setup 安装器；
3. macOS：构建 Swift 小助手和主程序 → 用固定的 Apple Development 身份签名 → 校验团队 ID 和指定要求 → 启动自检 → zip / dmg / Sparkle appcast；
4. 发布 Release，镜像到 Gitee。

### Tolk 仓库需要的 Secrets

| 名称 | 内容 |
|---|---|
| `TOLK_UPDATE_PRIVATE_KEY_PEM` | Tolk 的 Windows 更新私钥（`keygen` 生成的 `tolk-windows-update-private.pem` 全文） |
| `TOLK_SPARKLE_PRIVATE_KEY` | Tolk 的 Mac 更新私钥（`keygen` 生成的 `tolk-sparkle-private.txt` 里那一行） |
| `MACOS_DEV_CERT_P12_BASE64` / `MACOS_DEV_CERT_PASSWORD` | 与 bagayalu 相同的 Apple Development 证书 p12（base64）及密码。身份变了，老用户的授权会失效，所以工作流会拒绝发布 |
| `GITEE_TOKEN` | Gitee 私人令牌（projects 权限），只在勾选 gitee 时需要 |

### 生成 Tolk 的更新密钥

```sh
go run ./cmd/tolk-release keygen -out ~/tolk-keys
```

私钥写进 `~/tolk-keys`（权限 600，不会打印），屏幕上只显示两把**公钥**。把公钥填进 `internal/update/sig.go` 的 `WindowsPublicKeyB64` / `SparklePublicKeyB64`（现在它们暂时等于旧公钥），私钥存进上表的两个 Secret 后删掉本地文件。**公钥必须在第一次公开发布之前换好**，否则已经装出去的版本只认旧钥。

## 二、桥接（一次性）

两个旧仓库各有一个 `tolk-bridge` 工作流（分别在 `dazuo-win` 和 `bagayalu-translate` 的 PR 里，合并到 `main` 后才能手动运行）：

- `dazuo-win`：取 Tolk 的 Windows 更新包，用它自己的 `DZFYQ_UPDATE_PRIVATE_KEY_PEM` 签名，发布成 GitHub Release，可选镜像到 Gitee `dazuofanyiguan`（旧客户端 ≤1.2.10 只读 Gitee，需要该仓库的 `GITEE_TOKEN`）；
- `bagayalu-translate`：取 Tolk 的 Mac 应用，用它自己的证书和 `SPARKLE_PRIVATE_KEY` 重新签名，校验身份要求不变，生成 appcast，发布成最新 Release。

顺序：先在 Tolk 仓库发布 `v<版本>`，再到两个旧仓库分别运行 `tolk-bridge` 填同一个版本号。Tolk 仓库是私有时，旧仓库需要一个只读令牌 `TOLK_READ_TOKEN`（Tolk 公开后不需要）。

## 首次发布前

1. `Achordchan/tolk` 目前是私有仓库；Tolk 自己的自动更新要匿名下载，正式发布前需要改成公开。
2. 先自己装一遍（Windows 安装器、Mac dmg），确认没问题再运行桥接。
3. 版本号：大佐翻译官最后一版 1.2.x，bagayalu 构建号 141；Tolk 2.0.0 的构建号是 20000，远大于旧版。
