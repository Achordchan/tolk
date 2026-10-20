import Foundation

/// 协议里的一行 JSON。形状见 PROTOCOL.md「消息形状」：
/// - 请求  `{"id":"g1","op":"hello", ...参数}`
/// - 应答  `{"id":"g1","ok":true, ...结果}` / `{"id":"g1","ok":false,"error":"…"}`
/// - 事件  `{"event":"screenshot.state", ...}`
public enum Frame: Equatable {
    case request(id: String, op: String, params: [String: JSONValue])
    case response(id: String, ok: Bool, fields: [String: JSONValue], error: String?)
    case event(name: String, fields: [String: JSONValue])

    public static func success(id: String, _ fields: [String: JSONValue] = [:]) -> Frame {
        .response(id: id, ok: true, fields: fields, error: nil)
    }

    public static func failure(id: String, _ message: String) -> Frame {
        .response(id: id, ok: false, fields: [:], error: message)
    }
}

public enum FrameError: Error, Equatable, CustomStringConvertible {
    case invalidJSON
    case notAnObject
    case unrecognizedShape

    public var description: String {
        switch self {
        case .invalidJSON: return "不是合法的 JSON"
        case .notAnObject: return "不是 JSON 对象"
        case .unrecognizedShape: return "既不是请求、应答也不是事件"
        }
    }
}

public enum FrameCodec {
    public static func decode(_ line: Data) throws -> Frame {
        let object: Any
        do {
            object = try JSONSerialization.jsonObject(with: line, options: [.fragmentsAllowed])
        } catch {
            throw FrameError.invalidJSON
        }
        guard let raw = object as? [String: Any] else { throw FrameError.notAnObject }

        var fields: [String: JSONValue] = [:]
        for (key, value) in raw {
            // 表示不了的值（理论上 JSON 里不会有）直接略去。
            if let converted = JSONValue(propertyList: value) {
                fields[key] = converted
            }
        }

        if let name = fields["event"]?.stringValue {
            fields["event"] = nil
            return .event(name: name, fields: fields)
        }
        if let id = fields["id"]?.stringValue {
            if let op = fields["op"]?.stringValue {
                fields["id"] = nil
                fields["op"] = nil
                return .request(id: id, op: op, params: fields)
            }
            if let ok = fields["ok"]?.boolValue {
                let error = fields["error"]?.stringValue
                fields["id"] = nil
                fields["ok"] = nil
                fields["error"] = nil
                return .response(id: id, ok: ok, fields: fields, error: error)
            }
        }
        throw FrameError.unrecognizedShape
    }

    public static func decode(_ line: String) throws -> Frame {
        try decode(Data(line.utf8))
    }

    /// 编码成一行（不含换行符）。键按字典序排列，输出稳定，方便测试和排查。
    public static func encodeLine(_ frame: Frame) -> Data {
        var object: [String: Any] = [:]
        switch frame {
        case .request(let id, let op, let params):
            for (key, value) in params { object[key] = value.foundationObject() }
            object["id"] = id
            object["op"] = op
        case .response(let id, let ok, let fields, let error):
            for (key, value) in fields { object[key] = value.foundationObject() }
            object["id"] = id
            object["ok"] = NSNumber(value: ok)
            if let error {
                object["error"] = error
            } else if !ok {
                object["error"] = "未知错误"
            }
        case .event(let name, let fields):
            for (key, value) in fields { object[key] = value.foundationObject() }
            object["event"] = name
        }
        let options: JSONSerialization.WritingOptions = [.sortedKeys, .withoutEscapingSlashes]
        if let data = try? JSONSerialization.data(withJSONObject: object, options: options) {
            return data
        }
        // 走到这里只可能是数据里混进了非法值；退一个最小的错误应答，别让对方一直等。
        let fallback: [String: Any] = ["ok": false, "error": "内部错误：无法编码应答"]
        return (try? JSONSerialization.data(withJSONObject: fallback, options: [.sortedKeys])) ?? Data()
    }

    /// 编码成要写进 stdout 的完整一行（带结尾 `\n`）。
    public static func encode(_ frame: Frame) -> Data {
        var data = encodeLine(frame)
        data.append(0x0A)
        return data
    }
}
