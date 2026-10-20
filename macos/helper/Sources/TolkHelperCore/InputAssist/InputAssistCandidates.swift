import CoreGraphics
import Foundation

// 候选面板的纯状态、按键裁决、尺寸与定位几何。移植自 bagayalu 的 InputAssist/Candidates。

// MARK: - 行状态

/// 一行候选的来源，用于 ⚡ 缓存标识与 ⌥ 调试态。
public enum CandidateResultSource: String, Equatable {
    case network
    case cache
}

public enum CandidateRowState: Equatable {
    /// 骨架态。面板第一次出现时所有行都是它。
    case loading
    case translated(
        text: String,
        source: CandidateResultSource,
        latencyMilliseconds: Int,
        engineTitle: String
    )
    /// 单条失败不拖垮整个面板，支持点击重试。
    case failed(message: String)
    /// Apple 本地翻译缺语言包。
    case languagePackRequired
}

public struct CandidateRow: Equatable, Identifiable {
    public let languageCode: String
    public var state: CandidateRowState

    public init(languageCode: String, state: CandidateRowState) {
        self.languageCode = languageCode
        self.state = state
    }

    public var id: String { languageCode }

    /// 语言的紧凑标识，例如 EN / ES / ZH-CN（不用国旗）。
    public var displayCode: String {
        languageCode.uppercased()
    }

    public var translatedText: String? {
        guard case .translated(let text, _, _, _) = state else { return nil }
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : text
    }

    public var isCommittable: Bool { translatedText != nil }
}

/// 候选列表的纯状态机：只管行内容与高亮位置，不碰窗口、不碰翻译。
public struct CandidateListState: Equatable {
    public private(set) var rows: [CandidateRow]
    /// 默认永远高亮第一项，且不因为使用习惯自动改变。
    public private(set) var selectedIndex: Int

    public init(languageCodes: [String]) {
        rows = languageCodes.map { CandidateRow(languageCode: $0, state: .loading) }
        selectedIndex = 0
    }

    public var count: Int { rows.count }
    public var isEmpty: Bool { rows.isEmpty }

    public var selectedRow: CandidateRow? {
        guard rows.indices.contains(selectedIndex) else { return nil }
        return rows[selectedIndex]
    }

    public var committableIndices: Set<Int> {
        Set(rows.indices.filter { rows[$0].isCommittable })
    }

    public var isFullySettled: Bool {
        rows.allSatisfy { $0.state != .loading }
    }

    /// ↑ / ↓ 到头就停，不循环。
    @discardableResult
    public mutating func moveSelection(by delta: Int) -> Bool {
        guard !rows.isEmpty else { return false }
        let target = min(max(selectedIndex + delta, 0), rows.count - 1)
        guard target != selectedIndex else { return false }
        selectedIndex = target
        return true
    }

    @discardableResult
    public mutating func select(index: Int) -> Bool {
        guard rows.indices.contains(index), index != selectedIndex else { return false }
        selectedIndex = index
        return true
    }

    @discardableResult
    public mutating func update(languageCode: String, state: CandidateRowState) -> Bool {
        guard let index = rows.firstIndex(where: { $0.languageCode == languageCode }) else {
            return false
        }
        guard rows[index].state != state else { return false }
        rows[index].state = state
        return true
    }

    public func index(of languageCode: String) -> Int? {
        rows.firstIndex(where: { $0.languageCode == languageCode })
    }
}

// MARK: - 按键裁决

/// 一次按键的最小描述。刻意不依赖 CGEvent / NSEvent，这样按键规则可以直接单测。
public struct InputAssistKeyEvent: Equatable {
    public let keyCode: Int
    public let hasCommand: Bool
    public let hasOption: Bool
    public let hasControl: Bool
    public let hasShift: Bool

