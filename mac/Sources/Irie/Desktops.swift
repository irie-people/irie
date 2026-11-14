import AppKit
import Darwin

/// Out of sight (DESIGN §4.11): a window that Irie opens on its own, to draw
/// a snapshot, film a reel, show a preview or come back after an install,
/// never lands on a desktop the user can see, where it would sit over their
/// work. It goes to another desktop (a Space, in macOS's word) before it is
/// first shown. A window that is only drawn is also see-through and lets
/// clicks pass, and where every desktop is in sight the app hides instead.
/// `irie away` does the same for the windows of any command (Away.swift).
///
/// macOS has no public interface to desktops, so this one calls SkyLight,
/// the window server's private framework, by name as the app runs, as yabai
/// does (MIT, PROVENANCE.md). A macOS that lacks a call, or refuses it,
/// gets the hidden window instead. Not yet: macOS 26's own move operation,
/// which SkyLight does not export, so dlsym cannot find it.
@MainActor
enum Desktops {
    /// One desktop of one display, as Mission Control shows them.
    struct Desktop: Equatable {
        let id: UInt64 // the window server's, for this login
        let uuid: String // the same across logins
        let number: Int // Mission Control's "Desktop 2" is 2
        let display: String // its display's identifier
    }

    /// Every display's desktops, full-screen apps' left out, and the ones
    /// the user can see: the one each display shows.
    static func layout() -> (desktops: [Desktop], seen: Set<UInt64>) {
        guard let cid = connection, let copy = sky("SLSCopyManagedDisplaySpaces", CopyArray.self),
              let displays = copy(cid)?.takeRetainedValue() as? [[String: Any]] else { return ([], []) }
        var all: [Desktop] = [], seen = Set<UInt64>()
        for d in displays {
            let display = d["Display Identifier"] as? String ?? ""
            if let now = d["Current Space"] as? [String: Any], let id = (now["ManagedSpaceID"] as? NSNumber)?.uint64Value {
                seen.insert(id)
            }
            var n = 0
            for s in d["Spaces"] as? [[String: Any]] ?? [] {
                // Type 0 is a desktop; a full-screen app's own space is 4.
                guard (s["type"] as? NSNumber)?.intValue == 0, let id = (s["ManagedSpaceID"] as? NSNumber)?.uint64Value else { continue }
                n += 1
                all.append(Desktop(id: id, uuid: s["uuid"] as? String ?? "", number: n, display: display))
            }
        }
        return (all, seen)
    }

    /// The desktops a window is on: none before it first shows, unless it
    /// was moved to one, which it then opens on; every one for a window
    /// that shows on all desktops.
    static func desktops(of window: Int) -> [UInt64] {
        guard let cid = connection, let copy = sky("SLSCopySpacesForWindows", CopyForWindows.self),
              let ids = copy(cid, 0x7, list(window))?.takeRetainedValue() as? [NSNumber] else { return [] }
        return ids.map(\.uint64Value)
    }

    /// A desktop the user cannot see now, on the display named, or else the
    /// one they work on: `prefer` while it is out of sight, else the
    /// display's last that is. Nil when they can see every desktop.
    static func outOfSight(on display: String? = nil, prefer: UInt64? = nil) -> Desktop? {
        let (all, seen) = layout()
        let names = Set(all.map(\.display))
        let shown = display.flatMap { d in names.first { $0.caseInsensitiveCompare(d) == .orderedSame } } ?? working(among: names)
        let free = all.filter { $0.display == shown && !seen.contains($0.id) }
        return free.first { $0.id == prefer } ?? free.last
    }

    /// The window server's name for a screen's display, as the layout's
    /// desktops carry it.
    static func display(of screen: NSScreen?) -> String? {
        guard let n = screen?.deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? NSNumber else { return nil }
        return display(id: n.uint32Value)
    }

    /// The window server's name for a display, by its Core Graphics id.
    /// Unlike NSScreen, it signs no process in with Launch Services, which
    /// the away helper must never be (Away.swift).
    static func display(id: CGDirectDisplayID) -> String? {
        guard let uuid = CGDisplayCreateUUIDFromDisplayID(id)?.takeRetainedValue() else { return nil }
        return CFUUIDCreateString(nil, uuid) as String
    }

    /// Moves a window to a desktop and says whether the window server has it
    /// there, on that desktop alone. Each macOS takes its own call, as
    /// yabai's space_manager.c finds: a tag the desktop wears for a moment
    /// on 12.7, 13.6, 14.5 and from 15, the plain move before those; the
    /// other is tried when the first does not land.
    @discardableResult
    static func move(_ window: Int, to desktop: UInt64) -> Bool {
        guard connection != nil else { return false }
        if desktops(of: window) == [desktop] { return true }
        let ways: [@MainActor (Int, UInt64) -> Void] = tagged ? [moveByTag, moveStraight] : [moveStraight, moveByTag]
        for way in ways {
            way(window, desktop)
            for _ in 0..<5 {
                if desktops(of: window) == [desktop] { return true }
                usleep(20000)
            }
        }
        return false
    }

