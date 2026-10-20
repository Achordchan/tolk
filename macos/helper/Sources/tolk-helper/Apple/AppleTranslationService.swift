import AppKit
import SwiftUI
import Translation
import TolkHelperCore

/// `apple.status` / `apple.translate` 的实现：把 Tolk 的请求接到 `AppleTranslationCoordinator`。
///
/// `.translationTask` 必须挂在屏幕上的视图里才会跑，所以这里常驻一个看不见的宿主窗口
/// （和 bagayalu 的 InputAssistAppleTranslationPool 同一套做法）。
/// 需要下载语言包时，系统的下载确认框要附在窗口上，宿主窗口会临时显示出来。
@available(macOS 15.0, *)
@MainActor
final class AppleTranslationService {
    private let coordinator = AppleTranslationCoordinator()
    private var hostWindow: NSWindow?

    /// 协调器同一时刻只能处理一个请求（新请求会打断旧的），所以这里排队串行。
    private var queueTail: Task<Void, Never>?

    // MARK: - apple.status

    func status(sourceLanguageCode: String, targetLanguageCode: String) async -> String {
        let target = Locale.Language(identifier: Self.appleIdentifier(targetLanguageCode))
        let source: Locale.Language
        if sourceLanguageCode == LanguagePreset.auto.code {
            // 自动检测没有原文可判断：用英语（目标就是英语时用简体中文）估计。
            source = Locale.Language(identifier: targetLanguageCode == "en" ? "zh-Hans" : "en")
        } else {
            source = Locale.Language(identifier: Self.appleIdentifier(sourceLanguageCode))
        }
        let status = await LanguageAvailability().status(from: source, to: target)
        switch status {
        case .installed: return "installed"
        case .supported: return "supported"
        case .unsupported: return "unsupported"
        @unknown default: return "supported"
        }
    }

    // MARK: - apple.translate

    func translate(texts: [String], sourceLanguageCode: String, targetLanguageCode: String) async throws -> [String] {
        installHostWindowIfNeeded()
        // 排到上一个请求后面。
        let previous = queueTail
        let box = ResultBox()
        let task = Task { @MainActor [weak self] in
            await previous?.value
            guard let self else { return }
            do {
                var results: [String] = []
                results.reserveCapacity(texts.count)
                for text in texts {
                    if text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        results.append(text)
                        continue
                    }
                    let result = try await self.coordinator.translate(
                        text: text,
                        sourceLanguageCode: sourceLanguageCode,
                        targetLanguageCode: targetLanguageCode,
                        onPhaseChange: nil,
                        onLanguageDownloadStateChange: { [weak self] downloading in
                            Task { @MainActor in self?.setDownloadPromptVisible(downloading) }
                        }
                    )
                    results.append(result.translatedText)
                }
                box.result = .success(results)
            } catch {
                box.result = .failure(error)
            }
            self.setDownloadPromptVisible(false)
        }
        queueTail = task
        await task.value
        guard let result = box.result else { throw HelperTranslateError("Apple 本地翻译没有返回结果") }
        return try result.get()
    }

    private final class ResultBox {
        var result: Result<[String], Error>?
    }

    func shutdown() {
        coordinator.cancel()
        hostWindow?.orderOut(nil)
        hostWindow = nil
    }

    // MARK: - 宿主窗口

    private func installHostWindowIfNeeded() {
        if let hostWindow {
            hostWindow.orderFrontRegardless()
            return
        }
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 360, height: 140),
            styleMask: [.titled],
            backing: .buffered,
            defer: false
        )
        window.title = "Apple 本地翻译"
        window.isReleasedWhenClosed = false
        window.isExcludedFromWindowsMenu = true
        window.contentView = NSHostingView(rootView: AppleHostView(coordinator: coordinator))
        applyHiddenState(to: window)
        window.orderFrontRegardless()
        hostWindow = window
    }

    /// 平时：整个透明、不接收鼠标、压在桌面层，用户看不见。
    /// alphaValue 0 而不是 orderOut：窗口必须留在屏幕上，SwiftUI 才会持续驱动 `.translationTask`。
    private func applyHiddenState(to window: NSWindow) {
        window.alphaValue = 0
        window.ignoresMouseEvents = true
        window.level = NSWindow.Level(rawValue: Int(CGWindowLevelForKey(.desktopWindow)))
        window.collectionBehavior = [.canJoinAllSpaces, .stationary, .ignoresCycle]
    }

    private var previousFrontApp: NSRunningApplication?

    private func setDownloadPromptVisible(_ visible: Bool) {
        guard let window = hostWindow else { return }
        if visible {
            window.alphaValue = 1
            window.ignoresMouseEvents = false
            window.level = .floating
            window.collectionBehavior = [.moveToActiveSpace]
            window.center()
            if !NSApp.isActive {
                let front = NSWorkspace.shared.frontmostApplication
                if front?.processIdentifier != ProcessInfo.processInfo.processIdentifier {
                    previousFrontApp = front
                }
            }
            NSApp.activate(ignoringOtherApps: true)
            window.makeKeyAndOrderFront(nil)
        } else {
            applyHiddenState(to: window)
            // 下载提示是我们自己拉起来的，收起后把焦点还给原应用，
            // 否则输入助手随后的替换会因为前台不是目标应用而退化成复制。
            if NSApp.isActive, let app = previousFrontApp, !app.isTerminated {
                app.activate(options: [])
            }
            previousFrontApp = nil
        }
    }

    private static func appleIdentifier(_ code: String) -> String {
        switch code {
        case "zh-CN": return "zh-Hans"
        case "zh-TW": return "zh-Hant"
        default: return code
        }
    }
}

@available(macOS 15.0, *)
private struct AppleHostView: View {
    @ObservedObject var coordinator: AppleTranslationCoordinator

    var body: some View {
        VStack(spacing: 8) {
            ProgressView()
            Text("正在准备 Apple 本地翻译语言包…")
                .font(.system(size: 13))
            Text("请在系统弹出的窗口中确认下载。")
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
        }
        .frame(width: 360, height: 140)
        .appleTranslationSession(using: coordinator)
    }
}
