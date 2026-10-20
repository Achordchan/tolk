import AppKit
import ApplicationServices
import Foundation

@MainActor
final class GlobalHotkeyMonitor {
    private(set) var isRunning: Bool = false
    private(set) var isStarting: Bool = false
    private(set) var lastStartFailureMessage: String?

    var onDoubleCopy: ((Int) -> Void)?
    var onDoubleCut: (() -> Void)?
    /// 监听中途被迫停止（权限丢失、tap 失效）时回调，参数是中文原因。
    var onStopped: ((String) -> Void)?

    /// 两类快捷键各自开关（Tolk 通过 `hotkeys.configure` 控制）。回调线程里读，所以不隔离。
    nonisolated(unsafe) var doubleCopyEnabled: Bool = true
    nonisolated(unsafe) var screenshotEnabled: Bool = true

    private var windowMs: Int = 550

    nonisolated(unsafe) private var doubleCutKeyCode: Int = 7

    nonisolated(unsafe) private var eventTap: CFMachPort?
    nonisolated(unsafe) private var runLoopSource: CFRunLoopSource?
    nonisolated(unsafe) private var tapThread: Thread?
    nonisolated(unsafe) private var tapRunLoop: CFRunLoop?
    nonisolated private let lifecycleLock = NSLock()
    nonisolated(unsafe) private var startGeneration: UInt = 0

    private var lastCmdCDate: Date?
    /// 第一次 ⌘C 按下时的剪贴板计数。被动 tap 的回调可能晚于前台应用处理第二次复制，
    /// 所以拿第一次的计数当基线：两次复制里只要有一次真的写进了剪贴板，计数就会比它大。
    private var firstCopyChangeCount = 0
    private var lastCmdXDate: Date?

    var isTrusted: Bool {
        AXIsProcessTrusted()
    }

