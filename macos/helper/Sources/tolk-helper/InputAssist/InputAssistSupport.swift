import AppKit
import ApplicationServices
import Foundation
import TolkHelperCore

// 输入助手里依赖 AppKit / AX 的小工具：前台应用身份、外观映射、屏幕与锚点。

extension InputAssistAppIdentity {
    init(application: NSRunningApplication) {
        self.init(
            bundleIdentifier: application.bundleIdentifier,
            localizedName: application.localizedName,
            // 黑名单里写的是可执行文件名（比如 VS Code 的 Electron），不是 .app 包名。
            executableName: application.executableURL?.lastPathComponent,
            // 没有进程的应用这里是 -1。
            processIdentifier: application.processIdentifier > 0 ? application.processIdentifier : nil
        )
    }

    static func frontmost() -> InputAssistAppIdentity? {
        guard let app = NSWorkspace.shared.frontmostApplication else { return nil }
        return InputAssistAppIdentity(application: app)
    }
}

extension InputAssistAppearance {
    /// helper 的 DesignSystem 不区分主题，窗口一律跟随系统；这里只在用户明确指定时覆盖。
    /// 「粉」在 helper 里没有对应的视觉，按浅色处理。
    var nsAppearance: NSAppearance? {
        switch self {
        case .system: return nil
        case .light, .pink: return NSAppearance(named: .aqua)
        case .dark: return NSAppearance(named: .darkAqua)
        }
    }
}

/// 屏幕相关的定位辅助。纯几何在 TolkHelperCore 的 `CandidatePanelGeometry`。
@MainActor
enum InputAssistScreen {
    static func primaryScreenMaxY() -> CGFloat {
        NSScreen.screens.first?.frame.maxY ?? NSScreen.main?.frame.height ?? 0
    }

    /// 锚点落在哪块屏幕上。多显示器下必须按锚点选屏，不能永远用 main。
    static func screen(containing point: CGPoint) -> NSScreen? {
        NSScreen.screens.first(where: { $0.frame.contains(point) }) ?? NSScreen.main
    }

    /// 鼠标位置附近的一个小矩形：没有选区锚点时（比如没取到文字）弹提示用。
    static func mouseAnchor() -> CGRect {
        let mouse = NSEvent.mouseLocation
        return CGRect(x: mouse.x, y: mouse.y - 4, width: 1, height: 18)
    }

    /// 把一个面板放到锚点附近（下方优先，放不下翻上方，夹在可见区域内）。
    static func place(_ window: NSWindow, anchorRect: CGRect, size: CGSize) {
        let anchorPoint = CGPoint(x: anchorRect.midX, y: anchorRect.midY)
        guard let screen = screen(containing: anchorPoint) else {
            window.center()
            return
        }
        let origin = CandidatePanelGeometry.panelOrigin(
            anchorRect: anchorRect,
            panelSize: size,
            visibleFrame: screen.visibleFrame
        )
        window.setFrameOrigin(origin)
    }
}

/// 光标或选区在屏幕上的位置。三级降级：精确插入点 → 控件位置 → 鼠标位置。
@MainActor
enum InputAssistAnchor {
    static func rect(element: AXUIElement, range: InputAssistTextRange?) -> CGRect {
        let primaryMaxY = InputAssistScreen.primaryScreenMaxY()

        // Level 1：拿得到精确插入点。
        if let range, let bounds = boundsForRange(element: element, range: range) {
            return CandidatePanelGeometry.cocoaRect(fromAXRect: bounds, primaryScreenMaxY: primaryMaxY)
        }
        // Level 2：只知道控件在哪。
        if let frame = elementFrame(element) {
            return CandidatePanelGeometry.cocoaRect(fromAXRect: frame, primaryScreenMaxY: primaryMaxY)
        }
        // Level 3：退到鼠标位置。
        return InputAssistScreen.mouseAnchor()
    }

