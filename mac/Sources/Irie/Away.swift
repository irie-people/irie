import AppKit
import Darwin

/// `Irie --away-helper [--for SECONDS] [--app NAME]... [-- COMMAND ARGS...]`,
/// which `irie away` runs (DESIGN §4.11, "Out of sight"). It runs COMMAND,
/// and while it runs and for SECONDS after (10 by default), each window that
/// opens on a desktop the user can see goes to a desktop out of sight
/// (Desktops.swift), when it belongs to a process the command started, to an
/// app the command opens with open(1), or to an app named with --app. An
/// app that started meanwhile and has nowhere to go, or that macOS will not
/// move, is hidden instead. Every window open before the helper ran is left
/// alone, and so is a new window of an app that ran before once the user
/// brings that app to the front. Each window's fate goes to standard error;
/// the exit status is the command's, or 3 when a window stayed in sight.
///
/// The helper never makes an NSApplication or asks NSScreen: either signs
/// this process in with Launch Services as a second copy of the app, which
/// the Dock and open(1) could take for the app itself. The window server,
/// Core Graphics and NSWorkspace answer without signing it in.
@MainActor
enum Away {
    static func run(_ args: [String]) -> Never {
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
        // A window still in its moment of grace is decided now.
        if !watch.waiting.isEmpty {
            RunLoop.main.run(until: Date().addingTimeInterval(0.3))
            watch.sweep(last: true)
        }
        _ = sources
        if !watch.stuck.isEmpty { exit(3) }
        guard let p = child else { exit(0) }
        exit(p.terminationReason == .uncaughtSignal ? 128 + p.terminationStatus : p.terminationStatus)
    }

