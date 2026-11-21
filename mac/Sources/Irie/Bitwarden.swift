import AppKit
import Observation
import WebKit

// The Bitwarden plugin (DESIGN §4.17): the person's own logins fill the
// sign-in pages of their web panes (PluginPanes.swift). This app does it all
// itself, and the core only says how the plugin stands: a login never
// crosses the core's socket, which agents can reach, and nothing here is
// reachable from it.
//
// Unlocking takes the master password in a sheet, gives it to Bitwarden's
// own bw on standard input, reads the vault once, keeps only the logins of
// the panes' addresses, and locks bw again at once, so bw's session key
// lives a few seconds. The logins stay in this app's memory, never on disk,
// until the Mac locks or sleeps, the person locks it, its plugin goes off,
// or four hours pass.

// MARK: Logins

/// A login from the vault that fills a pane: kept in memory only.
struct BitwardenLogin: Equatable, Identifiable {
    let id: String
    let name: String
    let username: String
    let password: String
    let uris: [BitwardenURI]

    /// It fills a page at url.
    func fills(_ url: URL) -> Bool { uris.contains { $0.fills(url) } }

    /// Its name in a menu: the username, and the item's name when two
    /// logins share a page.
    var menuName: String {
        if username.isEmpty { return name }
        return name.isEmpty || name.caseInsensitiveCompare(username) == .orderedSame ? username : "\(username) (\(name))"
    }
}

/// One of a login's addresses, with Bitwarden's match detection: nil or
/// 0 base domain, 1 host, 2 starts with, 3 exact, 4 regular expression,
/// 5 never.
struct BitwardenURI: Equatable, Decodable {
    let uri: String?
    let match: Int?

    /// Whether it fills a page at url. Only on the page's own host and
    /// port, even where Bitwarden would match the whole domain: on one
    /// machine, every app on 127.0.0.1 shares a domain. Never for Never, nor
    /// a regular expression, which could match more than its owner meant.
    func fills(_ url: URL) -> Bool {
        let how = match ?? 0
        guard how != 4, how != 5 else { return false }
        let raw = (uri ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        guard !raw.isEmpty else { return false }
        // An address without a scheme, as people often save one, takes the
        // page's.
        let full = raw.contains("://") ? raw : "\(url.scheme ?? "http")://\(raw)"
        guard let u = URL(string: full), let host = u.host?.lowercased(), host == url.host?.lowercased(),
              Self.port(u) == Self.port(url) else { return false }
        switch how {
        case 2: return url.absoluteString.hasPrefix(full)
        case 3: return Self.trimmed(url.absoluteString) == Self.trimmed(full)
        default: return true
        }
    }

    static func port(_ u: URL) -> Int? {
        u.port ?? (u.scheme?.lowercased() == "https" ? 443 : u.scheme?.lowercased() == "http" ? 80 : nil)
    }

    private static func trimmed(_ s: String) -> String { s.hasSuffix("/") ? String(s.dropLast()) : s }
}

/// A vault item as bw list items prints it: only what a login needs.
private struct BitwardenItem: Decodable {
    let id: String
    let type: Int
    let name: String?
    let login: Login?

    struct Login: Decodable {
        let username: String?
        let password: String?
        let uris: [BitwardenURI]?
    }
}

// MARK: bw

/// Bitwarden's own CLI, bw, run as a program of this app's.
enum BitwardenCLI {
    /// Where it looks for bw: Homebrew's two prefixes, never a path a
    /// setting names, which an agent could point at a program of its own
    /// that records the master password. internal/plugins looks in the same
    /// places.
    static var places = ["/opt/homebrew/bin/bw", "/usr/local/bin/bw"]

    static var path: String? { places.first { FileManager.default.isExecutableFile(atPath: $0) } }

    struct Failure: Error {
        let message: String
    }

    /// What a pipe gives, read while bw runs, so a long answer can't fill
    /// the pipe and stall it.
    private final class Sink: @unchecked Sendable {
        var data = Data()
    }

