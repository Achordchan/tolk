import Foundation

// 输入助手的纯逻辑：应用过滤、语言策略、文本过滤、快捷键模型、`inputassist.configure` 参数解析。
// 移植自 bagayalu 的 InputAssist/（Filtering、Context、Settings）。不依赖 AppKit，方便单测。

// MARK: - 应用过滤

/// 目标 App 的三个可比对标识：用户在设置里可能填其中任意一个。
public struct InputAssistAppIdentity: Equatable {
    public let bundleIdentifier: String?
    public let localizedName: String?
    public let executableName: String?
    /// 进程号。不参与名单匹配，只用来在替换前确认前台还是通过检查的这个进程。
    public let processIdentifier: Int32?

    public init(
        bundleIdentifier: String?,
        localizedName: String?,
        executableName: String?,
        processIdentifier: Int32? = nil
    ) {
        self.bundleIdentifier = bundleIdentifier
        self.localizedName = localizedName
        self.executableName = executableName
        self.processIdentifier = processIdentifier
    }
}

public enum InputAssistAppScope: String, Equatable {
    /// 全局启用 + 黑名单排除（默认）。
    case globalWithBlocklist
    /// 仅指定 App 启用。
    case allowlistOnly
}

/// 应用黑白名单。纯匹配逻辑。
public enum InputAssistAppFilter {
    /// 默认黑名单：终端和密码类工具。
    public static let defaultBlocklist: [String] = [
        "com.apple.Terminal",
        "com.googlecode.iterm2",
        "com.apple.keychainaccess",
        "com.1password.1password",
        "com.agilebits.onepassword7",
        "com.bitwarden.desktop"
    ]

    /// 黑名单优先于全局规则。
    public static func allows(
        _ identity: InputAssistAppIdentity?,
        scope: InputAssistAppScope,
        blocklist: [String],
        allowlist: [String]
    ) -> Bool {
        // 连当前 App 都识别不出来，就不要动它的文本。
        guard let identity else { return false }

        if matches(identity, entries: blocklist) { return false }

        switch scope {
        case .globalWithBlocklist:
            return true
        case .allowlistOnly:
            return matches(identity, entries: allowlist)
        }
    }

    /// 用户可能填 "Terminal"、"com.apple.Terminal" 或 "Terminal.app"，三种都要认。
    public static func matches(_ identity: InputAssistAppIdentity, entries: [String]) -> Bool {
        guard !entries.isEmpty else { return false }
        let candidates = Set(
            [identity.bundleIdentifier, identity.localizedName, identity.executableName]
                .compactMap { $0.map(normalize) }
                .filter { !$0.isEmpty }
        )
        guard !candidates.isEmpty else { return false }
        return entries.contains { candidates.contains(normalize($0)) }
    }

    /// 把名单项按换行 / 逗号拆开并去空白。
    public static func parseList(_ entries: [String]) -> [String] {
        var result: [String] = []
        for entry in entries {
            for piece in entry.split(whereSeparator: { $0 == "\n" || $0 == "," }) {
                let trimmed = piece.trimmingCharacters(in: .whitespaces)
                if !trimmed.isEmpty { result.append(trimmed) }
            }
        }
        return result
    }

    private static func normalize(_ value: String) -> String {
        var normalized = value
            .trimmingCharacters(in: .whitespacesAndNewlines)
            .lowercased()
        if normalized.hasSuffix(".app") {
            normalized.removeLast(4)
        }
        return normalized
    }
}

// MARK: - 语言策略

/// 书写系统的存在情况（由 helper 里的 `TextScriptPresence` 填）。
public struct InputAssistScriptFlags: Equatable {
    public var containsHan: Bool
    public var containsKana: Bool
    public var containsHangul: Bool
    public var containsBopomofo: Bool

    public init(
        containsHan: Bool = false,
        containsKana: Bool = false,
        containsHangul: Bool = false,
        containsBopomofo: Bool = false
    ) {
        self.containsHan = containsHan
        self.containsKana = containsKana
        self.containsHangul = containsHangul
        self.containsBopomofo = containsBopomofo
    }
}

/// 目标语言列表的规整与同源过滤。全部是纯函数。
public enum InputAssistLanguagePolicy {
    public static let maximumTargetCount = 6
    public static let defaultTargetCodes = ["en"]

