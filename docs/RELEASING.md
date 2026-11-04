# 发布流程

## 发布一个版本

在 Tolk 仓库 Actions 里手动运行 **Release**，填版本号。它会：

1. 校验 `version.go`、`mygo.config.ts`、`CHANGELOG.md` 三处版本一致，建草稿 Release；
2. Windows：测试（含真 PowerShell 的更新替换脚本）→ 构建 → 更新包 `Tolk-update-windows_<版本>.zip` 及签名 → Inno Setup 安装器；
3. macOS：构建 Swift 小助手和主程序 → 用固定的 Apple Development 身份签名 → 校验团队 ID 和指定要求 → 启动自检 → zip / dmg / Sparkle appcast；
4. 写 `latest.json`（客户端检查更新读它），发布 Release。

只发布到 GitHub。国内网络慢，客户端会同时测试 GitHub 直连和几个公共 GitHub 中转（`internal/update/release.go` 的 `DefaultMirrors`），挑最快的下载；下来的东西一律用内置公钥验签，中转改不了内容。检查更新读的是 `latest.json` 而不是 GitHub API，因为中转只代理下载、不代理 API。

客户端发现新版本后在后台下载、验签，好了再提示「重启更新」；不点的话，在用户退出 Tolk 时安装。

### 需要的 Secrets

| 名称 | 内容 |
|---|---|
| `TOLK_UPDATE_PRIVATE_KEY_PEM` | Windows 更新私钥（`keygen` 生成的 `tolk-windows-update-private.pem` 全文） |
| `TOLK_SPARKLE_PRIVATE_KEY` | Mac 更新私钥（`keygen` 生成的 `tolk-sparkle-private.txt` 里那一行） |
| `MACOS_DEV_CERT_P12_BASE64` / `MACOS_DEV_CERT_PASSWORD` | Apple Development 证书 p12（base64）及密码。身份变了，用户的系统授权会失效、自动更新也会拒绝，所以工作流会拒绝发布 |

### 更新密钥

公钥写在 `internal/update/sig.go` 的 `WindowsPublicKeyB64` / `SparklePublicKeyB64`，私钥在上表两个 Secret 里。要重新生成（比如私钥泄露）：

```sh
go run ./cmd/tolk-release keygen -out ~/tolk-keys
```

私钥写进 `~/tolk-keys`（权限 600，不会打印），屏幕上只显示公钥。换公钥后，已经装出去的版本只认旧钥，所以之后还得再发一个用旧钥签名的过渡版本。

## 2.2.3 改名

2.2.3 把底层名字都换成了 Tolk：设置目录 `~/.dzfyq` → `~/.tolk`（启动时自动搬），Windows 主程序 `大佐翻译官.exe` → `Tolk.exe`、安装目录 → `Tolk`（安装器删掉旧目录），更新包名、Mac 包名 `achord.dazuofanyiguan` → `app.tolk.desktop`。2.2.2 及以前的客户端认不出新的更新包，需要手动装一次 2.2.3；Mac 换了包名，要重新授予一次辅助功能和录屏权限。

## Windows supervised entry point

`bun run build -- -platform windows/amd64` produces `Tolk.exe` (launcher) and `Tolk-core.exe` (GUI). Ship both together. Shortcuts and auto-start must target `Tolk.exe`. The release ZIP and Inno Setup installer include both files; the update script waits for the launcher to exit before replacement. Do not distribute MyGo-generated installers directly: they are generated before the launcher split. Windows installer and update E2E must be run before release.
