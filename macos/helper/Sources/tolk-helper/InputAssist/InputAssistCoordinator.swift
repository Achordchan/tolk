import AppKit
import ApplicationServices
import Foundation
import TolkHelperCore

/// 一次候选会话：取词时冻结下来的一切，加上面板定位与会话标识。
struct InputAssistActiveSession {
    let id = UUID()
    let session: CandidateSession
    let capture: InputAssistCapture
    let anchorRect: CGRect

    /// 这个会话是不是就为**这一段**选区开的。
    /// 用来防止两条延迟路径（自动显示等 180ms、快捷键的 Chromium 重试等 200ms）为同一段选区各开一次。
    func matchesSelection(of other: InputAssistCapture) -> Bool {
        CFEqual(capture.element, other.element)
            && capture.sourceText == other.sourceText
            && capture.sourceRange == other.sourceRange
    }
}

/// 输入助手的主编排：`选中文本 → 快捷键 → 候选 → 精确替换 / 复制`。
///
/// 移植自 bagayalu 的 InputAssistCoordinator。和老版的区别：
/// - 设置来自 `inputassist.configure`（内存里的 `InputAssistConfig`）。
/// - 翻译走 helper → Go 的 `translate`（见 `InputAssistTranslator`）。
/// - 没有 ToastCenter / LogStore / 统计：提示用小浮层显示，日志发给 Go。
/// - 注册状态变化时发 `inputassist.status` 事件。
@MainActor
final class InputAssistCoordinator {
    struct Status: Equatable {
        var active: Bool
        var error: String?
    }

    static let missingPermissionMessage = "需要辅助功能权限才能读取和替换选中文本"

    private(set) var config = InputAssistConfig()

    private let hotkeyMonitor = InputAssistHotkeyMonitor()
    private let selectionMonitor = InputAssistSelectionMonitor()
    private let panelController = CandidatePanelController()
    private let feedbackPanelController = InputAssistFeedbackPanelController()
    private let translator = InputAssistTranslator()

    private var currentSession: InputAssistActiveSession?
    private var currentRequest: InputAssistTranslationRequest?
    private var translationTask: Task<Void, Never>?
    /// Chromium 打开 AX 树后的延迟重试。必须留住句柄——它睡着的这 200ms 里用户完全可能把功能关掉。
    private var chromiumRetryTask: Task<Void, Never>?
    /// 是否已经有一次重试在等对方建树。用户连按快捷键时靠它避免把在途的那次掐掉。
    private var isChromiumRetryInFlight = false
    private var retryTasks: [Task<Void, Never>] = []
    private var isCommitting = false

    private var lastReportedStatus: Status?
    private var statusTimer: Timer?
    private var didObserveActivation = false

    init() {
        hotkeyMonitor.onTrigger = { [weak self] in
            self?.handleShortcutTrigger()
        }
        selectionMonitor.onSelection = { [weak self] capture in
            self?.handleAutomaticSelection(capture)
        }
        selectionMonitor.isSelectionAllowed = { [weak self] in
            self?.isAutomaticSelectionAllowed() ?? false
        }

        panelController.onCommit = { [weak self] index in
            self?.commitCandidate(at: index)
        }
        panelController.onCopy = { [weak self] index in
            self?.copyCandidate(at: index)
        }
        panelController.onRetry = { [weak self] index in
            self?.retryCandidate(at: index)
        }
        panelController.onDismiss = { [weak self] reason in
            self?.handleDismiss(reason: reason)
        }
    }

    // MARK: - 配置与状态

    /// `inputassist.configure`：套用新设置，返回当前注册状态。
    func configure(_ newConfig: InputAssistConfig) -> Status {
        config = newConfig
        observeApplicationActivationIfNeeded()
        applyEnabledState()
        updateStatusTimer()
        let status = currentStatus()
        // 这次状态由应答带回，不再另发事件。
        lastReportedStatus = status
        return status
    }

