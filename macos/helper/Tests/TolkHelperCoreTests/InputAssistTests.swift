import XCTest
@testable import TolkHelperCore

final class InputAssistFilterTests: XCTestCase {
    private let terminal = InputAssistAppIdentity(
        bundleIdentifier: "com.apple.Terminal",
        localizedName: "Terminal",
        executableName: "Terminal"
    )
    private let mail = InputAssistAppIdentity(
        bundleIdentifier: "com.apple.mail",
        localizedName: "Mail",
        executableName: "Mail"
    )

    func testMatchesNameBundleIDAndDotAppSuffix() {
        XCTAssertTrue(InputAssistAppFilter.matches(terminal, entries: ["com.apple.Terminal"]))
        XCTAssertTrue(InputAssistAppFilter.matches(terminal, entries: ["terminal"]))
        XCTAssertTrue(InputAssistAppFilter.matches(terminal, entries: ["Terminal.app"]))
        XCTAssertFalse(InputAssistAppFilter.matches(terminal, entries: ["Xcode"]))
        XCTAssertFalse(InputAssistAppFilter.matches(terminal, entries: []))
    }

    func testBlocklistWinsOverBothScopes() {
        XCTAssertFalse(InputAssistAppFilter.allows(
            terminal, scope: .globalWithBlocklist,
            blocklist: InputAssistAppFilter.defaultBlocklist, allowlist: []
        ))
        XCTAssertFalse(InputAssistAppFilter.allows(
            terminal, scope: .allowlistOnly,
            blocklist: ["com.apple.Terminal"], allowlist: ["com.apple.Terminal"]
        ))
    }

    func testUnknownApplicationIsNeverAutomated() {
        XCTAssertFalse(InputAssistAppFilter.allows(
            nil, scope: .globalWithBlocklist, blocklist: [], allowlist: []
        ))
    }

    func testAllowlistModeOnlyEnablesListedApplications() {
        XCTAssertTrue(InputAssistAppFilter.allows(
            mail, scope: .allowlistOnly, blocklist: [], allowlist: ["Mail"]
        ))
        XCTAssertFalse(InputAssistAppFilter.allows(
            mail, scope: .allowlistOnly, blocklist: [], allowlist: ["Notes"]
        ))
        XCTAssertFalse(InputAssistAppFilter.allows(
            mail, scope: .allowlistOnly, blocklist: [], allowlist: []
        ))
        XCTAssertTrue(InputAssistAppFilter.allows(
            mail, scope: .globalWithBlocklist, blocklist: ["Notes"], allowlist: []
        ))
    }

    func testDefaultBlocklistCoversTheHighRiskApplications() {
        let list = InputAssistAppFilter.defaultBlocklist
        XCTAssertTrue(list.contains("com.apple.Terminal"))
        XCTAssertTrue(list.contains("com.googlecode.iterm2"))
        XCTAssertTrue(list.contains("com.apple.keychainaccess"))
        XCTAssertTrue(list.contains("com.1password.1password"))
    }

    func testParseListAcceptsNewlinesAndCommas() {
        XCTAssertEqual(
            InputAssistAppFilter.parseList(["a, b\nc", "  d  ", ""]),
            ["a", "b", "c", "d"]
        )
    }
}

final class InputAssistLanguagePolicyTests: XCTestCase {
    private func source(_ scripts: InputAssistScriptFlags, _ detected: String?) -> String? {
        InputAssistLanguagePolicy.selectionSourceLanguageCode(scripts: scripts, detectedLanguageCode: detected)
    }

    func testKanaAndHangulAreDecisive() {
        XCTAssertEqual(source(InputAssistScriptFlags(containsKana: true), nil), "ja")
        XCTAssertEqual(source(InputAssistScriptFlags(containsHan: true, containsHangul: true), nil), "ko")
        XCTAssertEqual(source(InputAssistScriptFlags(containsHan: true, containsHangul: true), "ko"), "ko")
        XCTAssertEqual(source(InputAssistScriptFlags(containsHan: true, containsKana: true), "ja"), "ja")
    }

    func testBopomofoMeansTraditionalChinese() {
        XCTAssertEqual(source(InputAssistScriptFlags(containsBopomofo: true), nil), "zh-TW")
    }