    /// The apps a command opens with open(1), its own or one in the script
    /// a shell runs with -c: -a, -b and -e name them, -R is the Finder, and
    /// each file or URL opens in the app macOS gives it. A command that
    /// starts an app some other way names it with --app.
    static func opened(by command: [String]) -> [String] {
        guard let first = command.first else { return [] }
        if ["sh", "bash", "zsh", "dash", "ksh"].contains((first as NSString).lastPathComponent),
           let c = command.firstIndex(where: { $0.hasPrefix("-") && !$0.hasPrefix("--") && $0.hasSuffix("c") }), c + 1 < command.count {
            let words = command[c + 1].components(separatedBy: .whitespacesAndNewlines).filter { !$0.isEmpty }
                .map { $0.trimmingCharacters(in: CharacterSet(charactersIn: "\"'();&|")) }
            return words.indices.filter { words[$0] == "open" || words[$0] == "/usr/bin/open" }
                .flatMap { opened(by: Array(words[$0...])) }
        }
        guard first == "open" || first == "/usr/bin/open" else { return [] }
        var apps: [String] = [], docs: [String] = []
        var i = 1
        while i < command.count {
            let a = command[i]
            if a == "--args" || a.isEmpty || a.contains(where: { ";&|".contains($0) }) { break }
            switch a {
            case "-a", "-b":
                if i + 1 < command.count { apps.append(command[i + 1]) }
                i += 1
            case "-u", "--url":
                if i + 1 < command.count { docs.append(command[i + 1]) }
                i += 1
            case "-i", "-o", "-s", "--arch", "--env", "--stdin", "--stdout", "--stderr":
                i += 1
            case "-e":
                apps.append("com.apple.TextEdit")
            case "-R", "--reveal":
                apps.append("com.apple.finder")
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
        /// The windows there before, which are never touched, and the
        /// processes, without which no app counts as new.
        let windows: Set<Int>
        let pids: Set<pid_t>?
        /// The windows dealt with, the apps hidden, and when each new window
        /// of an app that ran before was first seen.
        var done = Set<Int>()
        var hidden = Set<pid_t>()
        var waiting: [Int: Date] = [:]
        var stuck: [String] = []

        init(names: [String]) {
            self.names = names
            guard let all = CGWindowListCopyWindowInfo([.optionAll], kCGNullWindowID) as? [[String: Any]] else {
                Away.fail("could not read the windows open now, so nothing could tell the command's from the user's")
            }
            windows = Set(all.compactMap { $0[kCGWindowNumber as String] as? Int })
            pids = Away.parents().map { Set($0.keys) }
        }

        /// Sends each window that is ours and in sight out of sight. A new
        /// window of an app that ran before waits a moment first: one the
        /// user opened, by a link in Mail, say, brings its app to the front.
        func sweep(last: Bool = false) {
            if !family.isEmpty, let parents = Away.parents() {
                for (pid, parent) in parents where family.contains(parent) {
                    family.insert(pid)
                }
            }
            let shown = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? []
            let front = NSWorkspace.shared.frontmostApplication?.processIdentifier
            var named: Set<pid_t>?
            for w in shown {
                guard let number = w[kCGWindowNumber as String] as? Int, !windows.contains(number), !done.contains(number),
                      let pid = (w[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value, pid != getpid(), !hidden.contains(pid),
                      let layer = w[kCGWindowLayer as String] as? Int, (0...8).contains(layer),
                      (w[kCGWindowAlpha as String] as? Double ?? 1) > 0,
                      let b = w[kCGWindowBounds as String] as? NSDictionary,
                      let bounds = CGRect(dictionaryRepresentation: b as CFDictionary), bounds.width >= 40, bounds.height >= 40
                else { continue }
                // Without the processes there before, no app counts as new.
                let new = family.contains(pid) || (pids.map { !$0.contains(pid) } ?? false)
                if !new {
                    if names.isEmpty { continue }
                    if named == nil {
                        named = Set(NSWorkspace.shared.runningApplications.filter { a in names.contains { Away.matches(a, $0) } }.map(\.processIdentifier))
                    }
                    guard named!.contains(pid) else { continue }
                    if pid == front {
                        done.insert(number) // the user's own
                        continue
                    }
                    let seen = waiting[number] ?? Date()
                    waiting[number] = seen
                    if !last, Date().timeIntervalSince(seen) < 0.25 { continue }
                } else if !family.contains(pid), !names.isEmpty {
                    // A new app, but is it one the command opened?
                    if named == nil {
                        named = Set(NSWorkspace.shared.runningApplications.filter { a in names.contains { Away.matches(a, $0) } }.map(\.processIdentifier))
                    }
                    guard named!.contains(pid) else { continue }
                } else if !family.contains(pid) {
                    continue
                }
                done.insert(number)
                send(number, pid: pid, bounds: bounds, new: new)
            }
        }

        private func send(_ number: Int, pid: pid_t, bounds: CGRect, new: Bool) {
            let app = NSRunningApplication(processIdentifier: pid)
            let name = app?.localizedName ?? "process \(pid)"
            let display = Away.display(holding: bounds)
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

    /// Whether a running app is the one named: by pid, bundle id or name, or
    /// for a name with a slash, by its bundle's path alone, so that a
    /// checkout's build never stands for the installed app.
    static func matches(_ app: NSRunningApplication, _ name: String) -> Bool {
        if let pid = Int32(name) { return app.processIdentifier == pid }
        if name.contains("/") {
            guard let url = app.bundleURL else { return false }
            let path = URL(fileURLWithPath: (name as NSString).expandingTildeInPath).standardizedFileURL.path
            return url.standardizedFileURL.path == path
        }
        if app.bundleIdentifier?.caseInsensitiveCompare(name) == .orderedSame { return true }
        let stem = name.hasSuffix(".app") ? String(name.dropLast(4)) : name
        if app.localizedName?.caseInsensitiveCompare(stem) == .orderedSame { return true }
        return app.bundleURL?.deletingPathExtension().lastPathComponent.caseInsensitiveCompare(stem) == .orderedSame
    }

    /// The window server's name for the display that holds most of a
    /// window, from its bounds, through Core Graphics: NSScreen would sign
    /// the helper in with Launch Services.
    static func display(holding bounds: CGRect) -> String? {
        var ids = [CGDirectDisplayID](repeating: 0, count: 16)
        var count: UInt32 = 0
        guard CGGetDisplaysWithRect(bounds, UInt32(ids.count), &ids, &count) == .success, count > 0 else { return nil }
        let best = ids.prefix(Int(count)).max { a, b in
            let x = CGDisplayBounds(a).intersection(bounds), y = CGDisplayBounds(b).intersection(bounds)
            return x.width * x.height < y.width * y.height
        }
        return best.flatMap { Desktops.display(id: $0) }
    }

    /// Each process's parent, from the kernel's process table; nil when the
    /// table could not be read, after a few tries.
    static func parents() -> [pid_t: pid_t]? {
        let stride = MemoryLayout<kinfo_proc>.stride
        for _ in 0..<3 {
            var mib: [Int32] = [CTL_KERN, KERN_PROC, KERN_PROC_ALL, 0]
            var size = 0
            guard sysctl(&mib, 4, nil, &size, nil, 0) == 0 else { continue }
            var procs = [kinfo_proc](repeating: kinfo_proc(), count: size / stride + 64)
            size = procs.count * stride
            guard sysctl(&mib, 4, &procs, &size, nil, 0) == 0 else { continue }
            var out: [pid_t: pid_t] = [:]
            for p in procs.prefix(size / stride) {
                out[p.kp_proc.p_pid] = p.kp_eproc.e_ppid
            }
            if !out.isEmpty { return out }
        }
        return nil
    }
}