    /// Readies a window the app only draws, for `--snapshot` and `--bench`:
    /// no one sees it or clicks it, and it opens on a desktop out of sight
    /// when there is one, so even ordered in it covers nothing.
    static func drawUnseen(_ window: NSWindow) {
        window.alphaValue = 0
        window.ignoresMouseEvents = true
        if let d = outOfSight(on: display(of: window.screen ?? NSScreen.main)) {
            move(window.windowNumber, to: d.id)
        }
    }

    /// Readies a window that must show to be filmed (`--record`): it takes
    /// no clicks and opens on a desktop out of sight. False when the user
    /// can see every desktop, or macOS would not move it.
    static func showUnseen(_ window: NSWindow) -> Bool {
        window.ignoresMouseEvents = true
        guard let d = outOfSight(on: display(of: window.screen ?? NSScreen.main)) else { return false }
        return move(window.windowNumber, to: d.id)
    }

    // MARK: SkyLight

    private typealias Connection = @convention(c) () -> Int32
    private typealias CopyArray = @convention(c) (Int32) -> Unmanaged<CFArray>?
    private typealias CopyString = @convention(c) (Int32) -> Unmanaged<CFString>?
    private typealias CopyForWindows = @convention(c) (Int32, Int32, CFArray) -> Unmanaged<CFArray>?
    private typealias MoveWindows = @convention(c) (Int32, CFArray, UInt64) -> Void
    private typealias SetTag = @convention(c) (Int32, UInt64, Int32) -> Int32
    private typealias FollowTag = @convention(c) (Int32, UnsafePointer<UInt32>, Int32, Int32) -> Int32

    private static let framework = dlopen("/System/Library/PrivateFrameworks/SkyLight.framework/SkyLight", RTLD_LAZY)

    private static func sky<T>(_ name: String, _: T.Type) -> T? {
        guard let framework, let f = dlsym(framework, name) else { return nil }
        return unsafeBitCast(f, to: T.self)
    }

    /// This process's connection to the window server.
    private static let connection: Int32? = sky("SLSMainConnectionID", Connection.self)?()

    /// The macOS versions that move a window by a tag (yabai's
    /// workspace_use_macos_space_workaround).
    private static let tagged: Bool = {
        let v = ProcessInfo.processInfo.operatingSystemVersion
        switch v.majorVersion {
        case 12: return v.minorVersion >= 7
        case 13: return v.minorVersion >= 6
        case 14: return v.minorVersion >= 5
        default: return v.majorVersion >= 15
        }
    }()

    /// The tag a desktop wears while a window follows it: "irie" in ASCII.
    private static let tag: Int32 = 0x6972_6965

    private static func list(_ window: Int) -> CFArray {
        [NSNumber(value: Int32(truncatingIfNeeded: window))] as CFArray
    }

    /// The display with the menu bar the user uses, or the only one.
    private static func working(among names: Set<String>) -> String? {
        if names.count == 1 { return names.first }
        if let cid = connection, let copy = sky("SLSCopyActiveMenuBarDisplayIdentifier", CopyString.self),
           let name = copy(cid)?.takeRetainedValue() as String?,
           let match = names.first(where: { $0.caseInsensitiveCompare(name) == .orderedSame }) {
            return match
        }
        return display(id: CGMainDisplayID()).flatMap { d in names.first { $0.caseInsensitiveCompare(d) == .orderedSame } } ?? names.sorted().first
    }

    private static func moveStraight(_ window: Int, _ desktop: UInt64) {
        guard let cid = connection, let move = sky("SLSMoveWindowsToManagedSpace", MoveWindows.self) else { return }
        move(cid, list(window), desktop)
    }

    private static func moveByTag(_ window: Int, _ desktop: UInt64) {
        guard let cid = connection, let wear = sky("SLSSpaceSetCompatID", SetTag.self),
              let follow = sky("SLSSetWindowListWorkspace", FollowTag.self) else { return }
        var id = UInt32(truncatingIfNeeded: window)
        _ = wear(cid, desktop, tag)
        _ = follow(cid, &id, 1, tag)
        _ = wear(cid, desktop, 0)
    }
}