    func testDetectedChineseVariantIsTrusted() {
        XCTAssertEqual(source(InputAssistScriptFlags(containsHan: true), "zh-TW"), "zh-TW")
        XCTAssertEqual(source(InputAssistScriptFlags(containsHan: true), "zh-CN"), "zh-CN")
    }

    func testHanOnlyKeepsJapaneseOrKoreanDetection() {
        XCTAssertEqual(source(InputAssistScriptFlags(containsHan: true), "ja"), "ja")
        XCTAssertEqual(source(InputAssistScriptFlags(containsHan: true), "ko"), "ko")
    }

    func testChineseOverridesAWrongDetection() {
        XCTAssertEqual(source(InputAssistScriptFlags(containsHan: true), "en"), "zh-CN")
        XCTAssertEqual(source(InputAssistScriptFlags(containsHan: true), nil), "zh-CN")
        XCTAssertEqual(source(InputAssistScriptFlags(), "en"), "en")
        XCTAssertNil(source(InputAssistScriptFlags(), nil))
    }

    func testSameLanguageKeepsChineseVariantsApart() {
        XCTAssertTrue(InputAssistLanguagePolicy.isSameLanguage("en", "en"))
        XCTAssertTrue(InputAssistLanguagePolicy.isSameLanguage("EN", "en-US"))
        XCTAssertTrue(InputAssistLanguagePolicy.isSameLanguage("en", "en_GB"))
        XCTAssertFalse(InputAssistLanguagePolicy.isSameLanguage("en", "es"))
        XCTAssertFalse(InputAssistLanguagePolicy.isSameLanguage("zh-CN", "zh-TW"))
        XCTAssertTrue(InputAssistLanguagePolicy.isSameLanguage("zh", "zh-CN"))
    }

    func testVisibleTargetsHideOnlyTheDetectedSourceLanguage() {
        let targets = ["en", "es", "ru"]
        XCTAssertEqual(InputAssistLanguagePolicy.visibleTargets(targets, sourceLanguageCode: "en"), ["es", "ru"])
        XCTAssertEqual(InputAssistLanguagePolicy.visibleTargets(targets, sourceLanguageCode: nil), targets)
        XCTAssertEqual(InputAssistLanguagePolicy.visibleTargets(targets, sourceLanguageCode: "auto"), targets)
        XCTAssertEqual(InputAssistLanguagePolicy.visibleTargets(targets, sourceLanguageCode: "ja"), targets)
    }

    func testSanitizeTargetsDeduplicatesAndCapsWithoutReordering() {
        let sanitized = InputAssistLanguagePolicy.sanitizeTargets(
            ["ru", "en", "EN", " es ", "ja", "ko", "de", "fr"]
        )
        XCTAssertEqual(sanitized, ["ru", "en", "es", "ja", "ko", "de"])
        XCTAssertEqual(sanitized.count, InputAssistLanguagePolicy.maximumTargetCount)
        XCTAssertTrue(InputAssistLanguagePolicy.sanitizeTargets(["", "  "]).isEmpty)
    }
}

final class InputAssistTextFilterTests: XCTestCase {
    func testShortHighFrequencyRepliesStayTranslatable() {
        for text in ["好的", "可以", "谢谢", "收到", "没问题"] {
            XCTAssertTrue(InputAssistTextFilter.looksTranslatable(text), text)
        }
    }

    func testStructuredTextIsNotTranslatable() {
        XCTAssertFalse(InputAssistTextFilter.looksTranslatable("https://example.com/a"))
        XCTAssertFalse(InputAssistTextFilter.looksTranslatable("someone@example.com"))
        XCTAssertFalse(InputAssistTextFilter.looksTranslatable("12345"))
        XCTAssertFalse(InputAssistTextFilter.looksTranslatable("   "))
        XCTAssertTrue(InputAssistTextFilter.looksTranslatable("SQ16 Marine Crane 价格是 12000 USD"))
    }

    func testBareDomains() {
        XCTAssertFalse(InputAssistTextFilter.looksTranslatable("example.com/产品"))
        XCTAssertFalse(InputAssistTextFilter.looksTranslatable("example.com?关键词"))
        XCTAssertFalse(InputAssistTextFilter.looksTranslatable("example.com#说明"))
        XCTAssertFalse(InputAssistTextFilter.looksTranslatable("example.com:8080/产品"))
        XCTAssertTrue(InputAssistTextFilter.looksTranslatable("我们提供16吨船吊.价格面议"))
        XCTAssertTrue(InputAssistTextFilter.looksTranslatable("版本 12.5 已发布"))
    }
}

