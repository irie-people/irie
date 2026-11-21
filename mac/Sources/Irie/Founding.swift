import AppKit
import SwiftUI

// The right sidebar's Founding tab (DESIGN §4.17): the Declaration of
// Independence, the Constitution and the Bill of Rights, word for word as
// the founding plugin's core op gives them, with a search that finds a
// passage whatever its spelling. A pane of the app's own view, which shows
// while the founding plugin is on (PluginPanes.swift), drawn from Roots'
// parts: a segmented choice, a field, the text in the app's type, notes
// muted (components.md).

// MARK: Wire

struct FoundingArgs: Encodable {
    var find: String?
}

/// One piece of a document: a heading (h2 to h4), the document's own words
/// (text), a note of ours (note), or names, one a line (list).
struct FoundingBlock: Decodable, Equatable {
    var kind: String
    var text: String?
    var items: [String]?
}

struct FoundingDoc: Decodable, Equatable, Identifiable {
    var id: String
    var title: String
    var about: String
    var sha512: String
    var blocks: [FoundingBlock]

    /// Its name in the tab's choice.
    var short: String {
        switch id {
        case "declaration": return "Declaration"
        case "constitution": return "Constitution"
        case "rights": return "Bill of Rights"
        case "foia": return "FOIA"
        default: return title
        }
    }
}

struct FoundingHit: Decodable, Equatable {
    var doc: String
    var block: Int
    var `where`: String
}

struct FoundingResult: Decodable {
    var docs: [FoundingDoc]?
    var found: [FoundingHit]?
    var chain: FoundingChainInfo?
}

/// The documents' local chain, as the core checked it: each version holds
/// every text and names the one before it.
struct FoundingChainInfo: Decodable, Equatable {
    var folder: String
    var head: String?
    var created: String?
    var versions: Int
    var verified: Bool
    var problem: String?
}

// MARK: The model

/// The Founding tab's state: the documents, read once, the one on show,
/// and what the search box found, which the core works out.
@MainActor
@Observable
final class FoundingModel {
    let client: CoreClient

    init(client: CoreClient) {
        self.client = client
    }

    private(set) var docs: [FoundingDoc] = []
    /// The document on show, by its id.
    var showing = "declaration"
    /// The words in the search box.
    var query = ""
    /// What the search found, in every document, in order.
    private(set) var hits: [FoundingHit] = []
    /// Which of the document on show's hits is on show.
    var current = 0
    /// Why the last call failed, under the box.
    var problem: String?
    private(set) var loaded = false
    /// The local chain, as the core checked it when the tab loaded.
    private(set) var chain: FoundingChainInfo?

    @ObservationIgnored private var isFixture = false
    @ObservationIgnored private var asked = ""

