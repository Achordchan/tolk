import XCTest
@testable import TolkHelperCore

final class FrameTests: XCTestCase {
    func testDecodeRequest() throws {
        let frame = try FrameCodec.decode(#"{"id":"g1","op":"apple.translate","texts":["a","b"],"source":"英语","target":"简体中文"}"#)
        guard case .request(let id, let op, let params) = frame else {
            return XCTFail("应该是请求")
        }
        XCTAssertEqual(id, "g1")
        XCTAssertEqual(op, "apple.translate")
        XCTAssertEqual(params["texts"]?.stringArray, ["a", "b"])
        XCTAssertEqual(params["source"]?.stringValue, "英语")
        XCTAssertNil(params["id"])
        XCTAssertNil(params["op"])
    }

    func testDecodeSuccessResponse() throws {
        let frame = try FrameCodec.decode(#"{"id":"h3","ok":true,"texts":["你好"]}"#)
        XCTAssertEqual(frame, .response(id: "h3", ok: true, fields: ["texts": ["你好"]], error: nil))
    }

    func testDecodeErrorResponse() throws {
        let frame = try FrameCodec.decode(#"{"id":"h3","ok":false,"error":"网络不通"}"#)
        XCTAssertEqual(frame, .response(id: "h3", ok: false, fields: [:], error: "网络不通"))
    }

    func testDecodeEvent() throws {
        let frame = try FrameCodec.decode(#"{"event":"log","level":"info","msg":"hi"}"#)
        XCTAssertEqual(frame, .event(name: "log", fields: ["level": "info", "msg": "hi"]))
    }

    func testDecodeRejectsGarbage() {
        XCTAssertThrowsError(try FrameCodec.decode("not json")) { error in
            XCTAssertEqual(error as? FrameError, .invalidJSON)
        }
        XCTAssertThrowsError(try FrameCodec.decode("[1,2]")) { error in
            XCTAssertEqual(error as? FrameError, .notAnObject)
        }
        XCTAssertThrowsError(try FrameCodec.decode(#"{"foo":1}"#)) { error in
            XCTAssertEqual(error as? FrameError, .unrecognizedShape)
        }
    }

    func testEncodeSuccessIsSingleLineAndStable() throws {
        let data = FrameCodec.encode(.success(id: "g1", ["texts": ["a\nb", "中文/斜杠"], "n": 3]))
        let text = String(decoding: data, as: UTF8.self)
        XCTAssertTrue(text.hasSuffix("\n"))
        XCTAssertEqual(text.filter { $0 == "\n" }.count, 1, "内容里的换行必须被转义")
        XCTAssertEqual(text, #"{"id":"g1","n":3,"ok":true,"texts":["a\nb","中文/斜杠"]}"# + "\n")
    }

    func testEncodeFailure() throws {
        let line = String(decoding: FrameCodec.encodeLine(.failure(id: "g2", "需要开启屏幕录制权限")), as: UTF8.self)
        XCTAssertEqual(line, #"{"error":"需要开启屏幕录制权限","id":"g2","ok":false}"#)
    }

    func testEncodeBooleansAreNotNumbers() {
        let line = String(decoding: FrameCodec.encodeLine(.success(id: "g1", ["accessibility": true, "screenRecording": false])), as: UTF8.self)
        XCTAssertEqual(line, #"{"accessibility":true,"id":"g1","ok":true,"screenRecording":false}"#)
    }

    func testReservedKeysWinOverFields() throws {
        let frame = Frame.event(name: "screenshot.done", fields: ["event": "evil", "text": "t"])
        let decoded = try FrameCodec.decode(FrameCodec.encodeLine(frame))
        XCTAssertEqual(decoded, .event(name: "screenshot.done", fields: ["text": "t"]))
    }

    func testRoundTripRequest() throws {
        let frame = Frame.request(id: "h1", op: "translate", params: [
            "texts": ["Hello", "World"],
            "source": "自动检测",
            "target": "简体中文"
        ])
        XCTAssertEqual(try FrameCodec.decode(FrameCodec.encodeLine(frame)), frame)
    }

    func testNonFiniteDoubleBecomesNull() {
        let line = String(decoding: FrameCodec.encodeLine(.event(name: "x", fields: ["v": .double(.nan)])), as: UTF8.self)
        XCTAssertEqual(line, #"{"event":"x","v":null}"#)
    }
}
