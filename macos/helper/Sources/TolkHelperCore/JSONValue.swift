import Foundation

/// 协议里出现的任意 JSON 值。不用 Codable：`JSONDecoder` 对数字和布尔的区分在不同系统版本上
/// 不完全一致，这里直接走 `JSONSerialization`，自己判断类型。
public enum JSONValue: Equatable {
    case null
    case bool(Bool)
    case int(Int64)
    case double(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])
}

public extension JSONValue {
    var stringValue: String? {
        if case .string(let s) = self { return s }
        return nil
    }

    var boolValue: Bool? {
        if case .bool(let b) = self { return b }
        return nil
    }

    var intValue: Int? {
        switch self {
        case .int(let i): return Int(exactly: i)
        case .double(let d): return Int(exactly: d)
        default: return nil
        }
    }

    var arrayValue: [JSONValue]? {
        if case .array(let a) = self { return a }
        return nil
    }

    var objectValue: [String: JSONValue]? {
        if case .object(let o) = self { return o }
        return nil
    }

    /// `["a","b"]` -> `["a","b"]`；任何一项不是字符串就整体返回 nil。
    var stringArray: [String]? {
        guard case .array(let items) = self else { return nil }
        var result: [String] = []
        result.reserveCapacity(items.count)
        for item in items {
            guard case .string(let s) = item else { return nil }
            result.append(s)
        }
        return result
    }

    /// 从 `JSONSerialization` / `UserDefaults` 里读出来的值转换。
    /// 遇到 JSON 表示不了的类型（Data、Date、NaN、无穷大等）返回 nil；数组、字典里有一项表示不了，整体也返回 nil。
    init?(propertyList value: Any) {
        if value is NSNull {
            self = .null
            return
        }
        if let string = value as? String {
            self = .string(string)
            return
        }
        if let number = value as? NSNumber {
            if CFGetTypeID(number) == CFBooleanGetTypeID() {
                self = .bool(number.boolValue)
                return
            }
            let type = String(cString: number.objCType)
            if type == "d" || type == "f" {
                let d = number.doubleValue
                guard d.isFinite else { return nil }
                self = .double(d)
            } else if type == "Q" {
                // 无符号 64 位：放不进 Int64 就退成 Double。
                let u = number.uint64Value
                if let i = Int64(exactly: u) {
                    self = .int(i)
                } else {
                    self = .double(Double(u))
                }
            } else {
                self = .int(number.int64Value)
            }
            return
        }
        if let array = value as? [Any] {
            var items: [JSONValue] = []
            items.reserveCapacity(array.count)
            for element in array {
                guard let converted = JSONValue(propertyList: element) else { return nil }
                items.append(converted)
            }
            self = .array(items)
            return
        }
        if let dictionary = value as? [String: Any] {
            var object: [String: JSONValue] = [:]
            object.reserveCapacity(dictionary.count)
            for (key, element) in dictionary {
                guard let converted = JSONValue(propertyList: element) else { return nil }
                object[key] = converted
            }
            self = .object(object)
            return
        }
        return nil
    }

    /// 转成可以交给 `JSONSerialization` 的对象。非有限小数写成 null，免得序列化时抛 ObjC 异常。
    func foundationObject() -> Any {
        switch self {
        case .null:
            return NSNull()
        case .bool(let b):
            return NSNumber(value: b)
        case .int(let i):
            return NSNumber(value: i)
        case .double(let d):
            return d.isFinite ? NSNumber(value: d) : NSNull()
        case .string(let s):
            return s
        case .array(let items):
            return items.map { $0.foundationObject() }
        case .object(let object):
            return object.mapValues { $0.foundationObject() }
        }
    }
}

extension JSONValue: ExpressibleByStringLiteral {
    public init(stringLiteral value: String) { self = .string(value) }
}

extension JSONValue: ExpressibleByBooleanLiteral {
    public init(booleanLiteral value: Bool) { self = .bool(value) }
}

extension JSONValue: ExpressibleByIntegerLiteral {
    public init(integerLiteral value: Int64) { self = .int(value) }
}

extension JSONValue: ExpressibleByArrayLiteral {
    public init(arrayLiteral elements: JSONValue...) { self = .array(elements) }
}

extension JSONValue: ExpressibleByDictionaryLiteral {
    public init(dictionaryLiteral elements: (String, JSONValue)...) {
        var object: [String: JSONValue] = [:]
        for (key, value) in elements { object[key] = value }
        self = .object(object)
    }
}
