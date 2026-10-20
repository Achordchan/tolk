import XCTest
@testable import TolkHelperCore

final class LegacyDataTests: XCTestCase {
    func testOnlyWhitelistedKeysAreKept() {
        let domain: [String: Any] = [
            "engineType": "google",
            "targetLanguageCode": "zh-CN",
            "doubleCopyEnabled": NSNumber(value: true),
            "doubleCopyWindowMs": NSNumber(value: 550),
            "inputAssistBlocklist": "a\nb",
            "NSWindow Frame main": "0 0 100 100",
            "SULastCheckTime": Date()
        ]
        let result = LegacyData.filterDefaults(domain)
        XCTAssertEqual(result["engineType"], "google")
        XCTAssertEqual(result["doubleCopyEnabled"], .bool(true))
        XCTAssertEqual(result["doubleCopyWindowMs"], .int(550))
        XCTAssertEqual(result["inputAssistBlocklist"], "a\nb")
        XCTAssertNil(result["NSWindow Frame main"])
        XCTAssertNil(result["SULastCheckTime"])
    }

    /// Go 侧输入助手迁移（macmigrate.applyInputAssist）读的九个键必须都能通过这道过滤。
    func testAllInputAssistKeysSurviveFilter() {
        let consumed = [
            "inputAssistEnabled": NSNumber(value: true),
            "inputAssistTargetLanguages": "en,ja" as Any,
            "inputAssistShortcutKeyCode": NSNumber(value: 49),
            "inputAssistShortcutModifiers": NSNumber(value: 2560),
            "inputAssistAppScope": "allowlist" as Any,
            "inputAssistBlocklist": "a" as Any,
            "inputAssistAllowlist": "b" as Any,
            "inputAssistSelectionAutoShowEnabled": NSNumber(value: true),
            "inputAssistShowsCacheBadge": NSNumber(value: false)
        ]
        let result = LegacyData.filterDefaults(consumed)
        for key in consumed.keys {
            XCTAssertNotNil(result[key], "\(key) 被过滤掉了")
        }
        XCTAssertEqual(result.count, consumed.count)
    }

    func testNonRepresentableWhitelistedValueIsDropped() {
        let result = LegacyData.filterDefaults(["globalHotkeyRecommendationAfter": Date(), "openAIModel": "m"])
        XCTAssertNil(result["globalHotkeyRecommendationAfter"])
        XCTAssertEqual(result["openAIModel"], "m")
    }

    func testKeychainIdentity() {
        XCTAssertEqual(LegacyData.keychainService, "com.achord.dazuofanyiguan")
        XCTAssertEqual(LegacyData.defaultsDomain, "achord.dazuofanyiguan")
        XCTAssertEqual(LegacyData.keychainAccounts, ["openAIAPIKey"])
    }
}
