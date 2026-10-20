import AppKit
import Foundation
import ScreenCaptureKit
import TolkHelperCore

/// 截图 OCR 翻译的总调度。移植自 bagayalu 的 `ScreenshotOCRCoordinator`：
/// 选区、截图、Vision 识别、回贴译文、钉图的流程保持原样；
/// 翻译不再在 helper 里做，统一通过协议里的 helper → Go `translate` 请求完成；
/// 状态、结果和错误用 `screenshot.*` 事件汇报给 Tolk 主程序。
@available(macOS 14.0, *)
@MainActor
final class ScreenshotOCRCoordinator {
    enum Mode: String {
        /// 框选后自动识别并翻译（默认）。
        case translate
        /// 框选后只识别文字，复制到剪贴板。
        case ocr
    }

    enum StartResult {
        case started
        case failed(message: String, needs: String?)
    }

    private(set) var isRunning: Bool = false

    private var mode: Mode = .translate
    private var session: ScreenshotOCRSession?
    private var selectionWindow: ScreenshotSelectionWindow?
    private var pinnedWindows: [PinnedScreenshotWindow] = []
    private var globalKeyMonitor: Any?
    private var previousFrontmostAppPID: pid_t?

    /// 回贴图在后台画，同一时间只画一张；画的时候又来的请求合并成画完后的一次。
    private var overlayRenderTask: Task<Void, Never>?
    private var overlayNeedsRender = false
    /// 同一批识别结果多次重画时复用量过的样式；重新识别时换一个。
    private var overlayStyleCache = ScreenshotTranslationRenderer.StyleCache()

    // MARK: - 对外入口

    func start(sourceLanguageCode: String, targetLanguageCode: String, mode: Mode) async -> StartResult {
        if isRunning {
            cancelAll()
        }
        self.mode = mode

        // 截图依赖屏幕录制权限。没有就触发系统授权（第一次弹窗，之后打开系统设置），并告诉主程序缺什么。
        guard ScreenCapturePermission.hasPermission() else {
            PermissionRequester.request(.screenRecording)
            let message = "需要开启“屏幕录制”权限才能截图翻译"
            Bridge.emit("screenshot.error", ["error": .string(message), "needs": "screenRecording"])
            return .failed(message: message, needs: "screenRecording")
        }

        isRunning = true
        let frozen = await captureFrozenBackgrounds()
        // 取背景的这一会儿里可能已经被 cancel 了。
        guard isRunning else {
            return .failed(message: "截图已取消", needs: nil)
        }
        presentSelection(
            sourceLanguageCode: sourceLanguageCode,
            targetLanguageCode: targetLanguageCode,
            frozenBackgrounds: frozen
        )
        Bridge.emit("screenshot.state", ["state": "selecting"])
        return .started
    }

    /// 主程序要求取消：关掉选区层，不发事件（是它自己要取消的）。钉图窗口保留。
    func cancelAll() {
        overlayRenderTask?.cancel()
        overlayRenderTask = nil
        overlayNeedsRender = false

        session = nil
        selectionWindow?.close()
        selectionWindow = nil

        if let globalKeyMonitor {
            NSEvent.removeMonitor(globalKeyMonitor)
            self.globalKeyMonitor = nil
        }

        if let pid = previousFrontmostAppPID,
           pid != NSRunningApplication.current.processIdentifier,
           let app = NSRunningApplication(processIdentifier: pid) {
            app.activate(options: [.activateAllWindows])
        }
        previousFrontmostAppPID = nil

        isRunning = false
    }

    /// helper 退出前：连钉图一起关掉。
    func shutdown() {
        cancelAll()
        closeAllPinnedWindows()
    }

    // MARK: - 选区层