    public init(
        keyCode: Int,
        hasCommand: Bool = false,
        hasOption: Bool = false,
        hasControl: Bool = false,
        hasShift: Bool = false
    ) {
        self.keyCode = keyCode
        self.hasCommand = hasCommand
        self.hasOption = hasOption
        self.hasControl = hasControl
        self.hasShift = hasShift
    }
}

public enum InputAssistKeyAction: Equatable {
    case moveUp
    case moveDown
    /// 用当前高亮译文替换原文。
    case commit
    /// 直接选中第 n 项并替换（⌘1–⌘6）。
    case commitIndex(Int)
    case copySelection
    case dismiss
    /// 关掉面板，但这次按键仍然要送进原 App（普通输入 / Space）。
    case dismissPassingEventThrough
}

public struct InputAssistKeyDecision: Equatable {
    public let action: InputAssistKeyAction
    /// true = 吞掉事件，不投递给原 App。
    public let swallowsEvent: Bool

    public init(action: InputAssistKeyAction, swallowsEvent: Bool) {
        self.action = action
        self.swallowsEvent = swallowsEvent
    }
}

/// 候选面板可见期间的按键裁决。返回 nil 表示「这个键与我们无关」：原样放行，面板保持不变。
public enum InputAssistKeyRouter {
    // HIToolbox virtual key codes。注意 5 和 6 不是连号，这是 ANSI 键盘的历史遗留。
    public static let upArrow = 126
    public static let downArrow = 125
    public static let leftArrow = 123
    public static let rightArrow = 124
    public static let returnKey = 36
    public static let keypadEnter = 76
    public static let escape = 53
    public static let space = 49
    public static let tab = 48
    public static let cKey = 8
    public static let digitKeyCodes = [18, 19, 20, 21, 23, 22]

    public static func decide(
        for event: InputAssistKeyEvent,
        candidateCount: Int,
        selectedIndex: Int,
        committableIndices: Set<Int>
    ) -> InputAssistKeyDecision? {
        if event.hasCommand {
            return decideCommandCombination(
                event,
                candidateCount: candidateCount,
                selectedIndex: selectedIndex,
                committableIndices: committableIndices
            )
        }

        // ⌃ 组合基本都是文本编辑光标操作（⌃A 行首、⌃E 行尾…），一律视为光标移动：关面板并放行。
        if event.hasControl {
            return InputAssistKeyDecision(action: .dismissPassingEventThrough, swallowsEvent: false)
        }

        // 候选操作只认没有任何修饰键的按键。
        // 最要命的是 ⇧Enter：聊天软件里那是「换行但不发送」。
        guard !event.hasShift, !event.hasOption else {
            return InputAssistKeyDecision(action: .dismissPassingEventThrough, swallowsEvent: false)
        }

        switch event.keyCode {
        case escape:
            return InputAssistKeyDecision(action: .dismiss, swallowsEvent: true)

        case upArrow:
            return InputAssistKeyDecision(action: .moveUp, swallowsEvent: true)

        case downArrow:
            return InputAssistKeyDecision(action: .moveDown, swallowsEvent: true)

        case returnKey, keypadEnter:
            // 还没有任何译文可用时不能把 Enter 吞掉：在聊天窗口里那等于让用户的消息发不出去。
            guard committableIndices.contains(selectedIndex) else {
                return InputAssistKeyDecision(action: .dismissPassingEventThrough, swallowsEvent: false)
            }
            return InputAssistKeyDecision(action: .commit, swallowsEvent: true)

        case leftArrow, rightArrow:
            // 光标移动 → 之前记录的 source range 立刻作废。
            return InputAssistKeyDecision(action: .dismissPassingEventThrough, swallowsEvent: false)

        default:
            // Space 与普通字符都算「用户继续输入」：关面板，键照送。
            return InputAssistKeyDecision(action: .dismissPassingEventThrough, swallowsEvent: false)
        }
    }

