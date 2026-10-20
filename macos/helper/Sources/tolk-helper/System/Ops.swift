import AppKit
import Foundation
import TolkHelperCore

/// Go → helper 的请求分发（op 列表见 PROTOCOL.md）。
@MainActor
enum Ops {
    static let protocolVersion = "1"

    private static var screenshotStorage: Any?
    private static var appleStorage: Any?
    private static let guide = PermissionGuideWindowController()
    private static let hotkeys = GlobalHotkeyMonitor()
    private static var hotkeysWired = false
    private static let inputAssist = InputAssistCoordinator()

    @available(macOS 14.0, *)
    static var screenshot: ScreenshotOCRCoordinator {
        if let existing = screenshotStorage as? ScreenshotOCRCoordinator { return existing }
        let created = ScreenshotOCRCoordinator()
        screenshotStorage = created
        return created
    }

    @available(macOS 15.0, *)
    static var apple: AppleTranslationService {
        if let existing = appleStorage as? AppleTranslationService { return existing }
        let created = AppleTranslationService()
        appleStorage = created
        return created
    }

    static func closeAuxiliaryWindows() {
        guide.close()
        hotkeys.stop()
        inputAssist.shutdown()
        if #available(macOS 15.0, *) {
            apple.shutdown()
        }
    }

    static var supportsApple: Bool {
        if #available(macOS 15.1, *) { return true }
        return false
    }

    // MARK: - 分发

    static func handle(id: String, op: String, params: [String: JSONValue]) async -> Frame {
        do {
            let fields = try await run(op: op, params: params)
            return .success(id: id, fields)
        } catch let failure as OpFailure {
            return .failure(id: id, failure.message)
        } catch {
            return .failure(id: id, error.localizedDescription)
        }
    }

    private static func run(op: String, params: [String: JSONValue]) async throws -> [String: JSONValue] {
        switch op {
        case "hello":
            return hello()

        case "permissions":
            return [
                "accessibility": .bool(AXIsProcessTrusted()),
                "screenRecording": .bool(CGPreflightScreenCaptureAccess())
            ]

        case "permissions.request":
            guard let raw = params["kind"]?.stringValue, let kind = PermissionKind(rawValue: raw) else {
                throw OpFailure("kind 必须是 accessibility 或 screenRecording")
            }
            PermissionRequester.request(kind)
            return [:]

        case "permissions.guide":
            guide.show()
            return [:]

        case "screenshot.start":
            return try await screenshotStart(params)

        case "screenshot.cancel":
            if #available(macOS 14.0, *) {
                screenshot.cancelAll()
            }
            return [:]

        case "selection.get":
            let result = try await SelectionService.get()
            return ["text": .string(result.text), "app": .string(result.app)]

        case "selection.replace":
            guard let text = params["text"]?.stringValue else { throw OpFailure("缺少 text") }
            try await SelectionService.replace(with: text)
            return [:]

        case "apple.status":
            guard #available(macOS 15.1, *) else { throw OpFailure("Apple 本地翻译需要 macOS 15.1 或更高版本") }
            let (source, target) = try languages(params)
            let state = await apple.status(sourceLanguageCode: source, targetLanguageCode: target)
            return ["state": .string(state)]

        case "apple.translate":
            guard #available(macOS 15.1, *) else { throw OpFailure("Apple 本地翻译需要 macOS 15.1 或更高版本") }
            guard let texts = params["texts"]?.stringArray else { throw OpFailure("缺少 texts") }
            let (source, target) = try languages(params)
            guard target != LanguagePreset.auto.code else { throw OpFailure("Apple 本地翻译需要指定目标语言") }
            do {
                let translated = try await apple.translate(
                    texts: texts,
                    sourceLanguageCode: source,
                    targetLanguageCode: target
                )
                return ["texts": .array(translated.map { JSONValue.string($0) })]
            } catch let failure as OpFailure {
                throw failure
            } catch {
                throw OpFailure(error.localizedDescription)
            }

        case "hotkeys.configure":
            return await configureHotkeys(params)

        case "inputassist.configure":
            return configureInputAssist(params)

        case "inputassist.trigger":
            try inputAssist.triggerManually()
            return [:]

        case "legacy.read":
            return LegacyReader.read()

        case "quit":
            // 先把应答写出去再退出。
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.15) {
                MainActor.assumeIsolated { Lifecycle.terminate(reason: "收到 quit") }
            }
            return [:]

        default:
            throw OpFailure("不认识的操作：\(op)")
        }
    }

    // MARK: - 各操作

    private static func hello() -> [String: JSONValue] {
        let v = ProcessInfo.processInfo.operatingSystemVersion
        var macos = "\(v.majorVersion).\(v.minorVersion)"
        if v.patchVersion > 0 { macos += ".\(v.patchVersion)" }
        var capabilities: [JSONValue] = []
        if #available(macOS 14.0, *) { capabilities.append("ocr") }
        capabilities.append(contentsOf: ["pin", "selection"])
        if supportsApple { capabilities.append("apple") }
        capabilities.append("legacy")
        capabilities.append("hotkeys")
        capabilities.append("inputassist")
        return [
            "version": .string(protocolVersion),
            "macos": .string(macos),
            "capabilities": .array(capabilities)
        ]
    }

    private static func languages(_ params: [String: JSONValue]) throws -> (String, String) {
        func code(_ key: String) throws -> String {
            guard let label = params[key]?.stringValue else { throw OpFailure("缺少 \(key)") }
            guard let code = TolkLanguages.code(forLabel: label) else { throw OpFailure("不支持的语言：\(label)") }
            return code
        }
        return (try code("source"), try code("target"))
    }

    private static func screenshotStart(_ params: [String: JSONValue]) async throws -> [String: JSONValue] {
        guard #available(macOS 14.0, *) else {
            throw OpFailure("截图翻译需要 macOS 14 或更高版本")
        }
        let (source, target) = try languages(params)
        guard target != LanguagePreset.auto.code else { throw OpFailure("截图翻译需要指定目标语言") }
        let modeRaw = params["mode"]?.stringValue ?? "translate"
        guard let mode = ScreenshotOCRCoordinator.Mode(rawValue: modeRaw) else {
            throw OpFailure("mode 必须是 translate 或 ocr")
        }
        switch await screenshot.start(sourceLanguageCode: source, targetLanguageCode: target, mode: mode) {
        case .started:
            return [:]
        case .failed(let message, _):
            throw OpFailure(message)
        }
    }

    /// `inputassist.configure`：参数不对也回 `ok:true`，`error` 说明原因；
    /// `active` 回报的是**实际**状态——被拒绝的设置不会改动之前已经在运行的监听。
    private static func configureInputAssist(_ params: [String: JSONValue]) -> [String: JSONValue] {
        let status: InputAssistCoordinator.Status
        let rejection: String?
        do {
            let config = try InputAssistConfig.parse(params)
            status = inputAssist.configure(config)
            rejection = nil
        } catch let failure as InputAssistConfigError {
            status = inputAssist.currentStatus()
            rejection = failure.message
        } catch {
            status = inputAssist.currentStatus()
            rejection = error.localizedDescription
        }
        var fields: [String: JSONValue] = ["active": .bool(status.active)]
        if let message = rejection ?? status.error {
            fields["error"] = .string(message)
        }
        return fields
    }

    private static func configureHotkeys(_ params: [String: JSONValue]) async -> [String: JSONValue] {
        let doubleCopy = params["doubleCopy"]?.boolValue ?? false
        let screenshotEnabled = params["screenshot"]?.boolValue ?? false
        let windowMs = max(100, params["windowMs"]?.intValue ?? 550)
        let keyCode = params["screenshotKeyCode"]?.intValue ?? 7

        if !hotkeysWired {
            hotkeysWired = true
            hotkeys.onDoubleCopy = { changeCount in
                Bridge.emit("hotkey", ["name": "doubleCopy", "changeCount": .int(Int64(changeCount))])
            }
            hotkeys.onDoubleCut = {
                Bridge.emit("hotkey", ["name": "screenshot"])
            }
            hotkeys.onStopped = { reason in
                Bridge.emit("hotkey.stopped", ["error": .string(reason)])
            }
        }

        guard doubleCopy || screenshotEnabled else {
            hotkeys.stop()
            return ["running": .bool(false)]
        }
        guard AXIsProcessTrusted() else {
            hotkeys.stop()
            return ["running": .bool(false), "error": "缺少“辅助功能”权限，无法监听全局快捷键"]
        }

        hotkeys.doubleCopyEnabled = doubleCopy
        hotkeys.screenshotEnabled = screenshotEnabled
        hotkeys.start(windowMs: windowMs, doubleCutKeyCode: keyCode)
        // 已在运行时 start 直接返回（只更新了 windowMs / 键码和开关）。
        // 新启动时 tap 在后台线程里建，等它就绪。
        for _ in 0..<20 where hotkeys.isStarting {
            try? await Task.sleep(nanoseconds: 50_000_000)
        }
        if hotkeys.isRunning {
            return ["running": true]
        }
        return [
            "running": .bool(false),
            "error": .string(hotkeys.lastStartFailureMessage ?? "无法启动全局快捷键监听")
        ]
    }
}
