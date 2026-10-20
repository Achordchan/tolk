import ApplicationServices
import Foundation

/// 取词时冻结下来的一切。`selection.replace` 之前拿它和现场比对；对不上就不动用户的文字。
/// （bagayalu 里这个类型还带候选浮层的定位和语言信息，helper 只留替换需要的部分。）
struct CandidateSession {
    /// 取词时前台应用的进程号。替换前比的是它，不是 bundle ID——没有 bundle ID 的应用两边都是 nil，
    /// 比不出换没换应用（见 `InputAssistReplacementSafetyGuard.isSameProcess`）。
    let appProcessIdentifier: pid_t?
    let element: AXUIElement
    let sourceText: String
    let sourceRange: InputAssistTextRange?
    let elementValueAtCapture: String?
    let capability: InputAssistSurfaceCapability
    let role: String?
    let allowsEditorPaste: Bool
    /// 取词那一刻的选区 / 光标位置，替换前要比对。
    let selectedRangeAtCapture: InputAssistTextRange?
    let createdAt: Date

    init(appProcessIdentifier: pid_t?, capture: InputAssistCapture, createdAt: Date = Date()) {
        self.appProcessIdentifier = appProcessIdentifier
        self.element = capture.element
        self.sourceText = capture.sourceText
        self.sourceRange = capture.sourceRange
        self.elementValueAtCapture = capture.elementValue
        self.capability = capture.capability
        self.role = capture.role
        self.allowsEditorPaste = capture.allowsEditorPaste
        self.selectedRangeAtCapture = capture.selectedRangeAtCapture
        self.createdAt = createdAt
    }
}
