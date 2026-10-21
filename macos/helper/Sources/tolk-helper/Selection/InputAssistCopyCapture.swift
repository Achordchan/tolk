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
    /// 检测到剪贴板变化后的稳定等待：这段时间内再变就当作多方写入。
    static let settleNanoseconds: UInt64 = 80_000_000

    /// 用模拟 ⌘C 取当前选中的文字。取不到（没选中、应用不响应、被取消、前台变了）返回 nil。
    /// 无论哪个出口，只要动过剪贴板就会还原。
    static func captureSelectedText(expectedProcessIdentifier: pid_t) async -> InputAssistCapture? {
        guard isTargetStillApproved(expectedProcessIdentifier) else { return nil }

        // 安全红线：在模拟任何按键之前检查。AX 取词在密码框上返回 nil 之后会走到这里，
        // 所以焦点控件是不是密码框也要在这里重新看一遍；等待松键期间焦点可能换了，发 ⌘C 前还要再看一次。
        guard focusAllowsCopy() else { return nil }

        // 修饰键一直没松开就放弃：带着 ⌥⇧ 发 ⌘C 会变成别的快捷键。
        guard await waitForModifiersReleased() else {
            Bridge.log(.info, "复制取词：触发快捷键的修饰键一直没松开，已跳过")
            return nil
        }
        guard !Task.isCancelled, isTargetStillApproved(expectedProcessIdentifier), focusAllowsCopy() else { return nil }

        let pasteboard = NSPasteboard.general
        let changeCountBeforeSnapshot = pasteboard.changeCount
        let savedItems = InputAssistPasteboardSnapshot.snapshot(from: pasteboard)
        // 拍快照的瞬间剪贴板被别人改了：快照不可信，宁可不取词也不冒险还原出旧内容。
        guard pasteboard.changeCount == changeCountBeforeSnapshot else { return nil }

        // 发键前最后一次确认：快照和前面的等待之间焦点可能已经挪进密码框。
        guard focusAllowsCopy(), isTargetStillApproved(expectedProcessIdentifier) else { return nil }
        guard InputAssistKeyboardSynthesizer.pressCopy() else { return nil }

        // 轮询 changeCount，而不是读内容：剪贴板里本来就有旧文本，只有 changeCount 变了才说明这次复制生效。
        var waited: UInt64 = 0
        while pasteboard.changeCount == changeCountBeforeSnapshot,
              waited < copyTimeoutNanoseconds,
              !Task.isCancelled {
            try? await Task.sleep(nanoseconds: pollNanoseconds)
            waited += pollNanoseconds
        }

        let changeCountAtDetection = pasteboard.changeCount
        if changeCountAtDetection == changeCountBeforeSnapshot {
            // 超时或被取消：没看到变化就不动剪贴板。之后如果有迟到的写入落地，无从判断是目标应用
            // 迟到的复制还是用户或别的程序写的，宁可不去覆盖，也不拿旧快照盲目还原。
            Bridge.log(.info, "复制取词：应用没有响应 ⌘C（可能没有选中文字）")
            return nil
        }

        // 检测到变化后再等一小段：期间如果又变了，说明有别的写入者，不去猜里面是什么。
        try? await Task.sleep(nanoseconds: settleNanoseconds)
        let changeCountAfterSettle = pasteboard.changeCount
        let pasteboardText = pasteboard.string(forType: .string)
        // 读字符串的瞬间也可能被别人写入：读完再对一次计数，对不上就当争用处理。
        let result: InputAssistCopyFallbackLogic.CopyResult = pasteboard.changeCount == changeCountAfterSettle
            ? InputAssistCopyFallbackLogic.judgeCopy(
                changeCountBefore: changeCountBeforeSnapshot,
                changeCountAtDetection: changeCountAtDetection,
                changeCountAfterSettle: changeCountAfterSettle,
                pasteboardText: pasteboardText
            )
            : .contended

        if result == .contended {
            // 争用时剪贴板里是谁写的无从判断：不翻译，也不覆盖，保留别人的写入。
            Bridge.log(.info, "复制取词：剪贴板在取词期间被多方写入，放弃这次触发")
            return nil
        }

        // 其余出口都还原用户原来的剪贴板；还原前再看一眼 changeCount，
        // 读完到这里之间又有人写了剪贴板的话，不去覆盖它。
        if pasteboard.changeCount == changeCountAfterSettle {
            InputAssistPasteboardSnapshot.restore(savedItems, to: pasteboard)
        }

        guard case .copied(let text) = result else {
            Bridge.log(.info, "复制取词：剪贴板里没有可用的文字")
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

    /// 焦点控件不是密码框（含系统级安全输入）才允许模拟 ⌘C。
    private static func focusAllowsCopy() -> Bool {
        let secure = InputAssistSecureInputGuard.isSecureEventInputEnabled
        guard let focused = InputAssistAXTextCapture.focusedElement() else {
            return InputAssistSecureInputGuard.allowsAutomation(role: nil, subrole: nil, isSecureEventInputEnabled: secure)
        }
        let role = InputAssistAXTextCapture.stringAttribute(focused, kAXRoleAttribute as String)
        let subrole = InputAssistAXTextCapture.stringAttribute(focused, kAXSubroleAttribute as String)
        guard InputAssistSecureInputGuard.allowsAutomation(role: role, subrole: subrole, isSecureEventInputEnabled: secure) else {
            Bridge.log(.info, "复制取词：焦点是安全输入框，已跳过")
            return false
        }
        return true
    }

    private static func isTargetStillApproved(_ expected: pid_t) -> Bool {
        InputAssistCopyFallbackLogic.validateTarget(
            expectedProcessIdentifier: expected,
            currentProcessIdentifier: NSWorkspace.shared.frontmostApplication?.processIdentifier,
            isSecureEventInputEnabled: InputAssistSecureInputGuard.isSecureEventInputEnabled
        ) == .allowed
    }

    /// 触发快捷键常带 ⌥⇧，用户还没松手时合成 ⌘C 会被系统合并成别的组合键。
    private static func waitForModifiersReleased() async -> Bool {
        let mask: CGEventFlags = [.maskCommand, .maskAlternate, .maskShift, .maskControl]
        var waited: UInt64 = 0
        while !CGEventSource.flagsState(.combinedSessionState).intersection(mask).isEmpty,
              waited < modifierReleaseTimeoutNanoseconds,
              !Task.isCancelled {
            try? await Task.sleep(nanoseconds: pollNanoseconds)
            waited += pollNanoseconds
        }
        return CGEventSource.flagsState(.combinedSessionState).intersection(mask).isEmpty
    }
}
