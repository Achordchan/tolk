import Foundation

enum InputAssistCommitMode: Equatable {
    case axReplace
    case editorPaste
    case copy
}

/// 候选提交时的唯一能力分级。
enum InputAssistCommitPolicy {
    static func mode(
        capability: InputAssistSurfaceCapability,
        allowsEditorPaste: Bool,
        hasSourceRange: Bool,
        hasElementValue: Bool,
        isCopyFallback: Bool = false
    ) -> InputAssistCommitMode {
        // 复制兜底的选区没有 AX 元素，没法原位替换，直接走粘贴
        // （前台进程是否还是取词时那个，由粘贴引擎校验，不过就退化成复制）。
        if isCopyFallback {
            return .editorPaste
        }
        if capability == .axDirect, hasSourceRange, hasElementValue {
            return .axReplace
        }
        if capability != .unavailable, allowsEditorPaste {
            return .editorPaste
        }
        return .copy
    }

}
