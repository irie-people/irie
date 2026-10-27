import Darwin
import Foundation

/// Where the core listens: the same rules as internal/paths, so the app
/// finds the core that `irie up` started. --socket PATH or IRIE_CORE_SOCKET
/// override them.
enum CorePaths {
    static let maxSocketPath = 104 // macOS caps sun_path at 104 bytes with the NUL

    static func socket(arguments: [String] = CommandLine.arguments,
                       environment env: [String: String] = ProcessInfo.processInfo.environment) -> String {
        if let i = arguments.firstIndex(of: "--socket"), i + 1 < arguments.count {
            return arguments[i + 1]
        }
        if let s = env["IRIE_CORE_SOCKET"], s.hasPrefix("/") {
            return s
        }
        let uid = getuid()
        let short = "/tmp/irie-\(uid)"
        var runtime = short
        if let run = env["XDG_RUNTIME_DIR"], run.hasPrefix("/") {
            runtime = (run as NSString).appendingPathComponent("irie")
        } else if let tmp = env["TMPDIR"], tmp.hasPrefix("/") {
            runtime = (tmp as NSString).appendingPathComponent("irie-\(uid)")
        }
        let path = (runtime as NSString).appendingPathComponent("core.sock")
        if path.utf8.count >= maxSocketPath {
            return (short as NSString).appendingPathComponent("core.sock")
        }
        return path
    }
}

enum CoreError: Error, LocalizedError {
    case system(String, Int32)
    case closed
    case badReply

    var errorDescription: String? {
        switch self {
        case let .system(call, code): return "\(call): \(String(cString: strerror(code)))"
        case .closed: return "the core closed the connection"
        case .badReply: return "the core sent a reply the app does not understand"
        }
    }
}

/// One blocking connection to the core. Use it off the main thread.
final class CoreConnection {
    private var fd: Int32
    private var buffer = [UInt8]()

    init(path: String, timeout: TimeInterval?) throws {
        fd = Darwin.socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { throw CoreError.system("socket", errno) }
        var on: Int32 = 1
        setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &on, socklen_t(MemoryLayout<Int32>.size))
        if let timeout {
            var tv = timeval(tv_sec: Int(timeout), tv_usec: 0)
            setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
        }
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        let bytes = Array(path.utf8)
        guard bytes.count < MemoryLayout.size(ofValue: addr.sun_path) else {
            Darwin.close(fd)
            throw CoreError.system("connect", ENAMETOOLONG)
        }
        withUnsafeMutableBytes(of: &addr.sun_path) { $0.copyBytes(from: bytes) }
        let rc = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                Darwin.connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        guard rc == 0 else {
            let e = errno
            Darwin.close(fd)
            throw CoreError.system("connect", e)
        }
    }

    deinit { close() }

    func close() {
        if fd >= 0 {
            Darwin.close(fd)
            fd = -1
        }
    }

    func send<A: Encodable>(op: String, args: A?, id: Int = 1) throws {
        var line = try Wire.encoder.encode(Request(id: id, op: op, args: args))
        line.append(0x0A)
        try line.withUnsafeBytes { raw in
            var off = 0
            while off < raw.count {
                let n = Darwin.write(fd, raw.baseAddress! + off, raw.count - off)
                if n < 0 {
                    if errno == EINTR { continue }
                    throw CoreError.system("write", errno)
                }
                off += n
            }
        }
    }

    /// The next line, without its newline.
    func readLine() throws -> Data {
        while true {
            if let nl = buffer.firstIndex(of: 0x0A) {
                let line = Data(buffer[..<nl])
                buffer.removeSubrange(...nl)
                return line
            }
            var chunk = [UInt8](repeating: 0, count: 64 * 1024)
            let n = chunk.withUnsafeMutableBytes { Darwin.read(fd, $0.baseAddress, $0.count) }
            if n == 0 { throw CoreError.closed }
            if n < 0 {
                if errno == EINTR { continue }
                throw CoreError.system("read", errno)
            }
            buffer.append(contentsOf: chunk[..<n])
        }
    }

    func reply<R: Decodable>(_: R.Type) throws -> R {
        let r = try Wire.decoder.decode(Reply<R>.self, from: readLine())
        if let e = r.error { throw e }
        guard r.ok, let result = r.result else { throw CoreError.badReply }
        return result
    }
}

/// Requests to the core, each on its own connection like ipc.Call.
struct CoreClient {
    let socketPath: String

    func call<A: Encodable, R: Decodable>(_ op: String, _ args: A?, timeout: TimeInterval = 10, as _: R.Type) async throws -> R {
        let path = socketPath
        return try await Task.detached(priority: .userInitiated) {
            let c = try CoreConnection(path: path, timeout: timeout)
            defer { c.close() }
            try c.send(op: op, args: args)
            return try c.reply(R.self)
        }.value
    }

    /// Streams frames until the connection ends. The core counts the app's
    /// subscription and zooms the slot while it is open.
    func subscribe(onFrame: @escaping @Sendable (Frame) -> Void) async throws {
        let path = socketPath
        try await Task.detached(priority: .userInitiated) {
            let c = try CoreConnection(path: path, timeout: nil)
            defer { c.close() }
            try c.send(op: "subscribe", args: SubscribeArgs())
            _ = try c.reply(Empty.self)
            while !Task.isCancelled {
                let line = try c.readLine()
                if let f = try? Wire.decoder.decode(Frame.self, from: line) {
                    onFrame(f)
                }
            }
        }.value
    }
}
