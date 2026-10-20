# Tolk

Windows 和 macOS 通用的翻译工具，由「大佐翻译官」（Windows，Python）和 bagayalu（macOS，Swift）合并而来，基于 [mygo](https://github.com/egoist/mygo)（Go + 系统 WebView）。

- 引擎：Google、微软、DeepL、AI 翻译（OpenAI 兼容）、Achord 内置引擎；macOS 另有 Apple 本地翻译
- 37 种语言；复制两次呼出翻译；Mini 模式；浅色 / 深色 / 粉色主题
- macOS：截图翻译、系统级 ⌘C⌘C 监听、权限引导（由 `macos/helper` 里的 Swift 小助手提供）
- 设置沿用 `~/.dzfyq/config.json`；老用户升级不丢设置，Mac 用户沿用原来的授权

## 目录

| 路径 | 内容 |
|---|---|
| `main.go` `service.go` `prefs.go` `settings.go` `app.go` `mini.go` | 主程序与前端可调用的服务 |
| `mac.go` `helper_darwin.go` `helper_stub.go` | macOS 小助手的接入（其他平台为空实现） |
| `internal/engine` | 翻译引擎与语言表 |
| `internal/legacy` | 旧配置读写，引擎构建 |
| `internal/update` | 更新器：Windows 旧格式 Ed25519 更新包、macOS Sparkle appcast |
| `internal/helper` `macos/helper` | 小助手的 Go 客户端、Swift 实现与协议（`PROTOCOL.md`） |
| `internal/macmigrate` | 导入旧版 Mac 的设置和 API Key |
| `cmd/tolk-release` | 发布工具：打包、签名、appcast、Gitee 镜像 |
| `src` `index.html` `mini.html` | 前端 |
| `installer/setup.iss` | Windows 安装器（沿用旧 AppId，原地升级） |

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
