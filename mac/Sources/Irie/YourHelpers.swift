import AppKit
import SwiftUI

// Your helpers (DESIGN §4.17): programs of yours that the core keeps
// running from your plugins' manifests, such as a listener that turns what
// you text yourself into sessions. The right sidebar is the same whichever
// session shows, so its Helpers tab is theirs. A session's own helpers, its
// subagents, show in that session's Helpers card, beside its terminal
// (Rolodex.swift).

// MARK: Wire

struct HelpersArgs: Encodable {
    var id: String?
    var restart: Bool?
}

struct HelpersResult: Decodable {
    var helpers: [HelperInfo]
}

/// One helper of yours, as the core says it stands (the helpers op).
struct HelperInfo: Decodable, Equatable, Identifiable {
    var id: String
    /// The plugin it comes with, whose switch turns it on.
    var plugin: String
    var title: String
    /// An SF Symbols name.
    var icon: String?
    var on: Bool
    /// It has a program for the core to keep running, which runs now.
    var runs: Bool
    var running: Bool
    /// When the core last started it, RFC 3339.
    var since: String?
    /// Why it doesn't run, or why its status file doesn't read.
    var problem: String?
    /// What its status file says: ok, working, needs_you or error.
    var state: String?
    var line: String?
    var items: [HelperItemInfo]?
    /// The file or folder its Open button opens.
    var open: String?

    /// It wants you: it can't run while it is on, or it says so.
    var needsYou: Bool { on && (problem != nil || state == "needs_you" || state == "error") }

    var mark: Look {
        if !on { return .paused }
        if problem != nil || state == "error" { return .failed }
        if state == "needs_you" { return .needs }
        if runs && !running { return .starting }
        return state == "working" ? .working : .idle
    }

    /// How it stands, in a few words.
    var standing: String {
        if !on { return "Off" }
        if let p = problem { return p }
        if runs && !running { return "Starting…" }
        if runs, let t = since.flatMap(ISO8601DateFormatter().date(from:)) {
            return "Running since " + t.formatted(date: Calendar.current.isDateInToday(t) ? .omitted : .abbreviated, time: .shortened)
        }
        return runs ? "Running" : "Watching its status"
    }
}

/// One thing a helper works on, as its status file lists it.
struct HelperItemInfo: Decodable, Equatable {
    var title: String
    var detail: String?
    /// working, waiting, needs_you, done or error.
    var state: String?
    /// A session of Irie's it is about, which a click shows.
    var session: String?
    /// A file of this Mac's that a click opens.
    var open: String?

    var look: Look {
        switch state {
        case "working": return .working
        case "waiting": return .waiting
        case "needs_you": return .needs
        case "done": return .done
        case "error": return .failed
        default: return .idle
        }
    }
}

extension Store {
    /// Reads your helpers again: every few seconds while the Helpers tab
    /// shows, and with the plugins once a minute, for the tab's count.
    func loadHelpers() async {
        guard canHelpers else {
            // A fixture's helpers stay (Snapshot.swift).
            if !isFixture, !ownHelpers.isEmpty { ownHelpers = [] }
            return
        }
        guard let r = try? await client.call("helpers", HelpersArgs(), timeout: 5, as: HelpersResult.self) else { return }
        if r.helpers != ownHelpers { ownHelpers = r.helpers }
    }

    /// Starts a helper of yours again, at once.
    func restartHelper(_ h: HelperInfo) {
        Task {
            do {
                let r = try await client.call("helpers", HelpersArgs(id: h.id, restart: true), timeout: 10, as: HelpersResult.self)
                if r.helpers != ownHelpers { ownHelpers = r.helpers }
            } catch {
                say("Couldn’t start \(h.title) again: \(error.localizedDescription)", error: true)
            }
        }
    }

    /// A helper's file: a text file in a code card beside the session on
    /// show, so it reads and edits as any file there; anything else, and a
    /// folder, in its own app.
    func openHelperFile(_ path: String) {
        var dir: ObjCBool = false
        let exists = FileManager.default.fileExists(atPath: path, isDirectory: &dir)
        guard exists else {
            say("\((path as NSString).lastPathComponent) isn’t there yet.", error: true)
            return
        }
        let text = ["md", "markdown", "txt", "json", "jsonl", "log", "toml", "yaml", "yml", "csv"]
        if !dir.boolValue, text.contains((path as NSString).pathExtension.lowercased()), let s = paneSession {
            showCard(.code, in: s.id, file: CardFile(path: path, line: 1))
        } else {
            NSWorkspace.shared.open(URL(fileURLWithPath: path))
        }
    }

