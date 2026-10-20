import AppKit
import Foundation
import TolkHelperCore

@main
struct TolkHelperMain {
    @MainActor
    static func main() {
        // 管道断了由写入处自己处理，不要被信号直接杀掉。
        signal(SIGPIPE, SIG_IGN)

        let arguments = CommandLine.arguments
        if let index = arguments.firstIndex(of: "--parent-pid"),
           index + 1 < arguments.count,
           let pid = pid_t(arguments[index + 1]), pid > 0 {
            ParentApp.pid = pid
            Lifecycle.watchParent(pid: pid)
        }

        // 没有 .app 包、没有 Dock 图标、没有菜单栏。
        let app = NSApplication.shared
        app.setActivationPolicy(.accessory)

        Bridge.shared.start { id, op, params in
            await Ops.handle(id: id, op: op, params: params)
        }
        Bridge.stderr("tolk-helper 已启动（协议 v\(Ops.protocolVersion)）")
        app.run()
    }
}
