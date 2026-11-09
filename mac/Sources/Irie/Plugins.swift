import AppKit
import SwiftUI

/// The menu bar's Plugins menu (DESIGN §4.17): each of Irie's plugins with
/// its switch, and under it each part by name and state, whose submenu holds
/// the core's sentence on what it has and lacks, its own switch and Set
/// Up…, as Settings › Plugins has them. Like IrieCommands, it reads only
/// the menu's facts, so it changes only when a plugin does.
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
                Text(f.canPlugins ? "Reading the plugins…" : "This Irie has no plugins yet")
            }
            ForEach(f.plugins) { p in
                Section(p.title) {
                    Toggle("\(p.title) Plugin", isOn: Binding(get: { p.on }, set: { on in
                        MainActor.assumeIsolated { store.setPlugin(p.name, on: on) }
                    }))
                    ForEach(p.parts) { x in
                        Menu(Self.title(x)) {
                            ForEach(Self.sentences(x.detail), id: \.self) { Text($0) }
                            if x.switch != nil || x.setup != nil { Divider() }
                            if x.switch != nil {
                                Toggle("On", isOn: Binding(get: { x.on }, set: { on in
                                    MainActor.assumeIsolated { store.setPlugin(p.name, part: x.name, on: on) }
                                }))
                                .disabled(!p.on)
                            }
                            if let words = x.setup {
                                Button("Set Up…", action: run { store.runWords(words) })
                                    .disabled(!p.on)
                            }
                        }
                    }
                    if p.on, let words = p.setup {
                        Button("Set Up Each Part…", action: run { store.runWords(words) })
                    }
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
    /// colour, as "Mullvad VPN — ready".
    static func title(_ x: PluginInfo.Part) -> String {
        "\(x.title) — \(x.stateWords)"
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
            guard let r = try? await client.call("plugins", PluginsArgs(), timeout: 5, as: PluginsResult.self) else { return }
            takePlugins(r.plugins)
        }
    }

    /// Turns a plugin, or one part of it, on or off (the plugins op, which
    /// knows the key each takes), and says why when the core refuses.
    func setPlugin(_ plugin: String, part: String? = nil, on: Bool) {
        Task {
            do {
                let r = try await client.call("plugins", PluginsArgs(plugin: plugin, part: part, on: on), timeout: 10, as: PluginsResult.self)
                takePlugins(r.plugins)
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

    /// Takes the plugins in once no menu is open: a change under an open
    /// menu would rebuild it under the pointer (take(_:)).
    private func takePlugins(_ all: [PluginInfo]) {
        guard all != plugins else { return }
        if RunLoop.current.currentMode == .eventTracking {
            RunLoop.main.perform(inModes: [.default]) { [weak self] in
                MainActor.assumeIsolated { self?.takePlugins(all) }
            }
            return
        }
        plugins = all
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