    var doc: FoundingDoc? { docs.first { $0.id == showing } ?? docs.first }
    /// The document on show's hits.
    var here: [FoundingHit] { hits.filter { $0.doc == doc?.id } }
    /// The block the search shows now, if any.
    var target: Int? { here.indices.contains(current) ? here[current].block : nil }
    var searching: Bool { !query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    func count(_ id: String) -> Int { hits.filter { $0.doc == id }.count }

    private func call(_ find: String?) async throws -> FoundingResult {
        do {
            return try await client.call("founding", FoundingArgs(find: find), timeout: 10, as: FoundingResult.self)
        } catch let e as WireError {
            throw e
        } catch {
            throw WireError(code: "offline", msg: "\(Brand.name)’s core isn’t answering now.")
        }
    }

    /// Reads the documents, and how their chain checks: once, for they
    /// never change while Irie runs.
    func load() async {
        guard !isFixture, !loaded else { return }
        do {
            let r = try await call(nil)
            docs = r.docs ?? []
            chain = r.chain
            loaded = true
            problem = nil
        } catch {
            problem = error.localizedDescription
        }
    }

    /// Asks the core for the passages that hold the box's words, and shows
    /// the first: in this document, or the first document that has one.
    func search() async {
        let q = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard q != asked else { return }
        asked = q
        current = 0
        guard !q.isEmpty else {
            hits = []
            return
        }
        guard !isFixture else { return }
        do {
            let found = try await call(q).found ?? []
            guard asked == q else { return } // a newer search took over
            hits = found
            if here.isEmpty, let first = hits.first { showing = first.doc }
            problem = nil
        } catch {
            problem = error.localizedDescription
        }
    }

    /// Shows the next hit, or the one before, round the document.
    func step(_ by: Int) {
        let n = here.count
        guard n > 0 else { return }
        current = (current + by + n) % n
    }

    func show(_ id: String) {
        showing = id
        current = 0
    }

    /// The snapshots' documents, which no core gives.
    func loadFixture() {
        isFixture = true
        loaded = true
        docs = [
            FoundingDoc(id: "declaration", title: "The Declaration of Independence",
                        about: "The National Archives’ transcription of the engrossed parchment.", sha512: String(repeating: "7447f938", count: 16),
                        blocks: [FoundingBlock(kind: "text", text: "In Congress, July 4, 1776"),
                                 FoundingBlock(kind: "text", text: "**The unanimous Declaration of the thirteen united States of America,** When in the Course of human events, it becomes necessary for one people to dissolve the political bands which have connected them with another…")]),
            FoundingDoc(id: "constitution", title: "The Constitution of the United States",
                        about: "The National Archives’ transcription of the Constitution as Jacob Shallus inscribed it on parchment in 1787, and of every amendment since.", sha512: String(repeating: "1e2a63f6", count: 16),
                        blocks: [FoundingBlock(kind: "h2", text: "Preamble"),
                                 FoundingBlock(kind: "text", text: "We the People of the United States, in Order to form a more perfect Union, establish Justice, insure domestic Tranquility, provide for the common defence, promote the general Welfare, and secure the Blessings of Liberty to ourselves and our Posterity, do ordain and establish this Constitution for the United States of America."),
                                 FoundingBlock(kind: "h2", text: "Article. I."),
                                 FoundingBlock(kind: "h3", text: "Section. 1."),
                                 FoundingBlock(kind: "text", text: "All legislative Powers herein granted shall be vested in a Congress of the United States, which shall consist of a Senate and House of Representatives."),
                                 FoundingBlock(kind: "h3", text: "Section. 2."),
                                 FoundingBlock(kind: "text", text: "The House of Representatives shall chuse their Speaker and other Officers; and shall have the sole Power of Impeachment.")]),
            FoundingDoc(id: "rights", title: "The Bill of Rights",
                        about: "The first ten amendments to the Constitution, ratified on December 15, 1791.", sha512: String(repeating: "03a89a22", count: 16),
                        blocks: [FoundingBlock(kind: "h3", text: "Amendment I"),
                                 FoundingBlock(kind: "text", text: "Congress shall make no law respecting an establishment of religion, or prohibiting the free exercise thereof; or abridging the freedom of speech, or of the press; or the right of the people peaceably to assemble, and to petition the Government for a redress of grievances.")]),
        ]
        docs.append(FoundingDoc(id: "foia", title: "The Freedom of Information Act",
                                about: "5 U.S.C. § 552, word for word as the United States Code sets it out.", sha512: String(repeating: "f18fc8dc", count: 16),
                                blocks: [FoundingBlock(kind: "h2", text: "§ 552. Public information; agency rules, opinions, orders, records, and proceedings"),
                                         FoundingBlock(kind: "text", text: "(a) Each agency shall make available to the public information as follows:")]))
        chain = FoundingChainInfo(folder: "~/.local/state/irie/chain/founding", head: "sha512:" + String(repeating: "9c1e40b7", count: 16),
                                  created: "2026-11-21T12:00:00Z", versions: 1, verified: true, problem: nil)
        showing = "constitution"
        query = "choose"
        asked = "choose"
        hits = [FoundingHit(doc: "constitution", block: 6, where: "Article. I. › Section. 2.")]
    }
}

// MARK: The tab

@MainActor
struct FoundingTab: View {
    @Environment(Store.self) private var store

