# Windows screenshot workspace

Entry: Ctrl+X twice within 550 ms, or Settings > Shortcuts > screenshot.
The first Ctrl+X preserves the application's cut operation. The second press
is consumed. Holding X does not count as two presses.

The native workspace freezes the virtual desktop, crops the selection, runs
Windows OCR locally, groups wrapped lines conservatively, and sends text blocks
through a private stdin/stdout JSON protocol to Tolk's selected translation
engine. Images never go to the translation service. Translation is drawn back
into the crop; the main translation editor is never modified.

Surface: `controls.ps1` holds the owner-drawn C# controls. Add-Type compiles them once per version
into `%LOCALAPPDATA%\Tolk\screenshot\controls-<hash>.dll` and later starts load that file; a cached
DLL that does not load is compiled again and replaced. If loading from that folder is refused
altogether (AppLocker), a `.blocked` marker skips the cache until Tolk next starts and sweeps the
folder, and the controls are compiled in memory as before. The controls are
a dimmed copy of the frozen desktop, the two-row toolbar, language pills with themed
menus, all scaled per monitor. Colours are the main window's tokens (`theme` in the start message:
light, dark, pink; anything else follows the Windows app theme). The GDI+ ceiling is
rounded corners and layered shadows, with no blur or animation.

Actions: translate, choose installed OCR language and target language, compare
original, extract original text, inspect full translation, copy the displayed
image, pin the image, reselect, exit. Translation starts from the 翻译 button, or right after the
selection when Settings > 框选后自动翻译 is on (`autoTranslate` in the start message, off by default).
Row 2 of the toolbar shows icon and words; only a monitor too narrow for them turns buttons into
icons with tooltips, one at a time. The top hint hides on mouse-down and comes back on mouse-up;
with a selection it shows only where it covers neither the selection nor the toolbar.
A pin closes selection but keeps its own
window alive: borderless and topmost with only the system drop shadow; its edge flashes twice when
it appears; drag to move, drag an edge to resize at the image's aspect, double-click or Esc to close.
While the pointer is over it, a 置于最前 toggle and a close button show at its top-right (as on
macOS); right-click to copy, save or toggle 置于最前. New selections can be opened while pins remain. Export is disabled
during recognition/translation. Overflow that cannot fit without clipping
retains the original image and exposes the complete translated text explicitly.

Warm start: Tolk keeps one workspace process started ahead of time (5 s after launch when the
shortcut is on, and again whenever a selection opens). It has PowerShell, the controls and OCR loaded
and writes `{"warm":...}`, then waits on stdin; the screen is captured only when the start message
(target language, theme, language list, auto-translate) arrives, so nothing it shows is stale. A spare that dies or
fails to warm is not relaunched until the next screenshot, which then starts cold and reports the
real error. Turning the shortcut off ends the spare.

Lifecycle: private UTF-8 BOM script, inherited anonymous pipes, bounded protocol
messages, per-engine translation timeout, and Windows kill-on-close job object.
Application quit cancels the workspace. No periodic server polling occurs.
The UI completion timer only checks a pending pipe task. Temporary scripts are
removed on normal completion; abrupt process termination can leave a script in
the OS temp directory (no captured image or recognized text is written there).

Checks: `go test ./internal/screenshot ./internal/doublekey` (with `TOLK_WIN_E2E=1` on Windows it
also launches the real workspace: compiled, cached and in-memory controls, warm, then Begin), PowerShell AST parse of the
concatenated scripts (controls, capture, layout, workspace),
`pwsh -File internal/screenshot/layout_test.ps1`, Windows cross-build.
Windows native OCR, clipboard, display scaling and hotkey acceptance are tested
by the user. macOS continues to use its existing Swift implementation.