    /// 选中文本为明确的汉字文本时，不盲信长文通用语言识别。
    public static func selectionSourceLanguageCode(
        scripts: InputAssistScriptFlags,
        detectedLanguageCode: String?
    ) -> String? {
        // 识别器明确认定是中文时信它——简繁之分只有它能给。
        if detectedLanguageCode == "zh-CN" || detectedLanguageCode == "zh-TW" {
            return detectedLanguageCode
        }

        // 假名和谚文是决定性的：中文里不会出现它们，不依赖识别器。
        // 顺序：假名 > 谚文 > 汉字。
        if scripts.containsKana { return "ja" }
        if scripts.containsHangul { return "ko" }
        // 注音符号是明确的繁体信号。
        if scripts.containsBopomofo { return "zh-TW" }

        guard scripts.containsHan else {
            return detectedLanguageCode
        }

        // 纯汉字的日文 / 韩文没有假名和谚文可依，只剩识别器的结论，保留它。
        if detectedLanguageCode == "ja" || detectedLanguageCode == "ko" {
            return detectedLanguageCode
        }

        // 纯汉字 / 中英混排按中文输入语义处理。
        return "zh-CN"
    }

    /// 去重并截断到 6 个。顺序严格按用户配置保留，只删不移。
    public static func sanitizeTargets(_ codes: [String]) -> [String] {
        var seen = Set<String>()
        var result: [String] = []
        for code in codes {
            let trimmed = code.trimmingCharacters(in: .whitespaces)
            guard !trimmed.isEmpty else { continue }
            let normalized = normalize(trimmed)
            guard !seen.contains(normalized) else { continue }
            seen.insert(normalized)
            result.append(trimmed)
            if result.count == maximumTargetCount { break }
        }
        return result
    }

    /// 隐藏与源语言相同的目标。
    /// 源语言未知时（auto、或短文本检测不出来）一个都不隐藏。
    public static func visibleTargets(
        _ targets: [String],
        sourceLanguageCode: String?
    ) -> [String] {
        guard let sourceLanguageCode,
              !sourceLanguageCode.isEmpty,
              sourceLanguageCode != TolkLanguages.autoCode
        else {
            return targets
        }
        return targets.filter { !isSameLanguage($0, sourceLanguageCode) }
    }

    /// 语言码是否指向同一种语言。
    /// 主语言相同时，只有「双方都带了地区 / 字体子标签且不一致」才算不同，
    /// 因此 zh-CN 与 zh-TW 是两种语言，而 en 与 en-US 是同一种。
    public static func isSameLanguage(_ lhs: String, _ rhs: String) -> Bool {
        let left = subtags(of: lhs)
        let right = subtags(of: rhs)
        guard left.base == right.base else { return false }
        guard let leftVariant = left.variant, let rightVariant = right.variant else {
            return true
        }
        return leftVariant == rightVariant
    }

    private static func normalize(_ code: String) -> String {
        code.trimmingCharacters(in: .whitespaces)
            .replacingOccurrences(of: "_", with: "-")
            .lowercased()
    }

    private static func subtags(of code: String) -> (base: String, variant: String?) {
        let parts = normalize(code).split(separator: "-", omittingEmptySubsequences: true)
        guard let base = parts.first else { return ("", nil) }
        return (String(base), parts.count > 1 ? String(parts[1]) : nil)
    }
}

// MARK: - 文本过滤

