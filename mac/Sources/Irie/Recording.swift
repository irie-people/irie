import Accelerate
import AppKit
import AVFoundation
import SwiftUI

/// `Irie --record OUT.mp4 --reel FILE` films the app's own window while a
/// reel types tasks, moves between cards, answers an agent and closes and
/// opens the window, then exits (docs/demo/README.md).
///
/// The window stands far off any screen, as --snapshot's does, and the app
/// never takes the focus. macOS lets an app read its own windows back from
/// the window server without the Screen Recording permission, so nothing
/// asks: each frame is the window as the screen would show it, its terminal
/// and animations included, which --snapshot's drawing leaves out. On a
/// quiet Mac a frame at 2x takes about 30 ms to read back, convert to sRGB
/// and encode, so the film runs at a steady 30 frames a second; --fps asks
/// for more, and the line it prints says how many it got and what a frame
/// took.
///
/// A reel types into the task bar through the defaults that hold its text,
/// so recording refuses to run as the installed app, whose defaults keep
/// the user's own unsent task. docs/demo/record.sh runs a copy with a
/// bundle id of its own, against a demo sandbox's core.
@MainActor
enum Recording {
    static func run(_ args: [String]) -> Never {
        do {
            let options = try Options(args)
            if let id = Bundle.main.bundleIdentifier, !id.hasSuffix(".recording") {
                fail("--record runs only as a copy whose bundle id ends in .recording, so the user's own task bar and defaults stay as they are; docs/demo/record.sh makes one")
            }
            NSApplication.shared.setActivationPolicy(.accessory)
            Task { @MainActor in exit(await record(options)) }
            NSApplication.shared.run()
            exit(0)
        } catch {
            fail(error.localizedDescription)
        }
    }

    private static func fail(_ why: String) -> Never {
        FileHandle.standardError.write(Data("Irie --record: \(why)\n".utf8))
        exit(2)
    }

    /// What the command line and the reel's Set lines ask for; the command
    /// line wins.
    struct Options {
        var out: URL
        var reel: Reel
        var size = CGSize(width: 1280, height: 800)
        var fps = 30
        var dark = false
        var palette = Palette.standard.id
        var backdrop = CGColor(srgbRed: 0, green: 0, blue: 0, alpha: 1)

        init(_ args: [String]) throws {
            func value(_ flag: String) -> String? {
                guard let i = args.firstIndex(of: flag), i + 1 < args.count else { return nil }
                return args[i + 1]
            }
            guard let out = value("--record") else { throw RecordError("say where the video goes: --record OUT.mp4") }
            guard let path = value("--reel") else { throw RecordError("name the reel to play: --reel FILE") }
            self.out = URL(fileURLWithPath: out)
            reel = try Reel(text: String(contentsOfFile: path, encoding: .utf8))
            if let s = reel.size { size = s }
            if let a = reel.appearance { dark = a == "dark" }
            if let p = reel.palette { palette = p }
            if let b = reel.backdrop { backdrop = b }
            if let s = value("--size") {
                guard let parsed = Reel.size(s) else { throw RecordError("--size wants WIDTHxHEIGHT in points, such as 1280x800") }
                size = parsed
            }
            if let a = value("--appearance") { dark = a == "dark" }
            if let p = value("--palette") { palette = p }
            if let f = value("--fps"), let n = Int(f), (1...120).contains(n) { fps = n }
            if let b = value("--backdrop") {
                guard let c = Self.color(b) else { throw RecordError("--backdrop wants a colour as RRGGBB") }
                backdrop = c
            }
            guard Palette.all.contains(where: { $0.id == palette }) else {
                throw RecordError("no palette \(palette); there are \(Palette.all.map(\.id).joined(separator: ", "))")
            }
        }

        static func color(_ hex: String) -> CGColor? {
            let h = hex.hasPrefix("#") ? String(hex.dropFirst()) : hex
            guard h.count == 6, let v = UInt32(h, radix: 16) else { return nil }
            return CGColor(srgbRed: CGFloat(v >> 16 & 0xFF) / 255, green: CGFloat(v >> 8 & 0xFF) / 255,
                           blue: CGFloat(v & 0xFF) / 255, alpha: 1)
        }
    }