    private func presentSelection(
        sourceLanguageCode: String,
        targetLanguageCode: String,
        frozenBackgrounds: [ScreenshotOCRSession.FrozenBackground]
    ) {
        if let pid = NSWorkspace.shared.frontmostApplication?.processIdentifier,
           pid != NSRunningApplication.current.processIdentifier {
            previousFrontmostAppPID = pid
        } else {
            previousFrontmostAppPID = nil
        }

        // Vision 认不出的源语言退回自动检测：以前是退回英语，中日韩文字会被当成英文去认，结果整段乱码。
        let selectable = Set(VisionOCRService.selectableSourceLanguages.map(\.code))
        let source = selectable.contains(sourceLanguageCode)
            ? sourceLanguageCode
            : LanguagePreset.auto.code

        let session = ScreenshotOCRSession(
            sourceLanguageCode: source,
            targetLanguageCode: targetLanguageCode
        )
        session.frozenBackgrounds = frozenBackgrounds
        self.session = session

        let window = ScreenshotSelectionWindow(
            session: session,
            onCancel: { [weak self] in
                Task { @MainActor in
                    self?.userDismissed()
                }
            },
            onSelectionConfirmed: { [weak self] _ in
                Task { @MainActor in
                    await self?.captureSelectionIfPossible()
                }
            },
            onTranslateTapped: { [weak self] in
                Task { @MainActor in
                    await self?.runOCRAndTranslateIfPossible()
                }
            },
            onExtractTapped: { [weak self] in
                Task { @MainActor in
                    await self?.extractOCRToPasteboardIfPossible()
                }
            },
            onPinTapped: { [weak self] in
                Task { @MainActor in
                    await self?.pinSelectionIfPossible()
                }
            },
            onFinishTapped: { [weak self] in
                Task { @MainActor in
                    // 按钮这时是灰的；真点进来了也别往下走，什么都没复制就退出，用户会以为复制好了。
                    guard self?.session?.canExport == true else { return }
                    guard await self?.finishCaptureToPasteboardIfPossible() == true else { return }
                    self?.emitDone()
                    try? await Task.sleep(nanoseconds: 650_000_000)
                    self?.cancelAll()
                }
            }
        )

        selectionWindow = window

        // helper 是 accessory 进程，要先激活自己，选区层才能拿到键盘焦点（Esc）和第一下点击。
        NSApp.activate(ignoringOtherApps: true)
        window.orderFrontRegardless()
        // 让窗口先成为 key，避免出现“第一次点击只是激活窗口、第二次才开始框选”的体验。
        window.makeKey()
        if let cv = window.contentView {
            window.makeFirstResponder(cv)
        }

        // Esc 即使在其它应用前台也可以退出截图翻译。
        globalKeyMonitor = NSEvent.addGlobalMonitorForEvents(matching: .keyDown) { [weak self] event in
            guard let self else { return }
            if event.keyCode == 53 { // ESC
                Task { @MainActor in
                    if self.isRunning { self.userDismissed() }
                }
            }
        }
    }

    // MARK: - 结束时的事件

    /// 已经有可以交给主程序的识别结果。
    private var hasResult: Bool {
        guard let session, !session.ocrBlocks.isEmpty else { return false }
        return session.stage == .translated || session.stage == .ocrReady
    }

    /// 用户按 Esc、右键退出、或关掉结果：有结果就算完成，没有就是取消。
    private func userDismissed() {
        if hasResult {
            emitDone()
        } else {
            Bridge.emit("screenshot.cancelled", [:])
        }
        cancelAll()
    }

    /// `screenshot.done`：原文 / 译文都按段落用空行分隔。
    private func emitDone() {
        guard let session, !session.ocrBlocks.isEmpty else { return }
        let blocks = session.ocrBlocks
        var fields: [String: JSONValue] = [
            "text": .string(blocks.map(\.text).joined(separator: "\n\n"))
        ]
        // 真的翻译过才带 translated；只提取了原文就按 ocr 汇报。
        if session.stage == .translated, !session.translations.isEmpty {
            fields["mode"] = "translate"
            fields["translated"] = .string(
                blocks.map { session.translations[$0.id] ?? $0.text }.joined(separator: "\n\n")
            )
        } else {
            fields["mode"] = "ocr"
        }
        Bridge.emit("screenshot.done", fields)
    }