/// Where the app's own window opens, and the desktop it remembers
/// (Desktops):
///
/// - With `--away`, which hot.sh gives its preview, out of sight: on the
///   desktop it was on last while the user cannot see that one, else on
///   another out of sight; with every desktop in sight, the app hides.
/// - With `--background`, which an install and the freeze dog give the app
///   they open again behind the user's work, back on the desktop it was
///   on, which it remembers as it goes; with none remembered, or where
///   macOS will not move it, where macOS puts it, behind the app in front,
///   since hiding the user's own app would lose it.
/// - Otherwise the user opened it, and macOS puts it where they are.
///
/// Only the app's own run keeps a desktop: `--snapshot`, `--bench` and
/// `--record` draw their windows without it.
@MainActor
final class WindowPlace {
    static let shared = WindowPlace()
    /// hot.sh's preview: out of sight.
    static let away = CommandLine.arguments.contains("--away")
    /// Opened again behind the user's work.
    static let background = CommandLine.arguments.contains("--background")
    /// The defaults key of the desktop the window was on last, by id and
    /// uuid.
    static let key = "windowDesktop"

    private var on = false
    private var placed = Set<ObjectIdentifier>()
    private weak var kept: NSWindow?
    private var watching: [NSObjectProtocol] = []
    private var timer: Timer?

    /// Called as the app has launched: places each window that is there
    /// already, before the window server first shows it.
    func start() {
        on = true
        for w in NSApp.windows where w.isVisible && w.styleMask.contains(.titled) && !(w is NSPanel) {
            place(w)
        }
    }

    /// The app's window, once a view is in it: placed, if it was not yet,
    /// and its desktop kept from then on.
    func adopt(_ window: NSWindow) {
        guard on else { return }
        place(window)
        keep(window)
    }

    private func place(_ window: NSWindow) {
        guard Self.away || Self.background, placed.insert(ObjectIdentifier(window)).inserted else { return }
        let last = Self.lastDesktop()
        let target: Desktops.Desktop
        if Self.away {
            guard let d = Desktops.outOfSight(on: Desktops.display(of: window.screen), prefer: last?.id) else {
                return hide("every desktop is in sight; add one in Mission Control")
            }
            target = d
        } else {
            // Back where it was; where the user is now, macOS puts it.
            guard let last, !Desktops.layout().seen.contains(last.id) else { return }
            target = last
        }
        let alpha = window.alphaValue
        window.alphaValue = 0
        defer { window.alphaValue = alpha }
        if Desktops.move(window.windowNumber, to: target.id) {
            say("the window is on desktop \(target.number), out of sight")
            // A first preview waits behind what is on that desktop, so whoever
            // goes there finds their own windows on top. Ordered once it has
            // shown: an order now would be undone as it first shows.
            if Self.away && Preview.replaces == nil {
                DispatchQueue.main.asyncAfter(deadline: .now() + 1) { [weak window] in
                    guard let window, !NSApp.isActive, window.isVisible, !window.isOnActiveSpace else { return }
                    window.orderBack(nil)
                }
            }
        } else if Self.away {
            hide("macOS would not move the window to desktop \(target.number)")
        } else {
            say("macOS would not move the window to desktop \(target.number), so it stays where macOS put it")
        }
    }

    /// Keeps the app hidden, with nothing on any desktop, until the user
    /// shows it from the Dock.
    private func hide(_ why: String) {
        NSApp.hide(nil)
        say("hidden: \(why)")
    }

    private func say(_ s: String) { print("\(Brand.name): \(s)") }

    /// Remembers the window's desktop whenever it may have moved: as the
    /// user changes desktops, as the window shows or hides or moves, and
    /// every half minute for a move none of these tells, as in Mission
    /// Control.
    private func keep(_ window: NSWindow) {
        guard kept !== window else { return }
        kept = window
        let centre = NotificationCenter.default
        for name in [NSWindow.didChangeOcclusionStateNotification, NSWindow.didMoveNotification] {
            watching.append(centre.addObserver(forName: name, object: window, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.remember() }
            })
        }
        watching.append(NSWorkspace.shared.notificationCenter.addObserver(forName: NSWorkspace.activeSpaceDidChangeNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.remember() }
        })
        timer = Timer.scheduledTimer(withTimeInterval: 30, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.remember() }
        }
        remember()
    }

    private func remember() {
        guard let w = kept, w.isVisible else { return }
        let on = Desktops.desktops(of: w.windowNumber)
        guard on.count == 1, let d = Desktops.layout().desktops.first(where: { $0.id == on[0] }) else { return }
        let value: [String: Any] = ["id": NSNumber(value: d.id), "uuid": d.uuid]
        if (UserDefaults.standard.dictionary(forKey: Self.key) as NSDictionary?) != value as NSDictionary {
            UserDefaults.standard.set(value, forKey: Self.key)
        }
    }

    /// The desktop the window was on last, by its uuid, which lasts across
    /// logins, or its id; nil once that desktop is gone.
    private static func lastDesktop() -> Desktops.Desktop? {
        guard let saved = UserDefaults.standard.dictionary(forKey: key) else { return nil }
        let all = Desktops.layout().desktops
        if let uuid = saved["uuid"] as? String, !uuid.isEmpty, let d = all.first(where: { $0.uuid == uuid }) { return d }
        let id = (saved["id"] as? NSNumber)?.uint64Value
        return all.first { $0.id == id }
    }
}