    /// Opens the window on the core the command line names, waits for its
    /// sessions, films the reel and returns the exit status.
    private static func record(_ o: Options) async -> Int32 {
        // The look is set for this run only, as --snapshot sets it.
        UserDefaults.standard.setVolatileDomain([
            Palette.defaultsKey: o.palette, "showTerminal": true,
            DetailsPanel.heightKey: DetailsPanel.compact, DetailsPanel.minimizedKey: false,
        ], forName: UserDefaults.argumentDomain)
        UserDefaults.standard.set("", forKey: "taskDraft")
        Theme.use(o.palette)
        // One phrase under the terminal for the whole film, the same on
        // every run, rather than one picked at random.
        PhraseBar.fixed = PhraseBar.phrases.firstIndex(of: PhraseBar.filmed)

        let window = FilmWindow(contentRect: NSRect(x: -40000, y: -40000, width: o.size.width, height: o.size.height),
                                styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
                                backing: .buffered, defer: false)
        window.appearance = NSAppearance(named: o.dark ? .darkAqua : .aqua)
        window.toolbarStyle = IrieApp.toolbarStyle
        window.titleVisibility = IrieApp.showsTitle ? .visible : .hidden
        window.setFrame(NSRect(x: -40000, y: -40000, width: o.size.width, height: o.size.height), display: true)

        // The window with a store of its own, as the app has after a
        // launch: what it shows comes from the core alone. It returns once
        // the core's sessions are in, or nil after 30 s.
        func openWindow() async -> Store? {
            let store = Store(socketPath: CorePaths.socket())
            let host = NSHostingView(rootView: ContentView().environment(store))
            host.sceneBridgingOptions = [.toolbars, .title]
            window.contentView = host
            window.orderFrontRegardless()
            store.start()
            let ready = Date().addingTimeInterval(30)
            while store.sessions.isEmpty || store.link == .connecting {
                if Date() > ready {
                    say("the core at \(store.socketPath) showed no sessions in 30 s")
                    return nil
                }
                try? await Task.sleep(nanoseconds: 100_000_000)
            }
            return store
        }

        // The core's sessions, then a moment for the terminal to attach and
        // draw. A reel's Spawn lines started them before the camera rolls.
        guard let store = await openWindow() else { return 1 }
        try? await Task.sleep(nanoseconds: 2_000_000_000)

        // An app with no window on screen is napped, and its timers slowed;
        // this keeps the film's clock on time until it is done.
        let awake = ProcessInfo.processInfo.beginActivity(options: [.userInitiated, .latencyCritical],
                                                          reason: "Filming Irie's window")
        defer { ProcessInfo.processInfo.endActivity(awake) }
        let scale = window.backingScaleFactor
        let film = Film(url: o.out, window: CGWindowID(window.windowNumber), fps: o.fps, backdrop: o.backdrop,
                        size: (Int((window.frame.width * scale).rounded()), Int((window.frame.height * scale).rounded())))
        do {
            try film.begin()
        } catch {
            say(error.localizedDescription)
            return 1
        }
        var events: [[String: Any]] = []
        let status: Int32
        do {
            // Close and Open: closing Irie's window quits the app
            // (IrieApp.swift), and its agents run on in the core's tmux.
            // The window goes, and its views with their store and terminal;
            // the film shows the backdrop alone, where the desktop would be.
            // Open makes the window again with a new store, as a fresh
            // launch does, and films it from its first frame.
            let stage = Reel.Stage(
                close: {
                    film.blank(true)
                    window.orderOut(nil)
                    window.contentView = NSView()
                },
                open: {
                    let opening = Task { await openWindow() }
                    // Its first frames, before the core's sessions are in,
                    // are filmed too: they are what a launch shows.
                    try? await Task.sleep(nanoseconds: 50_000_000)
                    film.blank(false)
                    guard let store = await opening.value else { throw RecordError("the window opened again, but the core showed no sessions") }
                    return store
                })
            try await o.reel.play(store: store, stage: stage) { what, text in
                var e: [String: Any] = ["ms": Int((film.elapsed * 1000).rounded()), "do": what]
                if let text { e["text"] = text }
                events.append(e)
            }
            status = 0
        } catch {
            say(error.localizedDescription)
            status = 1
        }
        let (frames, dropped, error) = await film.finish()
        if let error {
            say("the video did not finish: \(error.localizedDescription)")
            return 1
        }
        let log = o.out.deletingPathExtension().appendingPathExtension("events.json")
        if let data = try? JSONSerialization.data(withJSONObject: events, options: [.prettyPrinted, .sortedKeys]) {
            try? data.write(to: log)
        }
        let px = film.pixels, seconds = film.elapsed
        print("\(o.out.lastPathComponent): \(px.width)x\(px.height), \(String(format: "%.1f", seconds)) s, \(frames) frames (\(String(format: "%.1f", Double(frames) / max(seconds, 0.001))) a second), \(dropped) dropped; a frame took \(Int(film.cost.read.rounded())) ms to read and \(Int(film.cost.encode.rounded())) ms to encode")
        window.orderOut(nil)
        return status
    }

