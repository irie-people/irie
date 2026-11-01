import CryptoKit
import Foundation
import Network
import Security

// Talking to a machine's gateway (`irie remote serve`, docs/PHONE.md §2.1):
// its HTTP API, and the NDJSON streams of subscribe, which reconnect by
// themselves. Every connection is pinned to Irie's certificate authority
// for that machine (§3): the app trusts no other certificate, and needs no
// profile installed on the phone.

/// An error the gateway or the core gave, with its code and the words to show.
struct APIError: Error, LocalizedError, Equatable {
    var code: String
    var message: String
    var status = 0

    var errorDescription: String? { message }
    var unpaired: Bool { status == 401 || code == "unpaired" }
}

// MARK: - Trust

enum Trust {
    /// The SHA-256 of a certificate's DER bytes, as 64 uppercase hex digits.
    static func fingerprint(_ cert: SecCertificate) -> String {
        let der = SecCertificateCopyData(cert) as Data
        return SHA256.hash(data: der).map { String(format: "%02X", $0) }.joined()
    }

    /// A fingerprint as a pairing link carries it: 64 hex digits.
    static func normalized(_ text: String) -> String? {
        let hex = text.uppercased().filter { !$0.isWhitespace && $0 != ":" }
        guard hex.count == 64, hex.allSatisfy({ $0.isHexDigit }) else { return nil }
        return hex
    }

    /// Its first eight bytes in pairs, as the machine shows them:
    /// 53 BF B5 27 01 83 9A E6.
    static func start(_ fingerprint: String) -> String {
        let chars = Array(fingerprint.prefix(16))
        return stride(from: 0, to: chars.count, by: 2).map { String(chars[$0..<min($0 + 2, chars.count)]) }.joined(separator: " ")
    }

    enum Verdict {
        case trusted(authority: String)
        case refused(String)
    }

    /// Checks what a server sent against Irie's authority. With a pin, the
    /// authority is the certificate in the chain with that fingerprint;
    /// without one, as before a person confirms it, the chain's last. Either
    /// way the server's own certificate must be valid for the host dialed,
    /// under that authority alone.
    static func evaluate(_ trust: SecTrust, host: String, pin: String?) -> Verdict {
        guard let chain = SecTrustCopyCertificateChain(trust) as? [SecCertificate], chain.count >= 2 else {
            return .refused("\(host) didn’t send Irie’s certificate authority, so this phone can’t check it.")
        }
        let above = chain.dropFirst()
        let authority: SecCertificate
        if let pin {
            guard let ca = above.first(where: { fingerprint($0) == pin }) else {
                return .refused("\(host) answered with a certificate authority other than the one this phone has for it. Something else on this network may be answering in its place, so Irie won’t talk to it.")
            }
            authority = ca
        } else {
            authority = above[above.index(before: above.endIndex)]
        }
        SecTrustSetPolicies(trust, SecPolicyCreateSSL(true, host as CFString))
        SecTrustSetAnchorCertificates(trust, [authority] as CFArray)
        SecTrustSetAnchorCertificatesOnly(trust, true)
        var error: CFError?
        guard SecTrustEvaluateWithError(trust, &error) else {
            let why = error.map { CFErrorCopyDescription($0) as String } ?? "it isn’t valid"
            return .refused("\(host)’s certificate isn’t valid for that address under Irie’s authority: \(why)")
        }
        return .trusted(authority: fingerprint(authority))
    }
}

/// Answers each connection's server-trust challenge by Trust.evaluate, and
/// keeps what it saw: the authority a server presented, and why one was
/// refused, so a failed request can say so.
final class PinningDelegate: NSObject, URLSessionDelegate, URLSessionTaskDelegate {
    let pin: String?
    private let lock = NSLock()
    private var _seen: String?
    private var _refusal: String?

    init(pin: String?) {
        self.pin = pin
    }

    /// The authority the last trusted connection presented.
    var seen: String? { lock.withLock { _seen } }
    var refusal: String? { lock.withLock { _refusal } }