final class InputAssistShortcutTests: XCTestCase {
    func testDefaultShortcutIsOptionShiftSpace() {
        XCTAssertEqual(InputAssistShortcut.default.keyCode, 49)
        XCTAssertEqual(InputAssistShortcut.default.carbonModifiers, 2048 | 512)
        XCTAssertEqual(InputAssistShortcut.default.displayString, "⌥⇧Space")
    }

    func testDisplayStringOrdersModifiers() {
        let shortcut = InputAssistShortcut(keyCode: 38, carbonModifiers: 256 | 2048 | 4096 | 512)
        XCTAssertEqual(shortcut.displayString, "⌃⌥⇧⌘J")
        XCTAssertEqual(InputAssistShortcut.keyName(for: 999), "Key 999")
    }
}

final class InputAssistConfigTests: XCTestCase {
    func testDefaultsWhenEmpty() throws {
        let config = try InputAssistConfig.parse([:])
        XCTAssertFalse(config.enabled)
        XCTAssertEqual(config.targetCodes, ["en"])
        XCTAssertEqual(config.shortcut, .default)
        XCTAssertEqual(config.scope, .globalWithBlocklist)
        XCTAssertEqual(config.blocklist, InputAssistAppFilter.defaultBlocklist)
        XCTAssertFalse(config.autoShow)
        XCTAssertTrue(config.cacheBadge)
        XCTAssertEqual(config.appearance, .system)
    }

    func testParsesAllFieldsAndConvertsLabelsToCodes() throws {
        let params: [String: JSONValue] = [
            "enabled": .bool(true),
            "targets": .array([.string("英语"), .string("日语"), .string("简体中文"), .string("英语"), .string("自动检测")]),
            "shortcutKeyCode": .int(17),
            "shortcutModifiers": .int(2048 | 256),
            "scope": .string("allowlist"),
            "blocklist": .array([.string("Terminal")]),
            "allowlist": .array([.string("Mail"), .string("Notes")]),
            "autoShow": .bool(true),
            "cacheBadge": .bool(false),
            "appearance": .string("dark")
        ]
        let config = try InputAssistConfig.parse(params)
        XCTAssertTrue(config.enabled)
        XCTAssertEqual(config.targetCodes, ["en", "ja", "zh-CN"])
        XCTAssertEqual(config.shortcut, InputAssistShortcut(keyCode: 17, carbonModifiers: 2048 | 256))
        XCTAssertEqual(config.scope, .allowlistOnly)
        XCTAssertEqual(config.blocklist, ["Terminal"])
        XCTAssertEqual(config.allowlist, ["Mail", "Notes"])
        XCTAssertTrue(config.autoShow)
        XCTAssertFalse(config.cacheBadge)
        XCTAssertEqual(config.appearance, .dark)
    }

    func testEnabledRejectsUnknownLanguageButDisabledIgnoresIt() {
        let enabled: [String: JSONValue] = ["enabled": .bool(true), "targets": .array([.string("火星语")])]
        XCTAssertThrowsError(try InputAssistConfig.parse(enabled))
        let disabled: [String: JSONValue] = ["enabled": .bool(false), "targets": .array([.string("火星语")])]
        XCTAssertNoThrow(try InputAssistConfig.parse(disabled))
    }

    func testTargetsAreCappedAtSix() throws {
        let labels = ["英语", "日语", "韩语", "法语", "德语", "俄语", "意大利语"].map { JSONValue.string($0) }
        let config = try InputAssistConfig.parse(["enabled": .bool(true), "targets": .array(labels)])
        XCTAssertEqual(config.targetCodes.count, 6)
        XCTAssertEqual(config.targetCodes.first, "en")
    }

    func testInvalidKeyCodeIsRejectedWhenEnabled() {
        XCTAssertThrowsError(try InputAssistConfig.parse(["enabled": .bool(true), "shortcutKeyCode": .int(5000)]))
    }

    func testPinkAppearanceIsAccepted() throws {
        XCTAssertEqual(try InputAssistConfig.parse(["appearance": .string("pink")]).appearance, .pink)
        XCTAssertEqual(try InputAssistConfig.parse(["appearance": .string("nope")]).appearance, .system)
    }
}

