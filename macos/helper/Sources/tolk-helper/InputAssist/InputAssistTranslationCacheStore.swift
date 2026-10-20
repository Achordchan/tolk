import Foundation
import TolkHelperCore

/// L1 内存 + L2 本地持久的两级缓存。
///
/// 读永远只走内存，所以命中时能很快进入 UI 更新；落盘是节流后的后台写，不挡候选展示。
/// 文件放在 ~/Library/Caches/Tolk/（缓存可被系统清理，丢了只是少几个 ⚡），不用旧版的路径。
actor InputAssistTranslationCacheStore {
    static let shared = InputAssistTranslationCacheStore()

    private var index: InputAssistCacheIndex
    private let fileURL: URL?
    private var hasLoaded = false
    private var isDirty = false
    private var flushTask: Task<Void, Never>?
    private let flushDelayNanoseconds: UInt64

    init(
        index: InputAssistCacheIndex = InputAssistCacheIndex(),
        fileURL: URL? = InputAssistTranslationCacheStore.defaultFileURL(),
        flushDelayNanoseconds: UInt64 = 1_000_000_000
    ) {
        self.index = index
        self.fileURL = fileURL
        self.flushDelayNanoseconds = flushDelayNanoseconds
    }

    static func defaultFileURL() -> URL? {
        guard let base = FileManager.default.urls(
            for: .cachesDirectory,
            in: .userDomainMask
        ).first else {
            return nil
        }
        return base
            .appendingPathComponent("Tolk", isDirectory: true)
            .appendingPathComponent("InputAssistTranslationCache.json", isDirectory: false)
    }

    func value(for key: InputAssistCacheKey) -> String? {
        loadIfNeeded()
        guard let value = index.value(for: key.storageKey) else { return nil }
        // 命中会刷新 LRU 序号，也要落盘，否则重启后淘汰顺序和实际使用对不上。
        markDirty()
        return value
    }

    func store(_ translatedText: String, for key: InputAssistCacheKey) {
        let trimmed = translatedText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        loadIfNeeded()
        index.insert(translatedText, for: key.storageKey)
        markDirty()
    }

    func clear() {
        index.removeAll()
        hasLoaded = true
        isDirty = false
        flushTask?.cancel()
        flushTask = nil
        if let fileURL {
            try? FileManager.default.removeItem(at: fileURL)
        }
    }

    func flushNow() {
        flushTask?.cancel()
        flushTask = nil
        writeToDisk()
    }

    private func loadIfNeeded() {
        guard !hasLoaded else { return }
        hasLoaded = true
        guard let fileURL, let data = try? Data(contentsOf: fileURL) else { return }
        guard let decoded = try? JSONDecoder().decode([InputAssistCacheEntry].self, from: data) else {
            // 文件损坏时直接当作空缓存重来，不要让它把整个功能拖住。
            try? FileManager.default.removeItem(at: fileURL)
            return
        }
        index.load(decoded)
    }

    private func markDirty() {
        isDirty = true
        guard flushTask == nil else { return }
        let delay = flushDelayNanoseconds
        flushTask = Task { [weak self] in
            try? await Task.sleep(nanoseconds: delay)
            guard !Task.isCancelled else { return }
            await self?.performScheduledFlush()
        }
    }

    private func performScheduledFlush() {
        flushTask = nil
        writeToDisk()
    }

    private func writeToDisk() {
        guard isDirty, let fileURL else { return }
        isDirty = false
        let entries = index.sortedEntries()
        do {
            try FileManager.default.createDirectory(
                at: fileURL.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )
            let data = try JSONEncoder().encode(entries)
            try data.write(to: fileURL, options: .atomic)
        } catch {
            // 缓存写不进去不影响功能，下次再试即可，不打扰用户。
            isDirty = true
        }
    }
}
