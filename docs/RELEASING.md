# 发布流程

一次发布 = 在 GitHub Actions 里手动运行 **Release** 工作流，填版本号。它会：

1. 校验 `version.go`、`mygo.config.ts`、`CHANGELOG.md` 三处版本一致，并建一个草稿 Release；
2. Windows 任务：测试（含真 PowerShell 的更新替换脚本）→ 构建 → 旧格式 Ed25519 签名的更新包 → Inno Setup 安装器；
3. macOS 任务：构建 Swift 小助手和主程序 → 用固定的 Apple Development 身份签名 → 校验团队 ID 和指定要求 → 启动自检 → zip / dmg / Sparkle appcast；
4. 发布任务：发布 Release、镜像到 Gitee，选了「bridge」则向 `dazuo-win` 和 `bagayalu-translate` 各发一个桥接版本。

## 需要的 Secrets（仓库 Settings → Secrets and variables → Actions）

| 名称 | 内容 | 用途 |
|---|---|---|
| `DZFYQ_UPDATE_PRIVATE_KEY_PEM` | 大佐翻译官一直在用的 Ed25519 更新私钥（PEM） | Windows 更新包签名，必须与应用内置公钥匹配，否则工作流直接失败 |
| `SPARKLE_PRIVATE_KEY` | bagayalu 的 Sparkle 私钥（`generate_keys -x` 导出的内容） | appcast 签名，同样必须与应用内置的 `SUPublicEDKey` 匹配 |
| `MACOS_DEV_CERT_P12_BASE64` / `MACOS_DEV_CERT_PASSWORD` | 与 bagayalu 相同的 Apple Development 证书 p12（base64）及密码 | Mac 签名；身份变了，老用户的辅助功能 / 屏幕录制授权会失效，所以工作流会拒绝发布 |
| `GITEE_TOKEN` | Gitee 私人令牌（projects 权限） | 镜像到 Gitee（旧版 Windows 客户端只读 Gitee） |
| `LEGACY_RELEASE_TOKEN` | 对 `Achordchan/dazuo-win` 和 `Achordchan/bagayalu-translate` 有 `contents:write` 的令牌 | 桥接版本 |

私钥只放在 Secrets 里，工作流不会把它们写进仓库或日志。

## 桥接版本是什么

老用户的程序只会去老地方找更新：

- Windows（大佐翻译官）读 `dazuo-win` 的 GitHub Release，失败再读 Gitee 的 `dazuofanyiguan`；
- macOS（bagayalu）的 Sparkle 读 `bagayalu-translate` 最新 Release 里的 `appcast.xml`。

桥接版本就是把 Tolk 的构建按旧格式发布到这两个地方，老用户点「检查更新」就直接变成 Tolk，设置和授权都保留。之后 Tolk 自己从 `Achordchan/tolk`（GitHub，回退 Gitee）检查更新。

## 首次发布前

1. `Achordchan/tolk` 目前是私有仓库；自动更新要匿名下载，正式发布前需要改成公开（或把更新源换成别处）。
2. 先不勾选 bridge 发一个版本，自己在 Windows 和 Mac 上装一遍；确认没问题再勾选 bridge 发布给老用户。
3. 旧版本号：大佐翻译官最后一版 1.2.x，bagayalu 构建号 141；Tolk 2.0.0 的构建号是 20000，远大于旧版。
