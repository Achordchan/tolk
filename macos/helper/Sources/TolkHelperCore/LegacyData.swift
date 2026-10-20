import Foundation

/// `legacy.read` 要读的旧版 bagayalu 数据范围。
public enum LegacyData {
    /// 旧版（以及 Tolk 沿用）的 bundle id，也是它的 UserDefaults 域。
    public static let defaultsDomain = "achord.dazuofanyiguan"

    /// `Core/KeychainStore.swift` 使用的 service。
    public static let keychainService = "com.achord.dazuofanyiguan"

    /// 旧版 Keychain 里存过的账号名（目前只有 OpenAI 兼容接口的 API Key）。
    public static let keychainAccounts = ["openAIAPIKey"]

    /// `AppSettings` 里的键。
    public static let appSettingsKeys = [
        "engineType",
        "sourceLanguageCode",
        "targetLanguageCode",
        "openAIBaseURL",
        "openAIModel",
        "openAIEndpointMode",
        "appearance",
        "sourceTextFontSize",
        "translatedTextFontSize",
        "miniTextFontSize",
        "doubleCopyEnabled",
        "doubleCopyWindowMs",
        "globalHotkeyEnabled",
        "neverRecommendGlobalHotkey",
        "globalHotkeyRecommendationAfter",
        "miniModeEnabled",
        "screenshotHotkeyEnabled",
        "screenshotHotkeyKeyCode",
        "screenshotFreezeBackgroundEnabled"
    ]

    /// `InputAssistSettings` 里的键。
    public static let inputAssistKeys = [
        "inputAssistEnabled",
        "inputAssistTargetLanguages",
        "inputAssistShortcutKeyCode",
        "inputAssistShortcutModifiers",
        "inputAssistAppScope",
        "inputAssistBlocklist",
        "inputAssistAllowlist",
        "inputAssistSelectionAutoShowEnabled",
        "inputAssistShowsCacheBadge",
        "inputAssistDidOfferOnboarding"
    ]

    public static var defaultsKeys: [String] { appSettingsKeys + inputAssistKeys }

    /// 从整个偏好域里挑出白名单里的键；值 JSON 表示不了的丢掉。
    public static func filterDefaults(_ domain: [String: Any]) -> [String: JSONValue] {
        var result: [String: JSONValue] = [:]
        for key in defaultsKeys {
            guard let raw = domain[key], let value = JSONValue(propertyList: raw) else { continue }
            result[key] = value
        }
        return result
    }
}
