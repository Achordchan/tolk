import AppKit
import ApplicationServices
import Foundation
import TolkHelperCore

enum PermissionKind: String {
    case accessibility
    case screenRecording
}

/// 触发系统授权提示。第一次弹系统提示；用户已经拒绝过（再弹也没有反应）就直接打开对应的系统设置页。
@MainActor
enum PermissionRequester {
    private static func markerKey(_ kind: PermissionKind) -> String { "didRequest.\(kind.rawValue)" }

    static func isGranted(_ kind: PermissionKind) -> Bool {
        switch kind {
        case .accessibility: return AXIsProcessTrusted()
        case .screenRecording: return CGPreflightScreenCaptureAccess()
        }
    }

    static func request(_ kind: PermissionKind) {
        guard !isGranted(kind) else { return }
        let defaults = UserDefaults.standard
        let asked = defaults.bool(forKey: markerKey(kind))
        defaults.set(true, forKey: markerKey(kind))

        switch kind {
        case .accessibility:
            if asked {
                openSettings(kind)
            } else {
                let opts: NSDictionary = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as NSString: true]
                _ = AXIsProcessTrustedWithOptions(opts)
            }
        case .screenRecording:
            _ = CGRequestScreenCaptureAccess()
            if asked, !CGPreflightScreenCaptureAccess() {
                openSettings(kind)
            }
        }
    }

    static func openSettings(_ kind: PermissionKind) {
        switch kind {
        case .accessibility: GlobalHotkeyMonitor.openAccessibilitySettings()
        case .screenRecording: ScreenCapturePermission.openScreenRecordingSettings()
        }
    }
}

/// parent（Tolk.app）的身份：授权引导里要把它拖进系统设置的权限列表。
enum ParentApp {
    nonisolated(unsafe) static var pid: pid_t?

    static var runningApplication: NSRunningApplication? {
        pid.flatMap { NSRunningApplication(processIdentifier: $0) }
    }

    /// Tolk.app 的位置；拿不到（比如开发时直接从终端启动）就退回 helper 自己所在的位置。
    static var bundleURL: URL {
        runningApplication?.bundleURL ?? URL(fileURLWithPath: CommandLine.arguments.first ?? "/")
    }

    static var icon: NSImage {
        runningApplication?.icon ?? NSWorkspace.shared.icon(forFile: bundleURL.path)
    }
}

/// 权限引导窗口：定时刷新授权状态，缺哪个讲哪个。
@MainActor
final class PermissionGuideModel: ObservableObject {
    @Published var needsAccessibility = !AXIsProcessTrusted()
    @Published var needsScreenRecording = !CGPreflightScreenCaptureAccess()
    @Published var appearance: InputAssistAppearance = .system
    private var timer: Timer?

    func startRefreshing() {
        timer?.invalidate()
        timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.refresh() }
        }
    }

    func stopRefreshing() {
        timer?.invalidate()
        timer = nil
    }

    func refresh() {
        let accessibility = !AXIsProcessTrusted()
        let screen = !CGPreflightScreenCaptureAccess()
        if accessibility != needsAccessibility { needsAccessibility = accessibility }
        if screen != needsScreenRecording { needsScreenRecording = screen }
    }
}

@MainActor
final class PermissionGuideWindowController: NSObject, NSWindowDelegate {
    private var window: NSWindow?
    private let model = PermissionGuideModel()

    func show(appearance: InputAssistAppearance) {
        model.refresh()
        if let window {
            apply(appearance: appearance)
            NSApp.activate(ignoringOtherApps: true)
            window.makeKeyAndOrderFront(nil)
            return
        }
        let root = PermissionGuideContainer(
            model: model,
            onClose: { [weak self] in self?.close() }
        )
        let hosting = NSHostingView(rootView: root)
        hosting.sizingOptions = [] // 窗口尺寸写死；不让它把标题栏的安全区再算进窗口高度
        let window = NSWindow(
            contentRect: NSRect(origin: .zero, size: PermissionGuideView.windowSize),
            styleMask: [.titled, .closable, .fullSizeContentView],
            backing: .buffered,
            defer: false
        )
        window.title = "Tolk 权限"
        // 着色舞台一直铺到窗口顶，标题栏只留红绿灯；只有关闭一个按钮有用。
        window.titleVisibility = .hidden
        window.titlebarAppearsTransparent = true
        window.standardWindowButton(.miniaturizeButton)?.isHidden = true
        window.standardWindowButton(.zoomButton)?.isHidden = true
        window.contentView = hosting
        window.isReleasedWhenClosed = false
        window.delegate = self
        window.center()
        self.window = window
        apply(appearance: appearance)
        model.startRefreshing()
        NSApp.activate(ignoringOtherApps: true)
        window.makeKeyAndOrderFront(nil)
    }

    /// 跟随 Tolk 的主题。「跟随系统」时 `nsAppearance` 为 nil，系统切换深浅色会自己传下来。
    func apply(appearance: InputAssistAppearance) {
        model.appearance = appearance
        guard let window else { return }
        window.appearance = appearance.nsAppearance
        window.backgroundColor = NSColor(name: nil) { current in
            let isDark = current.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua
            return NSColor(ScreenshotPalette.make(appearance, isDark ? .dark : .light).stage)
        }
    }

    func close() {
        model.stopRefreshing()
        window?.orderOut(nil)
        window?.contentView = nil
        window = nil
    }

    func windowWillClose(_ notification: Notification) {
        model.stopRefreshing()
        window = nil
    }
}

import SwiftUI

private struct PermissionGuideContainer: View {
    @ObservedObject var model: PermissionGuideModel
    let onClose: () -> Void

    var body: some View {
        PermissionGuideView(
            needsAccessibility: model.needsAccessibility,
            needsScreenRecording: model.needsScreenRecording,
            showsScreenRecordingPermission: true,
            appearance: model.appearance,
            onOpenAccessibility: { PermissionRequester.openSettings(.accessibility) },
            onOpenScreenRecording: { PermissionRequester.openSettings(.screenRecording) },
            onClose: onClose
        )
    }
}
