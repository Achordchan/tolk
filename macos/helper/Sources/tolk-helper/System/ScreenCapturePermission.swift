import AppKit
import ApplicationServices
import Foundation

// 屏幕录制权限（Screen Recording）检查与触发申请
//
// 注意：
// - 截图识别要读取屏幕像素，这在 macOS 上通常需要“屏幕录制”权限。
// - 这里使用系统 API：CGPreflightScreenCaptureAccess / CGRequestScreenCaptureAccess。

enum ScreenCapturePermission {
    static func hasPermission() -> Bool {
        CGPreflightScreenCaptureAccess()
    }

    static func ensurePermission() async -> Bool {
        if hasPermission() {
            return true
        }

        // 会弹系统授权弹窗。
        let granted = CGRequestScreenCaptureAccess()
        if granted {
            return hasPermission()
        }
        return false
    }

    /// 系统设置里已经打开、但要 Tolk 重新打开才生效的状态。
    ///
    /// `CGPreflightScreenCaptureAccess` 只在进程启动时读一次授权，打开开关后一直是 false，
    /// 直到 Tolk 重新打开。读别的应用窗口的标题同样要这项权限，WindowServer 按调用时的授权来判断，
    /// 所以能读到标题就说明开关已经打开。判断不出来的时候，引导里的「已经开启，为什么还是未授权？」
    /// 也提供「重新打开 Tolk」。
    static func grantedPendingRelaunch() -> Bool {
        guard !hasPermission(),
              let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID)
                as? [[String: Any]] else { return false }
        let own: Set<pid_t> = [getpid(), ParentApp.pid].compactMap { $0 }.reduce(into: []) { $0.insert($1) }
        // Dock 和 WindowServer 自己的窗口不需要授权也读得到标题，不算数。
        let exempt: Set<String> = ["Dock", "Window Server", "WindowServer"]
        return list.contains { info in
            guard let pid = info[kCGWindowOwnerPID as String] as? pid_t, !own.contains(pid),
                  let owner = info[kCGWindowOwnerName as String] as? String, !exempt.contains(owner),
                  let name = info[kCGWindowName as String] as? String else { return false }
            return !name.isEmpty
        }
    }

    @MainActor
    static func openScreenRecordingSettings() {
        guard let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture") else {
            return
        }
        NSWorkspace.shared.open(url)
    }
}
