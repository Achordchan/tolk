import AppKit
import Carbon
import Foundation
import TolkHelperCore

/// 浮层关闭的原因，用于本地统计与调试。
enum CandidateDismissReason: String {
    case escape
    case continuedTyping
    case caretMoved
    case outsideClick
    case applicationSwitched
    case inputSourceChanged
    case committed
    case copied
    case replacedByNewSession
    case featureDisabled
}

/// 候选浮层的生命周期：显示、按键、关闭。
///
/// 刻意不设固定超时（PRD §15）：浮层一直在，直到出现明确的用户行为。
@MainActor
final class CandidatePanelController {
    let model = CandidatePanelModel()

    var onCommit: ((Int) -> Void)?
    var onCopy: ((Int) -> Void)?
    var onRetry: ((Int) -> Void)?
    var onDismiss: ((CandidateDismissReason) -> Void)?

    private lazy var panel: CandidatePanel = {
        let panel = CandidatePanel(model: model)
        return panel
    }()

    private let keyTap = CandidateKeyTap()
    private var mouseMonitor: Any?
    private var workspaceObserver: (any NSObjectProtocol)?
    private var inputSourceObserver: (any NSObjectProtocol)?

    private(set) var isVisible = false
    /// 助手自己被激活（语言包下载提示）期间不再拦截按键，免得吞掉系统对话框的回车/方向键；
    /// 用户回到原应用时面板会被收起，拦截随之结束。
    private var interceptionSuspended = false
    /// 弹出面板时的前台应用；下载提示结束后焦点还回它，不算切走。
    private var originProcessIdentifier: pid_t?
    private var anchorRect: CGRect = .zero

    init() {
        model.onCommitRow = { [weak self] index in
            guard let self else { return }
            guard self.model.state.committableIndices.contains(index) else { return }
            self.onCommit?(index)
        }
        model.onRetryRow = { [weak self] index in
            self?.onRetry?(index)
        }

        keyTap.onKeyEvent = { [weak self] event in
            self?.handleKeyEvent(event) ?? false
        }
        keyTap.onModifierFlagsChanged = { [weak self] option, command, control, shift in
            guard let self else { return }
            let shows = InputAssistKeyRouter.showsDebugOverlay(
                hasOption: option,
                hasCommand: command,
                hasControl: control,
                hasShift: shift
            )
            guard self.model.showsDebugInfo != shows else { return }
            self.model.showsDebugInfo = shows
            // 在事件 tap 的回调里：窗口操作挪到回调之外。
            Task { @MainActor [weak self] in
                self?.resizeToFitContent()
            }
        }
    }

    /// 返回 false 表示按键拦截没能装上（多半是辅助功能权限掉了），调用方应当放弃这次触发。
    @discardableResult
    func show(
        languageCodes: [String],
        sourceText: String,
        anchorRect: CGRect,
        appearance: InputAssistAppearance,
        showsCacheBadge: Bool,
        commitMode: InputAssistCommitMode
    ) -> Bool {
        interceptionSuspended = false
        originProcessIdentifier = NSWorkspace.shared.frontmostApplication?.processIdentifier
        model.state = CandidateListState(languageCodes: languageCodes)
        model.showsDebugInfo = false
        model.showsCacheBadge = showsCacheBadge
        model.commitMode = commitMode
        // 从原文估一次行高就定死，之后翻译陆续返回都不会再改变尺寸（PRD §12.2）。
        model.reservedLineCount = CandidatePanelLayout.reservedLineCount(forSourceText: sourceText)
        self.anchorRect = anchorRect

        panel.applyAppearance(appearance)
        panel.present(
            anchorRect: anchorRect,
            contentSize: currentPanelSize()
        )
        isVisible = true

        guard keyTap.start() else {
            dismiss(reason: .featureDisabled)
            return false
        }
        installDismissMonitors()
        return true
    }

    func updateRow(languageCode: String, state: CandidateRowState) {
        guard isVisible else { return }
        // 刻意不重新计算尺寸：行高在弹出时就定死了，
        // 译文陆续返回不能让浮层长高或者重新定位（PRD §12.2）。
        _ = model.state.update(languageCode: languageCode, state: state)
    }

    func dismiss(reason: CandidateDismissReason) {
        guard isVisible || keyTap.isRunning else { return }
        isVisible = false
        keyTap.stop()
        removeDismissMonitors()
        panel.orderOut(nil)
        onDismiss?(reason)
    }

    var selectedIndex: Int { model.state.selectedIndex }

    var selectedTranslatedText: String? {
        model.state.selectedRow?.translatedText
    }

    func translatedText(at index: Int) -> String? {
        guard model.state.rows.indices.contains(index) else { return nil }
        return model.state.rows[index].translatedText
    }

    func languageCode(at index: Int) -> String? {
        guard model.state.rows.indices.contains(index) else { return nil }
        return model.state.rows[index].languageCode
    }

    // MARK: - Private

    private func currentPanelSize() -> CGSize {
        CandidatePanelLayout.panelSize(
            rowCount: model.state.count,
            selectedIndex: model.state.selectedIndex,
            reservedLineCount: model.reservedLineCount,
            showsDebugInfo: model.showsDebugInfo,
            showsCopyNotice: model.commitMode == .copy
        )
    }

