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

    /// 与 Go 端语言表一致的 38 个标签（含「自动检测」），顺序即界面里的顺序。
    /// 某个语言能不能翻由具体的服务（OCR、Apple 翻译）自己判断并如实报错，这里不再替它们拒绝。
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
        TolkLanguage(label: "阿拉伯语", code: "ar"),
        TolkLanguage(label: "荷兰语", code: "nl"),
        TolkLanguage(label: "瑞典语", code: "sv"),
        TolkLanguage(label: "丹麦语", code: "da"),
        TolkLanguage(label: "挪威语", code: "no"),
        TolkLanguage(label: "芬兰语", code: "fi"),
        TolkLanguage(label: "波兰语", code: "pl"),
        TolkLanguage(label: "捷克语", code: "cs"),
        TolkLanguage(label: "匈牙利语", code: "hu"),
        TolkLanguage(label: "罗马尼亚语", code: "ro"),
        TolkLanguage(label: "保加利亚语", code: "bg"),
        TolkLanguage(label: "希腊语", code: "el"),
        TolkLanguage(label: "乌克兰语", code: "uk"),
        TolkLanguage(label: "土耳其语", code: "tr"),
        TolkLanguage(label: "希伯来语", code: "he"),
        TolkLanguage(label: "波斯语", code: "fa"),
        TolkLanguage(label: "印地语", code: "hi"),
        TolkLanguage(label: "孟加拉语", code: "bn"),
        TolkLanguage(label: "泰米尔语", code: "ta"),
        TolkLanguage(label: "印尼语", code: "id"),
        TolkLanguage(label: "马来语", code: "ms"),
        TolkLanguage(label: "菲律宾语", code: "fil"),
        TolkLanguage(label: "乌尔都语", code: "ur"),
        TolkLanguage(label: "斯瓦希里语", code: "sw")
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

    /// 内部代码 -> 标签。不在表里（比如检测出了拉丁语）返回 nil。
    public static func label(forCode code: String) -> String? {
        all.first(where: { $0.code == code })?.label
    }

    /// 发给 Go 的语言标签：内部代码不在 Tolk 支持的范围里时退回「自动检测」，让引擎自己判断。
    public static func labelForRequest(code: String) -> String {
        label(forCode: code) ?? autoLabel
    }

    /// 内部代码 -> 中文标签（含 auto），给 `legacy.read` 的 `languages` 字段用：
    /// 旧版设置里存的是 `zh-CN` 这样的代码，Tolk 迁移时直接查表。
    public static var codeToLabel: [String: String] {
        var map: [String: String] = [:]
        for language in all { map[language.code] = language.label }
        return map
    }
}