    var body: some View {
        let f = store.founding
        VStack(spacing: 0) {
            FoundingBar()
            if let p = f.problem {
                Text(p)
                    .font(Theme.font(12))
                    .foregroundStyle(Theme.needs)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, Theme.inset)
                    .padding(.bottom, 6)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .accessibilityLabel("Problem: \(p)")
            }
            if let doc = f.doc {
                FoundingPage(doc: doc)
            } else if !f.loaded, f.problem == nil {
                ProgressView().controlSize(.small).frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                Spacer()
            }
        }
        .task { await f.load() }
        // The core searches once typing pauses.
        .task(id: f.query) {
            try? await Task.sleep(nanoseconds: 250_000_000)
            guard !Task.isCancelled else { return }
            await f.search()
        }
    }
}

/// The choice of document, and the search box with what it found.
@MainActor
private struct FoundingBar: View {
    @Environment(Store.self) private var store
    @FocusState private var focused: Bool

    var body: some View {
        let f = store.founding
        VStack(alignment: .leading, spacing: 8) {
            Picker("Document", selection: Binding(get: { f.doc?.id ?? f.showing }, set: { f.show($0) })) {
                ForEach(f.docs) { d in
                    let n = f.count(d.id)
                    Text(f.searching && n > 0 ? "\(d.short) \(n)" : d.short).tag(d.id)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .accessibilityLabel("Which document")
            HStack(spacing: 6) {
                Image(systemName: "magnifyingglass")
                    .font(.system(size: 11))
                    .foregroundStyle(Theme.muted)
                    .accessibilityHidden(true)
                TextField("Find words, in any spelling", text: Bindable(f).query)
                    .textFieldStyle(.plain)
                    .font(Theme.font(12.5))
                    .focused($focused)
                    .onSubmit { f.step(1) }
                    .accessibilityLabel("Words to find")
                if f.searching {
                    let n = f.here.count
                    Text(n == 0 ? "None here" : "\(f.current + 1) of \(n)")
                        .font(Theme.font(11))
                        .foregroundStyle(Theme.muted)
                        .monospacedDigit()
                    Button { f.step(-1) } label: { Image(systemName: "chevron.up") }
                        .buttonStyle(.plain)
                        .disabled(n < 2)
                        .help("The passage before")
                        .accessibilityLabel("The passage before")
                    Button { f.step(1) } label: { Image(systemName: "chevron.down") }
                        .buttonStyle(.plain)
                        .disabled(n < 2)
                        .help("The next passage")
                        .accessibilityLabel("The next passage")
                    Button { f.query = "" } label: { Image(systemName: "xmark.circle.fill") }
                        .buttonStyle(.plain)
                        .foregroundStyle(Theme.muted)
                        .help("Clear the search")
                        .accessibilityLabel("Clear the search")
                }
            }
            .font(.system(size: 11))
            .padding(.leading, Theme.inset)
            .padding(.trailing, 8)
            .padding(.vertical, 5)
            .background(RoundedRectangle(cornerRadius: Theme.radius, style: .continuous).fill(Theme.field))
            .overlay(
                RoundedRectangle(cornerRadius: Theme.radius, style: .continuous)
                    .strokeBorder(focused ? Theme.accent : Theme.edge, lineWidth: focused ? 1.5 : 1)
            )
        }
        .padding(10)
    }
}

/// One document, its passages in order, the search's marked, and its
/// fingerprint at the foot.
@MainActor
private struct FoundingPage: View {
    @Environment(Store.self) private var store
    let doc: FoundingDoc

    var body: some View {
        let f = store.founding
        let found = Set(f.here.map(\.block))
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 8) {
                    Text(doc.title)
                        .font(Theme.font(16, .semibold))
                        .foregroundStyle(Theme.ink)
                    Text(doc.about)
                        .font(Theme.font(11.5))
                        .foregroundStyle(Theme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(.bottom, 4)
                    ForEach(Array(doc.blocks.enumerated()), id: \.offset) { i, b in
                        FoundingBlockView(block: b, found: found.contains(i), current: f.target == i)
                            .id(i)
                    }
                    FoundingPrint(doc: doc, chain: f.chain)
                        .padding(.top, 12)
                }
                .textSelection(.enabled)
                .padding(.horizontal, Theme.inset)
                .padding(.bottom, 16)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .onChange(of: f.target) { _, t in
                if let t { withAnimation(.easeInOut(duration: 0.2)) { proxy.scrollTo(t, anchor: .center) } }
            }
            .onChange(of: doc.id) { _, _ in
                proxy.scrollTo(f.target ?? 0, anchor: f.target == nil ? .top : .center)
            }
        }
    }
}

/// A block: headings in the app's type, the document's words with their
/// bold kept, our notes muted, names one a line. A passage the search found
/// is washed, and the one on show also edged.
@MainActor
private struct FoundingBlockView: View {
    let block: FoundingBlock
    let found: Bool
    let current: Bool

