import AppKit
import ApplicationServices
import Foundation

/// 一次取词的快照：替换时拿它和现场比对，对不上就放弃（见 InputAssistReplacementSafetyGuard）。
struct InputAssistCapture {
    let element: AXUIElement
    /// 要翻译并最终替换掉的文本。
    let sourceText: String
    /// `sourceText` 在控件全文里的位置（UTF-16）。拿不到就是 nil，
    /// 此时只能走「替换当前选区」而不能按范围替换。
    let sourceRange: InputAssistTextRange?
    /// 控件当前全文，用于 commit 前的二次校验。
    let elementValue: String?
    let capability: InputAssistSurfaceCapability
    let role: String?
    /// 是否可对明确选区执行受控粘贴替换。
    let allowsEditorPaste: Bool
    /// 取词那一刻控件的选区 / 光标位置。
    ///
    /// 替换前要拿它和当时的实际选区比一比。少了这一条的话，
    /// `selectRange` 会先把用户的光标覆盖掉，之后再怎么校验都只是在验证
    /// 「我刚设的那个选区还在不在」，永远发现不了用户其实已经把光标挪走了。
    let selectedRangeAtCapture: InputAssistTextRange?
}

/// AX 的 CFRange 以 UTF-16 code unit 计数，整条链路统一用它，避免中途换算出错。
struct InputAssistTextRange: Equatable {
    let location: Int
    let length: Int

    var upperBound: Int { location + length }
    var isEmpty: Bool { length <= 0 }
}

/// 跨 App 的 AX 文本读取。
///
/// 实现参考 lglot/translate-kit 的 `ReplaceEngine`（MIT）与
/// everettjf/TypeTide 的 `SelectionCapture`（MIT），移植自 bagayalu 的 InputAssist。
enum InputAssistAXTextCapture {
    static func focusedElement() -> AXUIElement? {
        let systemWide = AXUIElementCreateSystemWide()
        var value: CFTypeRef?
        let error = AXUIElementCopyAttributeValue(
            systemWide,
            kAXFocusedUIElementAttribute as CFString,
            &value
        )
        // 这里必须验类型再转：属性返回非 AXUIElement 时强转会直接崩。
        guard error == .success,
              let value,
              CFGetTypeID(value) == AXUIElementGetTypeID()
        else {
            return nil
        }
        return (value as! AXUIElement)
    }

