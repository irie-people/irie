import AppKit
import SwiftUI

/// The menu bar's Plugins menu (DESIGN §4.17): each of Irie's plugins with
/// its switch, and under it each part by name and state, whose submenu holds
/// the core's sentence on what it has and lacks, its own switch and Set
/// Up…, as Settings › Plugins has them, then the part's own commands: the
/// VPN's, and a part whose words start a session, such as :write, starts
/// it. Like IrieCommands, it reads only the menu's facts, so it changes
/// only when a plugin does.
struct PluginCommands: Commands {
    let store: Store
    let menu: MenuState

    /// An action on the main actor, where SwiftUI runs a menu's actions.
    private func run(_ work: @escaping @MainActor () -> Void) -> () -> Void {
        { MainActor.assumeIsolated(work) }
    }

    var body: some Commands {
        let f = MainActor.assumeIsolated { menu.facts }
        CommandMenu("Plugins") {
            if f.plugins.isEmpty {
                Text(f.canPlugins ? "Reading the plugins…" : "This \(Brand.name) has no plugins yet")
            }
            // Your bundles first: one switch for several plugins, ticked
            // while all of them are on (PluginPanes.swift).
            ForEach(f.bundles) { b in
                if b.problem == nil {
                    Toggle(b.state == "some" ? "\(b.title) Bundle (\(b.stateWords))" : "\(b.title) Bundle", isOn: Binding(get: { b.state == "on" }, set: { on in
                        MainActor.assumeIsolated { store.setBundle(b.name, on: on) }
                    }))
                } else {
                    Menu("\(b.title) Bundle — its manifest doesn’t read") {
                        ForEach(Self.sentences(b.problem ?? ""), id: \.self) { Text($0) }
                    }
                }
            }
            if !f.bundles.isEmpty, !f.plugins.isEmpty { Divider() }
            // A plugin's switch heads its parts, with a line between plugins.
            // Lines are Dividers: a Section draws one before and after itself,
            // so at the menu's top, or two side by side, it drew a stray or a
            // double line.
            ForEach(Array(f.plugins.enumerated()), id: \.element.id) { i, p in
                if i > 0 { Divider() }
                if let problem = p.problem {
                    // A manifest of yours that doesn't read has no switch.
                    Menu("\(p.title) Plugin — its manifest doesn’t read") {
                        ForEach(Self.sentences(problem), id: \.self) { Text($0) }
                    }
                } else {
                    Toggle("\(p.title) Plugin", isOn: Binding(get: { p.on }, set: { on in
                        MainActor.assumeIsolated { store.setPlugin(p.name, on: on) }
                    }))
                }
                ForEach(p.parts) { x in
                    Menu(Self.title(x)) {
                        let lines = Self.sentences(x.detail)
                        let acts = x.switch != nil || x.setup != nil || x.pane != nil && x.isOn
                        ForEach(lines, id: \.self) { Text($0) }
                        if acts, !lines.isEmpty { Divider() }
                        // A pane's part shows its pane.
                        if let pane = x.pane, x.isOn {
                            Button("Show in Right Sidebar", action: run { store.openPane(pane.id) })
                        }
                        if x.switch != nil {
                            Toggle("On", isOn: Binding(get: { x.on }, set: { on in
                                MainActor.assumeIsolated { store.setPlugin(p.name, part: x.name, on: on) }
                            }))
                            .disabled(!p.on)
                        }
                        if let words = x.setup {
                            Button(Self.setupTitle(x, words), action: run { store.runWords(words) })
                                .disabled(!p.on)
                        }
                        // The VPN: the same words as :vpn in a task bar
                        // (DESIGN §4.18), while the core serves the vpn op,
                        // and off until vpn.on. Lockdown and the account's
                        // time only for a VPN that has them, as Mullvad does.
                        // Bitwarden: the panes' key's Unlock and Lock
                        // (Bitwarden.swift), while its part is ready.
                        if p.name == "bitwarden", x.name == "autofill", x.state == "ready" {
                            if acts || !lines.isEmpty { Divider() }
                            if f.bitwarden == BitwardenVault.State.unlocked.rawValue {
                                Button("Lock Bitwarden", action: run { BitwardenVault.shared.lock() })
                            } else {
                                Button("Unlock Bitwarden…", action: run { store.askBitwarden(on: NSApp.keyWindow) })
                                    .disabled(f.bitwarden == BitwardenVault.State.unlocking.rawValue)
                            }
                        }
                        if x.name == "vpn", f.canVPN {
                            if acts || !lines.isEmpty { Divider() }
                            Group {
                                Button("Connect", action: run { store.runWords(":vpn on") })
                                Button("Disconnect", action: run { store.runWords(":vpn off") })
                                if f.vpnLockdown {
                                    Button(f.lockdown ? "Turn Lockdown Mode Off" : "Turn Lockdown Mode On", action: run {
                                        store.runWords(store.vpn?.lockdown == true ? ":vpn lockdown off" : ":vpn lockdown on")
                                    })
                                }
                                if f.vpnTime {
                                    Button("Account Time Left", action: run { store.runWords(":vpn time") })
                                }
                            }
                            .disabled(!f.vpn)
                        }
                    }
                }
                if p.on, let words = p.setup {
                    Button("Set Up Each Part…", action: run { store.runWords(words) })
                }
            }
            Divider()
            Button("Plugin Settings…", action: run {
                UserDefaults.standard.set(SettingsView.Tab.plugins.rawValue, forKey: SettingsView.tabKey)
                Self.openSettings()
            })
        }
    }