    /// ⌥ 按住时临时显示调试信息。只认单独按住 Option。
    public static func showsDebugOverlay(
        hasOption: Bool,
        hasCommand: Bool,
        hasControl: Bool,
        hasShift: Bool
    ) -> Bool {
        hasOption && !hasCommand && !hasControl && !hasShift
    }

    private static func decideCommandCombination(
        _ event: InputAssistKeyEvent,
        candidateCount: Int,
        selectedIndex: Int,
        committableIndices: Set<Int>
    ) -> InputAssistKeyDecision? {
        // ⌘ 必须是唯一的修饰键，否则 ⌘⇧3 / ⌘⇧4 这些系统截图快捷键会被当成 ⌘3 / ⌘4 吞掉。
        let hasOnlyCommand = !event.hasShift && !event.hasOption && !event.hasControl

        if hasOnlyCommand, let digitIndex = digitKeyCodes.firstIndex(of: event.keyCode) {
            // ⌘4 但只有 3 个候选：这多半是原 App 自己的快捷键，别抢也别关面板。
            guard digitIndex < candidateCount else { return nil }
            guard committableIndices.contains(digitIndex) else { return nil }
            return InputAssistKeyDecision(action: .commitIndex(digitIndex), swallowsEvent: true)
        }

        if hasOnlyCommand, event.keyCode == cKey {
            guard committableIndices.contains(selectedIndex) else { return nil }
            return InputAssistKeyDecision(action: .copySelection, swallowsEvent: true)
        }

        // 其它 ⌘ 组合（⌘Z / ⌘A / ⌘V…）都会改动文本或光标，面板记录的快照随即失效：
        // 关掉，但绝不拦截用户真正的快捷键。
        return InputAssistKeyDecision(action: .dismissPassingEventThrough, swallowsEvent: false)
    }
}

// MARK: - 尺寸

/// 候选面板的尺寸计算。纯函数。
public enum CandidatePanelLayout {
    public static let width: CGFloat = 420
    public static let horizontalPadding: CGFloat = 10
    public static let verticalPadding: CGFloat = 6
    public static let rowSpacing: CGFloat = 1
    public static let rowVerticalPadding: CGFloat = 5
    public static let lineHeight: CGFloat = 18
    public static let debugLineHeight: CGFloat = 14
    public static let copyNoticeHeight: CGFloat = 25
    /// 语言码那一列的宽度，纵向对齐用。
    public static let languageColumnWidth: CGFloat = 34

    /// 普通候选最多 2 行。
    public static let normalRowMaximumLines = 2
    /// 当前高亮候选可以展开到 4 行。
    public static let selectedRowMaximumLines = 4

    /// 一行大约能放多少个字符。中文比拉丁字母宽，取保守值。
    public static let estimatedCharactersPerLine = 42

    /// 中文译成拉丁语系时字符数会明显变多，预留高度时必须把这个膨胀算进去。
    public static let cjkExpansionFactor = 2.6
    public static let latinExpansionFactor = 1.2

    /// 在面板出现之前就把每行要占多少行文字定下来：译文是陆续到的，行高不能依赖译文，
    /// 所以从原文估算。
    public static func reservedLineCount(forSourceText sourceText: String) -> Int {
        let characters = Double(sourceText.count)
        guard characters > 0 else { return 1 }
        let factor = containsCJK(sourceText) ? cjkExpansionFactor : latinExpansionFactor
        let estimated = (characters * factor) / Double(estimatedCharactersPerLine)
        return max(1, min(selectedRowMaximumLines, Int(estimated.rounded(.up))))
    }

    /// 某一行实际渲染几行文字。只取决于预留值和是不是高亮行，与译文无关。
    public static func lineCount(reservedLineCount: Int, isSelected: Bool) -> Int {
        let maximum = isSelected ? selectedRowMaximumLines : normalRowMaximumLines
        return max(1, min(maximum, reservedLineCount))
    }