    private static func say(_ why: String) {
        FileHandle.standardError.write(Data("Irie --record: \(why)\n".utf8))
    }
}

struct RecordError: LocalizedError {
    let errorDescription: String?
    init(_ why: String) { errorDescription = why }
}

/// The recording's window. It draws as the front window of the active app
/// would, so the film shows Irie as its user sees it, though the app stays
/// in the background and never takes the keyboard.
final class FilmWindow: NSWindow {
    override var isKeyWindow: Bool { true }
    override var isMainWindow: Bool { true }
}

/// A reel: what to do on camera, one step a line, much as a vhs tape reads.
///
///     Set Size 1280x800           the window, in points (filmed at 2x)
///     Set Appearance light|dark
///     Set Palette evergreen
///     Set Backdrop E9EBDF         the colour round the window, and in its
///                                 place while it is closed
///     Set TypingSpeed 55ms        the pause between typed keys
///     Spawn @acme-api add …       a session record.sh starts before filming
///     Sleep 1.5s | 500ms
///     Type "text"                 types into the task bar
///     Enter                       starts what the task bar holds
///     Down [N] | Up [N]           the next or previous card, N times
///     Show N                      the Nth card in the list
///     Answer "text"               types into the shown session's terminal,
///                                 then Return
///     Close                       closes the window, which quits the app
///     Open                        opens it again, as a fresh launch does
///     Wait STATE [TIMEOUT]        until the newest session is in STATE
///
/// Blank lines and lines starting with # are comments.
struct Reel {
    enum Step {
        case sleep(Double)
        case type(String)
        case enter
        case step(Int)
        case show(Int)
        case answer(String)
        case close
        case open
        case wait(String, Double)
    }

    /// What Close and Open do to the window: the recording films it.
    struct Stage {
        var close: @MainActor () -> Void
        var open: @MainActor () async throws -> Store
    }

    var steps: [Step] = []
    var size: CGSize?
    var appearance: String?
    var palette: String?
    var backdrop: CGColor?
    var typing = 0.055

