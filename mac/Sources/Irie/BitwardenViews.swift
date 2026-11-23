import AppKit
import SwiftUI
import WebKit

// The Bitwarden plugin's parts in the app (Bitwarden.swift): unlocking in a
// sheet, the key in a web pane's header, the banner over a sign-in page
// while Bitwarden is locked, and Unlock… or Lock in Settings › Plugins.

// MARK: Unlocking

extension Store {
    /// The Bitwarden plugin's autofill part, while it and its plugin are
    /// on, as the core says.
    var bitwardenPart: PluginInfo.Part? {
        guard let p = plugins.first(where: { $0.name == "bitwarden" }), p.on else { return nil }
        return p.parts.first { $0.name == "autofill" && $0.isOn }
    }

    /// Takes the plugin's state from the core's list: the panes offer the
    /// key while its part is on, and it locks once the core says it is off.
    /// A list that is empty, as while the core restarts, changes nothing.
    func takeBitwarden() {
        guard !plugins.isEmpty else { return }
        let on = bitwardenPart != nil
        BitwardenVault.shared.offered = on
        if !on { BitwardenVault.shared.lock() }
    }

    /// Asks for the master password in a sheet on window, or on its own,
    /// then unlocks Bitwarden for the panes there are.
    func askBitwarden(on window: NSWindow?) {
        guard BitwardenVault.shared.state == .locked else { return }
        let alert = NSAlert()
        alert.messageText = "Unlock Bitwarden"
        alert.informativeText = "\(Brand.name) fills your panes’ sign-ins from your vault. Your master password goes only to Bitwarden’s own bw on this Mac, and \(Brand.name) keeps just your panes’ logins, in memory, until your Mac locks or sleeps."
        let field = NSSecureTextField(frame: NSRect(x: 0, y: 0, width: 260, height: 24))
        field.placeholderString = "Master password"
        alert.accessoryView = field
        alert.addButton(withTitle: "Unlock")
        alert.addButton(withTitle: "Cancel")
        alert.window.initialFirstResponder = field
        let answer: (NSApplication.ModalResponse) -> Void = { [weak self] r in
            let password = field.stringValue
            field.stringValue = ""
            guard r == .alertFirstButtonReturn, !password.isEmpty else { return }
            Task { await self?.unlockBitwarden(password) }
        }
        if let window, window.attachedSheet == nil {
            alert.beginSheetModal(for: window) { r in MainActor.assumeIsolated { answer(r) } }
        } else {
            answer(alert.runModal())
        }
    }

    /// Unlocks with the master password, then fills each pane's sign-in.
    func unlockBitwarden(_ password: String) async {
        #if !NOJAMMIN
        var origins = panes.filter(\.isWeb).compactMap { $0.origin.flatMap(URL.init(string:)) }
        // Spotify's sign-in on Jammin's sheet (JamminSignIn.swift), while
        // Jammin's Spotify part is on.
        if jamminSpotifyPart != nil, let spotify = URL(string: JamminSignInPage.origin) { origins.append(spotify) }
        #else
        let origins = panes.filter(\.isWeb).compactMap { $0.origin.flatMap(URL.init(string:)) }
        #endif
        if let problem = await BitwardenVault.shared.unlock(password: password, origins: origins) {
            say(problem, error: true, seconds: 8)
            return
        }
        // Locked again while it unlocked, as when the Mac went to sleep.
        guard BitwardenVault.shared.state == .unlocked else { return }
        let n = BitwardenVault.shared.logins.count
        say(n == 0
            ? "Bitwarden is unlocked, but has no login for your panes’ addresses."
            : "Bitwarden is unlocked: \(n == 1 ? "a login" : "\(n) logins") for your panes, until your Mac locks or sleeps.", error: n == 0)
        PluginPaneWebs.shared.lookAgain()
    }
}

// MARK: Views

/// The key in a web pane's header, or over Jammin's sign-in sheet:
/// unlocks Bitwarden, fills the page with one of its logins, or locks it
/// again.
@MainActor
struct BitwardenKey: View {
    @Environment(Store.self) private var store
    let pane: PluginPaneInfo
    var holder: (any BitwardenPage)?

    private var vault: BitwardenVault { .shared }

