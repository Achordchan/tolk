import Foundation
import TolkHelperCore

/// stdin / stdout 上的 JSON 行通道。stdout 只写协议行，其他一律走 stderr。
@MainActor
final class Bridge {
    static let shared = Bridge()

    enum LogLevel: String {
        case debug, info, warn, error
    }

    private var nextRequestID = 0
    private var pending: [String: CheckedContinuation<Frame, Never>] = [:]
    private var handler: ((String, String, [String: JSONValue]) async -> Frame)?

    nonisolated private static let writeLock = NSLock()

    // MARK: - 启动

    /// `handle(id, op, params)` 返回要回给 Go 的应答帧。
    func start(handle: @escaping (String, String, [String: JSONValue]) async -> Frame) {
        handler = handle
        let thread = Thread {
            Bridge.readLoop()
        }
        thread.name = "tolk-helper.stdin"
        thread.start()
    }

    /// 后台线程：按行切 stdin，每行交给主线程处理。EOF 即 parent 退出，helper 跟着退出。
    private nonisolated static func readLoop() {
        var buffer = Data()
        var chunk = [UInt8](repeating: 0, count: 64 * 1024)
        while true {
            let n = read(STDIN_FILENO, &chunk, chunk.count)
            if n < 0 {
                if errno == EINTR { continue }
                break
            }
            if n == 0 { break }
            buffer.append(chunk, count: n)
            while let newline = buffer.firstIndex(of: 0x0A) {
                let line = buffer.subdata(in: buffer.startIndex..<newline)
                buffer.removeSubrange(buffer.startIndex...newline)
                if line.allSatisfy({ $0 == 0x20 || $0 == 0x0D || $0 == 0x09 }) { continue }
                DispatchQueue.main.async {
                    MainActor.assumeIsolated {
                        Bridge.shared.receive(line)
                    }
                }
            }
        }
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                Lifecycle.terminate(reason: "stdin 已关闭")
            }
        }
    }

    private func receive(_ line: Data) {
        let frame: Frame
        do {
            frame = try FrameCodec.decode(line)
        } catch {
            Bridge.stderr("无法解析收到的一行：\(error)")
            return
        }
        switch frame {
        case .request(let id, let op, let params):
            guard let handler else { return }
            Task { @MainActor in
                let reply = await handler(id, op, params)
                Bridge.send(reply)
            }
        case .response(let id, _, _, _):
            if let continuation = pending.removeValue(forKey: id) {
                continuation.resume(returning: frame)
            }
        case .event:
            break
        }
    }

    // MARK: - 发送

    nonisolated static func send(_ frame: Frame) {
        let data = FrameCodec.encode(frame)
        writeLock.lock()
        defer { writeLock.unlock() }
        data.withUnsafeBytes { (raw: UnsafeRawBufferPointer) in
            guard var pointer = raw.baseAddress else { return }
            var remaining = raw.count
            while remaining > 0 {
                let written = write(STDOUT_FILENO, pointer, remaining)
                if written < 0 {
                    if errno == EINTR { continue }
                    // 管道断了：parent 已经不在，没必要继续。
                    if errno == EPIPE { exit(0) }
                    return
                }
                pointer = pointer.advanced(by: written)
                remaining -= written
            }
        }
    }

    nonisolated static func emit(_ name: String, _ fields: [String: JSONValue] = [:]) {
        send(.event(name: name, fields: fields))
    }

    nonisolated static func log(_ level: LogLevel, _ message: String) {
        stderr("[\(level.rawValue)] \(message)")
        emit("log", ["level": .string(level.rawValue), "msg": .string(message)])
    }

    nonisolated static func stderr(_ message: String) {
        FileHandle.standardError.write(Data((message + "\n").utf8))
    }

    // MARK: - helper -> Go 的请求

    /// 发请求并等对方应答；`timeout` 秒内没有回音就给一个失败应答。
    func request(op: String, params: [String: JSONValue], timeout: TimeInterval = 300) async -> Frame {
        nextRequestID += 1
        let id = "h\(nextRequestID)"
        return await withCheckedContinuation { continuation in
            pending[id] = continuation
            Bridge.send(.request(id: id, op: op, params: params))
            Task { @MainActor [weak self] in
                try? await Task.sleep(nanoseconds: UInt64(timeout * 1_000_000_000))
                if let continuation = self?.pending.removeValue(forKey: id) {
                    continuation.resume(returning: .failure(id: id, "等待 Tolk 回应超时"))
                }
            }
        }
    }
}

/// helper → Go 的 `translate`。
enum HelperTranslation {
    @MainActor
    static func translate(texts: [String], sourceLanguageCode: String, targetLanguageCode: String) async -> Result<[String], Error> {
        let reply = await Bridge.shared.request(op: "translate", params: [
            "texts": .array(texts.map { JSONValue.string($0) }),
            "source": .string(TolkLanguages.labelForRequest(code: sourceLanguageCode)),
            "target": .string(TolkLanguages.labelForRequest(code: targetLanguageCode))
        ])
        guard case .response(_, let ok, let fields, let error) = reply else {
            return .failure(HelperTranslateError("翻译服务返回了无法识别的数据"))
        }
        guard ok else {
            return .failure(HelperTranslateError(error ?? "翻译失败"))
        }
        guard let result = fields["texts"]?.stringArray else {
            return .failure(HelperTranslateError("翻译服务返回的数据不对"))
        }
        return .success(result)
    }
}