    init(text: String) throws {
        for (n, raw) in text.split(separator: "\n", omittingEmptySubsequences: false).enumerated() {
            let line = raw.trimmingCharacters(in: .whitespaces)
            if line.isEmpty || line.hasPrefix("#") { continue }
            let parts = line.split(separator: " ", maxSplits: 1).map(String.init)
            let rest = parts.count > 1 ? parts[1].trimmingCharacters(in: .whitespaces) : ""
            func bad(_ why: String) -> RecordError { RecordError("reel line \(n + 1): \(why): \(line)") }
            switch parts[0] {
            case "Set":
                let kv = rest.split(separator: " ", maxSplits: 1).map(String.init)
                guard kv.count == 2 else { throw bad("Set wants a name and a value") }
                switch kv[0] {
                case "Size":
                    guard let s = Self.size(kv[1]) else { throw bad("Size wants WIDTHxHEIGHT") }
                    size = s
                case "Appearance":
                    guard ["light", "dark"].contains(kv[1]) else { throw bad("Appearance is light or dark") }
                    appearance = kv[1]
                case "Palette": palette = kv[1]
                case "Backdrop":
                    guard let c = Recording.Options.color(kv[1]) else { throw bad("Backdrop wants a colour as RRGGBB") }
                    backdrop = c
                case "TypingSpeed":
                    guard let d = Self.duration(kv[1]) else { throw bad("TypingSpeed wants a duration") }
                    typing = d
                default: throw bad("no setting \(kv[0])")
                }
            case "Spawn": continue // record.sh starts these before filming
            case "Sleep":
                guard let d = Self.duration(rest) else { throw bad("Sleep wants a duration such as 1.5s or 500ms") }
                steps.append(.sleep(d))
            case "Type", "Answer":
                var t = rest
                if t.count >= 2, t.hasPrefix("\""), t.hasSuffix("\"") { t = String(t.dropFirst().dropLast()) }
                guard !t.isEmpty else { throw bad("\(parts[0]) wants text") }
                steps.append(parts[0] == "Type" ? .type(t) : .answer(t))
            case "Enter": steps.append(.enter)
            case "Close": steps.append(.close)
            case "Open": steps.append(.open)
            case "Down", "Up":
                let count = rest.isEmpty ? 1 : Int(rest) ?? 0
                guard count > 0 else { throw bad("\(parts[0]) takes a count") }
                steps.append(.step(parts[0] == "Down" ? count : -count))
            case "Show":
                guard let row = Int(rest), row > 0 else { throw bad("Show wants a row number from 1") }
                steps.append(.show(row))
            case "Wait":
                let w = rest.split(separator: " ").map(String.init)
                guard let state = w.first else { throw bad("Wait wants a state, such as working") }
                let timeout = w.count > 1 ? Self.duration(w[1]) : 30
                guard let timeout else { throw bad("Wait's timeout is a duration") }
                steps.append(.wait(state, timeout))
            default:
                throw bad("no step \(parts[0])")
            }
        }
    }

    static func size(_ s: String) -> CGSize? {
        let wh = s.lowercased().split(separator: "x").compactMap { Double($0) }
        guard wh.count == 2, wh[0] >= 820, wh[1] >= 500 else { return nil }
        return CGSize(width: wh[0], height: wh[1])
    }

    static func duration(_ s: String) -> Double? {
        if s.hasSuffix("ms") { return Double(s.dropLast(2)).map { $0 / 1000 } }
        if s.hasSuffix("s") { return Double(s.dropLast()) }
        return Double(s)
    }