    public static func rowHeight(
        reservedLineCount: Int,
        isSelected: Bool,
        showsDebugInfo: Bool
    ) -> CGFloat {
        let textHeight = CGFloat(
            lineCount(reservedLineCount: reservedLineCount, isSelected: isSelected)
        ) * lineHeight
        let debugHeight = showsDebugInfo ? debugLineHeight : 0
        return textHeight + debugHeight + rowVerticalPadding * 2
    }

    public static func panelSize(
        rowCount: Int,
        selectedIndex: Int,
        reservedLineCount: Int,
        showsDebugInfo: Bool,
        showsCopyNotice: Bool = false
    ) -> CGSize {
        guard rowCount > 0 else {
            return CGSize(width: width, height: lineHeight + verticalPadding * 2)
        }
        let rowsHeight = (0..<rowCount).reduce(CGFloat.zero) { total, index in
            total + rowHeight(
                reservedLineCount: reservedLineCount,
                isSelected: index == selectedIndex,
                showsDebugInfo: showsDebugInfo
            )
        }
        let spacing = rowSpacing * CGFloat(max(0, rowCount - 1))
        let noticeHeight = showsCopyNotice ? copyNoticeHeight : 0
        return CGSize(
            width: width,
            height: rowsHeight + spacing + verticalPadding * 2 + noticeHeight
        )
    }

    public static func containsCJK(_ text: String) -> Bool {
        text.unicodeScalars.contains { scalar in
            switch scalar.value {
            case 0x3040...0x30FF,   // 假名
                 0x3400...0x4DBF,
                 0x4E00...0x9FFF,
                 0xAC00...0xD7AF,   // 谚文
                 0xF900...0xFAFF:
                return true
            default:
                return false
            }
        }
    }
}

// MARK: - 定位几何

/// 面板定位的纯几何计算（不碰窗口和屏幕）。
public enum CandidatePanelGeometry {
    /// 面板与文字之间的间距。
    public static let anchorGap: CGFloat = 6
    public static let screenMargin: CGFloat = 8

    /// AX 矩形（左上原点）→ Cocoa 矩形（左下原点）。必须用主屏高度翻转。
    public static func cocoaRect(fromAXRect axRect: CGRect, primaryScreenMaxY: CGFloat) -> CGRect {
        CGRect(
            x: axRect.origin.x,
            y: primaryScreenMaxY - axRect.origin.y - axRect.size.height,
            width: axRect.size.width,
            height: axRect.size.height
        )
    }

    /// 在锚点附近放一个 `panelSize` 的面板，返回它的 origin（Cocoa 坐标）。
    /// 优先放在光标下方；下方空间不足就翻到上方；最后夹紧到 `visibleFrame` 里。
    public static func panelOrigin(
        anchorRect: CGRect,
        panelSize: CGSize,
        visibleFrame: CGRect
    ) -> CGPoint {
        var y = anchorRect.minY - anchorGap - panelSize.height
        if y < visibleFrame.minY + screenMargin {
            let above = anchorRect.maxY + anchorGap
            // 翻到上方也放不下时，保留「下方」这一侧，靠后面的夹紧收拾。
            if above + panelSize.height <= visibleFrame.maxY - screenMargin {
                y = above
            }
        }

        var x = anchorRect.minX
        if x + panelSize.width > visibleFrame.maxX - screenMargin {
            x = visibleFrame.maxX - screenMargin - panelSize.width
        }

        x = clamp(
            x,
            lower: visibleFrame.minX + screenMargin,
            upper: max(visibleFrame.minX + screenMargin, visibleFrame.maxX - screenMargin - panelSize.width)
        )
        y = clamp(
            y,
            lower: visibleFrame.minY + screenMargin,
            upper: max(visibleFrame.minY + screenMargin, visibleFrame.maxY - screenMargin - panelSize.height)
        )
        return CGPoint(x: x, y: y)
    }

    private static func clamp(_ value: CGFloat, lower: CGFloat, upper: CGFloat) -> CGFloat {
        min(max(value, lower), upper)
    }
}
