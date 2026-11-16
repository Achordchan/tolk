import CoreGraphics
import XCTest
@testable import TolkHelperCore

final class PanelPlacementTests: XCTestCase {
    // 主屏 1440×900 在左，副屏 1920×1080 在右（AppKit 坐标，y 向上；菜单栏 25、Dock 不算）。
    let primary = PanelPlacement.Screen(frame: CGRect(x: 0, y: 0, width: 1440, height: 900),
                                        visible: CGRect(x: 0, y: 0, width: 1440, height: 875))
    let secondary = PanelPlacement.Screen(frame: CGRect(x: 1440, y: 0, width: 1920, height: 1080),
                                          visible: CGRect(x: 1440, y: 0, width: 1920, height: 1055))

    func testPicksTheScreenHoldingMostOfTheWindow() {
        // 只有 40pt 留在主屏，其余都在副屏。
        let window = CGRect(x: 1400, y: 300, width: 800, height: 600)
        XCTAssertEqual(PanelPlacement.screen(for: window, among: [primary, secondary]), secondary)
        XCTAssertNil(PanelPlacement.screen(for: CGRect(x: 5000, y: 0, width: 10, height: 10), among: [primary, secondary]))
    }

    func testStripStaysUnderAWindowMostlyOnTheSecondScreen() {
        let window = CGRect(x: 1400, y: 300, width: 800, height: 600)
        let f = PanelPlacement.frame(below: window, height: 72, gap: 4, screens: [primary, secondary], fallback: primary)
        // 按主屏限位的话会被拉回 x ≤ 1440-520-8；按副屏就留在窗口正下方居中。
        XCTAssertEqual(f, CGRect(x: 1540, y: 224, width: 520, height: 72))
    }

    func testStripTucksInsideWhenThereIsNoRoomBelow() {
        let window = CGRect(x: 200, y: 20, width: 600, height: 500)
        let f = PanelPlacement.frame(below: window, height: 72, gap: 4, screens: [primary, secondary], fallback: primary)
        XCTAssertEqual(f.minY, 34)
        XCTAssertEqual(f.width, 520)
        XCTAssertEqual(f.midX, window.midX)
    }

    func testStripKeepsInsideTheScreenEdge() {
        let window = CGRect(x: -100, y: 400, width: 500, height: 300)
        let f = PanelPlacement.frame(below: window, height: 72, gap: 4, screens: [primary, secondary], fallback: primary)
        XCTAssertEqual(f.minX, 8)
        XCTAssertEqual(f.width, 460)
    }

    func testStripStaysOnScreenWhenTheWindowHangsBelowIt() {
        // 系统设置被拖到屏幕下沿以外，只露出上半截。
        let window = CGRect(x: 200, y: -200, width: 600, height: 500)
        let f = PanelPlacement.frame(below: window, height: 72, gap: 4, screens: [primary, secondary], fallback: primary)
        XCTAssertEqual(f.minY, 8)
        XCTAssertTrue(primary.visible.contains(f))
    }
}
