import Foundation

/// Tolk 对外使用的语言标签（中文）与 helper 内部语言代码（BCP-47 风格，沿用 bagayalu）的对应。
public struct TolkLanguage: Equatable {
    public let label: String
    public let code: String

    public init(label: String, code: String) {
        self.label = label
        self.code = code
    }
}

public enum TolkLanguages {
    public static let autoLabel = "自动检测"
    public static let autoCode = "auto"

    /// 协议规定的 15 个标签，顺序即界面里的顺序。
    public static let all: [TolkLanguage] = [
        TolkLanguage(label: "自动检测", code: "auto"),
        TolkLanguage(label: "简体中文", code: "zh-CN"),
        TolkLanguage(label: "繁体中文", code: "zh-TW"),
        TolkLanguage(label: "英语", code: "en"),
        TolkLanguage(label: "日语", code: "ja"),
        TolkLanguage(label: "韩语", code: "ko"),
        TolkLanguage(label: "法语", code: "fr"),
        TolkLanguage(label: "德语", code: "de"),
        TolkLanguage(label: "西班牙语", code: "es"),
        TolkLanguage(label: "俄语", code: "ru"),
        TolkLanguage(label: "意大利语", code: "it"),
        TolkLanguage(label: "葡萄牙语", code: "pt"),
        TolkLanguage(label: "越南语", code: "vi"),
        TolkLanguage(label: "泰语", code: "th"),
        TolkLanguage(label: "阿拉伯语", code: "ar")
    ]

    /// 旧版 bagayalu 界面里的叫法，以及直接传内部代码，都宽容地接受。
    private static let aliases: [String: String] = [
        "中文（简体）": "zh-CN",
        "中文(简体)": "zh-CN",
        "中文（繁体）": "zh-TW",
        "中文(繁体)": "zh-TW",
        "中文": "zh-CN",
        "自动": "auto"
    ]

    /// 标签 -> 内部代码。认不出返回 nil。
    public static func code(forLabel label: String) -> String? {
        let trimmed = label.trimmingCharacters(in: .whitespacesAndNewlines)
        if let hit = all.first(where: { $0.label == trimmed }) { return hit.code }
        if let alias = aliases[trimmed] { return alias }
        if let hit = all.first(where: { $0.code.caseInsensitiveCompare(trimmed) == .orderedSame }) {
            return hit.code
        }
        return nil
    }

    /// 内部代码 -> 标签。不在这 15 个里（比如检测出了荷兰语）返回 nil。
    public static func label(forCode code: String) -> String? {
        all.first(where: { $0.code == code })?.label
    }

    /// 发给 Go 的语言标签：内部代码不在 Tolk 支持的范围里时退回「自动检测」，让引擎自己判断。
    public static func labelForRequest(code: String) -> String {
        label(forCode: code) ?? autoLabel
    }
}