    private func reportError(_ message: String) {
        Bridge.emit("screenshot.error", ["error": .string(message)])
    }

    // MARK: - 截图、识别、翻译

    private func captureSelectionIfPossible() async {
        guard let session else { return }
        guard let selectionWindow else { return }

        let rectInScreen = selectionWindow.selectionRectInScreen().integral
        if rectInScreen.width < 8 || rectInScreen.height < 8 {
            return
        }

        // 框选完成后先截图缓存。
        session.resetResults()
        session.stage = .selected
        let generation = session.generation

        do {
            let image = try await captureImageExcludingOverlay(rect: rectInScreen, selectionWindow: selectionWindow)
            guard isCurrent(session, generation) else { return }
            session.capturedImage = image
        } catch {
            guard isCurrent(session, generation) else { return }
            session.stage = .failed(error.localizedDescription)
            session.showHUD(error.localizedDescription, style: .error)
            reportError(error.localizedDescription)
            return
        }

        // 协议里截图是一气呵成的：框完自动识别（+翻译）。
        switch mode {
        case .translate:
            await runOCRAndTranslateIfPossible()
        case .ocr:
            if await extractOCRToPasteboardIfPossible() {
                emitDone()
                try? await Task.sleep(nanoseconds: 650_000_000)
                guard isCurrent(session, generation) else { return }
                cancelAll()
            }
        }
    }

    /// 异步步骤回来时，选区和源语言还是发起时那一份吗。
    private func isCurrent(_ session: ScreenshotOCRSession, _ generation: Int) -> Bool {
        self.session === session && session.generation == generation
    }

    /// 识别文字并写进 session。没认出字、或结果已经过期时返回 false。
    private func recognizeText(in image: NSImage, session: ScreenshotOCRSession) async -> Bool {
        let generation = session.generation
        session.stage = .ocrRunning
        Bridge.emit("screenshot.state", ["state": "recognizing"])
        let blocks = await VisionOCRService.recognizeBlocks(from: image, languageCode: session.sourceLanguageCode)
        guard isCurrent(session, generation) else { return false }

        session.ocrBlocks = blocks
        session.translations = [:]
        session.translatedImage = nil
        session.overlayUnavailable = false
        overlayStyleCache = ScreenshotTranslationRenderer.StyleCache()
        session.translatedText = ""
        session.ocrText = blocks.map(\.text).joined(separator: "\n")
        if blocks.isEmpty {
            session.stage = .failed("未识别到文字")
            return false
        }
        session.stage = .ocrReady
        return true
    }

    /// 没有缓存截图时兜底再截一次（例如某些异常状态）。成功或本来就有图返回 true。
    private func ensureCapturedImage(_ session: ScreenshotOCRSession, in selectionWindow: ScreenshotSelectionWindow) async -> Bool {
        if session.capturedImage == nil {
            let rectInScreen = selectionWindow.selectionRectInScreen().integral
            if rectInScreen.width >= 8, rectInScreen.height >= 8 {
                do {
                    session.capturedImage = try await captureImageExcludingOverlay(rect: rectInScreen, selectionWindow: selectionWindow)
                } catch {
                    session.stage = .failed(error.localizedDescription)
                    session.showHUD(error.localizedDescription, style: .error)
                    reportError(error.localizedDescription)
                    return false
                }
            }
        }
        return session.capturedImage != nil
    }

    private func runOCRAndTranslateIfPossible() async {
        guard let session else { return }
        guard let selectionWindow else { return }

        if session.stage == .ocrReady || session.stage == .translated,
           !session.ocrText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            await runTranslateIfPossible()
            return
        }

        guard await ensureCapturedImage(session, in: selectionWindow), let image = session.capturedImage else { return }

        // 先 OCR
        let generation = session.generation
        guard await recognizeText(in: image, session: session) else {
            if isCurrent(session, generation) {
                session.showHUD("未识别到文字", style: .warning)
                reportError("未识别到文字")
            }
            return
        }

