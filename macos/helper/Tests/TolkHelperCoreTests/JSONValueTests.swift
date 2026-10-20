import XCTest
@testable import TolkHelperCore

final class JSONValueTests: XCTestCase {
    func testPropertyListScalars() {
        XCTAssertEqual(JSONValue(propertyList: "abc"), .string("abc"))
        XCTAssertEqual(JSONValue(propertyList: NSNumber(value: true)), .bool(true))
        XCTAssertEqual(JSONValue(propertyList: NSNumber(value: false)), .bool(false))
        XCTAssertEqual(JSONValue(propertyList: NSNumber(value: 7)), .int(7))
        XCTAssertEqual(JSONValue(propertyList: NSNumber(value: 1.5)), .double(1.5))
    }

    func testBooleanIsNotMistakenForInteger() {
        XCTAssertNotEqual(JSONValue(propertyList: NSNumber(value: true)), .int(1))
        XCTAssertNotEqual(JSONValue(propertyList: NSNumber(value: 1)), .bool(true))
    }

    func testNonRepresentableValuesAreDropped() {
        XCTAssertNil(JSONValue(propertyList: Data([1, 2, 3])))
        XCTAssertNil(JSONValue(propertyList: Date()))
        XCTAssertNil(JSONValue(propertyList: NSNumber(value: Double.nan)))
        XCTAssertNil(JSONValue(propertyList: NSNumber(value: Double.infinity)))
        XCTAssertNil(JSONValue(propertyList: ["a", Date()] as [Any]))
        XCTAssertNil(JSONValue(propertyList: ["k": Data()] as [String: Any]))
    }

    func testNestedContainers() {
        let value = JSONValue(propertyList: ["list": ["a", NSNumber(value: 2)], "on": NSNumber(value: true)] as [String: Any])
        XCTAssertEqual(value, .object(["list": .array(["a", .int(2)]), "on": .bool(true)]))
    }

    func testStringArrayRequiresAllStrings() {
        XCTAssertEqual(JSONValue.array(["a", "b"]).stringArray, ["a", "b"])
        XCTAssertNil(JSONValue.array(["a", .int(1)]).stringArray)
        XCTAssertNil(JSONValue.string("a").stringArray)
    }
}
