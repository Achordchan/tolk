import CoreGraphics
import Foundation

enum InputAssistKeyboardSynthesizer {
    static let injectionMarker: Int64 = 0x4241_4741_5941_4C55
    static let vKeyCode: CGKeyCode = 9
    static let cKeyCode: CGKeyCode = 8

    @discardableResult
    static func pressPaste() -> Bool {
        pressCommandKey(vKeyCode)
    }

    /// 模拟 ⌘C（复制取词兜底用）。
    @discardableResult
    static func pressCopy() -> Bool {
        pressCommandKey(cKeyCode)
    }

    private static func pressCommandKey(_ keyCode: CGKeyCode) -> Bool {
        let source = CGEventSource(stateID: .combinedSessionState)
        guard let down = CGEvent(keyboardEventSource: source, virtualKey: keyCode, keyDown: true),
              let up = CGEvent(keyboardEventSource: source, virtualKey: keyCode, keyDown: false)
        else {
            return false
        }
        down.flags = .maskCommand
        up.flags = .maskCommand
        down.setIntegerValueField(.eventSourceUserData, value: injectionMarker)
        up.setIntegerValueField(.eventSourceUserData, value: injectionMarker)
        down.post(tap: .cghidEventTap)
        up.post(tap: .cghidEventTap)
        return true
    }
}