    /// Plays the steps against the store, as the user's hands would, and
    /// reports each one as it starts. Close and Open go through the stage,
    /// and the window that opens again brings a store of its own.
    @MainActor
    func play(store first: Store, stage: Stage, log: (String, String?) -> Void) async throws {
        func pause(_ s: Double) async throws { try await Task.sleep(nanoseconds: UInt64(max(0, s) * 1_000_000_000)) }
        var store = first
        var closed = false
        // When the last Enter was pressed: Wait looks only at sessions
        // started since, so it never takes an older one for the new task.
        var since: Date?
        for step in steps {
            switch step {
            case .sleep, .open: break
            default:
                if closed { throw RecordError("the window is closed: Open it before the next step") }
            }
            switch step {
            case let .sleep(s):
                try await pause(s)
            case let .type(text):
                log("type", text)
                store.focusTaskBar += 1
                var typed = UserDefaults.standard.string(forKey: "taskDraft") ?? ""
                // A steady hand with a little life in it: a space and every
                // fourth key take a touch longer, the same on every run.
                for (i, ch) in text.enumerated() {
                    typed.append(ch)
                    UserDefaults.standard.set(typed, forKey: "taskDraft")
                    try await pause(typing * (ch == " " ? 1.5 : i % 4 == 3 ? 1.3 : 0.85))
                }
            case .enter:
                let text = (UserDefaults.standard.string(forKey: "taskDraft") ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                log("enter", text)
                // As the task bar does: it empties at once, and the task
                // starts behind it.
                UserDefaults.standard.set("", forKey: "taskDraft")
                since = Date()
                if !text.isEmpty { Task { [store] in _ = await store.input(text) } }
            case let .step(n):
                for i in 0..<abs(n) {
                    log(n > 0 ? "down" : "up", nil)
                    store.step(n > 0 ? 1 : -1)
                    if i < abs(n) - 1 { try await pause(0.35) }
                }
            case let .show(row):
                log("show", String(row))
                store.showRow(row)
            case let .answer(text):
                // As a person answers in the agent's own terminal: the keys
                // go through the core, which alone writes to tmux, at the
                // reel's typing speed, then Return.
                guard let id = store.selection else { throw RecordError("Answer types into the shown session: Show one first") }
                log("answer", text)
                for (i, ch) in text.enumerated() {
                    _ = try await store.client.call("keys", FilmKeys(session: id, text: String(ch)), as: Empty.self)
                    try await pause(typing * (ch == " " ? 1.5 : i % 4 == 3 ? 1.3 : 0.85))
                }
                try await pause(typing * 4)
                _ = try await store.client.call("keys", FilmKeys(session: id, keys: ["Enter"]), as: Empty.self)
            case .close:
                log("close", nil)
                stage.close()
                closed = true
            case .open:
                guard closed else { throw RecordError("Open comes after Close") }
                log("open", nil)
                store = try await stage.open()
                closed = false
                log("opened", nil)
            case let .wait(state, timeout):
                let until = Date().addingTimeInterval(timeout)
                while store.sessions.filter({ s in since.map { s.created >= $0 } ?? true })
                    .max(by: { $0.created < $1.created })?.state != state {
                    if Date() > until { throw RecordError("the newest session was not \(state) within \(Int(timeout)) s") }
                    try await pause(0.1)
                }
                log("reached", state)
            }
        }
    }
}

/// The core's keys op (internal/ipc/screen.go): keys pressed by name, or
/// text typed without Return, in a session's terminal.
struct FilmKeys: Encodable {
    var session: String
    var keys: [String]?
    var text: String?
}

/// Films one of the app's windows to an H.264 MP4, at a steady rate, from
/// queues of its own so the window keeps drawing on time: one reads the
/// window back on the beat, the other draws and encodes each picture, so a
/// slow frame on a busy Mac never holds up the next one's reading.
final class Film: @unchecked Sendable {
    private let url: URL
    private let window: CGWindowID
    private let fps: Int
    private let backdrop: CGColor
    private let queue = DispatchQueue(label: "Irie film", qos: .userInitiated)
    private let encoder = DispatchQueue(label: "Irie film encoding", qos: .userInitiated)
    private var writer: AVAssetWriter?
    private var input: AVAssetWriterInput?
    private var adaptor: AVAssetWriterInputPixelBufferAdaptor?
    private var timer: DispatchSourceTimer?
    private var start: CFTimeInterval = 0
    // On queue: the window's pictures read and on their way to the
    // encoder, past which a reading is dropped, and the time reading took.
    private var pending = 0
    private var skipped = 0
    private var reads = 0
    private var reading = 0.0
    // On encoder: what went into the video, and the time it took.
    private var last = CMTime.negativeInfinity
    private var frames = 0
    private var dropped = 0
    private var encoding = 0.0
    /// While the window is closed: each frame is the backdrop alone.
    private var empty = false
    private(set) var pixels = (width: 0, height: 0)
    /// The window's size in pixels, which every picture of it must have.
    private let size: (width: Int, height: Int)
    /// What one frame took on average, in milliseconds: reading the window
    /// back, then drawing and encoding it. Set by finish().
    private(set) var cost = (read: 0.0, encode: 0.0)