    /// `inputassist.trigger`：与快捷键同一条路径。
    func triggerManually() throws {
        guard config.enabled else { throw OpFailure("输入助手未开启") }
        handleShortcutTrigger()
    }

    /// helper 退出前收摊。
    func shutdown() {
        statusTimer?.invalidate()
        statusTimer = nil
        hotkeyMonitor.unregister()
        selectionMonitor.stop()
        chromiumRetryTask?.cancel()
        chromiumRetryTask = nil
        isChromiumRetryInFlight = false
        cancelInFlightTranslations()
        panelController.dismiss(reason: .featureDisabled)
        feedbackPanelController.hide()
    }

    /// 当前真实的运行状态。设置被拒绝时用它回报：之前的快捷键和监听还在原样运行。
    func currentStatus() -> Status {
        guard config.enabled else { return Status(active: false, error: nil) }
        guard AXIsProcessTrusted() else {
            return Status(active: false, error: Self.missingPermissionMessage)
        }
        if hotkeyMonitor.status.isActive {
            return Status(active: true, error: nil)
        }
        return Status(
            active: false,
            error: "快捷键 \(config.shortcut.displayString) 注册失败，可能被其它应用占用"
        )
    }

    private func applyEnabledState() {
        guard config.enabled else {
            hotkeyMonitor.unregister()
            selectionMonitor.stop()
            chromiumRetryTask?.cancel()
            chromiumRetryTask = nil
            isChromiumRetryInFlight = false
            cancelInFlightTranslations()
            panelController.dismiss(reason: .featureDisabled)
            feedbackPanelController.hide()
            return
        }

        guard AXIsProcessTrusted() else {
            hotkeyMonitor.unregister()
            selectionMonitor.stop()
            Bridge.log(.warn, "输入助手未启动：尚未获得辅助功能权限")
            return
        }

        if !(hotkeyMonitor.status.isActive && hotkeyMonitor.registeredShortcut == config.shortcut) {
            hotkeyMonitor.register(config.shortcut)
            if !hotkeyMonitor.status.isActive {
                Bridge.log(.warn, "输入助手快捷键 \(config.shortcut.displayString) 注册失败，可能被其它应用占用")
            }
        }

        if config.autoShow {
            if !selectionMonitor.start() {
                Bridge.log(.warn, "输入助手无法开始监听选中文字")
            }
        } else {
            selectionMonitor.stop()
        }
    }

