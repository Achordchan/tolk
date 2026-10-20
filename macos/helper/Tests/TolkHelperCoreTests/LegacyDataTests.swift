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