    /// Runs bw and returns what it printed. Its environment is made here,
    /// with nothing of the app's own: no one's session key, password or
    /// NODE_OPTIONS. bw never prompts (--nointeraction), and what it is given
    /// comes on standard input, never in its arguments, where ps shows them
    /// to every user.
    static func run(_ bin: String, _ args: [String], session: String? = nil, input: Data? = nil, timeout: TimeInterval = 60) async throws -> String {
        try await withCheckedThrowingContinuation { (done: CheckedContinuation<String, Error>) in
            DispatchQueue.global(qos: .userInitiated).async {
                let p = Process()
                p.executableURL = URL(fileURLWithPath: bin)
                p.arguments = args + ["--nointeraction"]
                let mine = ProcessInfo.processInfo.environment
                // Homebrew's bw is a node script, and finds node on PATH.
                var env = ["PATH": "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin", "HOME": NSHomeDirectory(),
                           "BW_NOINTERACTION": "true", "LANG": "en_US.UTF-8", "NO_COLOR": "1"]
                for k in ["USER", "LOGNAME", "TMPDIR"] { env[k] = mine[k] }
                if let session { env["BW_SESSION"] = session }
                p.environment = env
                let out = Pipe(), err = Pipe(), inp = Pipe()
                p.standardOutput = out
                p.standardError = err
                p.standardInput = inp
                let o = Sink(), e = Sink()
                let reading = DispatchGroup()
                for (pipe, sink) in [(out, o), (err, e)] {
                    reading.enter()
                    DispatchQueue.global(qos: .userInitiated).async {
                        sink.data = pipe.fileHandleForReading.readDataToEndOfFile()
                        reading.leave()
                    }
                }
                do {
                    try p.run()
                } catch {
                    try? out.fileHandleForWriting.close()
                    try? err.fileHandleForWriting.close()
                    done.resume(throwing: Failure(message: "bw didn’t start: \(error.localizedDescription)"))
                    return
                }
                if let input { try? inp.fileHandleForWriting.write(contentsOf: input) }
                try? inp.fileHandleForWriting.close()
                let start = Date()
                let stop = DispatchWorkItem { p.terminate() }
                DispatchQueue.global().asyncAfter(deadline: .now() + timeout, execute: stop)
                p.waitUntilExit()
                stop.cancel()
                reading.wait()
                let said = String(decoding: o.data, as: UTF8.self)
                if p.terminationReason == .uncaughtSignal, Date().timeIntervalSince(start) >= timeout - 1 {
                    return done.resume(throwing: Failure(message: "bw \(args.first ?? "") took more than \(Int(timeout)) seconds"))
                }
                guard p.terminationStatus == 0 else {
                    // bw's own words, which never repeat what it was given.
                    let why = String(decoding: e.data.isEmpty ? o.data : e.data, as: UTF8.self)
                        .split(whereSeparator: \.isNewline).last.map(String.init) ?? "it stopped with \(p.terminationStatus)"
                    return done.resume(throwing: Failure(message: String(why.prefix(200))))
                }
                done.resume(returning: said)
            }
        }
    }

    /// The last line that holds something: bw may print a notice about a
    /// newer version first.
    static func lastLine(_ s: String) -> String {
        s.split(whereSeparator: \.isNewline).last.map { String($0).trimmingCharacters(in: .whitespaces) } ?? ""
    }
}

// MARK: The vault

/// What the app holds of the person's vault: nothing while locked, and the
/// logins of the panes' addresses while unlocked.
@MainActor
@Observable
final class BitwardenVault {
    static let shared = BitwardenVault()

    enum State: String {
        case locked, unlocking, unlocked
    }

    private(set) var state = State.locked
    private(set) var logins: [BitwardenLogin] = []
    /// The plugin's part is on, as the core last said: the panes offer the
    /// key while it is.
    var offered = false
    /// How long it stays unlocked at most.
    static let lasts: TimeInterval = 4 * 3600

    private var era = 0
    private var watching = false

    /// The logins that fill a page at url, by name.
    func logins(for url: URL) -> [BitwardenLogin] {
        logins.filter { $0.fills(url) }
    }

