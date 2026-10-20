import AppKit
import ApplicationServices
import Foundation

struct OpFailure: Error {
    let message: String
    init(_ message: String) { self.message = message }
}

/// `selection.get` / `selection.replace`：只保留「读取当前选区文字」和「把译文替换回去」两个动作。
/// 逻辑取自 bagayalu 的 InputAssistCoordinator（取词 + Chromium 重试 + 三级提交），去掉了候选面板、监听和缓存。
@MainActor
enum SelectionService {
    private static var lastSession: CandidateSession?

    static func get() async throws -> (text: String, app: String) {
        guard AXIsProcessTrusted() else {
            throw OpFailure("需要开启“辅助功能”权限才能读取选中的文字")
        }
        let frontmost = NSWorkspace.shared.frontmostApplication
        let pid = frontmost?.processIdentifier

        var capture = InputAssistAXTextCapture.captureSelectedText()
        if capture == nil, let pid {
            // 取不到不代表用户没选中。Chromium / Electron 应用要先被明确告知
            // 「有人要用辅助功能」才会把树建起来。
            if InputAssistChromiumAccessibility.enableIfNeeded(pid: pid) {
                try? await Task.sleep(nanoseconds: InputAssistChromiumAccessibility.settleNanoseconds)
                capture = InputAssistAXTextCapture.captureSelectedText()
            }
        }
        guard let capture else {
            throw OpFailure("没有读到选中的文字，请先选中文字（当前应用也可能没有提供选中文本）")
        }
        lastSession = CandidateSession(appProcessIdentifier: pid, capture: capture)
        return (capture.sourceText, frontmost?.bundleIdentifier ?? "")
    }

    static func replace(with translatedText: String) async throws {
        guard AXIsProcessTrusted() else {
            throw OpFailure("需要开启“辅助功能”权限才能替换选中的文字")
        }

        // 没有先取词（或取词太久以前了）：对当前焦点控件的选区现取一份。
        var session = lastSession
        lastSession = nil
        if let existing = session, Date().timeIntervalSince(existing.createdAt) > 10 * 60 {
            session = nil
        }
        if session == nil {
            let pid = NSWorkspace.shared.frontmostApplication?.processIdentifier
            guard let capture = InputAssistAXTextCapture.captureSelectedText() else {
                throw OpFailure("没有找到要替换的选区")
            }
            session = CandidateSession(appProcessIdentifier: pid, capture: capture)
        }
        guard let session else { throw OpFailure("没有找到要替换的选区") }

        // 取词后原应用若已被别的窗口（比如 Tolk 自己）顶掉，先把它请回前台，再校验焦点控件和选区。
        await bringApplicationToFront(pid: session.appProcessIdentifier)

        let commitMode = InputAssistCommitPolicy.mode(
            capability: session.capability,
            allowsEditorPaste: session.allowsEditorPaste,
            hasSourceRange: session.sourceRange != nil,
            hasElementValue: session.elementValueAtCapture != nil
        )

        var lastOutcome: InputAssistReplacementOutcome?
        if commitMode == .axReplace {
            let outcome = await InputAssistTextReplaceEngine.replace(session: session, with: translatedText)
            if isSuccess(outcome) { return }
            lastOutcome = outcome
            if session.allowsEditorPaste, outcome.allowsPasteFallback {
                let pasteOutcome = await InputAssistEditorPasteEngine.replace(session: session, with: translatedText)
                if isSuccess(pasteOutcome) { return }
                if pasteOutcome.preservesForeignClipboard {
                    throw OpFailure("剪贴板正被其它应用使用，本次未替换，请稍后重试")
                }
                lastOutcome = pasteOutcome
            }
        } else if commitMode == .editorPaste {
            let outcome = await InputAssistEditorPasteEngine.replace(session: session, with: translatedText)
            if isSuccess(outcome) { return }
            if outcome.preservesForeignClipboard {
                throw OpFailure("剪贴板正被其它应用使用，本次未替换，请稍后重试")
            }
            lastOutcome = outcome
        }

        // 兜底：复制译文，让用户自己粘贴。
        let pasteboard = NSPasteboard.general
        pasteboard.clearContents()
        pasteboard.setString(translatedText, forType: .string)
        switch commitMode {
        case .copy:
            throw OpFailure("当前应用不支持原位替换，译文已复制")
        default:
            if lastOutcome?.allowsPasteFallback == false {
                throw OpFailure("无法确认替换是否生效，译文已复制")
            }
            throw OpFailure("无法安全替换，译文已复制")
        }
    }

    private static func isSuccess(_ outcome: InputAssistReplacementOutcome) -> Bool {
        if case .replaced = outcome {
            Bridge.log(.info, "选区已替换")
            return true
        }
        if case .aborted(let reason) = outcome {
            Bridge.log(.warn, "放弃替换原文：\(reason.rawValue)")
        } else if case .failed(let message) = outcome {
            Bridge.log(.warn, "无法替换原文：\(message)")
        }
        return false
    }

    private static func bringApplicationToFront(pid: pid_t?) async {
        guard let pid,
              NSWorkspace.shared.frontmostApplication?.processIdentifier != pid,
              let app = NSRunningApplication(processIdentifier: pid) else { return }
        app.activate(options: [])
        // 最多等 0.8 秒让它真正成为前台，再留一点时间让焦点控件恢复。
        for _ in 0..<16 {
            if NSWorkspace.shared.frontmostApplication?.processIdentifier == pid { break }
            try? await Task.sleep(nanoseconds: 50_000_000)
        }
        try? await Task.sleep(nanoseconds: 120_000_000)
    }
}