    func urlSession(_ session: URLSession, didReceive challenge: URLAuthenticationChallenge,
                    completionHandler: @escaping (URLSession.AuthChallengeDisposition, URLCredential?) -> Void) {
        answer(challenge, completionHandler)
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didReceive challenge: URLAuthenticationChallenge,
                    completionHandler: @escaping (URLSession.AuthChallengeDisposition, URLCredential?) -> Void) {
        answer(challenge, completionHandler)
    }

    private func answer(_ challenge: URLAuthenticationChallenge,
                        _ done: @escaping (URLSession.AuthChallengeDisposition, URLCredential?) -> Void) {
        guard challenge.protectionSpace.authenticationMethod == NSURLAuthenticationMethodServerTrust,
              let trust = challenge.protectionSpace.serverTrust else {
            // A gateway asks for nothing else; never offer a credential.
            done(.rejectProtectionSpace, nil)
            return
        }
        switch Trust.evaluate(trust, host: challenge.protectionSpace.host, pin: pin) {
        case .trusted(let authority):
            lock.withLock {
                _seen = authority
                _refusal = nil
            }
            done(.useCredential, URLCredential(trust: trust))
        case .refused(let why):
            lock.withLock { _refusal = why }
            done(.cancelAuthenticationChallenge, nil)
        }
    }
}

// MARK: - Gateway

/// One machine's gateway: its address, and a URL session pinned to its
/// authority. Without a pin it is a probe, for pairing by address.
final class Gateway: @unchecked Sendable {
    let base: URL
    let pin: String?
    let session: URLSession
    let delegate: PinningDelegate

    init(base: URL, pin: String?) {
        self.base = base
        self.pin = pin
        delegate = PinningDelegate(pin: pin)
        let cfg = URLSessionConfiguration.ephemeral
        cfg.requestCachePolicy = .reloadIgnoringLocalCacheData
        cfg.urlCache = nil
        cfg.httpShouldSetCookies = false
        cfg.httpCookieAcceptPolicy = .never
        cfg.waitsForConnectivity = false
        // Between bytes: a stream hears a ping every 15 s.
        cfg.timeoutIntervalForRequest = 45
        cfg.timeoutIntervalForResource = 7 * 24 * 3600
        session = URLSession(configuration: cfg, delegate: delegate, delegateQueue: nil)
    }

    deinit {
        session.invalidateAndCancel()
    }

    /// Its host and port, as the web app names a machine it can't reach.
    var host: String { Gateway.host(base) }

    static func host(_ url: URL) -> String {
        guard let h = url.host(percentEncoded: false) else { return url.absoluteString }
        let name = h.contains(":") ? "[\(h)]" : h
        return url.port.map { "\(name):\($0)" } ?? name
    }

    func request(_ path: String, method: String = "GET", token: String? = nil, json: Any? = nil, timeout: TimeInterval = 15) -> URLRequest {
        var r = URLRequest(url: base.appending(path: path), cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: timeout)
        r.httpMethod = method
        if let token { r.setValue("Bearer " + token, forHTTPHeaderField: "Authorization") }
        if let json {
            r.setValue("application/json", forHTTPHeaderField: "Content-Type")
            r.httpBody = try? JSONSerialization.data(withJSONObject: json)
        }
        return r
    }

    /// Sends a request and reads its answer, or throws the error to show.
    func send(_ r: URLRequest) async throws -> (Data, Int) {
        do {
            let (data, response) = try await session.data(for: r)
            return (data, (response as? HTTPURLResponse)?.statusCode ?? 0)
        } catch {
            throw problem(error)
        }
    }

    /// The words for a request that never got an answer.
    func problem(_ error: Error) -> APIError {
        let code = (error as? URLError)?.code
        // A connection the trust check turned away ends as cancelled, or as
        // a failed TLS handshake: say why it was turned away.
        if let why = delegate.refusal, code == .cancelled || code == .secureConnectionFailed || code == .serverCertificateUntrusted {
            return APIError(code: "untrusted", message: why)
        }
        switch code {
        case .timedOut?:
            return APIError(code: "timeout", message: "\(host) didn’t answer in time.")
        case .serverCertificateUntrusted?, .serverCertificateHasBadDate?, .serverCertificateNotYetValid?,
             .serverCertificateHasUnknownRoot?, .clientCertificateRejected?, .secureConnectionFailed?:
            return APIError(code: "untrusted", message: "\(host) answered, but not with a certificate this phone can check.")
        case .notConnectedToInternet?:
            return APIError(code: "offline", message: "Can’t reach \(host). Is this phone on Wi-Fi, and is Irie allowed on the local network (Settings › Privacy & Security › Local Network)?")
        case .cancelled?:
            return APIError(code: "cancelled", message: "Stopped before \(host) answered.")
        default:
            return APIError(code: "offline", message: "Can’t reach \(host). Is it awake and on this Wi-Fi?")
        }
    }

