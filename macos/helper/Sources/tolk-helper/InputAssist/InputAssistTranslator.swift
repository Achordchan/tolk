import Foundation
import TolkHelperCore

/// 当前翻译引擎的标识（来自 helper → Go 的 `inputassist.engine`）。
struct InputAssistEngineInfo: Equatable {
    /// 如 google、openai_compat:模型名@base_url、apple。空串 = 取不到，此时不用缓存。
    let fingerprint: String
    /// 引擎显示名，⚡ 提示和 ⌥ 调试信息里用。
    let title: String

    /// 引擎是 Apple 本地翻译时，Go 会把请求回调给 helper 的 `apple.translate`，
    /// 而它同一时刻只能处理一个请求（新请求会打断旧的），所以各语言要串行发出。
    var isApple: Bool {
        let lowered = fingerprint.lowercased()
        return lowered == "apple" || lowered.hasPrefix("apple:")
    }

    var cacheable: Bool { !fingerprint.isEmpty }

    /// 标识没取到（`.unknown`）时没法排除它是 Apple，保守地也串行。
    var serializesRequests: Bool { isApple || !cacheable }

    static let unknown = InputAssistEngineInfo(fingerprint: "", title: "Tolk")
}

/// 一次候选请求的不可变快照。
struct InputAssistTranslationRequest {
    let sourceText: String
    let sourceLanguageCode: String
    let targetLanguageCodes: [String]
}

/// 多目标语言的翻译编排。
///
/// 每个目标语言一次 helper → Go 的 `translate`：谁先回来谁先更新自己那一行，单条失败不拖垮整个面板。
/// 引擎是 Apple 时串行发出。翻译引擎本身在 Go 里，helper 不实现任何引擎。
@MainActor
final class InputAssistTranslator {
    typealias RowUpdate = @MainActor (_ languageCode: String, _ state: CandidateRowState) -> Void

    private let cache: InputAssistTranslationCacheStore
    /// 本次会话已取到的引擎信息，重试时复用。
    private var engine: InputAssistEngineInfo?

    init(cache: InputAssistTranslationCacheStore = .shared) {
        self.cache = cache
    }

    // Apple 本地翻译同一时刻只能处理一个请求，新请求会打断旧的。
    // 首轮各语言和用户点的重试都走这把 FIFO 锁，互相不打断。
    private var appleBusy = false
    private var appleWaiters: [CheckedContinuation<Void, Never>] = []

    /// 一次持锁的凭据，只会真正释放一次。
    @MainActor
    private final class AppleLease {
        private weak var owner: InputAssistTranslator?
        private var released = false
        init(owner: InputAssistTranslator) { self.owner = owner }
        func release() {
            guard !released else { return }
            released = true
            owner?.releaseApple()
        }
    }

    private func acquireApple() async -> AppleLease {
        if appleBusy {
            await withCheckedContinuation { appleWaiters.append($0) }
        } else {
            appleBusy = true
        }
        return AppleLease(owner: self)
    }

    private func releaseApple() {
        if appleWaiters.isEmpty {
            appleBusy = false
        } else {
            appleWaiters.removeFirst().resume()
        }
    }

    /// 向 Go 要当前引擎的标识。失败时返回 `.unknown`（照常翻译，只是不读写缓存）。
    func fetchEngine() async -> InputAssistEngineInfo {
        let reply = await Bridge.shared.request(op: "inputassist.engine", params: [:], timeout: 10)
        guard case .response(_, let ok, let fields, _) = reply, ok,
              let fingerprint = fields["fingerprint"]?.stringValue
        else {
            Bridge.log(.warn, "输入助手：取不到当前翻译引擎的标识，本次不使用缓存")
            return .unknown
        }
        let title = fields["title"]?.stringValue ?? ""
        return InputAssistEngineInfo(fingerprint: fingerprint, title: title.isEmpty ? "Tolk" : title)
    }

    /// 启动一次多语言翻译。返回的 Task 取消后所有子任务一起停。
    func start(request: InputAssistTranslationRequest, onRowUpdate: @escaping RowUpdate) -> Task<Void, Never> {
        engine = nil
        return Task { @MainActor [weak self] in
            guard let self else { return }
            let info = await self.fetchEngine()
            guard !Task.isCancelled else { return }
            self.engine = info

            if info.serializesRequests {
                for code in request.targetLanguageCodes {
                    if Task.isCancelled { return }
                    await self.translateOne(
                        request: request, engine: info, targetLanguageCode: code,
                        allowsLanguagePackDownload: false, onRowUpdate: onRowUpdate
                    )
                }
                return
            }

            await withTaskGroup(of: Void.self) { group in
                for code in request.targetLanguageCodes {
                    group.addTask { @MainActor in
                        await self.translateOne(
                            request: request, engine: info, targetLanguageCode: code,
                            allowsLanguagePackDownload: false, onRowUpdate: onRowUpdate
                        )
                    }
                }
                await group.waitForAll()
            }
        }
    }