    /// Unlocks with the master password, for the panes at origins, and
    /// says what went wrong, or nil.
    func unlock(password: String, origins: [URL]) async -> String? {
        guard state != .unlocking else { return nil }
        guard let bin = BitwardenCLI.path else {
            return "The Bitwarden CLI isn’t installed: brew install bitwarden-cli, then bw login once in your own terminal."
        }
        state = .unlocking
        era += 1
        let mine = era
        var found: [BitwardenLogin] = []
        var problem: String?
        do {
            // The master password goes on standard input: never in an
            // argument or an environment, where ps shows them.
            let out = try await BitwardenCLI.run(bin, ["unlock", "--raw", "--passwordfile", "/dev/stdin"], input: Data((password + "\n").utf8))
            let session = BitwardenCLI.lastLine(out)
            if session.isEmpty || session.contains(" ") {
                problem = "bw unlocked, but gave no session key."
            } else {
                // Logins saved elsewhere since bw last looked: a sync that
                // fails, as offline, leaves the ones it has.
                _ = try? await BitwardenCLI.run(bin, ["sync"], session: session, timeout: 30)
                do {
                    let list = try await BitwardenCLI.run(bin, ["list", "items"], session: session)
                    found = try Self.logins(in: BitwardenCLI.lastLine(list), origins: origins)
                } catch let f as BitwardenCLI.Failure {
                    problem = "Bitwarden unlocked, but its logins didn’t read: \(f.message)"
                } catch {
                    problem = "Bitwarden unlocked, but its logins didn’t read."
                }
            }
        } catch let f as BitwardenCLI.Failure {
            problem = Self.unlockProblem(f.message)
        } catch {
            problem = "Bitwarden didn’t unlock: \(error.localizedDescription)"
        }
        // bw lock ends the session key, whatever happened: the logins the
        // panes need are already here.
        _ = try? await BitwardenCLI.run(bin, ["lock"], timeout: 30)
        guard era == mine else { return nil }
        if let problem {
            state = .locked
            return problem
        }
        logins = found
        state = .unlocked
        watch()
        Task { [weak self] in
            try? await Task.sleep(nanoseconds: UInt64(Self.lasts * 1_000_000_000))
            guard let self, self.era == mine else { return }
            self.lock()
        }
        return nil
    }

    /// Lets every login go.
    func lock() {
        guard state != .locked || !logins.isEmpty else { return }
        era += 1
        logins = []
        state = .locked
    }

    /// The logins among items, a JSON list as bw prints it, that fill a
    /// page at any of origins.
    nonisolated static func logins(in json: String, origins: [URL]) throws -> [BitwardenLogin] {
        let items = try JSONDecoder().decode([BitwardenItem].self, from: Data(json.utf8))
        return items.compactMap { item -> BitwardenLogin? in
            guard item.type == 1, let l = item.login else { return nil }
            let login = BitwardenLogin(id: item.id, name: item.name ?? "", username: l.username ?? "", password: l.password ?? "", uris: l.uris ?? [])
            guard !login.username.isEmpty || !login.password.isEmpty, origins.contains(where: login.fills) else { return nil }
            return login
        }
        .sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
    }

    /// bw's refusal to unlock, in words.
    nonisolated static func unlockProblem(_ said: String) -> String {
        let s = said.lowercased()
        if s.contains("not logged in") || s.contains("unauthenticated") {
            return "The Bitwarden CLI isn’t signed in: run bw login once in your own terminal."
        }
        if s.contains("invalid master password") || s.contains("master password") {
            return "That master password didn’t unlock Bitwarden."
        }
        return "Bitwarden didn’t unlock: \(said)"
    }

    /// Locks when the Mac sleeps, its screen locks or its user switches
    /// away.
    private func watch() {
        guard !watching else { return }
        watching = true
        let lockNow: @Sendable (Notification) -> Void = { _ in
            Task { @MainActor in BitwardenVault.shared.lock() }
        }
        let ws = NSWorkspace.shared.notificationCenter
        for name in [NSWorkspace.willSleepNotification, NSWorkspace.screensDidSleepNotification, NSWorkspace.sessionDidResignActiveNotification] {
            ws.addObserver(forName: name, object: nil, queue: .main, using: lockNow)
        }
        DistributedNotificationCenter.default().addObserver(forName: .init("com.apple.screenIsLocked"), object: nil, queue: .main, using: lockNow)
    }
}

// MARK: Filling

/// A page Bitwarden fills: a web pane's (PluginPaneHolder).
@MainActor
protocol BitwardenPage: AnyObject {
    var webView: WKWebView { get }
    /// The page on show, while it is on the pane's own origin.
    var page: URL? { get }
    /// The pane's origin, without a slash at its end.
    var originText: String { get }
    var signIn: Bool { get set }
    var filled: Bool { get set }
}

/// Fills a pane's sign-in page from the vault, in a world of its own that
/// the page's scripts can't see into, in the page's main frame only, and
/// only while the page is on the pane's own origin.
@MainActor
enum BitwardenFill {
    private static let world = WKContentWorld.world(name: "\(Brand.slug)-bitwarden")

