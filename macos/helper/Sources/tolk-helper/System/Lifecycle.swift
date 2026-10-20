import AppKit
import Foundation

/// 进程生命周期：parent 没了就收摊。
@MainActor
enum Lifecycle {
    private static var parentTimer: Timer?
    private static var terminating = false

    static func watchParent(pid: pid_t) {
        parentTimer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { _ in
            if kill(pid, 0) != 0, errno == ESRCH {
                MainActor.assumeIsolated {
                    Lifecycle.terminate(reason: "parent 进程已退出")
                }
            }
        }
    }

    static func terminate(reason: String) {
        guard !terminating else { return }
        terminating = true
        Bridge.stderr("tolk-helper 退出：\(reason)")
        parentTimer?.invalidate()
        if #available(macOS 14.0, *) {
            Ops.screenshot.shutdown()
        }
        Ops.closeAuxiliaryWindows()
        exit(0)
    }
}