    /// 单条失败后的重试。正常结果不提供手动重译。
    func retry(
        request: InputAssistTranslationRequest,
        targetLanguageCode: String,
        onRowUpdate: @escaping RowUpdate
    ) -> Task<Void, Never> {
        Task { @MainActor [weak self] in
            guard let self else { return }
            onRowUpdate(targetLanguageCode, .loading)
            let info: InputAssistEngineInfo
            if let known = self.engine {
                info = known
            } else {
                info = await self.fetchEngine()
                self.engine = info
            }
            guard !Task.isCancelled else { return }
            // 重试是用户主动点的：这时候才允许去触发系统的语言包下载。
            await self.translateOne(
                request: request, engine: info, targetLanguageCode: targetLanguageCode,
                allowsLanguagePackDownload: true, onRowUpdate: onRowUpdate
            )
        }
    }

    // MARK: - Private

    private func translateOne(
        request: InputAssistTranslationRequest,
        engine: InputAssistEngineInfo,
        targetLanguageCode: String,
        allowsLanguagePackDownload: Bool,
        onRowUpdate: RowUpdate
    ) async {
        guard !Task.isCancelled else { return }
        let started = Date()
        let cacheKey = InputAssistCacheKey(
            sourceText: request.sourceText,
            sourceLanguageCode: request.sourceLanguageCode,
            targetLanguageCode: targetLanguageCode,
            engineFingerprint: engine.fingerprint
        )

        if engine.cacheable, let cached = await cache.value(for: cacheKey) {
            guard !Task.isCancelled else { return }
            onRowUpdate(targetLanguageCode, .translated(
                text: cached,
                source: .cache,
                latencyMilliseconds: milliseconds(since: started),
                engineTitle: engine.title
            ))
            return
        }

        let lease: AppleLease? = engine.serializesRequests ? await acquireApple() : nil
        defer { lease?.release() }
        guard !Task.isCancelled else { return }

        if engine.isApple, !allowsLanguagePackDownload {
            // 缺语言包只影响这一行，其它语言继续。首次不自动下载：那会弹系统对话框打断用户输入。
            // 用户点了那一行才往下走，Apple 翻译会引导完成下载。
            if await appleLanguagePackMissing(
                sourceLanguageCode: request.sourceLanguageCode,
                targetLanguageCode: targetLanguageCode
            ) {
                guard !Task.isCancelled else { return }
                Bridge.log(.warn, "输入助手：Apple 语言模型未安装：\(request.sourceLanguageCode) -> \(targetLanguageCode)")
                onRowUpdate(targetLanguageCode, .languagePackRequired)
                return
            }
        }

        // 缓存查询和 Apple 可用性检查都可能挂起：这期间面板被关掉或功能被关掉的话，
        // 不能再发翻译请求（OpenAI 之类是计费的）。
        guard !Task.isCancelled else { return }
        // Go 那头的请求不会因为取消而提前返回（最长等 300 秒）。会话被收起后
        // 立刻把锁交出去：Apple 服务本来就会让新请求打断旧请求，被遗弃的那个自然结束。
        let result = await withTaskCancellationHandler {
            await HelperTranslation.translateWithEngine(
                texts: [request.sourceText],
                sourceLanguageCode: request.sourceLanguageCode,
                targetLanguageCode: targetLanguageCode
            )
        } onCancel: {
            Task { @MainActor in lease?.release() }
        }
        guard !Task.isCancelled else { return }

        switch result {
        case .success(let reply):
            let translated = reply.texts.first ?? ""
            let trimmed = translated.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !trimmed.isEmpty else {
                onRowUpdate(targetLanguageCode, .failed(message: "翻译结果为空"))
                return
            }
            // 翻译期间引擎被换掉（回报的标识和取到的不一致）就不写缓存，免得串到别的引擎名下。
            if engine.cacheable, reply.fingerprint == engine.fingerprint {
                await cache.store(translated, for: cacheKey)
            }
            onRowUpdate(targetLanguageCode, .translated(
                text: translated,
                source: .network,
                latencyMilliseconds: milliseconds(since: started),
                engineTitle: engine.title
            ))
        case .failure(let error):
            onRowUpdate(targetLanguageCode, .failed(message: error.localizedDescription))
        }
    }

    /// 本机查一下 Apple 翻译的语言包装没装（`supported` = 受支持但要先下载）。
    private func appleLanguagePackMissing(sourceLanguageCode: String, targetLanguageCode: String) async -> Bool {
        // 没认出原文语言时 status 只能拿英语/中文估一个语言对，估出来「已安装」
        // 并不代表真实语言对可用；保守处理成需要用户点击，由 Apple 翻译引导下载。
        if sourceLanguageCode == LanguagePreset.auto.code { return true }
        if #available(macOS 15.1, *) {
            let state = await Ops.apple.status(
                sourceLanguageCode: sourceLanguageCode,
                targetLanguageCode: targetLanguageCode
            )
            return state == "supported"
        }
        return false
    }

    private func milliseconds(since start: Date) -> Int {
        max(0, Int(Date().timeIntervalSince(start) * 1000))
    }
}