final class InputAssistCacheTests: XCTestCase {
    func testCacheKeysWithDifferentFieldsNeverCollide() {
        func key(_ source: String, _ target: String, _ engine: String) -> String {
            InputAssistCacheKey(
                sourceText: "好的", sourceLanguageCode: source,
                targetLanguageCode: target, engineFingerprint: engine
            ).storageKey
        }
        let keys = Set([
            key("zh-CN", "en", "apple"),
            key("zh-CN", "es", "apple"),
            key("auto", "en", "apple"),
            key("zh-CN", "en", "google")
        ])
        XCTAssertEqual(keys.count, 4)
    }

    func testLruEvictsLeastRecentlyUsedFirst() {
        var index = InputAssistCacheIndex(maximumByteCount: 10_000_000, maximumEntryCount: 3)
        index.insert("1", for: "a")
        index.insert("2", for: "b")
        index.insert("3", for: "c")
        XCTAssertEqual(index.value(for: "a"), "1")
        index.insert("4", for: "d")
        XCTAssertNil(index.value(for: "b"))
        XCTAssertEqual(index.value(for: "a"), "1")
        XCTAssertEqual(index.value(for: "d"), "4")
        XCTAssertEqual(index.count, 3)
    }

    func testLruRespectsTheByteBudget() {
        let payload = String(repeating: "x", count: 200)
        var index = InputAssistCacheIndex(maximumByteCount: 700, maximumEntryCount: 1_000)
        for i in 0..<10 { index.insert(payload, for: "key-\(i)") }
        XCTAssertLessThanOrEqual(index.totalByteCount, 700)
        XCTAssertLessThan(index.count, 10)
        XCTAssertEqual(index.value(for: "key-9"), payload)
    }

    func testByteTotalStaysConsistentWhenOverwriting() {
        var index = InputAssistCacheIndex()
        index.insert("short", for: "k")
        let afterShort = index.totalByteCount
        index.insert(String(repeating: "y", count: 500), for: "k")
        XCTAssertEqual(index.count, 1)
        XCTAssertGreaterThan(index.totalByteCount, afterShort)
        index.insert("short", for: "k")
        XCTAssertEqual(index.totalByteCount, afterShort)
    }

    func testOrderSurvivesReloadAndCodableRoundTrip() throws {
        var index = InputAssistCacheIndex(maximumByteCount: 10_000_000, maximumEntryCount: 3)
        index.insert("1", for: "a")
        index.insert("2", for: "b")
        _ = index.value(for: "a")

        let data = try JSONEncoder().encode(index.sortedEntries())
        let decoded = try JSONDecoder().decode([InputAssistCacheEntry].self, from: data)
        var reloaded = InputAssistCacheIndex(maximumByteCount: 10_000_000, maximumEntryCount: 2)
        reloaded.load(decoded)
        reloaded.insert("3", for: "c")
        XCTAssertNil(reloaded.value(for: "b"))
        XCTAssertEqual(reloaded.value(for: "a"), "1")
    }
}

final class InputAssistCandidateStateTests: XCTestCase {
    private func ready(_ state: inout CandidateListState, _ code: String, _ text: String) {
        state.update(languageCode: code, state: .translated(
            text: text, source: .network, latencyMilliseconds: 1, engineTitle: "T"
        ))
    }

    func testSelectionStartsAtFirstRowAndClampsAtBothEnds() {
        var state = CandidateListState(languageCodes: ["en", "es", "ru"])
        XCTAssertEqual(state.selectedIndex, 0)
        XCTAssertFalse(state.moveSelection(by: -1))
        XCTAssertTrue(state.moveSelection(by: 1))
        XCTAssertTrue(state.moveSelection(by: 1))
        XCTAssertFalse(state.moveSelection(by: 1))
        XCTAssertEqual(state.selectedIndex, 2)
        XCTAssertTrue(state.select(index: 0))
        XCTAssertFalse(state.select(index: 9))
    }

    func testBlankTranslationIsNotCommittable() {
        var state = CandidateListState(languageCodes: ["en", "es"])
        ready(&state, "en", "   ")
        ready(&state, "es", "Hola")
        XCTAssertEqual(state.committableIndices, [1])
        XCTAssertTrue(state.isFullySettled)
    }

    func testDisplayCode() {
        XCTAssertEqual(CandidateRow(languageCode: "zh-CN", state: .loading).displayCode, "ZH-CN")
    }
}

