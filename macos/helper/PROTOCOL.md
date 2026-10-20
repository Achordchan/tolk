# tolk-helper 协议

`tolk-helper` 是 Tolk 在 macOS 上的原生小助手（Swift Package，可执行文件 `tolk-helper`）。
Tolk 主程序（Go / mygo）以子进程方式启动它，两边通过 **stdin / stdout 的 JSON 行**（每行一个 JSON 对象，UTF-8，以 `\n` 结尾）通信。stderr 只写人读的日志。

为什么是子进程而不是独立 App：屏幕录制、辅助功能的授权按「负责进程」计算，子进程沿用 Tolk.app 的授权，用户从旧版升级不需要重新授权。因此 helper **不要**自己拥有 .app 包、不要用 `open`/LaunchServices 启动自己，也不要申请别的 bundle id。

## 进程模型

- 启动参数：`tolk-helper --parent-pid <pid>`。parent 退出（`kill(pid,0)` 失败或 stdin EOF）时，helper 必须关掉所有窗口并退出。
- helper 自己跑 `NSApplication.shared`，`setActivationPolicy(.accessory)`（不进 Dock、没有菜单栏），主线程跑 RunLoop；stdin 读取放后台线程，处理结果回到主线程操作 UI。
- 弹出的窗口（选区、结果覆盖层、钉图）需要能获得键盘焦点时再 `NSApp.activate`；其余时间不抢焦点。
- stdout 只能输出协议 JSON 行，其他任何输出走 stderr。

## 消息形状

- 请求：`{"id":"<字符串>","op":"<名称>", ...参数}`，对方用同一个 id 回：
  `{"id":"<同一个>","ok":true, ...结果}` 或 `{"id":"<同一个>","ok":false,"error":"<给用户看的中文说明>"}`。
- 事件（不需要回复）：`{"event":"<名称>", ...}`。
- 双向都可以发请求。Go → helper 的 id 以 `g` 开头（`g1`、`g2`…），helper → Go 的以 `h` 开头。
- 语言一律用 Tolk 的中文标签：`自动检测`、`简体中文`、`繁体中文`、`英语`、`日语`、`韩语`、`法语`、`德语`、`西班牙语`、`俄语`、`意大利语`、`葡萄牙语`、`越南语`、`泰语`、`阿拉伯语`。helper 内部再映射到 `Locale.Language`。

## Go → helper

| op | 参数 | 结果 |
|---|---|---|
| `hello` | 无 | `{"version":"1","macos":"15.1","capabilities":["ocr","pin","selection","apple","legacy"]}`；`apple` 仅在系统 ≥ 15.1 时列出 |
| `permissions` | 无 | `{"accessibility":bool,"screenRecording":bool}`，不弹窗 |
| `permissions.request` | `kind`: `accessibility` \| `screenRecording` | `{}`，触发系统授权提示；已拒绝时打开对应系统设置页 |
| `permissions.guide` | 无 | `{}`，显示 bagayalu 里 `PermissionGuideView` 同款授权引导窗口（缺哪个讲哪个） |
| `screenshot.start` | `source`, `target`, `mode`: `translate`（默认）\| `ocr`（只识别文字） | 立即回 `{}` 表示选区层已显示；之后用事件汇报 |
| `screenshot.cancel` | 无 | `{}` |
| `selection.get` | 无 | `{"text":"…","app":"<前台 App 的 bundle id>"}`；读不到时 `ok:false` |
| `selection.replace` | `text` | `{}`，优先走辅助功能写入，不行再用粘贴兜底（与 bagayalu `InputAssistTextReplaceEngine` 一致） |
| `apple.status` | `source`, `target` | `{"state":"installed"\|"supported"\|"unsupported"}`（`supported` = 需要先下载语言包） |
| `apple.translate` | `texts`:[string], `source`, `target` | `{"texts":[string]}`，与输入等长同序；需要下载语言包时由系统弹下载窗口 |
| `hotkeys.configure` | `doubleCopy`:bool、`screenshot`:bool、`windowMs`:int（默认 550）、`screenshotKeyCode`:int（默认 7，即 X） | `{"running":bool,"error":"<中文原因，可省略>"}`，见下文「全局快捷键」 |
| `legacy.read` | 无 | 读出旧版 bagayalu 的设置和 API Key，见下文 |
| `quit` | 无 | `{}` 然后退出 |

## helper → Go

| op | 参数 | 结果 |
|---|---|---|
| `translate` | `texts`:[string], `source`, `target` | `{"texts":[string]}`，等长同序。Tolk 按用户当前选的引擎翻译（含 Apple 本地翻译时，Go 会反过来调用 `apple.translate`） |
| `log` | （事件）`level`: `debug/info/warn/error`, `msg` | |

截图相关事件（helper → Go）：

- `{"event":"screenshot.state","state":"selecting"|"recognizing"|"translating"|"showing"}`
- `{"event":"screenshot.done","mode":"translate|ocr","text":"<识别出的原文，按段落用空行分隔>","translated":"<译文，同样分段>"}`——用户关闭结果窗口或点复制后发；`mode=ocr` 时没有 `translated`
- `{"event":"screenshot.cancelled"}`——选区时按 Esc 或取消
- `{"event":"screenshot.error","error":"<中文说明>"}`——缺权限、识别不到文字等；缺权限时 `"needs":"screenRecording"`

## 截图 OCR 翻译（`screenshot.start`）