    static func stringAttribute(_ element: AXUIElement, _ attribute: String) -> String? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, attribute as CFString, &value) == .success else {
            return nil
        }
        return value as? String
    }

    static func selectedRange(_ element: AXUIElement) -> InputAssistTextRange? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(
            element,
            kAXSelectedTextRangeAttribute as CFString,
            &value
        ) == .success, let value, CFGetTypeID(value) == AXValueGetTypeID() else {
            return nil
        }
        var range = CFRange()
        guard AXValueGetValue(value as! AXValue, .cfRange, &range) else { return nil }
        return InputAssistTextRange(location: range.location, length: range.length)
    }

    static func isSettable(_ element: AXUIElement, _ attribute: String) -> Bool {
        var settable = DarwinBoolean(false)
        guard AXUIElementIsAttributeSettable(
            element,
            attribute as CFString,
            &settable
        ) == .success else {
            return false
        }
        return settable.boolValue
    }

    static func capability(of element: AXUIElement) -> InputAssistSurfaceCapability {
        if isSettable(element, kAXSelectedTextAttribute as String) {
            return .axDirect
        }
        if stringAttribute(element, kAXValueAttribute as String) != nil
            || stringAttribute(element, kAXSelectedTextAttribute as String) != nil {
            return .copyOnly
        }
        return .unavailable
    }

    /// 这个控件的文本能不能被 ⌘V 改动。
    ///
    /// **只认真正表明「文本可改」的属性，不认 role，也不认「选区可移动」。**
    ///
    /// - `role` 是线索不是证据：只读的 `NSTextView`（日志窗、只读代码预览、
    ///   聊天记录区）同样暴露 `AXTextArea`。
    /// - `kAXSelectedTextRange` 可写只说明能挪光标——只读文本视图通常也允许
    ///   辅助功能客户端移动选区，能挪光标不等于能改字。
    ///
    /// 判错的代价不是"少一个功能"而是走进死胡同：⌘V 什么都改不了，
    /// 而拿不到完整 AX value 时粘贴引擎又无法证伪，于是 Enter 既没替换、
    /// 也不会退到复制兜底，用户看到的只是"按了没反应"，没有任何线索。
    static func allowsEditorPaste(element: AXUIElement) -> Bool {
        isSettable(element, kAXValueAttribute as String)
            || isSettable(element, kAXSelectedTextAttribute as String)
    }

    /// 只读取用户明确选中的文本；没有选区就返回 nil。
    /// 全程只读 AX，不猜测光标前句子，不动剪贴板。
    /// 和 bagayalu 的区别：不再用 `looksTranslatable` 过滤网址、邮箱、纯符号——
    /// 要不要翻译由 Tolk 主程序决定，helper 只管如实读出来。
    static func captureSelectedText() -> InputAssistCapture? {
        guard let element = focusedElement() else { return nil }

        let role = stringAttribute(element, kAXRoleAttribute as String)
        let subrole = stringAttribute(element, kAXSubroleAttribute as String)
        guard InputAssistSecureInputGuard.allowsAutomation(
            role: role,
            subrole: subrole,
            isSecureEventInputEnabled: InputAssistSecureInputGuard.isSecureEventInputEnabled
        ) else {
            return nil
        }

        let capability = capability(of: element)
        guard capability != .unavailable else { return nil }

        let elementValue = stringAttribute(element, kAXValueAttribute as String)
        let selectedRange = selectedRange(element)

        guard let selectedText = stringAttribute(element, kAXSelectedTextAttribute as String),
              !selectedText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        else {
            return nil
        }

        let range = selectedRange.flatMap { $0.length > 0 ? $0 : nil }
        return InputAssistCapture(
            element: element,
            sourceText: selectedText,
            sourceRange: range,
            elementValue: elementValue,
            capability: capability,
            role: role,
            allowsEditorPaste: allowsEditorPaste(element: element),
            selectedRangeAtCapture: selectedRange
        )
    }

    /// 按 UTF-16 范围拼出「替换之后应该长什么样」。
    ///
    /// 用来做 AX 写入的精确校验：只有读回来的全文和它逐字相同，
    /// 才能说这次写确实把完整译文落在了预期位置。
    /// 范围越界或切在字符中间时返回 nil——算不出期望值就不该动手写。
    static func replacingRange(
        in text: String,
        range: InputAssistTextRange,
        with replacement: String
    ) -> String? {
        guard range.location >= 0, range.length >= 0 else { return nil }
        guard range.upperBound <= text.utf16.count else { return nil }
        guard let start = String.Index(utf16Offset: range.location, in: text)
            .samePosition(in: text),
            let end = String.Index(utf16Offset: range.upperBound, in: text)
                .samePosition(in: text),
            start <= end
        else {
            return nil
        }
        return text.replacingCharacters(in: start..<end, with: replacement)
    }

    /// 按 UTF-16 范围取子串。范围越界或落在字符中间时返回 nil，绝不返回一个「差不多」的结果。
    static func substring(of text: String, range: InputAssistTextRange) -> String? {
        guard range.location >= 0, range.length >= 0 else { return nil }
        guard range.upperBound <= text.utf16.count else { return nil }
        guard let start = String.Index(utf16Offset: range.location, in: text)
            .samePosition(in: text),
            let end = String.Index(utf16Offset: range.upperBound, in: text)
                .samePosition(in: text),
            start <= end
        else {
            return nil
        }
        return String(text[start..<end])
    }
}
