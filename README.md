# Tolk

Cross-platform translator, successor of `dazuo-win` (Windows) and `bagayalu-translate` (macOS). Built on [mygo](https://github.com/egoist/mygo).

## Status: phase-1 prototype

Checks that mygo covers the basics before the real migration:

- global shortcut `CmdOrCtrl+Shift+C` reads the clipboard and shows the window
- tray icon with a menu
- reads the legacy `~/.dzfyq/config.json` (without exposing keys) — `internal/legacy`
- pure-Go cross-compile for windows/amd64, darwin/arm64, linux/amd64 (verified)

## Develop

```sh
bun install
bun run dev        # live reload
bun run build      # needs NSIS for the Windows installer; dmg + signing need macOS
```