    private func observeApplicationActivationIfNeeded() {
        guard !didObserveActivation else { return }
        didObserveActivation = true
        NotificationCenter.default.addObserver(
            forName: NSApplication.didBecomeActiveNotification,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.reapplyIfPermissionChanged()
            }
        }
    }

    /// helper 是 accessory 进程，很少变成前台：用户在系统设置里给了（或收回）辅助功能权限，
    /// 靠定时检查才能及时发现。只在功能开启时跑。
    private func updateStatusTimer() {
        guard config.enabled else {
            statusTimer?.invalidate()
            statusTimer = nil
            return
        }
        guard statusTimer == nil else { return }
        statusTimer = Timer.scheduledTimer(withTimeInterval: 2, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.reapplyIfPermissionChanged()
            }
        }
    }

    /// 权限状态和注册状态对不上时重新套用一次，并在状态变化时发 `inputassist.status`。
    private func reapplyIfPermissionChanged() {
        guard config.enabled else { return }
        let trusted = AXIsProcessTrusted()
        let registered = hotkeyMonitor.status.isActive
        if (trusted && !registered) || (!trusted && registered) {
            applyEnabledState()
        }
        let status = currentStatus()
        guard status != lastReportedStatus else { return }
        lastReportedStatus = status
        var fields: [String: JSONValue] = ["active": .bool(status.active)]
        if let error = status.error {
            fields["error"] = .string(error)
        }
        Bridge.emit("inputassist.status", fields)
    }

    // MARK: - 触发

    private func handleShortcutTrigger() {
        guard config.enabled else { return }
        guard !isCommitting else { return }
        guard AXIsProcessTrusted() else {
            showMessage(
                "选中文本翻译需要辅助功能权限",
                style: .warning,
                anchorRect: InputAssistScreen.mouseAnchor()
            )
            return
        }

        let identity = InputAssistAppIdentity.frontmost()
        guard allowsApplication(identity) else {
            Bridge.log(.info, "选区翻译已跳过当前应用（按「应用范围」设置）")
            // 用户在被排除的应用里按快捷键，什么都不发生的话，他无从知道是被自己的设置挡掉的。
            showMessage(
                "当前应用在选区翻译的排除列表中",
                style: .info,
                anchorRect: InputAssistScreen.mouseAnchor()
            )
            return
        }

        if let capture = captureTranslatableSelection() {
            startSession(capture: capture, identity: identity)
            return
        }

        // 取不到不代表用户没选中。Chromium / Electron 应用要先被明确告知
        // 「有人要用辅助功能」才会把树建起来，在那之前这里必然是空的。

        // 已经有一次重试在等对方建树时，**让它跑完**：取消再新建只会更慢，
        // 因为第一次已经把这个进程记成"已打开"，新任务里 `enableIfNeeded` 直接返回 false。
        guard !isChromiumRetryInFlight else { return }

        // 把**当初通过检查的那个进程**钉下来。等一下重试时如果前台已经换了应用，
        // 不能拿这次批准过的 identity 去给新应用背书。
        guard let pid = NSWorkspace.shared.frontmostApplication?.processIdentifier else {
            showMessage(
                "请先选择要翻译的文字",
                style: .warning,
                anchorRect: InputAssistScreen.mouseAnchor()
            )
            return
        }

        isChromiumRetryInFlight = true
        chromiumRetryTask?.cancel()
        chromiumRetryTask = Task { @MainActor [weak self] in
            await self?.retryAfterEnablingChromiumAccessibility(pid: pid)
        }
    }

    /// 读取选中的文字；读到了但不值得翻译（网址、邮箱、纯数字符号）等同于没读到。
    private func captureTranslatableSelection() -> InputAssistCapture? {
        guard let capture = InputAssistAXTextCapture.captureSelectedText() else { return nil }
        guard InputAssistTextFilter.looksTranslatable(capture.sourceText) else { return nil }
        return capture
    }

    /// 打开目标应用的辅助功能树后重试一次取词。
    ///
    /// 只在第一次取词失败时走这条路——打开别人进程的 AX 树是有开销的，不该对每个切换到的应用都默认施加。
    private func retryAfterEnablingChromiumAccessibility(pid: pid_t) async {
        defer { isChromiumRetryInFlight = false }

        guard !isCommitting else { return }
        // 前台必须还是触发时那个进程。用户在这中间切走的话，继续下去就等于用刚才批准过的身份去读另一个应用——
        // 而那个应用完全可能在黑名单里（默认就有终端、钥匙串、密码管理器）。
        guard NSWorkspace.shared.frontmostApplication?.processIdentifier == pid else { return }
        guard InputAssistChromiumAccessibility.enableIfNeeded(pid: pid) else {
            // 这个应用已经开过了、或者根本不支持这个属性，重试没有意义。
            // 开过之后仍然读不到，说明是这个控件不暴露选中文本，再叫用户"先选择文字"就是误导。
            showMessage(
                InputAssistChromiumAccessibility.hasSettledResult(pid: pid)
                    ? "当前应用没有提供选中文本，可能需要重新选择一次"
                    : "请先选择要翻译的文字",
                style: .warning,
                anchorRect: InputAssistScreen.mouseAnchor()
            )
            return
        }

        Bridge.log(
            .info,
            "Input Assist 请求 \(InputAssistAppIdentity.frontmost()?.localizedName ?? "当前应用")"
                + " 打开辅助功能树后重试取词"
        )
        try? await Task.sleep(nanoseconds: InputAssistChromiumAccessibility.settleNanoseconds)

        guard !Task.isCancelled else { return }
        // 这 200ms 里用户可能已经把功能关掉了。
        guard config.enabled else { return }
        guard !isCommitting else { return }
        guard NSWorkspace.shared.frontmostApplication?.processIdentifier == pid else { return }

        // 应用范围**重新查一遍**，并且用现在读到的身份，而不是触发时那份。
        let identity = InputAssistAppIdentity.frontmost()
        guard allowsApplication(identity) else {
            Bridge.log(.info, "选区翻译已跳过当前应用（按「应用范围」设置）")
            return
        }

        guard let capture = captureTranslatableSelection() else {
            Bridge.log(.warn, "选区翻译未能读取选中的文字")
            showMessage(
                "已为当前应用开启辅助功能读取，请重新选中一次文字",
                style: .info,
                anchorRect: InputAssistScreen.mouseAnchor()
            )
            return
        }

        // 自动显示那条路可能已经先一步为**同一段选区**开好面板了，再开一次会闪一下并把同一段文字翻译两遍。
        if let currentSession, currentSession.matchesSelection(of: capture) {
            return
        }

        startSession(capture: capture, identity: identity)
    }

    private func handleAutomaticSelection(_ capture: InputAssistCapture) {
        guard config.enabled, config.autoShow else { return }
        guard !isCommitting, !panelController.isVisible else { return }
        guard InputAssistTextFilter.looksTranslatable(capture.sourceText) else { return }
        let identity = InputAssistAppIdentity.frontmost()
        guard allowsApplication(identity) else { return }

        startSession(capture: capture, identity: identity)
    }

    private func isAutomaticSelectionAllowed() -> Bool {
        guard config.enabled, config.autoShow else { return false }
        guard !isCommitting, !panelController.isVisible else { return false }
        return allowsApplication(InputAssistAppIdentity.frontmost())
    }

    private func allowsApplication(_ identity: InputAssistAppIdentity?) -> Bool {
        InputAssistAppFilter.allows(
            identity,
            scope: config.scope,
            blocklist: config.blocklist,
            allowlist: config.allowlist
        )
    }

    // MARK: - 会话

    private func detectSourceLanguage(of text: String) -> String? {
        let detected = LanguageDetectionService.shared
            .detectLanguage(in: text)?
            .languageCode
        let presence = TextScriptPresence(in: text)
        return InputAssistLanguagePolicy.selectionSourceLanguageCode(
            scripts: InputAssistScriptFlags(
                containsHan: presence.containsHan,
                containsKana: presence.containsKana,
                containsHangul: presence.containsHangul,
                containsBopomofo: presence.containsBopomofo
            ),
            detectedLanguageCode: detected
        )
    }

    private func startSession(capture: InputAssistCapture, identity: InputAssistAppIdentity?) {
        // 过滤检查和取词之间用户可能已经切到别的应用：取到的控件必须属于当初批准的进程，
        // 否则（比如排除列表里的应用）里的文字会被送去翻译。
        var ownerPid: pid_t = 0
        if AXUIElementGetPid(capture.element, &ownerPid) == .success, ownerPid > 0 {
            if let approved = identity?.processIdentifier {
                guard approved == ownerPid else {
                    Bridge.log(.info, "选区翻译：取词时前台应用已切换，放弃这次触发")
                    return
                }
            } else if let owner = NSRunningApplication(processIdentifier: ownerPid),
                      !allowsApplication(InputAssistAppIdentity(application: owner)) {
                Bridge.log(.info, "选区翻译：选区所在应用在排除范围内，放弃这次触发")
                return
            }
        }
        cancelInFlightTranslations()
        if panelController.isVisible {
            panelController.dismiss(reason: .replacedByNewSession)
        }

        let anchorRect = InputAssistAnchor.rect(element: capture.element, range: capture.sourceRange)

        let detectedSourceLanguage = detectSourceLanguage(of: capture.sourceText)
        let visibleTargets = InputAssistLanguagePolicy.visibleTargets(
            config.targetCodes,
            sourceLanguageCode: detectedSourceLanguage
        )
        guard !visibleTargets.isEmpty else {
            showMessage("选中文本已是配置的目标语言", style: .info, anchorRect: anchorRect)
            return
        }

        // 让自动取词知道这段选区已经处理过了。少了这一步，用快捷键打开的面板被 Esc 关掉之后会立刻自己弹回来
        // （Esc 的 key-up 会漏到选区监听，而那时它还没记过这段选区）。
        selectionMonitor.registerSelection(capture)

        let session = InputAssistActiveSession(
            session: CandidateSession(
                appProcessIdentifier: identity?.processIdentifier,
                capture: capture
            ),
            capture: capture,
            anchorRect: anchorRect
        )
        let request = InputAssistTranslationRequest(
            sourceText: capture.sourceText,
            sourceLanguageCode: detectedSourceLanguage ?? TolkLanguages.autoCode,
            targetLanguageCodes: visibleTargets
        )

        currentSession = session
        currentRequest = request

        guard panelController.show(
            languageCodes: visibleTargets,
            sourceText: capture.sourceText,
            anchorRect: anchorRect,
            appearance: config.appearance,
            showsCacheBadge: config.cacheBadge,
            commitMode: commitMode(for: session.session)
        ) else {
            Bridge.log(.error, "选区翻译无法接管候选快捷键，已取消本次操作")
            currentSession = nil
            currentRequest = nil
            return
        }

        let sessionID = session.id
        translationTask = translator.start(request: request) { [weak self] languageCode, state in
            guard let self, self.currentSession?.id == sessionID else { return }
            self.panelController.updateRow(languageCode: languageCode, state: state)
        }
    }

    private func commitMode(for session: CandidateSession) -> InputAssistCommitMode {
        InputAssistCommitPolicy.mode(
            capability: session.capability,
            allowsEditorPaste: session.allowsEditorPaste,
            hasSourceRange: session.sourceRange != nil,
            hasElementValue: session.elementValueAtCapture != nil
        )
    }

    // MARK: - 提交

    private func commitCandidate(at index: Int) {
        guard !isCommitting else { return }
        guard let active = currentSession,
              let translatedText = panelController.translatedText(at: index)
        else {
            return
        }
        let session = active.session
        let anchorRect = active.anchorRect

        isCommitting = true
        panelController.dismiss(reason: .committed)
        cancelInFlightTranslations()

        Task { @MainActor [weak self] in
            guard let self else { return }
            defer { self.isCommitting = false }

            let mode = self.commitMode(for: session)

            if mode == .axReplace {
                var lastOutcome = await InputAssistTextReplaceEngine.replace(
                    session: session,
                    with: translatedText
                )
                let outcome = lastOutcome
                if self.handleReplacementOutcome(outcome) {
                    self.clearSessionIfCurrent(active)
                    return
                }
                if session.allowsEditorPaste, outcome.allowsPasteFallback {
                    let pasteOutcome = await InputAssistEditorPasteEngine.replace(
                        session: session,
                        with: translatedText
                    )
                    if self.handleReplacementOutcome(pasteOutcome) {
                        self.clearSessionIfCurrent(active)
                        return
                    }
                    if pasteOutcome.preservesForeignClipboard {
                        self.reportClipboardContention(anchorRect: anchorRect)
                        self.clearSessionIfCurrent(active)
                        return
                    }
                    lastOutcome = pasteOutcome
                }
                // 写出去了但读不回来时，不能说"无法替换"——它可能已经生效了。
                self.copyTranslatedText(
                    translatedText,
                    message: lastOutcome.allowsPasteFallback
                        ? "无法安全替换，译文已复制"
                        : "无法确认替换是否生效，译文已复制",
                    style: .warning,
                    anchorRect: anchorRect
                )
            } else if mode == .editorPaste {
                let outcome = await InputAssistEditorPasteEngine.replace(
                    session: session,
                    with: translatedText
                )
                if self.handleReplacementOutcome(outcome) {
                    self.clearSessionIfCurrent(active)
                    return
                }
                if outcome.preservesForeignClipboard {
                    self.reportClipboardContention(anchorRect: anchorRect)
                    self.clearSessionIfCurrent(active)
                    return
                }
                self.copyTranslatedText(
                    translatedText,
                    message: outcome.allowsPasteFallback
                        ? "编辑器拒绝替换，译文已复制"
                        : "无法确认替换是否生效，译文已复制",
                    style: .warning,
                    anchorRect: anchorRect
                )
            } else {
                self.copyTranslatedText(
                    translatedText,
                    message: "当前应用不支持原位替换，译文已复制",
                    style: .info,
                    anchorRect: anchorRect
                )
            }

            self.clearSessionIfCurrent(active)
        }
    }

    /// 返回 true 表示已经成功完成原位提交。
    private func handleReplacementOutcome(_ outcome: InputAssistReplacementOutcome) -> Bool {
        switch outcome {
        case .replaced(let strategy):
            Bridge.log(.info, "选区翻译已替换原文（\(strategy.rawValue)）")
            return true
        case .aborted(let reason):
            Bridge.log(.warn, "选区翻译放弃替换原文：\(reason.rawValue)")
            return false
        case .failed(let message):
            Bridge.log(.warn, "选区翻译无法替换原文：\(message)")
            return false
        }
    }

    private func copyCandidate(at index: Int) {
        guard let text = panelController.translatedText(at: index),
              let active = currentSession else { return }
        let anchorRect = active.anchorRect
        panelController.dismiss(reason: .copied)
        copyTranslatedText(text, message: "已复制译文", style: .success, anchorRect: anchorRect)
    }

    /// 剪贴板正被别人改写时的收尾。
    ///
    /// **刻意不复制译文**：粘贴引擎正是为了保住那份新内容才失败返回的，
    /// 这里再 `clearContents()` 就把它亲手毁掉了。
    private func reportClipboardContention(anchorRect: CGRect) {
        showMessage("剪贴板正被其它应用使用，本次未替换，请稍后重试", style: .warning, anchorRect: anchorRect)
    }

    private func copyTranslatedText(
        _ text: String,
        message: String,
        style: InputAssistFeedbackPanelController.Style,
        anchorRect: CGRect
    ) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        // 复制类提示：成功用对勾，其余用「复制」图标（与老版一致）。
        let shown: InputAssistFeedbackPanelController.Style = style == .success ? .success : .copied
        showMessage(message, style: shown, anchorRect: anchorRect)
    }

    private func showMessage(
        _ message: String,
        style: InputAssistFeedbackPanelController.Style,
        anchorRect: CGRect
    ) {
        feedbackPanelController.show(
            message: message,
            style: style,
            anchorRect: anchorRect,
            appearance: config.appearance
        )
    }

    // MARK: - 重试与收尾

    private func retryCandidate(at index: Int) {
        guard let request = currentRequest,
              let languageCode = panelController.languageCode(at: index),
              let active = currentSession
        else {
            return
        }
        let sessionID = active.id
        let task = translator.retry(
            request: request,
            targetLanguageCode: languageCode
        ) { [weak self] code, state in
            guard let self, self.currentSession?.id == sessionID else { return }
            self.panelController.updateRow(languageCode: code, state: state)
        }
        retryTasks.removeAll { $0.isCancelled }
        retryTasks.append(task)
    }

    private func handleDismiss(reason: CandidateDismissReason) {
        guard reason != .committed else { return }
        cancelInFlightTranslations()
        currentSession = nil
        currentRequest = nil
    }

    private func clearSessionIfCurrent(_ session: InputAssistActiveSession) {
        guard currentSession?.id == session.id else { return }
        currentSession = nil
        currentRequest = nil
    }

    private func cancelInFlightTranslations() {
        translationTask?.cancel()
        translationTask = nil
        for task in retryTasks {
            task.cancel()
        }
        retryTasks.removeAll()
    }
}