按 bagayalu `dazuofanyiguan/screens-ocr/` 的行为和视觉做：全屏半透明选区层（多显示器）、框选、Vision OCR（`VisionOCRService`、`OCRParagraphGrouper`）、对每个文本块调用 helper → Go 的 `translate`、把译文覆盖回原截图上（`ScreenshotTranslationRenderer` / `ScreenshotTranslationLayout`）、结果窗口里可以复制图片、复制译文、保存、钉住、关闭。钉图（`PinnedScreenshotWindow`）是独立浮窗，helper 内可以同时存在多个，右键菜单保持原样。翻译走 Go，不要在 helper 里再实现任何翻译引擎（Google/Microsoft/OpenAI 都不要）。

## 全局快捷键（`hotkeys.configure`）

照搬 bagayalu `Core/GlobalHotkeyMonitor.swift`：只监听不拦截的 CGEventTap（listenOnly），需要辅助功能权限。

- ⌘C 在 `windowMs` 内连按两次 = `doubleCopy`；⌘ + `screenshotKeyCode` 键在 `windowMs` 内连按两次 = `screenshot`。
- `doubleCopy`、`screenshot` 都为 false 就停止监听；可重复调用（改配置，不会重建 tap）。
- 缺辅助功能权限时**不弹窗**，只回 `running:false` 和 `error`（Go 再调 `permissions.guide`）。
- 事件（helper → Go）：
  - `{"event":"hotkey","name":"doubleCopy","changeCount":<第二次 ⌘C 按下前的 NSPasteboard.changeCount>}`（helper 不读剪贴板，Go 自己等剪贴板更新再读）
  - `{"event":"hotkey","name":"screenshot"}`
  - `{"event":"hotkey.stopped","error":"<中文原因>"}`：权限丢失或 tap 失效、监听已停止。tap 只是超时被系统停用时 helper 会自动重新启用，不发这个事件。

## 划词（`selection.get` / `selection.replace`）

参考 `dazuofanyiguan/InputAssist/Capture/*` 和 `Replacement/*`：只保留「读取当前选区文字」和「把译文替换回去」两个动作，不要带候选面板、键盘监听、缓存等 InputAssist 的其他部分。

## 旧版数据（`legacy.read`）

用于从旧版 bagayalu（bundle id `achord.dazuofanyiguan`，Tolk 沿用同一个 id，所以读的是同一个 UserDefaults 域和同一批 Keychain 项）迁移。结果：

```json
{
  "defaults": { "<UserDefaults 键>": <JSON 可表示的值> },
  "keys": { "<Keychain 账号名>": "<API Key>" },
  "languages": { "zh-CN": "简体中文", "auto": "自动检测", "...": "..." }
}
```

- `languages`：旧版语言代码 → Tolk 中文标签（15 项，含 `auto`），迁移 `sourceLanguageCode` / `targetLanguageCode` 时直接查表；查不到的代码（如 `nl`）Tolk 自行处理。

- `defaults` 只放 `AppSettings`、`InputAssistSettings` 用到的键（见 `Core/AppSettings.swift`），值不是 JSON 可表示的类型就丢掉。
- `keys` 来自 `Core/KeychainStore.swift` 使用的 service（`com.achord.dazuofanyiguan`），账号名目前只有 `openAIAPIKey`。**不要写入、不要删除**任何 Keychain 项，只读；读不到（用户拒绝授权）就从结果里略去，不报错。
- API Key 属于机密：**不要写日志**，只在这个返回里出现。

## 其他要求

- 目标系统：macOS 13+（`apple.*` 在 15.1+ 才可用，低版本时 `hello` 的 capabilities 不含 `apple`，调用返回 `ok:false`）。
- 通用二进制：`swift build -c release --arch arm64 --arch x86_64`。
- 不引入第三方依赖。
- 用户可见的文字一律中文，沿用 bagayalu 现有措辞。
- 所有对外 JSON 字段名不可变（Go 端按这份文档写）；要新增字段先改这份文档。

## 实现补充（helper 端的具体行为）

- `screenshot.start` 缺屏幕录制权限时：回 `ok:false`（`error` 为中文说明），同时发 `screenshot.error`（带 `needs:"screenRecording"`），并触发系统授权提示（第一次弹窗，之后打开系统设置）。
- `screenshot.start` 的 `mode:translate` 框选松手后自动识别并翻译；`mode:ocr` 自动识别并把原文复制到剪贴板，随后结束并发 `screenshot.done`。
- `screenshot.error`（识别不到文字、翻译失败）发出后选区层不会自动关闭，用户可重新框选；最终仍会以 `screenshot.done` 或 `screenshot.cancelled` 收尾。
- `screenshot.done` 在会话结束时发一次（点「完成」、钉到屏幕、有结果时按 Esc / 右键退出）。只提取了原文没翻译时 `mode` 为 `ocr` 且无 `translated`；段落间用空行分隔。
- `selection.replace`：取词后原应用若已不在前台，helper 会先尝试把它激活再校验焦点控件与选区；校验不过或无法原位替换时把译文复制到剪贴板，并回 `ok:false`（`error` 说明已复制）。没有先 `selection.get` 时对当前焦点控件的选区直接替换。
- `apple.status` 的 `source` 为「自动检测」时无法按原文判断，按「目标语言能否由英语（目标为英语时由简体中文）翻入」估计。
- macOS 13：`hello` 的 `capabilities` 不含 `ocr`（截图依赖 macOS 14 的 ScreenCaptureKit 截图接口）；14.x 无 `apple`。
