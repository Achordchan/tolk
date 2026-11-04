# Windows screenshot workspace

Entry: Ctrl+X twice within 550 ms, or Settings > Shortcuts > screenshot.
The first Ctrl+X preserves the application's cut operation. The second press
is consumed. Holding X does not count as two presses.

The native workspace freezes the virtual desktop, crops the selection, runs
Windows OCR locally, groups wrapped lines conservatively, and sends text blocks
through a private stdin/stdout JSON protocol to Tolk's selected translation
engine. Images never go to the translation service. Translation is drawn back
into the crop; the main translation editor is never modified.

Actions: translate, choose installed OCR language and target language, compare
original, extract original text, inspect full translation, copy the displayed
image, pin the image, reselect, exit. A pin closes selection but keeps its own
window alive. New selections can be opened while pins remain. Export is disabled
during recognition/translation. Overflow that cannot fit without clipping
retains the original image and exposes the complete translated text explicitly.

Lifecycle: private UTF-8 BOM script, inherited anonymous pipes, bounded protocol
messages, per-engine translation timeout, and Windows kill-on-close job object.
Application quit cancels the workspace. No periodic server polling occurs.
The UI completion timer only checks a pending pipe task. Temporary scripts are
removed on normal completion; abrupt process termination can leave a script in
the OS temp directory (no captured image or recognized text is written there).

Checks: `go test ./internal/screenshot ./internal/doublekey`, PowerShell AST parse,
`pwsh -File internal/screenshot/layout_test.ps1`, Windows cross-build.
Windows native OCR, clipboard, display scaling and hotkey acceptance are tested
by the user. macOS continues to use its existing Swift implementation.