        // 再翻译
        await runTranslateIfPossible()
    }

    /// 提取原文并复制到剪贴板。成功返回 true。
    @discardableResult
    private func extractOCRToPasteboardIfPossible() async -> Bool {
        guard let session else { return false }
        guard let selectionWindow else { return false }

        // 已经有 OCR 文本就直接复制，不重复跑 OCR。
        let existing = session.ocrText.trimmingCharacters(in: .whitespacesAndNewlines)
        if !existing.isEmpty {
            let pb = NSPasteboard.general
            pb.clearContents()
            pb.setString(existing, forType: .string)
            session.didExtractTextToPasteboard = true
            session.showHUD("已提取原文并复制到剪贴板", style: .success)
            return true
        }

        guard await ensureCapturedImage(session, in: selectionWindow), let image = session.capturedImage else { return false }

        let generation = session.generation
        guard await recognizeText(in: image, session: session) else {
            if isCurrent(session, generation) {
                session.showHUD("未识别到文字", style: .warning)
                reportError("未识别到文字")
            }
            return false
        }
        let text = session.ocrText

        let pb = NSPasteboard.general
        pb.clearContents()
        pb.setString(text, forType: .string)
        session.didExtractTextToPasteboard = true
        session.showHUD("已提取原文并复制到剪贴板", style: .success)
        Bridge.emit("screenshot.state", ["state": "showing"])
        return true
    }

    private func pinSelectionIfPossible() async {
        guard let session else { return }
        guard let selectionWindow else { return }
        guard session.canExport else { return }

        guard await ensureCapturedImage(session, in: selectionWindow) else { return }
        // 翻完了钉的是贴了译文的图：钉图就是为了一边看译文一边做别的事。
        guard let export = session.imageForExport() else { return }
        let image = export.image

        let rectInScreen = selectionWindow.selectionRectInScreen().integral
        let maxW: CGFloat = 520
        let maxH: CGFloat = 360
        let w = image.size.width
        let h = image.size.height
        let scale = min(1.0, min(maxW / max(1, w), maxH / max(1, h)))
        let initialSize = CGSize(width: max(160, w * scale), height: max(120, h * scale))
        let initialRect = CGRect(
            x: rectInScreen.minX,
            y: rectInScreen.minY,
            width: initialSize.width,
            height: initialSize.height
        )

        var windowToRemove: PinnedScreenshotWindow?
        let pinned = PinnedScreenshotWindow(
            image: image,
            initialRect: initialRect,
            onRequestClose: { [weak self] in
                guard let self else { return }
                if let w = windowToRemove {
                    self.pinnedWindows.removeAll(where: { $0 === w })
                }
            },
            onRequestCloseAll: { [weak self] in
                self?.closeAllPinnedWindows()
            }
        )
        windowToRemove = pinned
        pinnedWindows.append(pinned)
        pinned.orderFrontRegardless()
        pinned.makeKey()
        if export.kind == .originalBecauseOverlayFailed {
            session.showHUD("译文图没画出来，钉的是原截图", style: .warning)
        } else {
            session.showHUD("已钉到屏幕", style: .success)
        }

        // 钉图后结束截图翻译（把识别结果交给主程序），但保留钉图窗口。
        emitDone()
        try? await Task.sleep(nanoseconds: 420_000_000)
        cancelAll()
    }

    private func closeAllPinnedWindows() {
        let windows = pinnedWindows
        pinnedWindows.removeAll()
        windows.forEach { $0.close() }
    }

    /// 「完成」：把选区里显示的图复制到剪贴板。复制成功（或有意跳过）返回 true。
    private func finishCaptureToPasteboardIfPossible() async -> Bool {
        guard let session else { return false }
        guard let selectionWindow else { return false }

        // 如果用户刚刚执行过“提取原文”，则“完成”不应再用图片覆盖剪贴板。
        if session.didExtractTextToPasteboard {
            session.showHUD("已提取原文：完成将不会覆盖剪贴板", style: .info)
            return true
        }

        guard await ensureCapturedImage(session, in: selectionWindow) else { return false }

        // 选区里显示的是哪张就复制哪张：翻完了是贴了译文的图。
        guard let export = session.imageForExport() else { return false }
        let image = export.image

        // 用 PNG 数据写入剪贴板，保证“原汁原味”的像素输出（避免某些情况下写 NSImage 导致边缘异常）。
        let pb = NSPasteboard.general
        pb.clearContents()
        if let data = pngData(from: image) {
            pb.setData(data, forType: .png)
        } else {
            pb.writeObjects([image])
        }
        switch export.kind {
        case .original:
            session.showHUD("截图已复制到剪贴板", style: .success)
        case .translated:
            session.showHUD("译文截图已复制到剪贴板", style: .success)
        case .originalBecauseOverlayFailed:
            session.showHUD("译文图没画出来，复制的是原截图", style: .warning)
        }
        return true
    }

    private func pngData(from image: NSImage) -> Data? {
        guard let cg = image.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
            return nil
        }
        let rep = NSBitmapImageRep(cgImage: cg)
        return rep.representation(using: .png, properties: [:])
    }

    private func captureImageExcludingOverlay(rect: CGRect, selectionWindow: ScreenshotSelectionWindow) async throws -> NSImage {
        let prevAlpha = selectionWindow.alphaValue
        let prevIgnoresMouse = selectionWindow.ignoresMouseEvents

        selectionWindow.alphaValue = 0
        selectionWindow.ignoresMouseEvents = true
        defer {
            selectionWindow.alphaValue = prevAlpha
            selectionWindow.ignoresMouseEvents = prevIgnoresMouse
        }

        // 给系统一点时间让窗口真正隐藏，否则仍可能被捕捉到边框。
        try? await Task.sleep(nanoseconds: 90_000_000)
        return try await ScreenRegionCapture.capture(rect: rect)
    }

    private func captureFrozenBackgrounds() async -> [ScreenshotOCRSession.FrozenBackground] {
        let virtualFrame = ScreenshotSelectionWindow.fullVirtualScreenFrame()
        if virtualFrame == .zero { return [] }

        var items: [ScreenshotOCRSession.FrozenBackground] = []
        items.reserveCapacity(NSScreen.screens.count)

        // 所有屏幕共用同一次 SCShareableContent 查询，避免每块屏都重新枚举全系统窗口。
        let content: SCShareableContent
        do {
            content = try await ScreenRegionCapture.shareableContent()
        } catch {
            Bridge.log(.warn, "冻结背景截图失败：\(error.localizedDescription)，将使用实时背景")
            return []
        }

        for screen in NSScreen.screens {
            let rectInScreen = screen.frame
            do {
                let image = try await ScreenRegionCapture.capture(
                    rect: rectInScreen,
                    content: content
                )
                // 转换到 selectionWindow content 坐标（左下原点）。
                let rectInContent = CGRect(
                    x: rectInScreen.minX - virtualFrame.minX,
                    y: rectInScreen.minY - virtualFrame.minY,
                    width: rectInScreen.width,
                    height: rectInScreen.height
                )
                items.append(.init(image: image, rect: rectInContent))
            } catch {
                Bridge.log(.warn, "冻结背景截图失败：\(error.localizedDescription)，将使用实时背景")
                return []
            }
        }

        return items
    }

    private func runTranslateIfPossible() async {
        guard let session else { return }
        let blocks = session.ocrBlocks
        guard !blocks.isEmpty else { return }
        let generation = session.generation
        let targetLanguageCode = session.targetLanguageCode

        session.stage = .translating
        session.translations = [:]
        session.translatedImage = nil
        session.overlayUnavailable = false
        Bridge.emit("screenshot.state", ["state": "translating"])

        // 按段翻译（折行已经拼回整段），每段各自判断源语言；已经是目标语言的段落原样保留。
        let sources = ScreenshotTranslationSourceResolver.resolve(
            blockTexts: blocks.map(\.text),
            sourceLanguageCode: session.sourceLanguageCode,
            targetLanguageCode: targetLanguageCode
        )
        var translations: [UUID: String] = [:]
        var jobs: [ScreenshotBlockTranslator.Job] = []
        for (block, source) in zip(blocks, sources) {
            if let source {
                jobs.append(.init(id: block.id, text: block.text, sourceLanguageCode: source))
            } else {
                translations[block.id] = block.text
            }
        }

        guard !jobs.isEmpty else {
            session.translations = translations
            session.translatedText = session.ocrText
            session.stage = .translated
            session.showHUD("选区里没有需要翻译成\(LanguagePreset.displayName(for: targetLanguageCode))的文字", style: .info)
            Bridge.emit("screenshot.state", ["state": "showing"])
            return
        }

        let translator = ScreenshotBlockTranslator(
            translate: { texts, sourceLanguageCode in
                await HelperTranslation.translate(
                    texts: texts,
                    sourceLanguageCode: sourceLanguageCode,
                    targetLanguageCode: targetLanguageCode
                )
            },
            followUpSource: { translation in
                ScreenshotTranslationSourceResolver.variantConversionSource(for: translation, targetLanguageCode: targetLanguageCode)
            }
        )
        let skipped = translations
        guard let outcome = await translator.run(
            jobs,
            shouldContinue: { [weak self] in self?.isCurrent(session, generation) ?? false },
            onProgress: { [weak self] progress in
                session.translations = skipped.merging(progress) { _, new in new }
                self?.scheduleOverlayRender()
            }
        ) else { return }
        guard isCurrent(session, generation) else { return }

        if outcome.succeeded == 0, let error = outcome.failures.first {
            session.translations = [:]
            session.translatedImage = nil
            session.stage = .failed(error.localizedDescription)
            session.showHUD(error.localizedDescription, style: .error)
            reportError(error.localizedDescription)
            return
        }

        translations.merge(outcome.translations) { _, new in new }
        session.translations = translations
        scheduleOverlayRender()
        session.translatedText = blocks.map { translations[$0.id] ?? $0.text }.joined(separator: "\n")
        session.stage = .translated
        Bridge.emit("screenshot.state", ["state": "showing"])
        if let warning = outcome.incompleteWarning(
            jobCount: jobs.count,
            targetName: LanguagePreset.displayName(for: targetLanguageCode)
        ) {
            session.showHUD(warning, style: .warning)
        }
    }

    /// 按当前的识别结果和译文重画回贴图。放在后台画；正在画的时候又来的请求合并成一次，画完再按最新的译文画。
    /// 画好的图对不上当前的选区（换了选区、换了源语言、重新识别了）就丢掉。
    /// 画的这段时间 `overlayRenderPending` 为 true，「钉到屏幕」「完成」是灰的，免得拿走上一批的图。
    private func scheduleOverlayRender() {
        overlayNeedsRender = true
        if let session, !session.overlayRenderPending {
            session.overlayRenderPending = true
        }
        guard overlayRenderTask == nil else { return }
        overlayRenderTask = Task { @MainActor [weak self] in
            while let self, self.overlayNeedsRender {
                self.overlayNeedsRender = false
                guard let session = self.session,
                      let image = session.capturedImage,
                      let cgImage = image.cgImage(forProposedRect: nil, context: nil, hints: nil) else { continue }
                let generation = session.generation
                let pointSize = image.size
                let input = ScreenshotTranslationRenderer.Input(
                    image: cgImage,
                    pointSize: pointSize,
                    blocks: session.ocrBlocks,
                    translations: session.translations,
                    cache: self.overlayStyleCache
                )
                let rendered = await Task.detached(priority: .userInitiated) {
                    ScreenshotTranslationRenderer.render(input)
                }.value
                guard self.isCurrent(session, generation) else { continue }
                session.applyOverlayRender(rendered.map { NSImage(cgImage: $0, size: pointSize) })
            }
            self?.overlayRenderTask = nil
            // 没有要画的了。换过会话的话旧会话已经扔掉，清当前这个就够。
            self?.session?.overlayRenderPending = false
        }
    }
}
