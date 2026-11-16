import CoreGraphics

/// 权限引导浮条贴在系统设置窗口下面的位置计算。纯几何，不碰 AppKit，方便测试。
public enum PanelPlacement {
    /// 一块屏幕：整块的范围，和去掉菜单栏、Dock 后能放东西的范围（都是 AppKit 坐标，y 向上）。
    public struct Screen: Equatable {
        public var frame: CGRect
        public var visible: CGRect
        public init(frame: CGRect, visible: CGRect) {
            self.frame = frame
            self.visible = visible
        }
    }

    /// 窗口所在的屏幕：和窗口重叠面积最大的那块。窗口跨两块屏幕时，跟着窗口的大头走；
    /// 和哪块都不重叠时返回 nil。
    public static func screen(for window: CGRect, among screens: [Screen]) -> Screen? {
        var best: (screen: Screen, area: CGFloat)?
        for s in screens {
            let overlap = s.frame.intersection(window)
            let area = overlap.isNull ? 0 : overlap.width * overlap.height
            if area > 0, area > (best?.area ?? 0) {
                best = (s, area)
            }
        }
        return best?.screen
    }

    /// 窗口正下方、和窗口一样宽（340–520），水平居中。下面放不下就收进窗口底部里面；
    /// 上下左右都不出窗口所在屏幕的可用范围。
    public static func frame(below window: CGRect, height: CGFloat, gap: CGFloat,
                             screens: [Screen], fallback: Screen?) -> CGRect {
        let width = min(max(window.width - 40, 340), 520)
        var frame = CGRect(x: window.midX - width / 2, y: window.minY - gap - height,
                           width: width, height: height)
        if let visible = (screen(for: window, among: screens) ?? fallback)?.visible {
            if frame.minY < visible.minY {
                // 收进窗口底部里面；窗口被拖到屏幕下沿以外时，也不出屏幕的可用范围。
                frame.origin.y = min(max(window.minY + 14, visible.minY + 8), visible.maxY - height - 8)
            }
            frame.origin.x = min(max(frame.minX, visible.minX + 8), visible.maxX - width - 8)
        }
        frame.origin = CGPoint(x: frame.minX.rounded(), y: frame.minY.rounded())
        return frame
    }
}