    func requestAccessibilityPermission() {
        let opts: NSDictionary = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as NSString: true]
        _ = AXIsProcessTrustedWithOptions(opts)
    }

    static func openAccessibilitySettings() {
        guard let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility") else {
            return
        }
        NSWorkspace.shared.open(url)
    }

    func start(windowMs: Int, doubleCutKeyCode: Int = 7) {
        self.windowMs = windowMs
        self.doubleCutKeyCode = doubleCutKeyCode

        if isRunning || isStarting {
            return
        }

        guard isTrusted else {
            isRunning = false
            lastStartFailureMessage = "缺少辅助功能权限"
            return
        }

        isStarting = true
        lastStartFailureMessage = nil
        let generation = beginStartGeneration()

        lastCmdCDate = nil
        lastCmdXDate = nil

        let thread = Thread { [weak self] in
            guard let self else { return }

            let mask = CGEventMask(1 << CGEventType.keyDown.rawValue)

            let callback: CGEventTapCallBack = { _, type, event, refcon in
                guard let refcon else {
                    return Unmanaged.passUnretained(event)
                }

                let monitor = Unmanaged<GlobalHotkeyMonitor>.fromOpaque(refcon).takeUnretainedValue()

                if type == .tapDisabledByTimeout || type == .tapDisabledByUserInput {
                    if AXIsProcessTrusted(), let tap = monitor.currentEventTap() {
                        CGEvent.tapEnable(tap: tap, enable: true)
                    } else {
                        Task { @MainActor in
                            monitor.stop()
                            monitor.onStopped?("辅助功能权限已失效，快捷键监听已停止")
                        }
                    }
                    return Unmanaged.passUnretained(event)
                }

                guard type == .keyDown else {
                    return Unmanaged.passUnretained(event)
                }

                let isCommandCopy = event.flags.contains(.maskCommand)
                    && event.getIntegerValueField(.keyboardEventKeycode) == 8
                let pasteboardChangeCount = isCommandCopy
                    ? NSPasteboard.general.changeCount
                    : 0
                monitor.handle(
                    event,
                    pasteboardChangeCountBeforeKeyDown: pasteboardChangeCount
                )
                return Unmanaged.passUnretained(event)
            }

            let refcon = Unmanaged.passUnretained(self).toOpaque()
            // 这里只监听、从不修改或吞掉事件（回调始终原样返回 event），
            // 所以用被动 tap：主动 tap 会把本进程插进全系统键盘输入的必经之路上，
            // 每一次按键都要等我们处理完才投递，卡顿时还会被系统判定超时而停用。
            guard let tap = CGEvent.tapCreate(
                tap: .cgSessionEventTap,
                place: .headInsertEventTap,
                options: .listenOnly,
                eventsOfInterest: mask,
                callback: callback,
                userInfo: refcon
            ) else {
                Task { @MainActor in
                    guard self.isStartGenerationCurrent(generation) else { return }
                    self.isStarting = false
                    self.isRunning = false
                    self.lastStartFailureMessage = "无法创建全局快捷键监听"
                }
                return
            }

            guard let runLoopSource = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, tap, 0) else {
                CFMachPortInvalidate(tap)
                Task { @MainActor in
                    guard self.isStartGenerationCurrent(generation) else { return }
                    self.isStarting = false
                    self.isRunning = false
                    self.lastStartFailureMessage = "无法创建全局快捷键运行循环"
                }
                return
            }

            guard let runLoop = CFRunLoopGetCurrent() else {
                CFRunLoopSourceInvalidate(runLoopSource)
                CFMachPortInvalidate(tap)
                Task { @MainActor in
                    guard self.isStartGenerationCurrent(generation) else { return }
                    self.isStarting = false
                    self.isRunning = false
                    self.lastStartFailureMessage = "无法获取全局快捷键运行循环"
                }
                return
            }
            guard self.installLifecycleResources(
                tap: tap,
                source: runLoopSource,
                runLoop: runLoop,
                generation: generation
            ) else {
                CFRunLoopSourceInvalidate(runLoopSource)
                CFMachPortInvalidate(tap)
                return
            }

            CFRunLoopAddSource(runLoop, runLoopSource, .commonModes)
            CGEvent.tapEnable(tap: tap, enable: true)

            Task { @MainActor in
                guard self.isStartGenerationCurrent(generation) else { return }
                self.isStarting = false
                self.isRunning = true
                self.lastStartFailureMessage = nil
            }

            CFRunLoopRun()

            Task { @MainActor in
                guard self.isStartGenerationCurrent(generation) else { return }
                self.stop()
                self.lastStartFailureMessage = "全局快捷键监听已停止"
                self.onStopped?("全局快捷键监听已停止")
            }
        }

        tapThread = thread
        thread.start()
    }

    func stop() {
        let resources = invalidateCurrentGeneration()
        isStarting = false

        if let tap = resources.tap {
            CGEvent.tapEnable(tap: tap, enable: false)
        }

        if let source = resources.source {
            CFRunLoopSourceInvalidate(source)
        }

        if let tap = resources.tap {
            CFMachPortInvalidate(tap)
        }

        if let rl = resources.runLoop {
            CFRunLoopStop(rl)
        }

        tapThread = nil
        lastCmdCDate = nil
        isRunning = false
    }

    nonisolated
    private func beginStartGeneration() -> UInt {
        lifecycleLock.lock()
        defer { lifecycleLock.unlock() }
        startGeneration &+= 1
        return startGeneration
    }

    nonisolated
    private func isStartGenerationCurrent(_ generation: UInt) -> Bool {
        lifecycleLock.lock()
        defer { lifecycleLock.unlock() }
        return generation == startGeneration
    }

    nonisolated
    private func installLifecycleResources(
        tap: CFMachPort,
        source: CFRunLoopSource,
        runLoop: CFRunLoop,
        generation: UInt
    ) -> Bool {
        lifecycleLock.lock()
        defer { lifecycleLock.unlock() }
        guard generation == startGeneration else { return false }
        eventTap = tap
        runLoopSource = source
        tapRunLoop = runLoop
        return true
    }

    nonisolated
    private func currentEventTap() -> CFMachPort? {
        lifecycleLock.lock()
        defer { lifecycleLock.unlock() }
        return eventTap
    }

    nonisolated
    private func invalidateCurrentGeneration() -> (
        tap: CFMachPort?,
        source: CFRunLoopSource?,
        runLoop: CFRunLoop?
    ) {
        lifecycleLock.lock()
        defer { lifecycleLock.unlock() }
        startGeneration &+= 1
        let resources = (eventTap, runLoopSource, tapRunLoop)
        eventTap = nil
        runLoopSource = nil
        tapRunLoop = nil
        return resources
    }

    nonisolated
    private func handle(
        _ event: CGEvent,
        pasteboardChangeCountBeforeKeyDown: Int
    ) {
        let flags = event.flags
        let keyCode = event.getIntegerValueField(.keyboardEventKeycode)

        let isCmdDown = flags.contains(.maskCommand)
        let isCKey = keyCode == 8 && doubleCopyEnabled
        let isXKey = keyCode == doubleCutKeyCode && screenshotEnabled

        if !(isCmdDown && (isCKey || isXKey)) { return }

        Task { @MainActor in
            let now = Date()

            if isCKey {
                if let last = lastCmdCDate {
                    let delta = now.timeIntervalSince(last) * 1000
                    if delta <= Double(windowMs) {
                        lastCmdCDate = nil
                        onDoubleCopy?(firstCopyChangeCount)
                        return
                    }
                }
                lastCmdCDate = now
                firstCopyChangeCount = pasteboardChangeCountBeforeKeyDown
                return
            }

            if isXKey {
                if let last = lastCmdXDate {
                    let delta = now.timeIntervalSince(last) * 1000
                    if delta <= Double(windowMs) {
                        lastCmdXDate = nil
                        onDoubleCut?()
                        return
                    }
                }
                lastCmdXDate = now
                return
            }
        }
    }
}
