# Tolk

Windows 和 macOS 通用的翻译工具，基于 [mygo](https://github.com/egoist/mygo)（Go + 系统 WebView）。

- 引擎：Google、微软、DeepL、AI 翻译（OpenAI 兼容）、Achord 内置引擎；macOS 另有 Apple 本地翻译
- 37 种语言；复制两次呼出翻译；Mini 模式；浅色 / 深色 / 粉色主题
- 输入助手：选中文字，按快捷键，弹出多语言候选，选一个替换回去（Windows 与 macOS 都有）
- macOS：截图翻译、系统级 ⌘C⌘C 监听、权限引导（由 `macos/helper` 里的 Swift 小助手提供）；两端功能差异见 [docs/platform-differences.md](docs/platform-differences.md)
- 设置在 `~/.tolk/config.json`；新版本在后台下载，下好后提示重启，或在退出时安装

## 目录

| 路径 | 内容 |
|---|---|
| `main.go` `service.go` `prefs.go` `settings.go` `app.go` `mini.go` | 主程序与前端可调用的服务 |
| `mac.go` `helper_darwin.go` `helper_stub.go` | macOS 小助手的接入（其他平台为空实现） |
| `ia_windows.go` `internal/winsel` | Windows 输入助手：读选区（UI 自动化 / 模拟复制）、粘贴替换、热键、不抢焦点的候选浮层 |
| `internal/inputassist` | 输入助手的设置和与平台无关的逻辑（应用过滤、语言、候选列表、按键裁决、版面、缓存、翻译编排） |
| `internal/engine` | 翻译引擎与语言表 |
| `internal/legacy` | 配置读写（`~/.tolk`），引擎构建 |
| `internal/update` | 更新器：GitHub 直连与中转测速选线、Windows Ed25519 更新包、macOS Sparkle appcast |
| `internal/helper` `macos/helper` | 小助手的 Go 客户端、Swift 实现与协议（`PROTOCOL.md`） |
| `cmd/tolk-release` | 发布工具：打包、签名、appcast、latest.json |
| `src` `index.html` `mini.html` `panel.html` | 前端（`panel.html` 是 Windows 输入助手的候选面板） |
| `installer/setup.iss` | Windows 安装器 |

## 开发

```sh
bun install
bun run dev          # 热更新
go test ./...
bun run build        # 当前平台
```

macOS 上要带上小助手：

```sh
(cd macos/helper && swift build -c release --arch arm64 --arch x86_64)
mkdir -p resources/darwin-universal
cp "$(cd macos/helper && swift build -c release --arch arm64 --arch x86_64 --show-bin-path)/tolk-helper" resources/darwin-universal/
bun run build -- -platform darwin/universal
```

开发时也可以用环境变量 `TOLK_HELPER=/path/to/tolk-helper` 指定小助手。

发布流程见 [docs/RELEASING.md](docs/RELEASING.md)，更新日志见 [CHANGELOG.md](CHANGELOG.md)。

## 许可证

本项目以 [GNU GPL v3](LICENSE) 发布。内置的 DeepLX 部分沿用其 MIT 许可证，见 [third_party/DeepLX-LICENSE](third_party/DeepLX-LICENSE)。