    /// An answer as JSON, or the error it carries.
    func decode<R: Decodable>(_ data: Data, _ status: Int, as: R.Type) throws -> R {
        if status == 204, let empty = Ignored() as? R { return empty }
        let env = try? Wire.decoder.decode(Envelope<Ignored>.self, from: data)
        guard (200..<300).contains(status) else {
            throw APIError(code: env?.error?.code ?? "http_\(status)", message: env?.error?.msg ?? "\(host) answered \(status).", status: status)
        }
        do {
            return try Wire.decoder.decode(R.self, from: data)
        } catch {
            throw APIError(code: "bad_answer", message: "\(host) sent an answer this app can’t read.")
        }
    }

    /// GET /api/info: the machine's id, name, version and API, with no pairing.
    func info() async throws -> MachineInfo {
        let (data, status) = try await send(request("api/info", timeout: 6))
        return try decode(data, status, as: MachineInfo.self)
    }

    /// POST /api/pair with the code the machine shows.
    func pair(code: String, device: String) async throws -> PairResult {
        let (data, status) = try await send(request("api/pair", method: "POST", json: ["code": code, "device": device], timeout: 10))
        return try decode(data, status, as: PairResult.self)
    }

    /// DELETE /api/device: the machine forgets this phone.
    func forget(token: String) async throws {
        let (data, status) = try await send(request("api/device", method: "DELETE", token: token, timeout: 6))
        _ = try decode(data, status, as: Ignored.self)
    }

    /// One op through /api/call: its result, or an APIError with the
    /// core's words. A 401 means the machine no longer knows this phone.
    func call<R: Decodable>(_ op: String, _ args: [String: Any] = [:], token: String, timeout: TimeInterval = 15, as: R.Type) async throws -> R {
        let (data, status) = try await send(request("api/call", method: "POST", token: token, json: ["op": op, "args": args], timeout: timeout))
        let env = try decode(data, status, as: Envelope<R>.self)
        guard env.ok == true else {
            throw APIError(code: env.error?.code ?? "error", message: env.error?.msg ?? "Something went wrong.")
        }
        if let r = env.result { return r }
        if let empty = Ignored() as? R { return empty }
        throw APIError(code: "bad_answer", message: "\(host) sent an answer this app can’t read.")
    }
}

// MARK: - Streams

/// A subscription: POST /api/subscribe, read as lines, one frame each. It
/// reconnects with backoff (0.5 s to 8 s) when the stream ends, as it does
/// when the core restarts, and takes 40 s without a line as a dead stream.
/// kick() reconnects at once, for when the app comes back to the front.
@MainActor
final class FrameStream {
    enum Phase { case connecting, connected, offline }

    var onFrame: (Frame) -> Void = { _ in }
    var onPhase: (Phase) -> Void = { _ in }
    var onUnpaired: (String) -> Void = { _ in }
    /// A refusal from the core; true to stop retrying.
    var onRefused: (APIError) -> Bool = { _ in false }

    private let gateway: Gateway
    private let token: String
    private let body: [String: Any]
    private var task: Task<Void, Never>?
    private var stopped = true
    private var delay = 0.5
    private var gen = 0
    private var since = Date()
    private(set) var connected = false

    init(gateway: Gateway, token: String, args: [String: Any]) {
        self.gateway = gateway
        self.token = token
        body = args
    }

    func start() {
        guard stopped else { return }
        stopped = false
        run()
    }

    func stop() {
        stopped = true
        gen += 1
        task?.cancel()
        task = nil
        connected = false
    }

    /// Reconnects now, dropping a stream that may have died in the background.
    func kick() {
        guard !stopped else { return }
        delay = 0.5
        run()
    }

    private enum Outcome {
        case ended, failed, unpaired(String), refused(APIError)
    }

