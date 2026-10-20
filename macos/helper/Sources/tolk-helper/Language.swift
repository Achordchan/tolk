import Foundation
import TolkHelperCore

struct Language: Identifiable, Hashable {
    let id: String
    let code: String
    let name: String

    init(code: String, name: String) {
        self.id = code
        self.code = code
        self.name = name
    }
}

enum LanguagePreset {
    static let auto = Language(code: TolkLanguages.autoCode, name: TolkLanguages.autoLabel)

    /// Tolk 支持的 15 种语言（含「自动检测」），界面里的语言选择器只列这些。
    static let common: [Language] = TolkLanguages.all.map { Language(code: $0.code, name: $0.label) }

    /// 语言识别（NLLanguageRecognizer）可以认出来的语言代码：沿用 bagayalu 的 38 种。
    /// 比 Tolk 的 15 种多：识别出荷兰语、土耳其语这类，就能如实判断「不是目标语言、要翻」，
    /// 发给 Tolk 时再由 `TolkLanguages.labelForRequest` 退成「自动检测」。
    static let detectableCodes: Set<String> = [
        "zh-CN", "zh-TW", "en", "ja", "ko", "it", "fr", "de", "es", "pt", "nl", "sv", "da", "no",
        "fi", "pl", "cs", "hu", "ro", "bg", "el", "uk", "ru", "tr", "ar", "he", "fa", "hi", "bn",
        "ta", "th", "vi", "id", "ms", "fil", "ur", "sw"
    ]

    private static let extraNames: [String: String] = [
        "nl": "荷兰语", "sv": "瑞典语", "da": "丹麦语", "no": "挪威语", "fi": "芬兰语", "pl": "波兰语",
        "cs": "捷克语", "hu": "匈牙利语", "ro": "罗马尼亚语", "bg": "保加利亚语", "el": "希腊语",
        "uk": "乌克兰语", "tr": "土耳其语", "he": "希伯来语", "fa": "波斯语", "hi": "印地语",
        "bn": "孟加拉语", "ta": "泰米尔语", "id": "印尼语", "ms": "马来语", "fil": "菲律宾语",
        "ur": "乌尔都语", "sw": "斯瓦希里语"
    ]

    static func displayName(for code: String) -> String {
        TolkLanguages.label(forCode: code) ?? extraNames[code] ?? code
    }
}