final class InputAssistKeyRouterTests: XCTestCase {
    private func decide(_ event: InputAssistKeyEvent, count: Int = 3, selected: Int = 0, ready: Set<Int> = [0, 1, 2]) -> InputAssistKeyDecision? {
        InputAssistKeyRouter.decide(for: event, candidateCount: count, selectedIndex: selected, committableIndices: ready)
    }

    func testEnterCommitsOnlyWhenReady() {
        XCTAssertEqual(decide(InputAssistKeyEvent(keyCode: 36))?.action, .commit)
        let notReady = decide(InputAssistKeyEvent(keyCode: 36), ready: [])
        XCTAssertEqual(notReady?.action, .dismissPassingEventThrough)
        XCTAssertEqual(notReady?.swallowsEvent, false)
    }

    func testCommandDigitsMapIncludingSwapped5And6() {
        let codes = [18, 19, 20, 21, 23, 22]
        for (index, code) in codes.enumerated() {
            let result = decide(
                InputAssistKeyEvent(keyCode: code, hasCommand: true),
                count: 6, ready: Set(0..<6)
            )
            XCTAssertEqual(result?.action, .commitIndex(index))
            XCTAssertEqual(result?.swallowsEvent, true)
        }
    }

    func testOutOfRangeCommandDigitIsLeftToTheHost() {
        XCTAssertNil(decide(InputAssistKeyEvent(keyCode: 21, hasCommand: true), count: 3))
    }

    func testSystemScreenshotShortcutsAreNotSwallowed() {
        let result = decide(InputAssistKeyEvent(keyCode: 20, hasCommand: true, hasShift: true))
        XCTAssertEqual(result?.action, .dismissPassingEventThrough)
        XCTAssertEqual(result?.swallowsEvent, false)
    }

    func testCommandCNeedsAReadyCandidateAndCommandAlone() {
        XCTAssertEqual(decide(InputAssistKeyEvent(keyCode: 8, hasCommand: true))?.action, .copySelection)
        XCTAssertNil(decide(InputAssistKeyEvent(keyCode: 8, hasCommand: true), ready: []))
        XCTAssertEqual(
            decide(InputAssistKeyEvent(keyCode: 8, hasCommand: true, hasShift: true))?.action,
            .dismissPassingEventThrough
        )
    }

    func testShiftEnterAndModifiedArrowsBelongToTheHost() {
        XCTAssertEqual(decide(InputAssistKeyEvent(keyCode: 36, hasShift: true))?.action, .dismissPassingEventThrough)
        XCTAssertEqual(decide(InputAssistKeyEvent(keyCode: 125, hasOption: true))?.action, .dismissPassingEventThrough)
        XCTAssertEqual(decide(InputAssistKeyEvent(keyCode: 126, hasControl: true))?.action, .dismissPassingEventThrough)
    }

    func testNavigationIsSwallowedTypingIsNot() {
        XCTAssertEqual(decide(InputAssistKeyEvent(keyCode: 126))?.action, .moveUp)
        XCTAssertEqual(decide(InputAssistKeyEvent(keyCode: 125))?.action, .moveDown)
        XCTAssertEqual(decide(InputAssistKeyEvent(keyCode: 53))?.action, .dismiss)
        XCTAssertEqual(decide(InputAssistKeyEvent(keyCode: 53))?.swallowsEvent, true)
        XCTAssertEqual(decide(InputAssistKeyEvent(keyCode: 0))?.swallowsEvent, false)
        XCTAssertEqual(decide(InputAssistKeyEvent(keyCode: 49))?.action, .dismissPassingEventThrough)
        XCTAssertEqual(decide(InputAssistKeyEvent(keyCode: 123))?.swallowsEvent, false)
    }

    func testDebugOverlayNeedsOptionAlone() {
        XCTAssertTrue(InputAssistKeyRouter.showsDebugOverlay(hasOption: true, hasCommand: false, hasControl: false, hasShift: false))
        XCTAssertFalse(InputAssistKeyRouter.showsDebugOverlay(hasOption: true, hasCommand: true, hasControl: false, hasShift: false))
        XCTAssertFalse(InputAssistKeyRouter.showsDebugOverlay(hasOption: false, hasCommand: false, hasControl: false, hasShift: false))
    }
}