    var body: some View {
        content
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(found ? 6 : 0)
            .background(
                RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .fill(found ? Theme.selection.opacity(current ? 1 : 0.45) : .clear)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .strokeBorder(current ? Theme.accent : .clear, lineWidth: 1.5)
            )
            .accessibilityAddTraits(current ? .isSelected : [])
    }

    @ViewBuilder private var content: some View {
        let text = block.text ?? ""
        switch block.kind {
        case "h2":
            Text(text).font(Theme.font(14, .semibold)).foregroundStyle(Theme.ink).padding(.top, 10)
        case "h3":
            Text(text).font(Theme.font(12.5, .semibold)).foregroundStyle(Theme.ink).padding(.top, 4)
        case "h4":
            Text(text).font(Theme.font(12, .semibold)).foregroundStyle(Theme.muted)
        case "note":
            Text(text).font(Theme.font(11.5).italic()).foregroundStyle(Theme.muted)
        case "list":
            Text((block.items ?? []).joined(separator: "\n")).font(Theme.font(12.5)).foregroundStyle(Theme.ink)
        default:
            Text(Self.inline(text)).font(Theme.font(12.5)).foregroundStyle(Theme.ink).lineSpacing(2)
        }
    }

    /// The words with their **bold** drawn, and line breaks kept.
    static func inline(_ s: String) -> AttributedString {
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        return (try? AttributedString(markdown: s, options: options)) ?? AttributedString(s)
    }
}

/// The document's SHA-512, which shows a copy is this one, to copy whole,
/// and how the local chain that keeps every version of the texts checked.
@MainActor
private struct FoundingPrint: View {
    let doc: FoundingDoc
    let chain: FoundingChainInfo?
    @State private var copied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            fingerprint
            if let c = chain {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Image(systemName: c.verified ? "link" : "exclamationmark.triangle")
                        .font(.system(size: 11))
                        .foregroundStyle(c.verified ? Theme.muted : Theme.needs)
                        .accessibilityHidden(true)
                    Text(verbatim: Self.line(c))
                        .font(Theme.font(11))
                        .foregroundStyle(c.verified ? Theme.muted : Theme.needs)
                        .fixedSize(horizontal: false, vertical: true)
                        .help(c.head ?? c.folder)
                }
            }
        }
    }

    /// How the chain checked, in words.
    static func line(_ c: FoundingChainInfo) -> String {
        guard c.verified, let head = c.head else {
            return "The local chain doesn’t check: " + (c.problem ?? "it can’t be read.")
        }
        let versions = c.versions == 1 ? "1 version" : "\(c.versions) versions"
        return "On your local chain: \(versions), each checked; the newest is " + String(head.dropFirst("sha512:".count).prefix(12)) + "."
    }

    private var fingerprint: some View {
        HStack(spacing: 6) {
            Image(systemName: "checkmark.seal")
                .font(.system(size: 11))
                .foregroundStyle(Theme.muted)
                .accessibilityHidden(true)
            Text(verbatim: "SHA-512 " + String(doc.sha512.prefix(16)) + "…")
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(Theme.muted)
                .help(doc.sha512)
            Button(copied ? "Copied" : "Copy") {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(doc.sha512, forType: .string)
                copied = true
            }
            .buttonStyle(.plain)
            .font(Theme.font(11, .semibold))
            .foregroundStyle(Theme.accentInk)
            .help("Copy the whole SHA-512, to check a copy against")
            .accessibilityLabel("Copy the document’s SHA-512")
        }
    }
}
