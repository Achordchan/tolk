import AppKit
import ApplicationServices
import Foundation

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

    func show() {
        model.refresh()
        if let window {
            NSApp.activate(ignoringOtherApps: true)
            window.makeKeyAndOrderFront(nil)
            return
        }
        let root = PermissionGuideContainer(
            model: model,
            onClose: { [weak self] in self?.close() }
        )
        let hosting = NSHostingView(rootView: root)
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 620, height: 590),
            styleMask: [.titled, .closable],
            backing: .buffered,
            defer: false
        )
        window.title = "Tolk 权限"
        window.contentView = hosting
        window.isReleasedWhenClosed = false
        window.delegate = self
        window.center()
        self.window = window
        model.startRefreshing()
        NSApp.activate(ignoringOtherApps: true)
        window.makeKeyAndOrderFront(nil)
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
            onOpenAccessibility: { PermissionRequester.openSettings(.accessibility) },
            onOpenScreenRecording: { PermissionRequester.openSettings(.screenRecording) },
            onClose: onClose
        )
    }
}
