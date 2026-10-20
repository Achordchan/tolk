import Foundation

/// 缓存键。引擎指纹由 Tolk 通过 `inputassist.engine` 给出（含模型名、base url，不含密钥），
/// 换引擎、换模型后旧缓存不再命中。
public struct InputAssistCacheKey: Hashable, Sendable {
    public let sourceText: String
    public let sourceLanguageCode: String
    public let targetLanguageCode: String
    public let engineFingerprint: String

    public init(
        sourceText: String,
        sourceLanguageCode: String,
        targetLanguageCode: String,
        engineFingerprint: String
    ) {
        self.sourceText = sourceText
        self.sourceLanguageCode = sourceLanguageCode
        self.targetLanguageCode = targetLanguageCode
        self.engineFingerprint = engineFingerprint
    }

    public var storageKey: String {
        [engineFingerprint, sourceLanguageCode, targetLanguageCode, sourceText]
            .joined(separator: "\u{1F}")
    }
}

public struct InputAssistCacheEntry: Codable, Equatable {
    public let key: String
    public let translatedText: String
    public let createdAt: Date
    public var lastUsedSequence: UInt64

    public init(key: String, translatedText: String, createdAt: Date, lastUsedSequence: UInt64) {
        self.key = key
        self.translatedText = translatedText
        self.createdAt = createdAt
        self.lastUsedSequence = lastUsedSequence
    }

    /// 粗略的字节占用：用于 LRU 容量控制。
    public var estimatedByteCount: Int {
        key.utf8.count + translatedText.utf8.count + 32
    }
}

/// 缓存的内存索引（LRU）。用「单调递增序号」记录访问顺序，查询和写入都是 O(1)，
/// 只有超出容量时才排序淘汰。
public struct InputAssistCacheIndex: Equatable {
    public private(set) var entries: [String: InputAssistCacheEntry] = [:]
    public private(set) var totalByteCount: Int = 0
    private var sequence: UInt64 = 0

    public let maximumByteCount: Int
    public let maximumEntryCount: Int

    public init(maximumByteCount: Int = 8 * 1024 * 1024, maximumEntryCount: Int = 5_000) {
        self.maximumByteCount = max(1, maximumByteCount)
        self.maximumEntryCount = max(1, maximumEntryCount)
    }

    public var count: Int { entries.count }

    public mutating func value(for key: String) -> String? {
        guard var entry = entries[key] else { return nil }
        sequence &+= 1
        entry.lastUsedSequence = sequence
        entries[key] = entry
        return entry.translatedText
    }

    public mutating func insert(_ translatedText: String, for key: String, createdAt: Date = Date()) {
        sequence &+= 1
        let entry = InputAssistCacheEntry(
            key: key,
            translatedText: translatedText,
            createdAt: createdAt,
            lastUsedSequence: sequence
        )
        if let previous = entries.updateValue(entry, forKey: key) {
            totalByteCount -= previous.estimatedByteCount
        }
        totalByteCount += entry.estimatedByteCount
        evictIfNeeded()
    }

    public mutating func removeAll() {
        entries.removeAll()
        totalByteCount = 0
        sequence = 0
    }

    public mutating func load(_ loaded: [InputAssistCacheEntry]) {
        entries = Dictionary(loaded.map { ($0.key, $0) }, uniquingKeysWith: { $1 })
        totalByteCount = entries.values.reduce(0) { $0 + $1.estimatedByteCount }
        sequence = entries.values.map { $0.lastUsedSequence }.max() ?? 0
        evictIfNeeded()
    }

    /// 落盘用：按访问顺序从旧到新，下次加载后仍然保有 LRU 次序。
    public func sortedEntries() -> [InputAssistCacheEntry] {
        entries.values.sorted { $0.lastUsedSequence < $1.lastUsedSequence }
    }

    private mutating func evictIfNeeded() {
        guard entries.count > maximumEntryCount || totalByteCount > maximumByteCount else {
            return
        }
        for entry in sortedEntries() {
            guard entries.count > maximumEntryCount || totalByteCount > maximumByteCount else {
                return
            }
            entries[entry.key] = nil
            totalByteCount -= entry.estimatedByteCount
        }
    }
}