    /// The Helpers tab's count: what of your helpers needs you, in red, or
    /// else what they work on now.
    var helpersCount: (count: Int, needsYou: Bool) {
        let on: [HelperInfo] = ownHelpers.filter(\.on)
        let items: [HelperItemInfo] = on.flatMap { $0.items ?? [] }
        let wanting: Int = items.filter { $0.state == "needs_you" || $0.state == "error" }.count
        let needs: Int = on.filter(\.needsYou).count + wanting
        if needs > 0 { return (needs, true) }
        return (items.filter { $0.state == "working" }.count, false)
    }
}

// MARK: The tab

/// The right sidebar's Helpers tab: your helpers, each with how it stands,
/// its line and what it works on. A row of a session shows it; a row of a
/// file opens it.
@MainActor
struct YourHelpersTab: View {
    @Environment(Store.self) private var store

    var body: some View {
        Group {
            if !store.canHelpers, !store.isFixture {
                SideNote(title: "Helpers",
                         text: "This \(Brand.name) core is from before helpers of your own. They show here once it is up to date.")
            } else if store.ownHelpers.isEmpty {
                SideNote(title: "No helpers yet",
                         text: "A helper is a program of yours that \(Brand.name) keeps running for you, such as a listener for what you text yourself. A [helpers.NAME] table in a manifest in the plugins folder adds one. A session’s own helpers show in its Helpers card, beside its terminal (⇧⌘H).")
            } else {
                ScrollView {
                    VStack(spacing: 8) {
                        ForEach(store.ownHelpers) { card($0) }
                    }
                    .padding(10)
                }
            }
        }
        .task {
            while !Task.isCancelled {
                await store.loadHelpers()
                try? await Task.sleep(nanoseconds: 3_000_000_000)
            }
        }
    }

    private func card(_ h: HelperInfo) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .top, spacing: 8) {
                Image(systemName: h.icon ?? "gearshape.2")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(h.on ? Theme.ink : Theme.muted)
                    .frame(width: 18, height: 18)
                VStack(alignment: .leading, spacing: 2) {
                    Text(h.title)
                        .font(Theme.font(13, .semibold))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                    HStack(alignment: .top, spacing: 5) {
                        StateMark(look: h.mark, size: 10)
                            .padding(.top, 2)
                        Text(h.standing)
                            .font(Theme.font(11.5))
                            .foregroundStyle(h.needsYou ? Theme.needs : Theme.muted)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                Spacer(minLength: 4)
                if h.on, h.runs {
                    button("arrow.clockwise", help: "Start \(h.title) again") { store.restartHelper(h) }
                }
                if let open = h.open {
                    button("arrow.up.forward.square", help: "Open \((open as NSString).lastPathComponent)") { store.openHelperFile(open) }
                }
                Toggle("", isOn: Binding(get: { h.on }, set: { store.setPlugin(h.plugin, on: $0) }))
                    .toggleStyle(.switch)
                    .controlSize(.mini)
                    .labelsHidden()
                    .help(h.on ? "Turn \(h.title) off, with its plugin" : "Turn \(h.title) on, with its plugin")
                    .accessibilityLabel(h.title)
            }
            if let line = h.line, !line.isEmpty, h.on {
                Text(line)
                    .font(Theme.font(12))
                    .foregroundStyle(Theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if h.on, let items = h.items, !items.isEmpty {
                VStack(spacing: 1) {
                    ForEach(Array(items.enumerated()), id: \.offset) { _, it in item(it) }
                }
            }
        }
        .padding(10)
        .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(Theme.card))
        .accessibilityElement(children: .contain)
        .accessibilityLabel("\(h.title), \(h.standing)")
    }

    private func item(_ it: HelperItemInfo) -> some View {
        let shows = it.session.map { s in store.sessions.contains { $0.id == s } } ?? false
        return Button {
            if let s = it.session, shows {
                store.show(s)
            } else if let path = it.open {
                store.openHelperFile(path)
            }
        } label: {
            HStack(alignment: .top, spacing: 6) {
                StateMark(look: it.look, size: 10)
                    .padding(.top, 3)
                VStack(alignment: .leading, spacing: 1) {
                    Text(it.title)
                        .font(Theme.font(12, .semibold))
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                    if let d = it.detail, !d.isEmpty {
                        Text(d)
                            .font(Theme.font(11.5))
                            .foregroundStyle(it.state == "needs_you" ? Theme.needs : Theme.muted)
                            .lineLimit(2)
                    }
                }
                Spacer(minLength: 0)
            }
            .padding(.vertical, 3)
            .padding(.horizontal, 6)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!shows && it.open == nil)
        .help(shows ? "Show its session" : it.open.map { "Open \(($0 as NSString).lastPathComponent)" } ?? "")
    }

    private func button(_ symbol: String, help: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 11, weight: .semibold))
                .foregroundStyle(Theme.muted)
                .frame(width: 20, height: 20)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(help)
        .accessibilityLabel(help)
    }
}
