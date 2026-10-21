import AppKit
import ApplicationServices
import CoreGraphics
import Foundation
import TolkHelperCore

/// 复制取词兜底：AX 读不到选区的应用（微信等自绘界面）改为模拟 ⌘C，读剪贴板里的新文本当选区，
/// 读完立刻还原用户原来的剪贴板。
///
/// **只给手动快捷键用。** 自动显示（选区监听）不能走这里，否则每次选字都会动剪贴板。
/// 调用方负责在调用前检查应用范围（黑白名单）；本文件负责安全输入检查、前台进程校验和剪贴板还原。
@MainActor
enum InputAssistCopyCapture {
    /// 模拟 ⌘C 后等目标应用写入剪贴板的上限与轮询间隔。
    static let copyTimeoutNanoseconds: UInt64 = 300_000_000
    static let pollNanoseconds: UInt64 = 20_000_000
    /// 等用户松开触发快捷键的修饰键（⌥⇧ 等）的上限：带着它们按 ⌘C 会变成别的快捷键。
    static let modifierReleaseTimeoutNanoseconds: UInt64 = 400_000_000

    /// 用模拟 ⌘C 取当前选中的文字。取不到（没选中、应用不响应、被取消、前台变了）返回 nil。
    /// 无论哪个出口，只要动过剪贴板就会还原。
    static func captureSelectedText(expectedProcessIdentifier: pid_t) async -> InputAssistCapture? {
        guard isTargetStillApproved(expectedProcessIdentifier) else { return nil }

        // 安全红线：在模拟任何按键之前检查。AX 取词在密码框上返回 nil 之后会走到这里，
        // 所以焦点控件是不是密码框也要在这里重新看一遍。
        if let focused = InputAssistAXTextCapture.focusedElement() {
            let role = InputAssistAXTextCapture.stringAttribute(focused, kAXRoleAttribute as String)
            let subrole = InputAssistAXTextCapture.stringAttribute(focused, kAXSubroleAttribute as String)
            guard InputAssistSecureInputGuard.allowsAutomation(
                role: role,
                subrole: subrole,
                isSecureEventInputEnabled: InputAssistSecureInputGuard.isSecureEventInputEnabled
            ) else {
                Bridge.log(.info, "复制取词：焦点是安全输入框，已跳过")
                return nil
            }
        }

        await waitForModifiersReleased()
        guard !Task.isCancelled, isTargetStillApproved(expectedProcessIdentifier) else { return nil }

        let pasteboard = NSPasteboard.general
        let changeCountBeforeSnapshot = pasteboard.changeCount
        let savedItems = InputAssistPasteboardSnapshot.snapshot(from: pasteboard)
        // 拍快照的瞬间剪贴板被别人改了：快照不可信，宁可不取词也不冒险还原出旧内容。
        guard pasteboard.changeCount == changeCountBeforeSnapshot else { return nil }

        guard InputAssistKeyboardSynthesizer.pressCopy() else { return nil }

        // 轮询 changeCount，而不是读内容：剪贴板里本来就有旧文本，只有 changeCount 变了才说明这次复制生效。
        var waited: UInt64 = 0
        while pasteboard.changeCount == changeCountBeforeSnapshot,
              waited < copyTimeoutNanoseconds,
              !Task.isCancelled {
            try? await Task.sleep(nanoseconds: pollNanoseconds)
            waited += pollNanoseconds
        }

        let changeCountAfterCopy = pasteboard.changeCount
        let result = InputAssistCopyFallbackLogic.judgeCopy(
            changeCountBefore: changeCountBeforeSnapshot,
            changeCountNow: changeCountAfterCopy,
            pasteboardText: pasteboard.string(forType: .string)
        )

        // 所有出口（成功、没复制到、超时、被取消）都还原：只要 changeCount 变过，剪贴板里就是别人写的新内容。
        // 还原前再看一眼 changeCount——读完到这里之间又有人写了剪贴板的话，不去覆盖它。
        if changeCountAfterCopy != changeCountBeforeSnapshot, pasteboard.changeCount == changeCountAfterCopy {
            InputAssistPasteboardSnapshot.restore(savedItems, to: pasteboard)
        }

        guard case .copied(let text) = result else {
            switch result {
            case .clipboardUnchanged:
                Bridge.log(.info, "复制取词：应用没有响应 ⌘C（可能没有选中文字）")
            default:
                Bridge.log(.info, "复制取词：剪贴板里没有可用的文字")
            }
            return nil
        }
        guard !Task.isCancelled, isTargetStillApproved(expectedProcessIdentifier) else {
            Bridge.log(.info, "复制取词：取词期间前台应用已切换，放弃这次触发")
            return nil
        }

        return InputAssistCapture(
            element: nil,
            sourceText: text,
            sourceRange: nil,
            elementValue: nil,
            // 没有 AX 元素，能力只能算「读得到、写不了」，提交由 CommitPolicy 按 isCopyFallback 选粘贴。
            capability: .copyOnly,
            role: nil,
            allowsEditorPaste: true,
            selectedRangeAtCapture: nil,
            isCopyFallback: true
        )
    }

    private static func isTargetStillApproved(_ expected: pid_t) -> Bool {
        InputAssistCopyFallbackLogic.validateTarget(
            expectedProcessIdentifier: expected,
            currentProcessIdentifier: NSWorkspace.shared.frontmostApplication?.processIdentifier,
            isSecureEventInputEnabled: InputAssistSecureInputGuard.isSecureEventInputEnabled
        ) == .allowed
    }

    /// 触发快捷键常带 ⌥⇧，用户还没松手时合成 ⌘C 会被系统合并成别的组合键。
    private static func waitForModifiersReleased() async {
        let mask: CGEventFlags = [.maskCommand, .maskAlternate, .maskShift, .maskControl]
        var waited: UInt64 = 0
        while !CGEventSource.flagsState(.combinedSessionState).intersection(mask).isEmpty,
              waited < modifierReleaseTimeoutNanoseconds,
              !Task.isCancelled {
            try? await Task.sleep(nanoseconds: pollNanoseconds)
            waited += pollNanoseconds
        }
    }
}
