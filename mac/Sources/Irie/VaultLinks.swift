import Foundation

// The Obsidian vault's links, for its cards (VaultPane.swift, Vault.swift):
// what a link names and which note or file that is, as Obsidian's help
// says ("Internal links"), with no view or store, so a harness can check
// it alone.

// MARK: The notes and their links

/// The vault's notes and how they link: Obsidian's [[links]], ![[embeds]]
/// and Markdown links, found as Obsidian finds them ("Internal links"):
/// a path from the vault's folder, with or without .md, or a note's name,
/// the one beside the note linking from first, then the one nearest the
/// vault's top.
enum VaultLinks {
    /// What a link names: the note or file, and a heading or block in it.
    struct Target: Equatable {
        var name: String
        var heading: String?
        var block: String?
    }

    /// The notes are its Markdown files outside the folders whose names
    /// start with a dot, as Obsidian's own.
    static func isNote(_ relative: String) -> Bool {
        relative.lowercased().hasSuffix(".md") && !relative.split(separator: "/").contains { $0.hasPrefix(".") }
    }

    /// Any file of the vault a link may name.
    static func isFile(_ relative: String) -> Bool {
        !relative.split(separator: "/").contains { $0.hasPrefix(".") }
    }

    /// A link's words cut into what it names: [[Note#Heading|shown]],
    /// [[Note#^block]], or a Markdown link's address, its %20s and all.
    static func target(_ link: String) -> Target {
        var s = link
        if let bar = s.firstIndex(of: "|") { s = String(s[..<bar]) }
        s = s.removingPercentEncoding ?? s
        var t = Target(name: s.trimmingCharacters(in: .whitespaces))
        if let hash = s.firstIndex(of: "#") {
            t.name = String(s[..<hash]).trimmingCharacters(in: .whitespaces)
            let anchor = String(s[s.index(after: hash)...]).trimmingCharacters(in: .whitespaces)
            if anchor.hasPrefix("^") {
                t.block = String(anchor.dropFirst())
            } else if !anchor.isEmpty {
                // Nested headings name the last.
                t.heading = anchor.split(separator: "#").last.map(String.init)
            }
        }
        return t
    }

    /// The file a link names, by its path from the vault's folder; nil
    /// when none is there. from is the note it links from.
    static func resolve(_ name: String, from: String?, files: [String]) -> String? {
        let want = name.hasPrefix("/") ? String(name.dropFirst()) : name
        guard !want.isEmpty else { return from }
        let low = want.lowercased()
        let withMD = low.hasSuffix(".md") ? low : low + ".md"
        // A path from the vault's folder.
        if want.contains("/") {
            return files.first { $0.lowercased() == withMD } ?? files.first { $0.lowercased() == low }
        }
        // A name: the note's, or another file's with its extension.
        let named = files.filter { f in
            let base = (f as NSString).lastPathComponent.lowercased()
            return base == withMD || base == low
        }
        guard !named.isEmpty else { return nil }
        let here = from.map { ($0 as NSString).deletingLastPathComponent }
        return named.min { a, b in
            let ah = (a as NSString).deletingLastPathComponent == here, bh = (b as NSString).deletingLastPathComponent == here
            if ah != bh { return ah }
            let ad = a.split(separator: "/").count, bd = b.split(separator: "/").count
            if ad != bd { return ad < bd }
            if a.lowercased().hasSuffix(".md") != b.lowercased().hasSuffix(".md") { return a.lowercased().hasSuffix(".md") }
            return a.localizedStandardCompare(b) == .orderedAscending
        }
    }

    /// Where a new note a link names goes, as Obsidian makes one: at the
    /// folder its path names, else at the vault's top.
    static func newNote(_ name: String) -> String? {
        var p = name.hasPrefix("/") ? String(name.dropFirst()) : name
        p = p.trimmingCharacters(in: .whitespaces)
        guard !p.isEmpty, !p.split(separator: "/").contains(where: { $0 == ".." || $0.hasPrefix(".") }) else { return nil }
        if !p.lowercased().hasSuffix(".md") { p += ".md" }
        return p
    }

    /// The link around a place in a text: a [[link]] or ![[embed]]'s
    /// words, a Markdown link's address, or an https address.
    static func link(in text: NSString, at offset: Int) -> String? {
        guard offset >= 0, offset <= text.length else { return nil }
        let line = text.lineRange(for: NSRange(location: min(offset, max(0, text.length - 1)), length: 0))
        let s = text.substring(with: line)
        let at = offset - line.location
        let ns = s as NSString
        for (pattern, group) in [(#"!?\[\[([^\]\n]+)\]\]"#, 1), (#"\[[^\]\n]*\]\(([^)\s]+)\)"#, 1), (#"https?://[^\s<>"\)\]]+"#, 0)] {
            guard let re = try? NSRegularExpression(pattern: pattern) else { continue }
            for m in re.matches(in: s, range: NSRange(location: 0, length: ns.length)) where NSLocationInRange(at, m.range) || at == NSMaxRange(m.range) && at > m.range.location {
                let found = ns.substring(with: m.range(at: group))
                // A page's address ends before the full stop or comma after it.
                return group == 0 ? String(found.reversed().drop { ".,;:!?".contains($0) }.reversed()) : found
            }
        }
        return nil
    }

    /// The links a note's text makes, by what each names.
    static func links(in text: String) -> [String] {
        var out: [String] = []
        let ns = text as NSString
        for (pattern, md) in [(#"!?\[\[([^\]\n|#]*)(?:[#|][^\]\n]*)?\]\]"#, false), (#"\[[^\]\n]*\]\(([^)\s]+)\)"#, true)] {
            guard let re = try? NSRegularExpression(pattern: pattern) else { continue }
            for m in re.matches(in: text, range: NSRange(location: 0, length: ns.length)) {
                var t = ns.substring(with: m.range(at: 1))
                if md {
                    if t.contains("://") || t.hasPrefix("mailto:") { continue }
                    t = target(t).name
                }
                if !t.isEmpty { out.append(t) }
            }
        }
        return out
    }

    /// The notes that link to note, each by its path from the vault's
    /// folder; read from the disk, at most 2 MB of each.
    static func backlinks(to note: String, vault: String, files: [String]) -> [String] {
        let notes = files.filter(isNote)
        let name = ((note as NSString).lastPathComponent as NSString).deletingPathExtension.lowercased()
        var out: [String] = []
        for n in notes where n != note {
            let url = URL(fileURLWithPath: (vault as NSString).appendingPathComponent(n))
            guard let size = (try? url.resourceValues(forKeys: [.fileSizeKey]))?.fileSize, size <= 2 << 20,
                  let text = try? String(contentsOf: url, encoding: .utf8), text.lowercased().contains(name) else { continue }
            if links(in: text).contains(where: { resolve($0, from: n, files: files) == note }) {
                out.append(n)
            }
        }
        return out.sorted { $0.localizedStandardCompare($1) == .orderedAscending }
    }

    /// The line a heading or block of a note is on, 1-based.
    static func line(of t: Target, in text: String) -> Int? {
        let lines = text.components(separatedBy: "\n")
        for (i, l) in lines.enumerated() {
            if let b = t.block, l.trimmingCharacters(in: .whitespaces).hasSuffix("^" + b) { return i + 1 }
            if let h = t.heading?.lowercased(), l.hasPrefix("#") {
                let words = l.drop { $0 == "#" }.trimmingCharacters(in: .whitespaces).lowercased()
                if words == h { return i + 1 }
            }
        }
        return nil
    }
}