    /// What a page has: a sign-in form, and whether it was filled.
    struct Look {
        var form = false
        var filled = false
    }

    /// Looks at a page that just loaded: it notes a sign-in form, for the
    /// banner while Bitwarden is locked, and fills it once from the first
    /// login for the page while it is unlocked.
    static func look(_ h: BitwardenPage) async {
        let vault = BitwardenVault.shared
        guard vault.offered, let page = h.page else {
            h.signIn = false
            return
        }
        let login = vault.state == .unlocked && !h.filled ? vault.logins(for: page).first : nil
        let r = await run(h, login: login, force: false)
        guard h.page == page else { return }
        h.signIn = r.form
        if r.filled { h.filled = true }
    }

    /// Fills the page with login at the person's word: the fields it finds,
    /// over what they hold.
    static func fill(_ h: BitwardenPage, with login: BitwardenLogin) async -> Bool {
        guard let page = h.page, login.fills(page) else { return false }
        let r = await run(h, login: login, force: true)
        if r.filled { h.filled = true }
        return r.filled
    }

    private static func run(_ h: BitwardenPage, login: BitwardenLogin?, force: Bool) async -> Look {
        let args: [String: Any] = [
            "origin": h.originText,
            "username": login.map { $0.username as Any } ?? NSNull(),
            "secret": login?.password ?? "",
            "force": force,
        ]
        return await withCheckedContinuation { (done: CheckedContinuation<Look, Never>) in
            h.webView.callAsyncJavaScript(script, arguments: args, in: nil, in: world) { result in
                guard case let .success(value) = result, let d = value as? [String: Any] else { return done.resume(returning: Look()) }
                done.resume(returning: Look(form: d["form"] as? Bool ?? false, filled: d["filled"] as? Bool ?? false))
            }
        }
    }

    /// The fill, as an async function's body. With username null it only
    /// looks. Unasked (force false) it fills only a form that signs in, as
    /// one with a username before a password does, or an email field on a
    /// page or form that says it signs in, and only fields still empty; it
    /// fills no form that asks for a new password, and it never sends one.
    static let script = """
    if (location.origin !== origin || window.top !== window) return { form: false, filled: false };
    const shown = (el) => {
      if (el.disabled || el.readOnly) return false;
      const r = el.getBoundingClientRect();
      if (r.width < 2 || r.height < 2) return false;
      const s = getComputedStyle(el);
      return s.visibility !== "hidden" && s.display !== "none" && Number(s.opacity) > 0.05;
    };
    const auto = (el) => (el.getAttribute("autocomplete") || "").toLowerCase();
    const inputs = (root) => Array.from(root.querySelectorAll("input")).filter(shown);
    const textual = (el) => ["email", "text", "tel"].includes(el.type);
    const userish = (el) => /\\b(username|email)\\b/.test(auto(el)) || el.type === "email" || /user|email|login|account/i.test((el.name || "") + " " + (el.id || ""));
    const signs = (s) => /sign.?in|log.?in|auth/i.test(s || "");
    const all = inputs(document);
    const password = all.find((el) => el.type === "password" && !auto(el).includes("new-password")) || null;
    let user = null, form = false;
    if (password) {
      const root = password.form || document;
      const before = inputs(root).filter(textual).filter((el) => el.compareDocumentPosition(password) & Node.DOCUMENT_POSITION_FOLLOWING);
      user = before.filter(userish).pop() || before.pop() || null;
      const changes = Array.from(root.querySelectorAll("input[type=password]")).some((el) => auto(el).includes("new-password"));
      form = !!user && !changes;
    } else {
      const fields = all.filter((el) => textual(el) && userish(el));
      const signIn = fields.find((el) => el.form && (signs(el.form.getAttribute("action")) || signs(location.pathname)));
      form = !!signIn;
      user = signIn || (force ? fields[0] || null : null);
    }
    if (username === null || (!form && !force)) return { form, filled: false };
    // Another account typed in already: the person chose it.
    if (!force && user && user.value && user.value !== username) return { form, filled: false };
    const value = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
    const put = (el, v) => {
      if (!el || !v || (!force && el.value)) return false;
      value.call(el, v);
      el.dispatchEvent(new Event("input", { bubbles: true }));
      el.dispatchEvent(new Event("change", { bubbles: true }));
      return true;
    };
    const a = put(user, username);
    const b = put(password, secret);
    return { form, filled: a || b };
    """
}
