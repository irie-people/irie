import AppKit
import Darwin

/// `Irie --away-helper [--for SECONDS] [--app NAME]... [-- COMMAND ARGS...]`,
/// which `irie away` runs (DESIGN §4.11, "Out of sight"). It runs COMMAND,
/// and while it runs and for SECONDS after (10 by default), each window that
/// opens on a desktop the user can see goes to a desktop out of sight
/// (Desktops.swift), when it belongs to a process the command started, to an
/// app the command opens with open(1), or to an app named with --app. An
/// app that started meanwhile and has nowhere to go, or that macOS will not
/// move, is hidden instead. A window of an app that ran before is left
/// alone while the user has that app in front, and so is every window that
/// was open before. Each window's fate goes to standard error; the exit
/// status is the command's, or 3 when a window stayed in sight.
@MainActor
enum Away {
    static func run(_ args: [String]) -> Never {
        NSApplication.shared.setActivationPolicy(.prohibited)
        var wait: TimeInterval = 10
        var names: [String] = []
        var command: [String] = []
        var rest = Array(args.drop { $0 != "--away-helper" }.dropFirst())
        while !rest.isEmpty {
            let a = rest.removeFirst()
            switch a {
            case "--for":
                guard let v = rest.first, let s = TimeInterval(v), s >= 0 else { fail("--for wants a number of seconds") }
                wait = s
                rest.removeFirst()
            case "--app":
                guard let v = rest.first else { fail("--app wants an app's name, bundle id, path or pid") }
                names.append(v)
                rest.removeFirst()
            case "--":
                command = rest
                rest = []
            default:
                fail("unknown argument \(a)")
            }
        }
        if command.isEmpty && names.isEmpty { fail("name a command after --, or an app with --app") }
        names += opened(by: command)

        let watch = Watch(names: names)
        let child: Process? = command.isEmpty ? nil : {
            let p = Process()
            p.executableURL = URL(fileURLWithPath: "/usr/bin/env")
            p.arguments = command
            do {
                try p.run()
            } catch {
                fail("could not run \(command[0]): \(error.localizedDescription)")
            }
            return p
        }()
        if let child { watch.family.insert(child.processIdentifier) }
        // A signal goes on to the command, and the watch ends with it.
        let stop = Stop()
        var sources: [DispatchSourceSignal] = []
        for sig in [SIGINT, SIGTERM, SIGHUP] {
            signal(sig, SIG_IGN)
            let s = DispatchSource.makeSignalSource(signal: sig, queue: .main)
            s.setEventHandler {
                stop.asked = true
                if let p = child, p.isRunning { kill(p.processIdentifier, sig) }
            }
            s.resume()
            sources.append(s)
        }
        let start = Date()
        var until = start.addingTimeInterval(wait)
        while true {
            let running = child?.isRunning ?? false
            if running {
                until = Date().addingTimeInterval(wait)
            } else if stop.asked || Date() > until {
                break
            }
            watch.sweep()
            // Quick while windows are likeliest to open: as the command
            // starts, and after it ends.
            let quick = !running || Date().timeIntervalSince(start) < 5
            RunLoop.main.run(until: Date().addingTimeInterval(quick ? 0.025 : 0.25))
        }
        _ = sources
        if !watch.stuck.isEmpty { exit(3) }
        guard let p = child else { exit(0) }
        exit(p.terminationReason == .uncaughtSignal ? 128 + p.terminationStatus : p.terminationStatus)
    }

    /// The apps an open(1) command opens: -a, -b and -e name them, and each
    /// file or URL opens in the app macOS gives it.
    static func opened(by command: [String]) -> [String] {
        guard let first = command.first, first == "open" || first == "/usr/bin/open" else { return [] }
        var apps: [String] = [], docs: [String] = []
        var i = 1
        while i < command.count {
            let a = command[i]
            if a == "--args" { break }
            switch a {
            case "-a", "-b":
                if i + 1 < command.count { apps.append(command[i + 1]) }
                i += 1
            case "-u":
                if i + 1 < command.count { docs.append(command[i + 1]) }
                i += 1
            case "--env", "--stdin", "--stdout", "--stderr":
                i += 1
            case "-e":
                apps.append("com.apple.TextEdit")
            default:
                if !a.hasPrefix("-") { docs.append(a) }
            }
            i += 1
        }
        guard apps.isEmpty else { return apps }
        return docs.compactMap { d in
            // An app opened by its path is matched by its path.
            if d.hasSuffix(".app") || d.hasSuffix(".app/") { return d }
            guard let url = d.contains("://") ? URL(string: d) : URL(fileURLWithPath: d),
                  let app = NSWorkspace.shared.urlForApplication(toOpen: url) else { return nil }
            return Bundle(url: app)?.bundleIdentifier ?? app.path
        }
    }

    /// Set once a signal asks the watch to end.
    final class Stop {
        var asked = false
    }

    static func fail(_ why: String) -> Never {
        FileHandle.standardError.write(Data("\(Brand.slug) away: \(why)\n".utf8))
        exit(2)
    }

    /// What the watch has seen and done.
    @MainActor
    final class Watch {
        let names: [String]
        /// The processes the command started, and theirs.
        var family = Set<pid_t>()
        /// The windows and processes there before.
        let windows: Set<Int>
        let pids: Set<pid_t>
        /// The windows dealt with, and the apps hidden.
        var done = Set<Int>()
        var hidden = Set<pid_t>()
        var stuck: [String] = []

