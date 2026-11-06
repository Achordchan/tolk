import AppKit
import SwiftUI
import TolkHelperCore

/// 点了「打开…设置」之后，贴在系统设置窗口正下方的一条小浮条，里面是能拖的 Tolk 图标。
/// 系统设置挪到哪它跟到哪，不用在两个窗口之间来回找。
///
/// 系统设置的窗口位置用 `CGWindowListCopyWindowInfo` 读：位置和所属进程不需要任何权限
/// （只有窗口标题要屏幕录制权限，这里用不到），所以缺辅助功能、缺屏幕录制时都能用。
/// 浮条是不激活的面板：从它上面拖图标时，系统设置一直在前台，列表能直接接住。
@MainActor
final class PermissionDragPanelController {
    private static let settingsBundleID = "com.apple.systempreferences"
    private static let height: CGFloat = 72
    private static let gap: CGFloat = 4 // 浮条四周留了 6pt 给阴影，看上去离窗口 10pt

    private var panel: NSPanel?
    private var timer: Timer?
    private var kind: PermissionKind?
    private var appearance: InputAssistAppearance = .system
    private var lastGrantCheck = Date.distantPast
    private var shown: Bool?

    /// 开始跟着系统设置；这一项授权了、引导窗口关了，或者换成另一项时结束。
    func follow(_ kind: PermissionKind, appearance: InputAssistAppearance) {
        self.kind = kind
        self.appearance = appearance
        let panel = self.panel ?? makePanel()
        self.panel = panel
        panel.contentView = NSHostingView(rootView: PermissionDragStrip(kind: kind, appearance: appearance))
        panel.appearance = appearance.nsAppearance
        if timer == nil {
            // 放进 common 模式：拖拽进行中 run loop 在 event tracking 模式，计时器也得照走。
            let timer = Timer(timeInterval: 1.0 / 60, repeats: true) { [weak self] _ in
                MainActor.assumeIsolated { self?.tick() }
            }
            RunLoop.main.add(timer, forMode: .common)
            self.timer = timer
        }
        tick()
    }

    func apply(appearance: InputAssistAppearance) {
        guard let kind, panel != nil else { return }
        follow(kind, appearance: appearance)
    }

    func stop() {
        timer?.invalidate()
        timer = nil
        kind = nil
        shown = nil
        panel?.orderOut(nil)
        panel?.contentView = nil
        panel = nil
    }

    private func makePanel() -> NSPanel {
        let panel = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: 420, height: Self.height),
            styleMask: [.borderless, .nonactivatingPanel],
            backing: .buffered,
            defer: true
        )
        panel.isFloatingPanel = true
        panel.level = .floating
        panel.hidesOnDeactivate = false
        panel.becomesKeyOnlyIfNeeded = true
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = false // 阴影由 SwiftUI 画，贴着圆角
        panel.collectionBehavior = [.fullScreenAuxiliary, .transient, .ignoresCycle]
        panel.isReleasedWhenClosed = false
        panel.alphaValue = 0
        return panel
    }

    private func tick() {
        guard let kind, let panel else { return }

        if Date().timeIntervalSince(lastGrantCheck) > 0.5 {
            lastGrantCheck = Date()
            // 屏幕录制开好后要重新打开 Tolk 才算数，那一步在引导窗口里，浮条到这里就可以收了。
            if PermissionRequester.isGranted(kind)
                || (kind == .screenRecording && ScreenCapturePermission.grantedPendingRelaunch()) {
                stop()
                return
            }
        }

        guard isRelevantAppFrontmost(), let frame = Self.settingsWindowFrame() else {
            fade(panel, visible: false)
            return
        }
        let target = Self.panelFrame(below: frame)
        if panel.frame != target {
            panel.setFrame(target, display: true)
        }
        fade(panel, visible: true)
    }

    /// 系统设置在前台时显示；用户回到 Tolk 的引导窗口看说明时也留着。切到别的应用就收起来。
    private func isRelevantAppFrontmost() -> Bool {
        guard let front = NSWorkspace.shared.frontmostApplication else { return false }
        return front.bundleIdentifier == Self.settingsBundleID
            || front.processIdentifier == ProcessInfo.processInfo.processIdentifier
            || front.processIdentifier == ParentApp.pid
    }

    private func fade(_ panel: NSPanel, visible: Bool) {
        guard shown != visible else { return }
        shown = visible
        if visible { panel.orderFrontRegardless() }
        NSAnimationContext.runAnimationGroup { context in
            context.duration = visible ? 0.18 : 0.12
            panel.animator().alphaValue = visible ? 1 : 0
        } completionHandler: { [weak self] in
            MainActor.assumeIsolated {
                if self?.shown == false { panel.orderOut(nil) }
            }
        }
    }

    /// 系统设置主窗口在屏幕上的位置（AppKit 坐标，原点在主屏左下）。不在当前桌面、最小化、没开时为 nil。
    static func settingsWindowFrame() -> CGRect? {
        guard let app = NSRunningApplication.runningApplications(withBundleIdentifier: settingsBundleID).first,
              let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID)
                as? [[String: Any]] else { return nil }
        let windows = list.compactMap { info -> CGRect? in
            guard (info[kCGWindowOwnerPID as String] as? pid_t) == app.processIdentifier,
                  (info[kCGWindowLayer as String] as? Int) == 0,
                  (info[kCGWindowAlpha as String] as? Double ?? 1) > 0,
                  let bounds = info[kCGWindowBounds as String] as? NSDictionary,
                  let rect = CGRect(dictionaryRepresentation: bounds),
                  rect.width > 200, rect.height > 200 else { return nil }
            return rect
        }
        // 只有一个主窗口；万一有别的（比如弹出的表单），取最大的那个。
        guard let top = windows.max(by: { $0.width * $0.height < $1.width * $1.height }) else { return nil }
        // CGWindow 的原点在主屏左上、y 向下；换成 AppKit 的。
        let primaryHeight = NSScreen.screens.first?.frame.height ?? 0
        return CGRect(x: top.minX, y: primaryHeight - top.maxY, width: top.width, height: top.height)
    }

    /// 窗口正下方、和窗口一样宽（有上下限）。下面放不下就收进窗口底部里面。
    static func panelFrame(below window: CGRect) -> CGRect {
        let width = min(max(window.width - 40, 340), 520)
        var frame = CGRect(
            x: window.midX - width / 2,
            y: window.minY - gap - height,
            width: width,
            height: height
        )
        let screen = NSScreen.screens.first { $0.frame.intersects(window) } ?? NSScreen.main
        if let visible = screen?.visibleFrame {
            if frame.minY < visible.minY {
                frame.origin.y = window.minY + 14
            }
            frame.origin.x = min(max(frame.minX, visible.minX + 8), visible.maxX - width - 8)
        }
        frame.origin = CGPoint(x: frame.minX.rounded(), y: frame.minY.rounded())
        return frame
    }
}