    /// 复制兜底（微信这类不给选区接口的应用）没有选区元素。鼠标常常不在文字旁边
    /// （快捷键是键盘按的），所以依次试：焦点控件的插入点/控件位置 → 前台窗口
    /// （鼠标在窗口里就跟鼠标，否则贴着窗口底部的输入区）→ 鼠标。
    static func fallbackRect(processIdentifier: pid_t?) -> CGRect {
        let primaryMaxY = InputAssistScreen.primaryScreenMaxY()
        let window = processIdentifier.flatMap(focusedWindowFrame)
        if let element = InputAssistAXTextCapture.focusedElement() {
            if let range = InputAssistAXTextCapture.selectedRange(element),
               let bounds = boundsForRange(element: element, range: range) {
                return CandidatePanelGeometry.cocoaRect(fromAXRect: bounds, primaryScreenMaxY: primaryMaxY)
            }
            // 只认像输入框的控件：和整个窗口差不多大的（只暴露了窗口）没有意义。
            if let frame = elementFrame(element), frame.width > 20, frame.height > 10,
               window.map({ frame.height < $0.height * 0.6 }) ?? true {
                return CandidatePanelGeometry.cocoaRect(fromAXRect: frame, primaryScreenMaxY: primaryMaxY)
            }
        }
        if let window {
            let cocoa = CandidatePanelGeometry.cocoaRect(fromAXRect: window, primaryScreenMaxY: primaryMaxY)
            if cocoa.contains(NSEvent.mouseLocation) { return InputAssistScreen.mouseAnchor() }
            // 聊天类应用的输入框在窗口底部：锚在那一条，面板会翻到它上方。
            let h = min(140, cocoa.height / 3)
            return CGRect(x: cocoa.minX + 24, y: cocoa.minY + 12, width: max(1, cocoa.width - 48), height: h)
        }
        return InputAssistScreen.mouseAnchor()
    }

    private static func focusedWindowFrame(_ pid: pid_t) -> CGRect? {
        let app = AXUIElementCreateApplication(pid)
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(app, kAXFocusedWindowAttribute as CFString, &value) == .success,
              let value, CFGetTypeID(value) == AXUIElementGetTypeID() else { return nil }
        return elementFrame(value as! AXUIElement)
    }

    private static func boundsForRange(element: AXUIElement, range: InputAssistTextRange) -> CGRect? {
        var cfRange = CFRange(location: range.location, length: max(range.length, 1))
        guard let rangeValue = AXValueCreate(.cfRange, &cfRange) else { return nil }
        var boundsValue: CFTypeRef?
        guard AXUIElementCopyParameterizedAttributeValue(
            element,
            kAXBoundsForRangeParameterizedAttribute as CFString,
            rangeValue,
            &boundsValue
        ) == .success, let boundsValue, CFGetTypeID(boundsValue) == AXValueGetTypeID() else {
            return nil
        }
        var rect = CGRect.zero
        guard AXValueGetValue(boundsValue as! AXValue, .cgRect, &rect),
              rect.width > 0 || rect.height > 0 else {
            return nil
        }
        return rect
    }

    private static func elementFrame(_ element: AXUIElement) -> CGRect? {
        var positionValue: CFTypeRef?
        var sizeValue: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, kAXPositionAttribute as CFString, &positionValue) == .success,
              AXUIElementCopyAttributeValue(element, kAXSizeAttribute as CFString, &sizeValue) == .success,
              let positionValue,
              let sizeValue,
              CFGetTypeID(positionValue) == AXValueGetTypeID(),
              CFGetTypeID(sizeValue) == AXValueGetTypeID()
        else {
            return nil
        }
        var origin = CGPoint.zero
        var size = CGSize.zero
        guard AXValueGetValue(positionValue as! AXValue, .cgPoint, &origin),
              AXValueGetValue(sizeValue as! AXValue, .cgSize, &size) else {
            return nil
        }
        return CGRect(origin: origin, size: size)
    }
}