    /// Opens the Settings window as its own item in the app menu does, the
    /// one with ⌘,. macOS 14's SwiftUI offers only SettingsLink, which can't
    /// pick the tab first, and refuses showSettingsWindow:.
    static func openSettings() {
        guard let menu = NSApp.mainMenu?.items.first?.submenu,
              let i = menu.items.firstIndex(where: { $0.keyEquivalent == "," && $0.keyEquivalentModifierMask == .command })
        else { return }
        menu.performActionForItem(at: i)
    }

    /// A part's item: its name and its state in words, which a menu can't
    /// colour, as "Tailscale — ready".
    static func title(_ x: PluginInfo.Part) -> String {
        "\(x.title) — \(x.stateWords)"
    }

    /// What a part's words do: :setup words set it up, and others start
    /// its own session, as :write starts Writing in Variorum.
    static func setupTitle(_ x: PluginInfo.Part, _ words: String) -> String {
        // :mail opens the Mail tab, where an account is added.
        if words == ":mail" { return "Show Mail" }
        return words.hasPrefix(":setup") ? "Set Up…" : "Start \(x.title)…"
    }

    /// The core's sentence on a part, a line each, so the submenu stays
    /// narrow.
    static func sentences(_ text: String) -> [String] {
        var out: [String] = []
        var rest = Substring(text)
        while let r = rest.range(of: ". ") {
            out.append(String(rest[..<r.lowerBound]) + ".")
            rest = rest[r.upperBound...]
        }
        if !rest.isEmpty { out.append(String(rest)) }
        return out
    }
}

extension Store {
    /// Reads the plugins again, in the background. The menu has them as the
    /// core says they stand: the store asks as it connects, as the app comes
    /// forward, once a minute, when the VPN changes and after a change.
    func loadPluginsSoon() {
        guard canPlugins else {
            if !plugins.isEmpty { plugins = [] }
            return
        }
        Task {
            // Your helpers too, for the Helpers tab's count (YourHelpers.swift).
            await loadHelpers()
            guard let r = try? await client.call("plugins", PluginsArgs(), timeout: 5, as: PluginsResult.self) else { return }
            takePlugins(r)
            // The Chat tab's count of drafts and mentions moves while the
            // tab is shut too; the core answers from memory (Chat.swift).
            if chatPane != nil { await chat.refresh() }
        }
    }

    /// Turns a plugin, or one part of it, on or off (the plugins op, which
    /// knows the key each takes), and says why when the core refuses.
    func setPlugin(_ plugin: String, part: String? = nil, on: Bool) {
        Task {
            do {
                let r = try await client.call("plugins", PluginsArgs(plugin: plugin, part: part, on: on), timeout: 10, as: PluginsResult.self)
                takePlugins(r)
                // A helper of yours goes on and off with its plugin.
                await loadHelpers()
                // Settings › Plugins and the switches elsewhere in Settings
                // show the change too.
                await settings.load()
            } catch {
                let what = plugins.first { $0.name == plugin }.map { p in
                    part.flatMap { name in p.parts.first { $0.name == name }?.title } ?? "the \(p.title) plugin"
                } ?? plugin
                say("Couldn’t turn \(what) \(on ? "on" : "off"): \(error.localizedDescription)", error: true)
            }
        }
    }

    /// Sends words as the task bar would, such as :setup anon.
    func runWords(_ words: String) {
        Task { _ = await input(words) }
    }

    /// Takes the plugins and bundles in once no menu is open: a change
    /// under an open menu would rebuild it under the pointer (take(_:)). A
    /// pane whose part went off lets its web view go (PluginPanes.swift).
    func takePlugins(_ r: PluginsResult) {
        let all = r.plugins, bs = r.bundles ?? []
        guard all != plugins || bs != bundles else { return }
        if RunLoop.current.currentMode == .eventTracking {
            RunLoop.main.perform(inModes: [.default]) { [weak self] in
                MainActor.assumeIsolated { self?.takePlugins(r) }
            }
            return
        }
        plugins = all
        bundles = bs
        PluginPaneWebs.shared.keep(Set(panes.map(\.id)))
        takeBitwarden()
    }

    /// Reads the plugins once a minute while the app runs, for a part set up
    /// in a terminal of its own.
    func keepPlugins() {
        guard pluginsTimer == nil else { return }
        pluginsTimer = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 60_000_000_000)
                self?.loadPluginsSoon()
            }
        }
    }
}