    /// 只有**用户操作**才会改变尺寸：移动高亮（高亮行可以展开到 4 行）、按住 ⌥ 看调试信息。
    /// 翻译返回不走这里。
    private func resizeToFitContent() {
        guard isVisible else { return }
        let size = currentPanelSize()
        guard panel.frame.size != size else { return }
        // 重新跑一遍定位而不是就地把顶边钉住：高亮行展开到 4 行时浮层会长高，
        // 就地生长会把它顶出屏幕下边缘，定位器会负责该翻上方就翻上方。
        panel.present(anchorRect: anchorRect, contentSize: size)
    }

    private func handleKeyEvent(_ event: InputAssistKeyEvent) -> Bool {
        guard isVisible, !interceptionSuspended else { return false }

        guard let decision = InputAssistKeyRouter.decide(
            for: event,
            candidateCount: model.state.count,
            selectedIndex: model.state.selectedIndex,
            committableIndices: model.state.committableIndices
        ) else {
            return false
        }

        // 这个函数在事件 tap 的回调里同步执行：回调不返回，全系统的键盘投递都在等。
        // 所以只同步改选中状态（连按 ↓ 时下一次判定要看到最新的选中项），
        // 窗口操作、写剪贴板、提交替换这些可能慢的动作都挪到回调之外再做。
        var deferredAction: (@MainActor (CandidatePanelController) -> Void)?
        switch decision.action {
        case .moveUp:
            if model.state.moveSelection(by: -1) {
                deferredAction = { $0.resizeToFitContent() }
            }
        case .moveDown:
            if model.state.moveSelection(by: 1) {
                deferredAction = { $0.resizeToFitContent() }
            }
        case .commit:
            let index = model.state.selectedIndex
            deferredAction = { $0.onCommit?(index) }
        case .commitIndex(let index):
            let changed = model.state.select(index: index)
            deferredAction = {
                if changed { $0.resizeToFitContent() }
                $0.onCommit?(index)
            }
        case .copySelection:
            let index = model.state.selectedIndex
            deferredAction = { $0.onCopy?(index) }
        case .dismiss:
            // 面板已经决定要收起：窗口操作可以晚一点，但拦截必须立刻停，
            // 否则排队期间紧跟着的 Return 还会被当成提交旧候选吞掉。
            interceptionSuspended = true
            deferredAction = { $0.dismiss(reason: .escape) }
        case .dismissPassingEventThrough:
            interceptionSuspended = true
            let reason = dismissReason(for: event)
            deferredAction = { $0.dismiss(reason: reason) }
        }

        if let deferredAction {
            Task { @MainActor [weak self] in
                guard let self else { return }
                deferredAction(self)
            }
        }

        return decision.swallowsEvent
    }

    private func dismissReason(for event: InputAssistKeyEvent) -> CandidateDismissReason {
        switch event.keyCode {
        case InputAssistKeyRouter.leftArrow, InputAssistKeyRouter.rightArrow:
            return .caretMoved
        default:
            return .continuedTyping
        }
    }

    private func installDismissMonitors() {
        removeDismissMonitors()

        mouseMonitor = NSEvent.addGlobalMonitorForEvents(
            matching: [.leftMouseDown, .rightMouseDown, .otherMouseDown]
        ) { [weak self] _ in
            Task { @MainActor in
                self?.dismiss(reason: .outsideClick)
            }
        }

        workspaceObserver = NSWorkspace.shared.notificationCenter.addObserver(
            forName: NSWorkspace.didActivateApplicationNotification,
            object: nil,
            queue: .main
        ) { [weak self] note in
            // 助手自己被激活（比如弹出 Apple 语言包下载提示）不是用户切走了应用，
            // 这时收起面板会取消用户刚点的那一行。
            let activated = note.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication
            if activated?.processIdentifier == ProcessInfo.processInfo.processIdentifier {
                MainActor.assumeIsolated { self?.interceptionSuspended = true }
                return
            }
            MainActor.assumeIsolated {
                guard let self else { return }
                // 下载提示结束后焦点还给了弹出面板时的应用：恢复拦截，保留候选。
                if self.interceptionSuspended,
                   let origin = self.originProcessIdentifier,
                   activated?.processIdentifier == origin {
                    self.interceptionSuspended = false
                    return
                }
                self.dismiss(reason: .applicationSwitched)
            }
        }

        inputSourceObserver = DistributedNotificationCenter.default.addObserver(
            forName: NSNotification.Name(kTISNotifySelectedKeyboardInputSourceChanged as String),
            object: nil,
            queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.dismiss(reason: .inputSourceChanged)
            }
        }
    }

    private func removeDismissMonitors() {
        if let mouseMonitor {
            NSEvent.removeMonitor(mouseMonitor)
        }
        mouseMonitor = nil

        if let workspaceObserver {
            NSWorkspace.shared.notificationCenter.removeObserver(workspaceObserver)
        }
        workspaceObserver = nil

        if let inputSourceObserver {
            DistributedNotificationCenter.default.removeObserver(inputSourceObserver)
        }
        inputSourceObserver = nil
    }
}