    private enum Event {
        case connected, frame(Frame)
    }

    private struct Head: Decodable {
        var ok: Bool?
        var t: String?
        var error: WireError?
    }

    private func run() {
        gen += 1
        let g = gen
        task?.cancel()
        if !connected { onPhase(.connecting) }
        let request = gateway.request("api/subscribe", method: "POST", token: token, json: body, timeout: 45)
        let session = gateway.session
        let me = Weak(self)
        task = Task.detached(priority: .userInitiated) {
            let outcome = await FrameStream.read(session, request) { event in
                await me.value?.handle(event, g)
            }
            await me.value?.ended(g, outcome)
        }
    }

    private func handle(_ event: Event, _ g: Int) {
        guard g == gen, !stopped else { return }
        switch event {
        case .connected:
            delay = 0.5
            connected = true
            since = Date()
            onPhase(.connected)
        case .frame(let f):
            onFrame(f)
        }
    }

    private func ended(_ g: Int, _ outcome: Outcome) {
        guard g == gen, !stopped else { return }
        let was = connected
        // One that keeps ending at once backs off, as one that never connects.
        let steady = was && Date().timeIntervalSince(since) > 3
        connected = false
        switch outcome {
        case .unpaired(let msg):
            stopped = true
            onUnpaired(msg)
            return
        case .refused(let e):
            if onRefused(e) {
                stopped = true
                onPhase(.offline)
                return
            }
        case .ended, .failed:
            break
        }
        // A stream that ended after it worked, as when the core restarts,
        // is only reconnecting; one that never connected is offline.
        onPhase(was ? .connecting : .offline)
        let wait = steady ? 0.3 : delay
        if !steady { delay = min(delay * 2, 8) }
        task = Task { [weak self] in
            try? await Task.sleep(nanoseconds: UInt64(wait * 1_000_000_000))
            guard let self, !Task.isCancelled, g == self.gen, !self.stopped else { return }
            self.run()
        }
    }

    /// Reads one stream to its end, off the main thread.
    private nonisolated static func read(_ session: URLSession, _ request: URLRequest,
                                         deliver: @escaping @Sendable (Event) async -> Void) async -> Outcome {
        let last = LastLine()
        let reader = Task<Outcome, Never> {
            do {
                let (bytes, response) = try await session.bytes(for: request)
                let status = (response as? HTTPURLResponse)?.statusCode ?? 0
                if status == 401 {
                    var data = Data()
                    for try await b in bytes {
                        data.append(b)
                        if data.count > 65536 { break }
                    }
                    let env = try? Wire.decoder.decode(Envelope<Ignored>.self, from: data)
                    return .unpaired(env?.error?.msg ?? "")
                }
                guard (200..<300).contains(status) else { return .failed }
                var first = true
                for try await line in bytes.lines {
                    last.pet()
                    if line.isEmpty { continue }
                    let data = Data(line.utf8)
                    if first {
                        first = false
                        let head = try? Wire.decoder.decode(Head.self, from: data)
                        if head?.ok == false {
                            return .refused(APIError(code: head?.error?.code ?? "error", message: head?.error?.msg ?? "The machine refused to follow it."))
                        }
                        await deliver(.connected)
                        if head?.t != nil, let f = try? Wire.decoder.decode(Frame.self, from: data) {
                            await deliver(.frame(f))
                        }
                        continue
                    }
                    if line.hasPrefix(#"{"t":"ping""#) { continue }
                    guard let f = try? Wire.decoder.decode(Frame.self, from: data), f.t != "ping" else { continue }
                    await deliver(.frame(f))
                }
                return .ended
            } catch {
                return .failed
            }
        }
        // 40 s without a line is a dead stream, as after the phone slept.
        let dog = Task {
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 2_000_000_000)
                if last.idle > 40 {
                    reader.cancel()
                    return
                }
            }
        }
        let outcome = await withTaskCancellationHandler {
            await reader.value
        } onCancel: {
            reader.cancel()
        }
        dog.cancel()
        return outcome
    }
}

/// A weak reference a detached task can carry.
private final class Weak<T: AnyObject>: @unchecked Sendable {
    weak var value: T?

    init(_ value: T) {
        self.value = value
    }
}