    init(url: URL, window: CGWindowID, fps: Int, backdrop: CGColor, size: (width: Int, height: Int)) {
        self.size = size
        self.url = url
        self.window = window
        self.fps = fps
        self.backdrop = backdrop
    }

    /// Seconds since filming began.
    var elapsed: Double { start == 0 ? 0 : CACurrentMediaTime() - start }

    // CGWindowListCreateImage reads a window back from the window server.
    // It is looked up when it runs: newer SDKs no longer declare it, and on
    // the app's own windows it needs no Screen Recording permission.
    private typealias CreateImage = @convention(c) (CGRect, UInt32, UInt32, UInt32) -> Unmanaged<CGImage>?
    private static let createImage: CreateImage? = {
        guard let sym = dlsym(UnsafeMutableRawPointer(bitPattern: -2), "CGWindowListCreateImage") else { return nil }
        return unsafeBitCast(sym, to: CreateImage.self)
    }()

    /// The window as the screen shows it, at full resolution, without its
    /// shadow.
    private func grab() -> CGImage? {
        let includingWindow: UInt32 = 1 << 3 // kCGWindowListOptionIncludingWindow
        let options: UInt32 = (1 << 0) | (1 << 3) // kCGWindowImageBoundsIgnoreFraming | kCGWindowImageBestResolution
        return Self.createImage?(.null, includingWindow, window, options)?.takeRetainedValue()
    }

    func begin() throws {
        try queue.sync {
            // The first picture sets the frame; one of the wrong size is
            // asked for again.
            var picture = grab()
            for _ in 0..<20 where picture.map({ $0.width != size.width || $0.height != size.height }) ?? true {
                Thread.sleep(forTimeInterval: 0.05)
                picture = grab()
            }
            guard let first = picture, first.width == size.width, first.height == size.height else {
                throw RecordError("the window server gave no picture of the window at its size, \(size.width)x\(size.height)")
            }
            // H.264 wants even sides.
            pixels = (first.width & ~1, first.height & ~1)
            try? FileManager.default.removeItem(at: url)
            let writer = try AVAssetWriter(outputURL: url, fileType: .mp4)
            let input = AVAssetWriterInput(mediaType: .video, outputSettings: [
                AVVideoCodecKey: AVVideoCodecType.h264,
                AVVideoWidthKey: pixels.width,
                AVVideoHeightKey: pixels.height,
                AVVideoColorPropertiesKey: [
                    AVVideoColorPrimariesKey: AVVideoColorPrimaries_ITU_R_709_2,
                    AVVideoTransferFunctionKey: AVVideoTransferFunction_ITU_R_709_2,
                    AVVideoYCbCrMatrixKey: AVVideoYCbCrMatrix_ITU_R_709_2,
                ],
                AVVideoCompressionPropertiesKey: [
                    AVVideoAverageBitRateKey: 24_000_000,
                    AVVideoMaxKeyFrameIntervalKey: fps,
                    AVVideoExpectedSourceFrameRateKey: fps,
                    AVVideoProfileLevelKey: AVVideoProfileLevelH264HighAutoLevel,
                ],
            ])
            input.expectsMediaDataInRealTime = true
            let adaptor = AVAssetWriterInputPixelBufferAdaptor(assetWriterInput: input, sourcePixelBufferAttributes: [
                kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32BGRA,
                kCVPixelBufferWidthKey as String: pixels.width,
                kCVPixelBufferHeightKey as String: pixels.height,
            ])
            writer.add(input)
            guard writer.startWriting() else {
                throw writer.error ?? RecordError("the video could not start")
            }
            writer.startSession(atSourceTime: .zero)
            self.writer = writer
            self.input = input
            self.adaptor = adaptor
            start = CACurrentMediaTime()
            encoder.sync { append(first, bytes: first.dataProvider?.data, at: 0) }
            // Strict: a busy or idle Mac stretches an ordinary timer's beat
            // to save power, here from 33 ms to some 150, which held the film
            // to 7 frames a second. A film lasts a minute or so.
            let timer = DispatchSource.makeTimerSource(flags: .strict, queue: queue)
            timer.schedule(deadline: .now() + 1.0 / Double(fps), repeating: 1.0 / Double(fps), leeway: .milliseconds(1))
            timer.setEventHandler { [weak self] in self?.tick() }
            self.timer = timer
            timer.resume()
        }
    }

