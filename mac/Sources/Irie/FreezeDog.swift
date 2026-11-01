import AppKit
import Darwin

/// Answers the core's freeze dog (docs/MAC.md, DESIGN §2.4). Over a
/// connection of its own the core pings the app once a second, and the main
/// thread's answer says the app still answers you. When a ping goes
/// unanswered for ui.freeze_dog, the core has sample(1) write down where the
/// app is stuck, force quits it, and opens it again as it stood: in front,
/// behind, or hidden. Agents keep running, as with any restart of the app.
///
/// A thread of its own reads the pings and writes the answers, so neither
/// Swift's thread pool nor the store's connection holds them up, and the
/// main thread only says that it is there and how the app stands.
enum FreezeDog {
    /// How many seconds the app had not answered when the dog restarted it
    /// (--freeze-dog N); nil when it opened any other way.
    static let restartedAfter: Int? = {
        let args = CommandLine.arguments
        guard let i = args.firstIndex(of: "--freeze-dog"), i + 1 < args.count else { return nil }
        return Int(args[i + 1])
    }()

    private static var started = false

    /// Starts answering the dog of the core at socketPath, for as long as
    /// the app runs; a second call does nothing.
    @MainActor
    static func start(socketPath: String) {
        guard !started else { return }
        started = true
        // Only an app can be opened again; a bare binary is only force quit.
        let bundle = Bundle.main.bundlePath.hasSuffix(".app") ? Bundle.main.bundlePath : nil
        let thread = Thread { answer(socketPath: socketPath, bundle: bundle) }
        thread.name = "Irie freeze dog"
        thread.qualityOfService = .userInteractive
        thread.start()
    }

    /// Asks the core to watch the app, then answers each ping; when the
    /// connection ends it waits and asks again, so a core that restarts or
    /// starts later watches too. A core from before the freeze dog refuses,
    /// and is asked again now and then.
    private static func answer(socketPath: String, bundle: String?) {
        while true {
            var wait: UInt32 = 2
            do {
                let c = try CoreConnection(path: socketPath, timeout: nil)
                defer { c.close() }
                try c.send(op: "subscribe", args: WatchArgs(pid: Int(getpid()), bundle: bundle))
                do {
                    _ = try c.reply(Empty.self)
                } catch let e as WireError {
                    wait = e.code == "bad_args" ? 60 : 5
                    throw e
                }
                while true {
                    let line = try c.readLine()
                    guard let ping = try? Wire.decoder.decode(Ping.self, from: line), ping.t == "ping", let n = ping.ping else {
                        continue
                    }
                    let stand = Stand()
                    let answered = DispatchSemaphore(value: 0)
                    // Waits here for as long as the main thread is stuck,
                    // which is what the dog looks for.
                    DispatchQueue.main.async {
                        MainActor.assumeIsolated {
                            stand.front = NSApp.isActive
                            stand.hidden = NSApp.isHidden
                        }
                        answered.signal()
                    }
                    answered.wait()
                    try c.write(Pong(pong: n, front: stand.front, hidden: stand.hidden))
                }
            } catch {}
            sleep(wait)
        }
    }

    /// How the app stood when its main thread answered.
    private final class Stand: @unchecked Sendable {
        var front = false
        var hidden = false
    }

    private struct WatchArgs: Encodable {
        var view = "watch"
        var pid: Int
        var bundle: String?
    }

    private struct Ping: Decodable {
        var t: String
        var ping: UInt64?
    }

    private struct Pong: Encodable {
        var pong: UInt64
        var front: Bool
        var hidden: Bool
    }
}