    var body: some View {
        Menu {
            items
        } label: {
            Text(Image(systemName: vault.state == .unlocked ? "key.fill" : "key"))
                .font(.system(size: 10, weight: .semibold))
                .foregroundStyle(Theme.muted)
        }
        .menuStyle(.borderlessButton)
        .menuIndicator(.hidden)
        .fixedSize()
        .frame(width: 20, height: 20)
        .help(help)
        .accessibilityLabel(help)
    }

    private var help: String {
        switch vault.state {
        case .locked: return "Bitwarden: unlock it to fill this pane’s sign-in"
        case .unlocking: return "Bitwarden: unlocking…"
        case .unlocked: return "Bitwarden: fill this pane’s sign-in, or lock it"
        }
    }

    @ViewBuilder private var items: some View {
        if let part = store.bitwardenPart, part.state != "ready" {
            ForEach(PluginCommands.sentences(part.detail), id: \.self) { Text($0) }
            Divider()
            Button("Plugin Settings…") {
                UserDefaults.standard.set(SettingsView.Tab.plugins.rawValue, forKey: SettingsView.tabKey)
                PluginCommands.openSettings()
            }
        } else {
            switch vault.state {
            case .locked:
                Button("Unlock Bitwarden…") { store.askBitwarden(on: holder?.webView.window ?? NSApp.keyWindow) }
            case .unlocking:
                Text("Unlocking Bitwarden…")
            case .unlocked:
                let page = holder?.page ?? pane.origin.flatMap(URL.init(string:))
                let logins = page.map(vault.logins(for:)) ?? []
                if logins.isEmpty {
                    Text("Bitwarden has no login for \(page.map(PluginPaneHolder.hostPort) ?? pane.title)")
                }
                ForEach(logins) { login in
                    Button("Fill \(login.menuName)") {
                        guard let holder else { return }
                        Task { if await !BitwardenFill.fill(holder, with: login) { store.say("This page has no field for \(login.menuName).", error: true) } }
                    }
                    .disabled(holder == nil)
                }
                Divider()
                Button("Lock Bitwarden") { vault.lock() }
            }
        }
    }
}

/// The banner over a pane's sign-in page while Bitwarden is locked: it
/// could fill it, once unlocked: a Banner, as Roots' components.md has it.
@MainActor
struct BitwardenBar: View {
    @Environment(Store.self) private var store
    @ObservedObject var holder: PluginPaneHolder

    private var vault: BitwardenVault { .shared }

    var body: some View {
        if vault.offered, holder.signIn, vault.state != .unlocked, store.bitwardenPart?.state == "ready" {
            BitwardenBarContent(unlocking: vault.state == .unlocking) {
                store.askBitwarden(on: holder.web.window)
            }
        }
    }
}

/// What the banner shows, apart from when, so the snapshots draw it too.
@MainActor
struct BitwardenBarContent: View {
    let unlocking: Bool
    let unlock: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                Text(Image(systemName: "key"))
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(Theme.muted)
                    .accessibilityHidden(true)
                Text("Bitwarden can fill this sign-in.")
                    .font(Theme.font(12))
                    .foregroundStyle(Theme.ink)
                    .lineLimit(2)
                Spacer(minLength: 6)
                Button(unlocking ? "Unlocking…" : "Unlock…", action: unlock)
                    .controlSize(.small)
                    .disabled(unlocking)
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(Theme.field)
            Rectangle().fill(Theme.edge).frame(height: 1)
        }
        .accessibilityElement(children: .contain)
    }
}

/// Unlock… or Lock in Settings › Plugins, beside the part.
@MainActor
struct BitwardenLockButton: View {
    @Environment(Store.self) private var store

    var body: some View {
        switch BitwardenVault.shared.state {
        case .locked:
            Button("Unlock…") { store.askBitwarden(on: NSApp.keyWindow) }
                .help("Asks for your master password, then fills your panes’ sign-ins until your Mac locks or sleeps")
        case .unlocking:
            Button("Unlocking…") {}.disabled(true)
        case .unlocked:
            Button("Lock") { BitwardenVault.shared.lock() }
                .help("Lets the logins go: the next sign-in asks for your master password again")
        }
    }
}