/// 这段文字看起来值不值得翻译（取自老版 `InputAssistSentenceBoundary`）。
/// 不设最小字数（「好的」「收到」必须支持），只挡掉明显不该动的结构化内容。
public enum InputAssistTextFilter {
    public static func looksTranslatable(_ text: String) -> Bool {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return false }
        guard trimmed.contains(where: { $0.isLetter }) else { return false }
        guard !looksLikeURL(trimmed), !looksLikeEmail(trimmed) else { return false }
        return true
    }

    public static func looksLikeURL(_ text: String) -> Bool {
        let lowered = text.lowercased()
        if lowered.hasPrefix("http://") || lowered.hasPrefix("https://") || lowered.hasPrefix("www.") {
            return true
        }
        guard !text.contains(where: { $0.isWhitespace }) else { return false }
        if lowered.contains("://") { return true }
        return looksLikeBareDomain(text)
    }

    /// 不带协议头的域名，例如 `example.com/产品`。
    public static func looksLikeBareDomain(_ text: String) -> Bool {
        guard !text.contains(where: { $0.isWhitespace }) else { return false }

        // 主机名到 `/`、`?`、`#` 任意一个为止。
        let host = text.prefix { $0 != "/" && $0 != "?" && $0 != "#" }
        // 再去掉端口。
        let hostWithoutPort = host.prefix { $0 != ":" }
        let labels = hostWithoutPort.split(separator: ".", omittingEmptySubsequences: false)
        guard labels.count >= 2 else { return false }

        // 顶级域必须是两个以上的 ASCII 字母：这样 "12.5" 这类小数不会被误判。
        guard let topLevel = labels.last,
              topLevel.count >= 2,
              topLevel.allSatisfy({ $0.isASCII && $0.isLetter })
        else {
            return false
        }
        // 其余各段是 ASCII 字母数字或连字符。
        return labels.dropLast().allSatisfy { label in
            !label.isEmpty && label.allSatisfy { $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-") }
        }
    }

    public static func looksLikeEmail(_ text: String) -> Bool {
        guard !text.contains(where: { $0.isWhitespace }) else { return false }
        let parts = text.split(separator: "@", omittingEmptySubsequences: false)
        guard parts.count == 2, !parts[0].isEmpty else { return false }
        return parts[1].contains(".")
    }
}

// MARK: - 快捷键

/// 手动触发快捷键。修饰键是 Carbon 位：⌘ 256、⇧ 512、⌥ 2048、⌃ 4096。
public struct InputAssistShortcut: Hashable {
    public static let commandMask: UInt32 = 256
    public static let shiftMask: UInt32 = 512
    public static let optionMask: UInt32 = 2048
    public static let controlMask: UInt32 = 4096
    public static let allModifierMask: UInt32 = 256 | 512 | 2048 | 4096

    public var keyCode: Int
    public var carbonModifiers: UInt32

    public init(keyCode: Int, carbonModifiers: UInt32) {
        self.keyCode = keyCode
        self.carbonModifiers = carbonModifiers
    }

    /// 默认值：⌥⇧ Space（kVK_Space = 49）。避开 Gemini、Alfred 等常见的 ⌥Space。
    public static let `default` = InputAssistShortcut(
        keyCode: 49,
        carbonModifiers: optionMask | shiftMask
    )

    public var displayString: String {
        var text = ""
        if carbonModifiers & InputAssistShortcut.controlMask != 0 { text += "⌃" }
        if carbonModifiers & InputAssistShortcut.optionMask != 0 { text += "⌥" }
        if carbonModifiers & InputAssistShortcut.shiftMask != 0 { text += "⇧" }
        if carbonModifiers & InputAssistShortcut.commandMask != 0 { text += "⌘" }
        return text + InputAssistShortcut.keyName(for: keyCode)
    }

    private static let keyNames: [Int: String] = [
        0: "A", 1: "S", 2: "D", 3: "F", 4: "H", 5: "G", 6: "Z", 7: "X", 8: "C", 9: "V",
        11: "B", 12: "Q", 13: "W", 14: "E", 15: "R", 16: "Y", 17: "T",
        18: "1", 19: "2", 20: "3", 21: "4", 22: "6", 23: "5", 24: "=", 25: "9", 26: "7",
        27: "-", 28: "8", 29: "0", 30: "]", 31: "O", 32: "U", 33: "[", 34: "I", 35: "P",
        36: "Return", 37: "L", 38: "J", 39: "'", 40: "K", 41: ";", 42: "\\", 43: ",",
        44: "/", 45: "N", 46: "M", 47: ".", 48: "Tab", 49: "Space", 50: "`", 51: "Delete",
        53: "Esc", 96: "F5", 97: "F6", 98: "F7", 99: "F3", 100: "F8", 101: "F9", 103: "F11",
        109: "F10", 111: "F12", 118: "F4", 120: "F2", 122: "F1",
        123: "←", 124: "→", 125: "↓", 126: "↑"
    ]

    public static func keyName(for keyCode: Int) -> String {
        keyNames[keyCode] ?? "Key \(keyCode)"
    }
}