        init(names: [String]) {
            self.names = names
            let all = CGWindowListCopyWindowInfo([.optionAll], kCGNullWindowID) as? [[String: Any]] ?? []
            windows = Set(all.compactMap { $0[kCGWindowNumber as String] as? Int })
            pids = Set(Away.parents().keys)
        }

        /// Sends each window that is ours and in sight out of sight.
        func sweep() {
            if !family.isEmpty {
                for (pid, parent) in Away.parents() where family.contains(parent) {
                    family.insert(pid)
                }
            }
            let shown = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? []
            let front = NSWorkspace.shared.frontmostApplication?.processIdentifier
            var named: Set<pid_t>?
            for w in shown {
                guard let number = w[kCGWindowNumber as String] as? Int, !done.contains(number),
                      let pid = (w[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value, pid != getpid(), !hidden.contains(pid),
                      let layer = w[kCGWindowLayer as String] as? Int, (0...8).contains(layer),
                      (w[kCGWindowAlpha as String] as? Double ?? 1) > 0,
                      let b = w[kCGWindowBounds as String] as? NSDictionary,
                      let bounds = CGRect(dictionaryRepresentation: b as CFDictionary), bounds.width >= 40, bounds.height >= 40
                else { continue }
                let new = !pids.contains(pid)
                var ours = family.contains(pid)
                if !ours, !names.isEmpty {
                    if named == nil { named = Set(NSWorkspace.shared.runningApplications.filter { a in names.contains { Away.matches(a, $0) } }.map(\.processIdentifier)) }
                    // An app the user may be using keeps what they open in it.
                    ours = named!.contains(pid) && (new || (!windows.contains(number) && pid != front))
                }
                guard ours else { continue }
                done.insert(number)
                send(number, pid: pid, bounds: bounds, new: new || family.contains(pid))
            }
        }

        private func send(_ number: Int, pid: pid_t, bounds: CGRect, new: Bool) {
            let app = NSRunningApplication(processIdentifier: pid)
            let name = app?.localizedName ?? "process \(pid)"
            let display = Desktops.display(of: Away.screen(holding: bounds))
            guard let d = Desktops.outOfSight(on: display) else {
                return hideOrStick(app, name, pid: pid, new: new, why: "every desktop is in sight")
            }
            if Desktops.move(number, to: d.id) {
                Away.note("\(name)'s window is on desktop \(d.number), out of sight")
            } else {
                hideOrStick(app, name, pid: pid, new: new, why: "macOS would not move its window")
            }
        }

        /// An app the command started is hidden; one that ran before may
        /// hold the user's own windows, so it is left, and said.
        private func hideOrStick(_ app: NSRunningApplication?, _ name: String, pid: pid_t, new: Bool, why: String) {
            if new, let app, app.hide() {
                hidden.insert(pid)
                Away.note("\(name) is hidden: \(why)")
            } else {
                stuck.append(name)
                Away.note("\(name)'s window stayed where the user can see it: \(why); close it")
            }
        }
    }

    static func note(_ s: String) {
        FileHandle.standardError.write(Data("\(Brand.slug) away: \(s)\n".utf8))
    }

    /// Whether a running app is the one named: by pid, bundle id, name, or
    /// its bundle's path or file name.
    static func matches(_ app: NSRunningApplication, _ name: String) -> Bool {
        if let pid = Int32(name) { return app.processIdentifier == pid }
        if app.bundleIdentifier?.caseInsensitiveCompare(name) == .orderedSame { return true }
        if app.localizedName?.caseInsensitiveCompare(name) == .orderedSame { return true }
        guard let url = app.bundleURL else { return false }
        if url.standardizedFileURL.path == URL(fileURLWithPath: (name as NSString).expandingTildeInPath).standardizedFileURL.path { return true }
        let base = (name as NSString).lastPathComponent
        let stem = base.hasSuffix(".app") ? String(base.dropLast(4)) : base
        return url.deletingPathExtension().lastPathComponent.caseInsensitiveCompare(stem) == .orderedSame
    }

    /// The screen that holds most of a window, from its bounds in the window
    /// server's coordinates, which count down from the main screen's top.
    static func screen(holding bounds: CGRect) -> NSScreen? {
        guard let top = NSScreen.screens.first?.frame.maxY else { return nil }
        let frame = NSRect(x: bounds.minX, y: top - bounds.maxY, width: bounds.width, height: bounds.height)
        return NSScreen.screens.max { a, b in
            let x = a.frame.intersection(frame), y = b.frame.intersection(frame)
            return x.width * x.height < y.width * y.height
        }
    }

    /// Each process's parent, from the kernel's process table.
    static func parents() -> [pid_t: pid_t] {
        var mib: [Int32] = [CTL_KERN, KERN_PROC, KERN_PROC_ALL, 0]
        var size = 0
        guard sysctl(&mib, 4, nil, &size, nil, 0) == 0 else { return [:] }
        let stride = MemoryLayout<kinfo_proc>.stride
        var procs = [kinfo_proc](repeating: kinfo_proc(), count: size / stride + 32)
        size = procs.count * stride
        guard sysctl(&mib, 4, &procs, &size, nil, 0) == 0 else { return [:] }
        var out: [pid_t: pid_t] = [:]
        for p in procs.prefix(size / stride) {
            out[p.kp_proc.p_pid] = p.kp_eproc.e_ppid
        }
        return out
    }
}