    /// Films the backdrop alone from now on, while the window is closed, or
    /// the window again.
    func blank(_ on: Bool) {
        queue.sync { empty = on }
    }

    private func tick() {
        let t = CACurrentMediaTime() - start
        // Two pictures waiting is as far as the encoder may fall behind.
        guard pending < 2 else {
            skipped += 1
            return
        }
        let image: CGImage?
        let bytes: CFData?
        if empty {
            (image, bytes) = (nil, nil)
        } else {
            // The window server's picture, and its bytes, which come over
            // only when asked for.
            let began = CACurrentMediaTime()
            guard let read = grab() else {
                skipped += 1
                return
            }
            // Now and then, as a terminal attaches or a card starts, the
            // window server hands back a picture wider than the window,
            // the window shifted right within it. The window keeps its size
            // through a reel, so such a picture is left out and the film
            // holds the frame before.
            guard read.width == size.width, read.height == size.height else {
                skipped += 1
                return
            }
            image = read
            bytes = read.dataProvider?.data
            reading += CACurrentMediaTime() - began
            reads += 1
        }
        pending += 1
        encoder.async { [self, image, bytes] in
            append(image, bytes: bytes, at: t)
            queue.async { self.pending -= 1 }
        }
    }

    /// Converts the picture's colours, those of the screen, to sRGB with
    /// Accelerate: about a quarter of what Core Graphics' drawing takes on
    /// a frame at 2x, which held the film to 10 frames a second on a busy
    /// Mac. One converter serves every picture of the same kind.
    private var converter: (space: CGColorSpace, info: CGBitmapInfo, bits: Int, convert: vImageConverter)?
    private lazy var sRGBFormat = vImage_CGImageFormat(
        bitsPerComponent: 8, bitsPerPixel: 32, colorSpace: CGColorSpace(name: CGColorSpace.sRGB)!,
        bitmapInfo: CGBitmapInfo(rawValue: CGImageAlphaInfo.premultipliedFirst.rawValue | CGBitmapInfo.byteOrder32Little.rawValue))!

    /// Puts the picture into the frame in sRGB, its top-left corner at the
    /// frame's, and lays its rounded corners, which are clear, over the
    /// backdrop. False when the picture is not the frame's size or kind.
    private func convert(_ image: CGImage, bytes: CFData, into buffer: CVPixelBuffer) -> Bool {
        guard image.width >= pixels.width, image.height >= pixels.height, let space = image.colorSpace else { return false }
        if let c = converter, c.space == space, c.info == image.bitmapInfo, c.bits == image.bitsPerPixel {
            // The same kind of picture as the last.
        } else {
            guard let format = vImage_CGImageFormat(cgImage: image),
                  let made = try? vImageConverter.make(sourceFormat: format, destinationFormat: sRGBFormat) else { return false }
            converter = (space, image.bitmapInfo, image.bitsPerPixel, made)
        }
        let source = vImage_Buffer(data: UnsafeMutableRawPointer(mutating: CFDataGetBytePtr(bytes)),
                                   height: vImagePixelCount(pixels.height), width: vImagePixelCount(pixels.width),
                                   rowBytes: image.bytesPerRow)
        var frame = vImage_Buffer(data: CVPixelBufferGetBaseAddress(buffer), height: vImagePixelCount(pixels.height),
                                  width: vImagePixelCount(pixels.width), rowBytes: CVPixelBufferGetBytesPerRow(buffer))
        guard (try? converter!.convert.convert(source: source, destination: &frame)) != nil else { return false }
        // The corners: what shows through each clear or half-clear pixel
        // is the backdrop, as when Core Graphics draws over it.
        let c = backdrop.converted(to: CGColorSpace(name: CGColorSpace.sRGB)!, intent: .defaultIntent, options: nil)?.components ?? [0, 0, 0, 1]
        let under = (b: Int(c[2] * 255 + 0.5), g: Int(c[1] * 255 + 0.5), r: Int(c[0] * 255 + 0.5))
        let side = min(48, pixels.width / 2, pixels.height / 2)
        let row = CVPixelBufferGetBytesPerRow(buffer)
        let base = CVPixelBufferGetBaseAddress(buffer)!.assumingMemoryBound(to: UInt8.self)
        for y0 in [0, pixels.height - side] {
            for x0 in [0, pixels.width - side] {
                for y in y0..<y0 + side {
                    var p = base + y * row + x0 * 4
                    for _ in 0..<side {
                        let clear = 255 - Int(p[3])
                        if clear > 0 {
                            p[0] = UInt8(Int(p[0]) + (under.b * clear + 127) / 255)
                            p[1] = UInt8(Int(p[1]) + (under.g * clear + 127) / 255)
                            p[2] = UInt8(Int(p[2]) + (under.r * clear + 127) / 255)
                            p[3] = 255
                        }
                        p += 4
                    }
                }
            }
        }
        return true
    }