// MARK: - 外观

public enum InputAssistAppearance: String, Equatable {
    case system
    case light
    case dark
    case pink
}

// MARK: - 配置（inputassist.configure）

public struct InputAssistConfigError: Error, Equatable {
    public let message: String
    public init(_ message: String) { self.message = message }
}

/// `inputassist.configure` 推来的全部设置。只在内存里，不落盘。
public struct InputAssistConfig: Equatable {
    public var enabled: Bool
    /// 内部语言代码（zh-CN、en…），顺序即面板里的顺序。
    public var targetCodes: [String]
    public var shortcut: InputAssistShortcut
    public var scope: InputAssistAppScope
    public var blocklist: [String]
    public var allowlist: [String]
    public var autoShow: Bool
    public var cacheBadge: Bool
    public var appearance: InputAssistAppearance

    public init(
        enabled: Bool = false,
        targetCodes: [String] = InputAssistLanguagePolicy.defaultTargetCodes,
        shortcut: InputAssistShortcut = .default,
        scope: InputAssistAppScope = .globalWithBlocklist,
        blocklist: [String] = InputAssistAppFilter.defaultBlocklist,
        allowlist: [String] = [],
        autoShow: Bool = false,
        cacheBadge: Bool = true,
        appearance: InputAssistAppearance = .system
    ) {
        self.enabled = enabled
        self.targetCodes = targetCodes
        self.shortcut = shortcut
        self.scope = scope
        self.blocklist = blocklist
        self.allowlist = allowlist
        self.autoShow = autoShow
        self.cacheBadge = cacheBadge
        self.appearance = appearance
    }

    /// 解析 `inputassist.configure` 的参数。目标语言从 Tolk 中文标签转成内部代码。
    /// 关闭状态下不挑剔（认不出的语言直接略去）；开启状态下认不出的语言、非法的键码会报错。
    public static func parse(_ params: [String: JSONValue]) throws -> InputAssistConfig {
        let enabled = params["enabled"]?.boolValue ?? false

        var codes: [String] = []
        if let labels = params["targets"]?.stringArray {
            for label in labels {
                let trimmed = label.trimmingCharacters(in: .whitespacesAndNewlines)
                if trimmed.isEmpty { continue }
                guard let code = TolkLanguages.code(forLabel: trimmed) else {
                    if enabled { throw InputAssistConfigError("不支持的目标语言：\(trimmed)") }
                    continue
                }
                if code == TolkLanguages.autoCode { continue }
                codes.append(code)
            }
        }
        let sanitized = InputAssistLanguagePolicy.sanitizeTargets(codes)
        let targets = sanitized.isEmpty ? InputAssistLanguagePolicy.defaultTargetCodes : sanitized

        var shortcut = InputAssistShortcut.default
        if let keyCode = params["shortcutKeyCode"]?.intValue {
            if keyCode < 0 || keyCode > 127 {
                if enabled { throw InputAssistConfigError("快捷键的键码无效：\(keyCode)") }
            } else {
                shortcut.keyCode = keyCode
            }
        }
        if let modifiers = params["shortcutModifiers"]?.intValue {
            let masked = UInt32(clamping: max(0, modifiers)) & InputAssistShortcut.allModifierMask
            shortcut.carbonModifiers = masked
        }

        let scope: InputAssistAppScope = params["scope"]?.stringValue == "allowlist"
            ? .allowlistOnly
            : .globalWithBlocklist

        let blocklist: [String]
        if let given = params["blocklist"]?.stringArray {
            blocklist = InputAssistAppFilter.parseList(given)
        } else {
            blocklist = InputAssistAppFilter.defaultBlocklist
        }
        let allowlist = InputAssistAppFilter.parseList(params["allowlist"]?.stringArray ?? [])

        let appearance = params["appearance"]?.stringValue
            .flatMap { InputAssistAppearance(rawValue: $0) } ?? .system

        return InputAssistConfig(
            enabled: enabled,
            targetCodes: targets,
            shortcut: shortcut,
            scope: scope,
            blocklist: blocklist,
            allowlist: allowlist,
            autoShow: params["autoShow"]?.boolValue ?? false,
            cacheBadge: params["cacheBadge"]?.boolValue ?? true,
            appearance: appearance
        )
    }
}