/// 浮条本身：左边是能拖的图标，右边一句话加一个朝上的箭头，指向系统设置里的列表。
private struct PermissionDragStrip: View {
    let kind: PermissionKind
    let appearance: InputAssistAppearance

    @Environment(\.colorScheme) private var colorScheme

    private var palette: ScreenshotPalette { .make(appearance, colorScheme) }

    var body: some View {
        HStack(spacing: 14) {
            Image(nsImage: ParentApp.icon)
                .resizable()
                .scaledToFit()
                .frame(width: 44, height: 44)
                .shadow(color: .black.opacity(0.18), radius: 6, x: 0, y: 3)
                .overlay(alignment: .bottomTrailing) {
                    Image(systemName: "hand.draw.fill")
                        .font(.system(size: 9, weight: .bold))
                        .foregroundStyle(.white)
                        .frame(width: 18, height: 18)
                        .background(Circle().fill(palette.accentFill))
                        .overlay(Circle().strokeBorder(palette.card, lineWidth: 1.5))
                        .offset(x: 4, y: 4)
                }
                .help("将 Tolk 拖到系统设置的权限列表")
                .overlay {
                    PermissionGuideApplicationDragSource(
                        applicationURL: ParentApp.bundleURL,
                        toolTip: "将 Tolk 拖到系统设置的权限列表"
                    )
                }

            VStack(alignment: .leading, spacing: 2) {
                Text("把 Tolk 拖进上面的列表")
                    .font(.system(size: 13, weight: .semibold))
                Text(kind == .accessibility ? "再打开它右边的开关，辅助功能就开好了" : "再打开它右边的开关，然后按提示重新打开 Tolk")
                    .font(.system(size: 12))
                    .foregroundStyle(palette.muted)
            }
            .lineLimit(1)

            Spacer(minLength: 8)

            Image(systemName: "arrow.up")
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(palette.accentText)
                .frame(width: 30, height: 30)
                .background(Circle().fill(palette.accent.opacity(0.12)))
        }
        .foregroundStyle(palette.fg)
        .padding(.leading, 14)
        .padding(.trailing, 16)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(RoundedRectangle(cornerRadius: 14, style: .continuous).fill(palette.card))
        .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(palette.stroke, lineWidth: 1))
        .padding(6) // 给阴影留地方
        .shadow(color: .black.opacity(0.18), radius: 10, x: 0, y: 4)
    }
}