final class InputAssistLayoutTests: XCTestCase {
    func testPanelHeightScalesWithRowCountFromTheStart() {
        let one = CandidatePanelLayout.panelSize(rowCount: 1, selectedIndex: 0, reservedLineCount: 1, showsDebugInfo: false)
        let three = CandidatePanelLayout.panelSize(rowCount: 3, selectedIndex: 0, reservedLineCount: 1, showsDebugInfo: false)
        XCTAssertGreaterThan(three.height, one.height)
        XCTAssertEqual(three.width, CandidatePanelLayout.width)
    }

    func testCopyOnlyModeReservesNoticeSpace() {
        let plain = CandidatePanelLayout.panelSize(rowCount: 2, selectedIndex: 0, reservedLineCount: 1, showsDebugInfo: false)
        let notice = CandidatePanelLayout.panelSize(rowCount: 2, selectedIndex: 0, reservedLineCount: 1, showsDebugInfo: false, showsCopyNotice: true)
        XCTAssertEqual(notice.height - plain.height, CandidatePanelLayout.copyNoticeHeight)
    }

    func testRowLineLimits() {
        XCTAssertEqual(CandidatePanelLayout.lineCount(reservedLineCount: 9, isSelected: false), 2)
        XCTAssertEqual(CandidatePanelLayout.lineCount(reservedLineCount: 9, isSelected: true), 4)
        XCTAssertEqual(CandidatePanelLayout.lineCount(reservedLineCount: 1, isSelected: true), 1)
    }

    func testReservedHeightAccountsForChineseExpanding() {
        XCTAssertEqual(CandidatePanelLayout.reservedLineCount(forSourceText: "好的"), 1)
        XCTAssertGreaterThan(CandidatePanelLayout.reservedLineCount(forSourceText: String(repeating: "我们可以提供船吊", count: 6)), 1)
        XCTAssertEqual(CandidatePanelLayout.reservedLineCount(forSourceText: ""), 1)
        XCTAssertLessThanOrEqual(CandidatePanelLayout.reservedLineCount(forSourceText: String(repeating: "字", count: 5000)), 4)
    }
}

final class InputAssistGeometryTests: XCTestCase {
    func testAXToCocoaFlipUsesPrimaryScreenHeight() {
        let ax = CGRect(x: 100, y: 50, width: 200, height: 20)
        let cocoa = CandidatePanelGeometry.cocoaRect(fromAXRect: ax, primaryScreenMaxY: 900)
        XCTAssertEqual(cocoa, CGRect(x: 100, y: 830, width: 200, height: 20))
    }

    func testPanelPrefersBelowAndFlipsAboveWhenNoRoom() {
        let visible = CGRect(x: 0, y: 0, width: 1440, height: 900)
        let size = CGSize(width: 420, height: 100)
        let roomy = CandidatePanelGeometry.panelOrigin(
            anchorRect: CGRect(x: 200, y: 500, width: 100, height: 18), panelSize: size, visibleFrame: visible
        )
        XCTAssertEqual(roomy.y, 500 - CandidatePanelGeometry.anchorGap - 100)
        let tight = CandidatePanelGeometry.panelOrigin(
            anchorRect: CGRect(x: 200, y: 40, width: 100, height: 18), panelSize: size, visibleFrame: visible
        )
        XCTAssertEqual(tight.y, 40 + 18 + CandidatePanelGeometry.anchorGap)
    }

    func testPanelNeverLeavesTheVisibleFrame() {
        let visible = CGRect(x: 0, y: 0, width: 800, height: 600)
        let size = CGSize(width: 420, height: 100)
        let origin = CandidatePanelGeometry.panelOrigin(
            anchorRect: CGRect(x: 790, y: 300, width: 10, height: 18), panelSize: size, visibleFrame: visible
        )
        XCTAssertLessThanOrEqual(origin.x + size.width, visible.maxX - CandidatePanelGeometry.screenMargin)
        XCTAssertGreaterThanOrEqual(origin.x, visible.minX + CandidatePanelGeometry.screenMargin)
    }

    func testPanelOnASecondaryScreenStaysThere() {
        let visible = CGRect(x: 1440, y: 0, width: 1920, height: 1080)
        let origin = CandidatePanelGeometry.panelOrigin(
            anchorRect: CGRect(x: 1600, y: 500, width: 10, height: 18),
            panelSize: CGSize(width: 420, height: 100), visibleFrame: visible
        )
        XCTAssertGreaterThanOrEqual(origin.x, visible.minX)
    }
}