    /// Puts the picture into a frame in sRGB, over the backdrop, which shows
    /// in the window's rounded corners, and adds it at its time. With no
    /// picture the frame is the backdrop alone.
    private func append(_ image: CGImage?, bytes: CFData?, at t: Double) {
        let began = CACurrentMediaTime()
        defer { encoding += CACurrentMediaTime() - began }
        guard let input, let adaptor, input.isReadyForMoreMediaData, let pool = adaptor.pixelBufferPool else {
            dropped += 1
            return
        }
        var buffer: CVPixelBuffer?
        CVPixelBufferPoolCreatePixelBuffer(nil, pool, &buffer)
        guard let buffer else {
            dropped += 1
            return
        }
        CVPixelBufferLockBaseAddress(buffer, [])
        if let image, let bytes, convert(image, bytes: bytes, into: buffer) {
            // Done: the quick way.
        } else if let space = CGColorSpace(name: CGColorSpace.sRGB),
                  let ctx = CGContext(data: CVPixelBufferGetBaseAddress(buffer), width: pixels.width, height: pixels.height,
                                      bitsPerComponent: 8, bytesPerRow: CVPixelBufferGetBytesPerRow(buffer), space: space,
                                      bitmapInfo: CGImageAlphaInfo.premultipliedFirst.rawValue | CGBitmapInfo.byteOrder32Little.rawValue) {
            ctx.setFillColor(backdrop)
            ctx.fill(CGRect(x: 0, y: 0, width: pixels.width, height: pixels.height))
            // Top-left aligned: the origin is at the bottom.
            if let image {
                ctx.draw(image, in: CGRect(x: 0, y: pixels.height - image.height, width: image.width, height: image.height))
            }
        }
        CVPixelBufferUnlockBaseAddress(buffer, [])
        let at = CMTime(seconds: t, preferredTimescale: 60000)
        guard at > last else { return }
        if adaptor.append(buffer, withPresentationTime: at) {
            frames += 1
            last = at
        } else {
            dropped += 1
        }
    }

    /// Stops filming and closes the file: how many frames went in, how many
    /// were dropped, and why it failed, if it did.
    func finish() async -> (Int, Int, Error?) {
        // The beat stops first; the encoder then takes what is on its way.
        let (missed, read) = queue.sync {
            timer?.cancel()
            timer = nil
            return (skipped, reads > 0 ? reading / Double(reads) * 1000 : 0)
        }
        let (writer, counts): (AVAssetWriter?, (Int, Int)) = encoder.sync {
            input?.markAsFinished()
            cost = (read, frames > 0 ? encoding / Double(frames) * 1000 : 0)
            return (self.writer, (frames, dropped + missed))
        }
        guard let writer else { return (counts.0, counts.1, RecordError("it never started")) }
        await writer.finishWriting()
        return (counts.0, counts.1, writer.status == .completed ? nil : writer.error)
    }
}
