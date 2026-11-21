import AppKit
import Foundation
import Observation

// The obsidian plugin in the app (DESIGN §4.17): where the vault is, as
// the core's obsidian op says, its notes and how they link, for the Vault
// cards (VaultPane.swift) and the Notes panel's Open in a Pane. A vault is
// a folder of Markdown notes (Obsidian's help, "How Obsidian stores
// data"), so the app reads and writes them itself and Obsidian is the
// person's to have or not; a note opens in Obsidian through its own
// obsidian:// links ("Obsidian URI").

// MARK: Wire

struct ObsidianArgs: Encodable {}

/// The notepad's note in the vault, while the notepad is kept there too.
struct NotepadVault: Codable, Equatable {
    var path: String
    var synced: Date?
    var problem: String?
}

/// Where the vault is, and how it stands.
struct ObsidianResult: Decodable, Equatable {
    /// ready, needs (text says what it lacks) or plugin_off.
    var state: String
    var text: String
    var vault: String?
    var name: String?
    /// Obsidian has opened the folder as a vault, so its links reach it.
    var opened: Bool?
    var notes: NotepadVault?
}

// MARK: The model

/// The vault as the app knows it: the core's word on where it is, asked
/// as a Vault card shows and every few seconds while one does.
@MainActor
@Observable
final class VaultModel {
    private(set) var result: ObsidianResult?
    /// The core serves the obsidian op.
    var served = false
    @ObservationIgnored weak var store: Store?
    @ObservationIgnored private var fixture = false

    /// The vault's folder, while there is one to show.
    var folder: String? { result?.state == "ready" ? result?.vault : nil }
    /// The notepad's note, while the notepad is kept in the vault: as the
    /// op said, else as the notepad's last frame did.
    var notesNote: String? {
        guard let p = result?.notes?.path ?? store?.notes.pad.vault?.path, !p.isEmpty else { return nil }
        return p
    }

    /// `--snapshot`'s vault.
    func loadFixture(_ r: ObsidianResult?) {
        fixture = true
        result = r
    }

    /// Asks the core where the vault is.
    func look() async {
        guard !fixture, served, let client = store?.client else { return }
        if let r = try? await client.call("obsidian", ObsidianArgs(), timeout: 5, as: ObsidianResult.self), r != result {
            result = r
        }
    }

    /// Whether Obsidian is on this Mac: an app takes its obsidian:// links.
    static var obsidianHere: Bool {
        guard let u = URL(string: "obsidian://open") else { return false }
        return NSWorkspace.shared.urlForApplication(toOpen: u) != nil
    }

    /// The link that opens a file in Obsidian, by its path, as Obsidian
    /// URI's open action takes it: the most specific vault Obsidian knows
    /// that holds it.
    static func obsidianLink(_ path: String) -> URL? {
        var allowed = CharacterSet.alphanumerics
        allowed.insert(charactersIn: "-._~")
        guard let p = path.addingPercentEncoding(withAllowedCharacters: allowed) else { return nil }
        return URL(string: "obsidian://open?path=" + p)
    }
}

// MARK: Opening the vault

extension Store {
    /// Opens the vault on a Vault card beside the session on show, with a
    /// note on show: the one asked for, else the notepad's note while the
    /// notepad is kept in the vault. The card comes to the front, with the
    /// keyboard.
    func openVault(note: String? = nil) {
        guard let s = paneSession else {
            say("Pick a session first: the vault opens beside its terminal.", error: false)
            return
        }
        guard paneKinds.contains(.vault) else {
            say("The Obsidian plugin is off: Settings › Plugins turns it on.", error: false)
            return
        }
        let r = rolodex
        let deck = r.deck(s.id)
        let card = (deck.frontPane?.kind == .vault ? deck.frontPane : deck.panes.first { $0.kind == .vault })
            ?? r.add(.vault, to: s.id, avoiding: s.hue.map { ($0 - 1) % Theme.agentSlots })
        if let path = note ?? (card.file == nil ? vault.notesNote : nil) {
            r.open(path, on: card.id, in: s.id)
        }
        r.reveal()
        r.turn(to: card.id, in: s.id, keys: true, layout: tabPlace(for: s.id))
        Task { await vault.look() }
    }

    /// Choose Vault…: a folder of the person's, as the vault.
    func chooseVault() async -> Bool {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.canCreateDirectories = true
        panel.prompt = "Use as Vault"
        panel.message = "Choose your Obsidian vault: the folder that holds your notes. \(Brand.name) keeps your Notes in it too, as \(Brand.name) Notes.md."
        guard panel.runModal() == .OK, let url = panel.url else { return false }
        return await useVault(url.path)
    }

    /// New Vault…: a new folder for notes, made where the person says.
    func newVault() async -> Bool {
        let panel = NSSavePanel()
        panel.nameFieldStringValue = "Notes"
        panel.nameFieldLabel = "Vault:"
        panel.prompt = "Make Vault"
        panel.message = "Name your new vault, a folder for your notes. Obsidian opens it too, with Open folder as vault."
        panel.directoryURL = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first
        guard panel.runModal() == .OK, let url = panel.url else { return false }
        do {
            try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        } catch {
            say("Couldn’t make the vault: \(error.localizedDescription)", error: true)
            return false
        }
        return await useVault(url.path)
    }

    /// Sets obsidian.vault, as a ~ path when it is in the home folder.
    private func useVault(_ path: String) async -> Bool {
        var p = path
        let home = NSHomeDirectory()
        if p == home {
            p = "~"
        } else if p.hasPrefix(home + "/") {
            p = "~" + p.dropFirst(home.count)
        }
        do {
            _ = try await client.call("config", ConfigArgs(set: [ConfigSet(key: "obsidian.vault", value: .string(p))]), timeout: 10, as: ConfigResult.self)
            loadPluginsSoon()
            await settings.load()
            await vault.look()
            return true
        } catch {
            say("Couldn’t use that folder: \(error.localizedDescription)", error: true)
            return false
        }
    }

    /// Opens a note, or the vault, in Obsidian.
    func openInObsidian(_ path: String) {
        guard VaultModel.obsidianHere else {
            say("Obsidian isn’t on this Mac: obsidian.md has it, and your vault opens in it as it is.", error: false)
            return
        }
        guard let url = VaultModel.obsidianLink(path) else { return }
        NSWorkspace.shared.open(url)
    }
}