final class InputAssistCopyFallbackTests: XCTestCase {
    func testChangedPasteboardWithTextIsCopied() {
        XCTAssertEqual(
            InputAssistCopyFallbackLogic.judgeCopy(changeCountBefore: 5, changeCountNow: 6, pasteboardText: "你好"),
            .copied("你好")
        )
    }

    func testUnchangedPasteboardNeverTreatsOldContentAsSelection() {
        // 剪贴板里有旧文本，但 changeCount 没变：目标应用没复制，旧文本不能当选区。
        XCTAssertEqual(
            InputAssistCopyFallbackLogic.judgeCopy(changeCountBefore: 5, changeCountNow: 5, pasteboardText: "旧内容"),
            .clipboardUnchanged
        )
        XCTAssertEqual(
            InputAssistCopyFallbackLogic.judgeCopy(changeCountBefore: 5, changeCountNow: 5, pasteboardText: nil),
            .clipboardUnchanged
        )
    }

    func testSameTextAsBeforeStillCountsWhenChangeCountMoved() {
        // 用户原本就复制过同一段字：内容相同，但 changeCount 变了，说明这次复制生效。
        XCTAssertEqual(
            InputAssistCopyFallbackLogic.judgeCopy(changeCountBefore: 1, changeCountNow: 2, pasteboardText: "hello"),
            .copied("hello")
        )
    }

    func testChangedPasteboardWithoutUsableTextIsNoText() {
        XCTAssertEqual(
            InputAssistCopyFallbackLogic.judgeCopy(changeCountBefore: 1, changeCountNow: 2, pasteboardText: nil),
            .noText
        )
        XCTAssertEqual(
            InputAssistCopyFallbackLogic.judgeCopy(changeCountBefore: 1, changeCountNow: 3, pasteboardText: " \n\t"),
            .noText
        )
    }

    func testSecondWriterDuringSettleIsContended() {
        // 检测到变化(6)后又变了(7)：多方写入，不能把里面的文字当选区。
        XCTAssertEqual(
            InputAssistCopyFallbackLogic.judgeCopy(
                changeCountBefore: 5, changeCountAtDetection: 6, changeCountAfterSettle: 7, pasteboardText: "别人写的"
            ),
            .contended
        )
    }

    func testStableChangeAfterSettleIsCopied() {
        XCTAssertEqual(
            InputAssistCopyFallbackLogic.judgeCopy(
                changeCountBefore: 5, changeCountAtDetection: 6, changeCountAfterSettle: 6, pasteboardText: "选中的字"
            ),
            .copied("选中的字")
        )
    }

    func testNoChangeAtDetectionStaysUnchangedEvenIfLaterWritten() {
        // 超时没看到变化：由迟到写入的收尾任务处理，判定本身仍是未变化，旧内容不能当选区。
        XCTAssertEqual(
            InputAssistCopyFallbackLogic.judgeCopy(
                changeCountBefore: 5, changeCountAtDetection: 5, changeCountAfterSettle: 6, pasteboardText: "旧内容"
            ),
            .clipboardUnchanged
        )
    }

    func testTargetValidationOnlyChecksProcessAndSecureInput() {
        XCTAssertEqual(
            InputAssistCopyFallbackLogic.validateTarget(
                expectedProcessIdentifier: 100, currentProcessIdentifier: 100, isSecureEventInputEnabled: false
            ),
            .allowed
        )
        XCTAssertEqual(
            InputAssistCopyFallbackLogic.validateTarget(
                expectedProcessIdentifier: 100, currentProcessIdentifier: 200, isSecureEventInputEnabled: false
            ),
            .applicationChanged
        )
        XCTAssertEqual(
            InputAssistCopyFallbackLogic.validateTarget(
                expectedProcessIdentifier: nil, currentProcessIdentifier: nil, isSecureEventInputEnabled: false
            ),
            .applicationChanged
        )
        XCTAssertEqual(
            InputAssistCopyFallbackLogic.validateTarget(
                expectedProcessIdentifier: 100, currentProcessIdentifier: nil, isSecureEventInputEnabled: false
            ),
            .applicationChanged
        )
        XCTAssertEqual(
            InputAssistCopyFallbackLogic.validateTarget(
                expectedProcessIdentifier: 100, currentProcessIdentifier: 100, isSecureEventInputEnabled: true
            ),
            .secureInputActive
        )
    }
}
