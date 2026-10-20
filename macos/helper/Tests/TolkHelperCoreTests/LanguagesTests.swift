import XCTest
@testable import TolkHelperCore

final class LanguagesTests: XCTestCase {
    func testAllLabelsMapAndBack() {
        XCTAssertEqual(TolkLanguages.all.count, 38)
        for language in TolkLanguages.all {
            XCTAssertEqual(TolkLanguages.code(forLabel: language.label), language.code)
            XCTAssertEqual(TolkLanguages.label(forCode: language.code), language.label)
        }
    }

    func testSpecificMappings() {
        XCTAssertEqual(TolkLanguages.code(forLabel: "自动检测"), "auto")
        XCTAssertEqual(TolkLanguages.code(forLabel: "简体中文"), "zh-CN")
        XCTAssertEqual(TolkLanguages.code(forLabel: "繁体中文"), "zh-TW")
        XCTAssertEqual(TolkLanguages.code(forLabel: "越南语"), "vi")
        XCTAssertEqual(TolkLanguages.code(forLabel: "阿拉伯语"), "ar")
    }

    func testLenientInput() {
        XCTAssertEqual(TolkLanguages.code(forLabel: " 英语 "), "en")
        XCTAssertEqual(TolkLanguages.code(forLabel: "中文（简体）"), "zh-CN")
        XCTAssertEqual(TolkLanguages.code(forLabel: "ZH-tw"), "zh-TW")
        XCTAssertNil(TolkLanguages.code(forLabel: "克林贡语"))
        XCTAssertNil(TolkLanguages.code(forLabel: ""))
    }

    func testCodesAreUnique() {
        let codes = TolkLanguages.all.map(\.code)
        XCTAssertEqual(Set(codes).count, codes.count)
        let labels = TolkLanguages.all.map(\.label)
        XCTAssertEqual(Set(labels).count, labels.count)
    }

    func testRequestLabelFallsBackToAuto() {
        XCTAssertEqual(TolkLanguages.labelForRequest(code: "ja"), "日语")
        XCTAssertEqual(TolkLanguages.labelForRequest(code: "nl"), "荷兰语")
        XCTAssertEqual(TolkLanguages.labelForRequest(code: "la"), "自动检测")
    }
}

final class LegacyLanguageMapTests: XCTestCase {
    func testCodeToLabelCoversAllIncludingAuto() {
        let map = TolkLanguages.codeToLabel
        XCTAssertEqual(map.count, 38)
        XCTAssertEqual(map["auto"], "自动检测")
        XCTAssertEqual(map["zh-CN"], "简体中文")
        XCTAssertEqual(map["zh-TW"], "繁体中文")
        XCTAssertEqual(map["ar"], "阿拉伯语")
    }
}