/// When a stream last said anything.
private final class LastLine: @unchecked Sendable {
    private let lock = NSLock()
    private var at = Date()

    func pet() { lock.withLock { at = Date() } }
    var idle: TimeInterval { lock.withLock { Date().timeIntervalSince(at) } }
}

// MARK: - Addresses and links

enum Address {
    /// An address as typed, such as 10.0.0.7, studio.local, studio.local:7400
    /// or a full URL, as the gateway's origin: https and port 7387 unless it says.
    static func normalize(_ typed: String) -> URL? {
        let text = typed.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }
        let hasScheme = text.range(of: #"^[a-zA-Z][a-zA-Z0-9+.-]*://"#, options: .regularExpression) != nil
        guard var c = URLComponents(string: hasScheme ? text : "https://" + text), let host = c.host, !host.isEmpty else { return nil }
        if c.scheme?.lowercased() == "http" { c.scheme = "https" }
        guard c.scheme?.lowercased() == "https" else { return nil }
        if c.port == nil { c.port = 7387 }
        c.path = ""
        c.query = nil
        c.fragment = nil
        c.user = nil
        c.password = nil
        return c.url
    }

    /// Whether Irie's authority may vouch for a host (docs/PHONE.md §3,
    /// internal/remote/certs.go): a .local, home.arpa or localhost name, or
    /// a private, shared, link-local or loopback address. A gateway answers
    /// only on its own networks, so an address anywhere else isn't Irie's.
    static func isLocal(_ host: String) -> Bool {
        var h = host.lowercased()
        if h.hasPrefix("["), h.hasSuffix("]") { h = String(h.dropFirst().dropLast()) }
        if h.hasSuffix(".") { h.removeLast() }
        if let v4 = IPv4Address(h) {
            let b = [UInt8](v4.rawValue)
            switch (b[0], b[1]) {
            case (10, _), (127, _), (172, 16...31), (192, 168), (100, 64...127), (169, 254): return true
            default: return false
            }
        }
        if let v6 = IPv6Address(h) {
            let b = [UInt8](v6.rawValue)
            return v6 == IPv6Address.loopback || b[0] & 0xFE == 0xFC || (b[0] == 0xFE && b[1] & 0xC0 == 0x80)
        }
        return ["localhost", "local", "home.arpa"].contains { h == $0 || h.hasSuffix("." + $0) }
    }

    /// Whether a gateway's URL names a host Irie's authority may vouch for.
    static func isLocal(_ url: URL) -> Bool {
        url.host(percentEncoded: false).map(isLocal) ?? false
    }
}

/// What a pairing link carries: irie://pair?u=<https base>&c=<code>&f=<fingerprint>&n=<name>.
struct PairLink: Equatable {
    var base: URL
    var code: String
    var pin: String
    var name: String

    init?(_ url: URL) {
        guard url.scheme?.lowercased() == "irie", url.host?.lowercased() == "pair" || url.path == "pair" || url.path == "/pair" else { return nil }
        var q: [String: String] = [:]
        // The query as Go's url.Values writes it: + is a space.
        for pair in (url.query(percentEncoded: true) ?? "").split(separator: "&") {
            let kv = pair.split(separator: "=", maxSplits: 1).map(String.init)
            guard let k = kv.first else { continue }
            let v = kv.count > 1 ? kv[1] : ""
            q[k] = v.replacingOccurrences(of: "+", with: " ").removingPercentEncoding ?? v
        }
        guard let u = q["u"], let base = URL(string: u), base.scheme?.lowercased() == "https", base.host != nil,
              let code = q["c"].map({ $0.filter(\.isNumber) }), code.count == 6,
              let pin = q["f"].flatMap(Trust.normalized) else { return nil }
        var c = URLComponents(url: base, resolvingAgainstBaseURL: false)
        c?.path = ""
        c?.query = nil
        c?.fragment = nil
        // Irie's own links name a machine on the person's network; one that
        // points anywhere else didn't come from `irie remote pair`.
        guard let clean = c?.url, Address.isLocal(clean) else { return nil }
        self.base = clean
        self.code = code
        self.pin = pin
        let n = (q["n"] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        name = n.isEmpty ? Gateway.host(clean) : n
    }
}
