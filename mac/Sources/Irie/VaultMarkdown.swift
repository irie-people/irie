import Foundation

// A note as Obsidian's Reading view shows it, for the Vault card's
// (VaultReading.swift draws it): its blocks, each with the line it starts
// on, and the words in them made Markdown that Foundation reads, with no
// view, so a harness can check it alone. As Obsidian's help says ("Views
// and editing mode", "Basic formatting syntax", "Callouts", "Embed files",
// "Properties", "Tags", "Internal links").

// MARK: What a note is made of

/// One block of a note, and the line it starts on, from 0, for going to a
/// heading and ticking a task.
struct VaultBlock: Equatable, Sendable {
    var line: Int
    var kind: Kind

    indirect enum Kind: Equatable, Sendable {
        /// The note's properties, from the YAML at its top.
        case properties([VaultProperty])
        case heading(Int, String)
        case paragraph(String)
        case item(VaultItem)
        /// A fenced block: its info string's first word, lower case, and
        /// its lines as written.
        case code(String, String)
        case quote([VaultBlock])
        case callout(VaultCallout)
        case table([[String]])
        case embed(VaultEmbed)
        case rule
    }
}

/// A property: its name and its values, a list's one by one.
struct VaultProperty: Equatable, Sendable {
    var key: String
    var values: [String]
}

/// A list's item, a task's too.
struct VaultItem: Equatable, Sendable {
    /// How deep it sits, from 0.
    var depth: Int
    /// "•", or its number as written, as "3.".
    var marker: String
    /// A task's mark: " " to do, any other character done, as Obsidian
    /// takes any; nil for an item that is no task.
    var task: Character?
    var text: String

    var done: Bool { task.map { $0 != " " } ?? false }
}

/// A callout: > [!type] Title, and what is under it.
struct VaultCallout: Equatable, Sendable {
    /// The type as written, in lower case.
    var type: String
    /// The type it looks like: tldr is an abstract, and a type Obsidian
    /// doesn't know is a note.
    var family: String
    var title: String
    /// "+" open and "-" closed to start with; nil for one that doesn't fold.
    var fold: Character?
    var body: [VaultBlock]
}

/// A file shown in the note: ![[Note]], ![[picture.png|300]], or a
/// Markdown image, ![250](address).
struct VaultEmbed: Equatable, Sendable {
    /// What it names, as a link's words: a note or file, and a heading,
    /// block or page in it.
    var link: String
    /// Words to show for it, when it has some that aren't a size.
    var shown: String?
    var width: Int?
    var height: Int?
    /// A Markdown image rather than a [[link]]: its address is a path from
    /// the note, or a page's.
    var markdown = false
}

enum VaultMarkdown {
    // MARK: Blocks

    /// The note's blocks, as Reading view shows them.
    static func parse(_ source: String) -> [VaultBlock] {
        var lines = source.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n")
        if let first = lines.first, first.hasPrefix("\u{feff}") { lines[0] = String(first.dropFirst()) }
        var out: [VaultBlock] = []
        var from = 0
        if let p = properties(lines) {
            if !p.list.isEmpty { out.append(VaultBlock(line: 0, kind: .properties(p.list))) }
            from = p.end + 1
        }
        out += blocks(visible(lines, from: from))
        return finish(out)
    }

    /// The properties at a note's top, between its first line's --- and
    /// the next, and the line that closes them; nil for a note with none.
    static func properties(_ lines: [String]) -> (list: [VaultProperty], end: Int)? {
        guard let first = lines.first, first.trimmingCharacters(in: .whitespaces) == "---" else { return nil }
        guard let end = lines.indices.dropFirst().first(where: { lines[$0].trimmingCharacters(in: .whitespaces) == "---" }) else { return nil }
        var list: [VaultProperty] = []
        for raw in lines[1..<end] {
            let line = raw.trimmingCharacters(in: .whitespaces)
            if line.isEmpty || line.hasPrefix("#") { continue }
            if line.hasPrefix("- "), raw.first == " " || raw.first == "\t" || raw.first == "-", !list.isEmpty {
                list[list.count - 1].values.append(unquote(String(line.dropFirst(2))))
                continue
            }
            guard raw.first != " ", raw.first != "\t", let colon = line.firstIndex(of: ":") else { continue }
            let key = unquote(String(line[..<colon]))
            var value = line[line.index(after: colon)...].trimmingCharacters(in: .whitespaces)
            var values: [String] = []
            if value.hasPrefix("["), value.hasSuffix("]"), !value.hasPrefix("[[") {
                value = String(value.dropFirst().dropLast())
                values = value.split(separator: ",").map { unquote(String($0)) }.filter { !$0.isEmpty }
            } else if !value.isEmpty {
                values = [unquote(value)]
            }
            list.append(VaultProperty(key: key, values: values))
        }
        return (list, end)
    }

    private static func unquote(_ s: String) -> String {
        let t = s.trimmingCharacters(in: .whitespaces)
        if t.count >= 2, let f = t.first, f == t.last, f == "\"" || f == "'" { return String(t.dropFirst().dropLast()) }
        return t
    }

    /// The lines Reading view shows, by their number: %%comments%% left
    /// out, on a line or across lines, and a block's ^id at a line's end,
    /// but nothing in a fenced block changed. A line a comment or an ^id
    /// fills is left out altogether.
    static func visible(_ lines: [String], from: Int) -> [(Int, String)] {
        var out: [(Int, String)] = []
        var comment = false
        var fence: String?
        for i in lines.indices where i >= from {
            let line = lines[i]
            if let f = fence {
                out.append((i, line))
                if closes(line, f) { fence = nil }
                continue
            }
            if !comment, let f = opens(line) {
                fence = f
                out.append((i, line))
                continue
            }
            var kept = ""
            var rest = Substring(line)
            var touched = comment
            while true {
                guard let r = rest.range(of: "%%") else {
                    if !comment { kept += rest }
                    break
                }
                if !comment { kept += rest[..<r.lowerBound] }
                comment.toggle()
                touched = true
                rest = rest[r.upperBound...]
            }
            if touched, kept.trimmingCharacters(in: .whitespaces).isEmpty { continue }
            let shown = withoutBlockID(kept)
            if shown.count != kept.count, shown.trimmingCharacters(in: .whitespaces).isEmpty { continue }
            out.append((i, shown))
        }
        return out
    }

    /// A line without the ^id at its end that names it as a block.
    static func withoutBlockID(_ line: String) -> String {
        var end = line.endIndex
        while end > line.startIndex, line[line.index(before: end)] == " " || line[line.index(before: end)] == "\t" {
            end = line.index(before: end)
        }
        var i = end
        while i > line.startIndex, isBlockIDCharacter(line[line.index(before: i)]) { i = line.index(before: i) }
        guard i < end, i > line.startIndex, line[line.index(before: i)] == "^" else { return line }
        let caret = line.index(before: i)
        guard caret == line.startIndex || line[line.index(before: caret)] == " " || line[line.index(before: caret)] == "\t" else { return line }
        var cut = caret
        while cut > line.startIndex, line[line.index(before: cut)] == " " || line[line.index(before: cut)] == "\t" { cut = line.index(before: cut) }
        return String(line[..<cut])
    }

    private static func isBlockIDCharacter(_ c: Character) -> Bool {
        c == "-" || (c.isASCII && (c.isLetter || c.isNumber))
    }

    /// A fence a line opens: its marker, as ``` or ~~~~.
    static func opens(_ line: String) -> String? {
        let t = line.trimmingCharacters(in: .whitespaces)
        guard let c = t.first, c == "`" || c == "~" else { return nil }
        let marker = t.prefix { $0 == c }
        guard marker.count >= 3 else { return nil }
        // A ``` fence's info string has no backtick.
        if c == "`", t.dropFirst(marker.count).contains("`") { return nil }
        return String(marker)
    }

    /// Whether a line closes the fence a marker opened.
    static func closes(_ line: String, _ marker: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        guard let c = marker.first, t.count >= marker.count else { return false }
        return t.allSatisfy { $0 == c }
    }

    /// The lines' blocks, their words still as written.
    static func blocks(_ lines: [(Int, String)]) -> [VaultBlock] {
        var out: [VaultBlock] = []
        var para: [String] = []
        var paraLine = 0
        // The indents of the lists open, the outermost first.
        var indents: [Int] = []
        // The item a line with no marker under it goes on.
        var openItem = false

        func flush() {
            if !para.isEmpty { out.append(VaultBlock(line: paraLine, kind: .paragraph(para.joined(separator: "\n")))) }
            para = []
        }
        func block(_ line: Int, _ kind: VaultBlock.Kind) {
            flush()
            indents = []
            openItem = false
            out.append(VaultBlock(line: line, kind: kind))
        }

        var i = 0
        while i < lines.count {
            let (n, line) = lines[i]
            let t = line.trimmingCharacters(in: .whitespaces)
            if let f = opens(line) {
                var body: [String] = []
                var j = i + 1
                while j < lines.count, !closes(lines[j].1, f) {
                    body.append(lines[j].1)
                    j += 1
                }
                let info = t.dropFirst(f.count).trimmingCharacters(in: .whitespaces).lowercased()
                block(n, .code(info.split(separator: " ").first.map(String.init) ?? "", body.joined(separator: "\n")))
                i = j + 1
                continue
            }
            if t.isEmpty {
                flush()
                openItem = false
                i += 1
                continue
            }
            if !para.isEmpty, !t.isEmpty, t.allSatisfy({ $0 == "=" }) || t.allSatisfy({ $0 == "-" }) {
                let level = t.first == "=" ? 1 : 2
                let words = para.joined(separator: "\n")
                para = []
                block(paraLine, .heading(level, words))
                i += 1
                continue
            }
            if let h = heading(t) {
                block(n, .heading(h.level, h.text))
                i += 1
                continue
            }
            if isRule(t) {
                block(n, .rule)
                i += 1
                continue
            }
            if t.hasPrefix(">") {
                var inner: [(Int, String)] = []
                var j = i
                while j < lines.count {
                    let q = lines[j].1.trimmingCharacters(in: .whitespaces)
                    guard q.hasPrefix(">") else { break }
                    var s = q.dropFirst()
                    if s.first == " " { s = s.dropFirst() }
                    inner.append((lines[j].0, String(s)))
                    j += 1
                }
                block(n, quote(inner))
                i = j
                continue
            }
            if let e = embed(t) {
                block(n, .embed(e))
                i += 1
                continue
            }
            if i + 1 < lines.count, t.contains("|"), isTableRule(lines[i + 1].1.trimmingCharacters(in: .whitespaces)) {
                var rows = [cells(t)]
                var j = i + 2
                while j < lines.count {
                    let r = lines[j].1.trimmingCharacters(in: .whitespaces)
                    guard !r.isEmpty, r.contains("|") else { break }
                    rows.append(cells(r))
                    j += 1
                }
                block(n, .table(rows))
                i = j
                continue
            }
            if let it = item(line) {
                flush()
                while let last = indents.last, last > it.indent { indents.removeLast() }
                if indents.last.map({ $0 < it.indent }) ?? true { indents.append(it.indent) }
                out.append(VaultBlock(line: n, kind: .item(VaultItem(depth: indents.count - 1, marker: it.marker, task: it.task, text: it.text))))
                openItem = true
                i += 1
                continue
            }
            // A line under an item, with no marker, goes on with it.
            if openItem, para.isEmpty, var last = out.last, case var .item(it) = last.kind {
                it.text += "\n" + t
                last.kind = .item(it)
                out[out.count - 1] = last
                i += 1
                continue
            }
            if para.isEmpty {
                paraLine = n
                indents = []
                openItem = false
            }
            para.append(line.trimmingCharacters(in: .whitespaces))
            i += 1
        }
        flush()
        return out
    }

    /// A quote's lines, the > taken off: a callout when its first says so.
    static func quote(_ inner: [(Int, String)]) -> VaultBlock.Kind {
        if let first = inner.first, let head = calloutHead(first.1) {
            return .callout(VaultCallout(type: head.type, family: family(head.type), title: head.title ?? titleCase(head.type),
                                         fold: head.fold, body: blocks(Array(inner.dropFirst()))))
        }
        return .quote(blocks(inner))
    }

    /// A callout's first line: [!type], a + or - to fold it, and a title.
    static func calloutHead(_ line: String) -> (type: String, fold: Character?, title: String?)? {
        let t = line.trimmingCharacters(in: .whitespaces)
        guard t.hasPrefix("[!"), let close = t.firstIndex(of: "]") else { return nil }
        let type = t[t.index(t.startIndex, offsetBy: 2)..<close].lowercased()
        guard !type.isEmpty, !type.contains(" ") else { return nil }
        var rest = t[t.index(after: close)...]
        var fold: Character?
        if let f = rest.first, f == "+" || f == "-" {
            fold = f
            rest = rest.dropFirst()
        }
        let title = rest.trimmingCharacters(in: .whitespaces)
        return (type, fold, title.isEmpty ? nil : title)
    }

    /// The type a callout's looks like, as Obsidian's help lists them and
    /// their other names; one it doesn't list is a note.
    static func family(_ type: String) -> String {
        switch type {
        case "abstract", "summary", "tldr": return "abstract"
        case "info": return "info"
        case "todo": return "todo"
        case "tip", "hint", "important": return "tip"
        case "success", "check", "done": return "success"
        case "question", "help", "faq": return "question"
        case "warning", "caution", "attention": return "warning"
        case "failure", "fail", "missing": return "failure"
        case "danger", "error": return "danger"
        case "bug": return "bug"
        case "example": return "example"
        case "quote", "cite": return "quote"
        default: return "note"
        }
    }

    /// A callout's title when it has none of its own: its type in title
    /// case, as faq's is Faq.
    static func titleCase(_ type: String) -> String {
        type.prefix(1).uppercased() + type.dropFirst()
    }

    private static func heading(_ s: String) -> (level: Int, text: String)? {
        let hashes = s.prefix { $0 == "#" }.count
        guard (1...6).contains(hashes) else { return nil }
        let rest = s.dropFirst(hashes)
        guard rest.isEmpty || rest.first == " " || rest.first == "\t" else { return nil }
        var text = rest.trimmingCharacters(in: .whitespaces)
        // A closing run of #s, after a space, is no part of it.
        let closing = text.reversed().prefix { $0 == "#" }.count
        if closing > 0 {
            let cut = text.dropLast(closing)
            if cut.isEmpty || cut.last == " " { text = cut.trimmingCharacters(in: .whitespaces) }
        }
        return (hashes, text)
    }

    private static func isRule(_ s: String) -> Bool {
        let chars = s.filter { $0 != " " && $0 != "\t" }
        guard chars.count >= 3, let c = chars.first, "-*_".contains(c) else { return false }
        return chars.allSatisfy { $0 == c }
    }

    private static func isTableRule(_ s: String) -> Bool {
        s.contains("-") && s.allSatisfy { "|-: \t".contains($0) } && (s.contains("|") || s.contains(":"))
    }

    /// A table row's cells: split at each | not after a backslash, and a
    /// \| a | again, as a [[link|words]] in a table is written.
    static func cells(_ s: String) -> [String] {
        var t = Substring(s)
        if t.hasPrefix("|") { t = t.dropFirst() }
        if t.hasSuffix("|"), !t.hasSuffix("\\|") { t = t.dropLast() }
        var out: [String] = []
        var cell = ""
        var escaped = false
        for c in t {
            if escaped {
                cell += c == "|" ? "|" : "\\" + String(c)
                escaped = false
            } else if c == "\\" {
                escaped = true
            } else if c == "|" {
                out.append(cell.trimmingCharacters(in: .whitespaces))
                cell = ""
            } else {
                cell.append(c)
            }
        }
        if escaped { cell += "\\" }
        out.append(cell.trimmingCharacters(in: .whitespaces))
        return out
    }

    /// A line that is one embed and nothing else.
    static func embed(_ t: String) -> VaultEmbed? {
        if t.hasPrefix("![["), t.hasSuffix("]]"), t.count > 5 {
            let inner = String(t.dropFirst(3).dropLast(2))
            guard !inner.contains("[["), !inner.contains("]]") else { return nil }
            var parts = inner.components(separatedBy: "|")
            var e = VaultEmbed(link: parts.removeFirst().trimmingCharacters(in: .whitespaces))
            for p in parts { sized(p.trimmingCharacters(in: .whitespaces), into: &e) }
            return e.link.isEmpty ? nil : e
        }
        // ![words](address): the address up to the first space, a title
        // after it left out, <> round it taken off.
        if t.hasPrefix("!["), t.hasSuffix(")"), let mid = t.range(of: "](") {
            let alt = String(t[t.index(t.startIndex, offsetBy: 2)..<mid.lowerBound])
            guard !alt.contains("]") else { return nil }
            var address = String(t[mid.upperBound..<t.index(before: t.endIndex)]).trimmingCharacters(in: .whitespaces)
            if address.hasPrefix("<"), let close = address.firstIndex(of: ">") {
                address = String(address[address.index(after: address.startIndex)..<close])
            } else {
                guard !address.contains(")") else { return nil }
                if let space = address.firstIndex(of: " ") { address = String(address[..<space]) }
            }
            guard !address.isEmpty else { return nil }
            var e = VaultEmbed(link: address, markdown: true)
            sized(alt, into: &e)
            return e
        }
        return nil
    }

    /// An embed's words after a |: a width, a width x height, or words to
    /// show.
    private static func sized(_ p: String, into e: inout VaultEmbed) {
        let parts = p.split(separator: "x", omittingEmptySubsequences: false)
        if !p.isEmpty, p.allSatisfy(\.isNumber) {
            e.width = Int(p)
        } else if parts.count == 2, !parts[0].isEmpty, !parts[1].isEmpty, parts.allSatisfy({ $0.allSatisfy(\.isNumber) }) {
            e.width = Int(parts[0])
            e.height = Int(parts[1])
        } else if !p.isEmpty, e.shown == nil {
            e.shown = p
        }
    }

    /// A list item's indent, in columns with a tab to the next four, its
    /// marker, its task's mark if it is one, and its words.
    static func item(_ line: String) -> (indent: Int, marker: String, task: Character?, text: String)? {
        var indent = 0
        var i = line.startIndex
        while i < line.endIndex, line[i] == " " || line[i] == "\t" {
            indent += line[i] == "\t" ? 4 - indent % 4 : 1
            i = line.index(after: i)
        }
        let rest = line[i...]
        var marker: String
        var after: Substring
        if let c = rest.first, "-*+".contains(c) {
            marker = "•"
            after = rest.dropFirst()
        } else {
            let digits = rest.prefix { $0.isASCII && $0.isNumber }
            let tail = rest.dropFirst(digits.count)
            guard (1...9).contains(digits.count), let p = tail.first, p == "." || p == ")" else { return nil }
            marker = digits + "."
            after = tail.dropFirst()
        }
        guard after.isEmpty || after.first == " " || after.first == "\t" else { return nil }
        var text = after.drop { $0 == " " || $0 == "\t" }
        var task: Character?
        let chars = Array(text.prefix(4))
        if chars.count >= 3, chars[0] == "[", chars[2] == "]", chars[1] != "]", chars.count == 3 || chars[3] == " " || chars[3] == "\t" {
            task = chars[1]
            text = text.dropFirst(3).drop { $0 == " " || $0 == "\t" }
        }
        return (indent, marker, task, String(text).trimmingCharacters(in: .whitespaces))
    }

    /// Every block's words made Markdown Foundation reads.
    static func finish(_ blocks: [VaultBlock]) -> [VaultBlock] {
        blocks.map { b in
            var b = b
            switch b.kind {
            case let .properties(list):
                b.kind = .properties(list.map { VaultProperty(key: $0.key, values: $0.values.map(inline)) })
            case let .heading(level, text):
                b.kind = .heading(level, inline(text))
            case let .paragraph(text):
                b.kind = .paragraph(inline(text))
            case var .item(it):
                it.text = inline(it.text)
                b.kind = .item(it)
            case let .quote(inner):
                b.kind = .quote(finish(inner))
            case var .callout(c):
                c.title = inline(c.title)
                c.body = finish(c.body)
                b.kind = .callout(c)
            case let .table(rows):
                b.kind = .table(rows.map { $0.map(inline) })
            case .code, .embed, .rule:
                break
            }
            return b
        }
    }

    // MARK: Words

    /// Where a highlight starts: the first of seven, the theme's colour
    /// and then red, orange, yellow, green, blue and purple, as a colour
    /// emoji after == names them.
    static let highlightStart: [Character] = ["\u{E010}", "\u{E011}", "\u{E012}", "\u{E013}", "\u{E014}", "\u{E015}", "\u{E016}"]
    static let highlightEnd: Character = "\u{E001}"
    static let tagStart: Character = "\u{E002}"
    static let tagEnd: Character = "\u{E003}"
    /// A [[link]]'s address in the Markdown made from it.
    static let noteScheme = "vault-note"

    private static let colours: [Character: Int] = ["🔴": 1, "🟠": 2, "🟡": 3, "🟢": 4, "🔵": 5, "🟣": 6]

    /// A block's words made Markdown Foundation reads, its own marks kept:
    /// a [[link]] a link to vault-note:, a #tag and an ==highlight== marked
    /// with characters of their own for the view to colour, a page's
    /// address a link, and an ![[embed]] in a line of words a link to it.
    /// Code in backticks stays as written.
    static func inline(_ s: String) -> String {
        let c = Array(s)
        var out = ""
        var i = 0
        var highlight = false

        func at(_ k: Int, _ p: String) -> Bool {
            var k = k
            for ch in p {
                guard k < c.count, c[k] == ch else { return false }
                k += 1
            }
            return true
        }
        func find(_ p: String, from: Int) -> Int? {
            var k = from
            while k < c.count {
                if at(k, p) { return k }
                k += 1
            }
            return nil
        }
        func wordStart(_ k: Int) -> Bool { k == 0 || c[k - 1].isWhitespace }

        while i < c.count {
            let ch = c[i]
            if ch == "`" {
                var n = 0
                while i + n < c.count, c[i + n] == "`" { n += 1 }
                var k = i + n
                var close: Int?
                while k < c.count {
                    if c[k] == "`" {
                        var m = 0
                        while k + m < c.count, c[k + m] == "`" { m += 1 }
                        if m == n {
                            close = k
                            break
                        }
                        k += m
                    } else {
                        k += 1
                    }
                }
                if let close {
                    out += String(c[i..<close + n])
                    i = close + n
                } else {
                    out += String(c[i..<i + n])
                    i += n
                }
                continue
            }
            if ch == "\\", i + 1 < c.count {
                out.append(ch)
                out.append(c[i + 1])
                i += 2
                continue
            }
            if ch == "!", at(i + 1, "[["), let end = find("]]", from: i + 3) {
                let inner = String(c[(i + 3)..<end])
                var parts = inner.components(separatedBy: "|")
                let link = parts.removeFirst().trimmingCharacters(in: .whitespaces)
                let shown = parts.first { p in !p.isEmpty && !p.allSatisfy { $0.isNumber || $0 == "x" } }
                out += "[⧉ " + escaped(shown ?? link) + "](" + noteScheme + ":" + encoded(link) + ")"
                i = end + 2
                continue
            }
            if ch == "[", at(i + 1, "["), let end = find("]]", from: i + 2) {
                let inner = String(c[(i + 2)..<end])
                var link = inner
                var shown: String?
                if let bar = inner.firstIndex(of: "|") {
                    link = String(inner[..<bar])
                    shown = String(inner[inner.index(after: bar)...]).trimmingCharacters(in: .whitespaces)
                }
                link = link.trimmingCharacters(in: .whitespaces)
                out += "[" + escaped(shown.flatMap { $0.isEmpty ? nil : $0 } ?? display(link)) + "](" + noteScheme + ":" + encoded(link) + ")"
                i = end + 2
                continue
            }
            // A Markdown link's address stays as written.
            if ch == "]", at(i + 1, "(") {
                var depth = 0
                var k = i + 1
                var end: Int?
                while k < c.count {
                    if c[k] == "\\" {
                        k += 2
                        continue
                    }
                    if c[k] == "(" { depth += 1 }
                    if c[k] == ")" {
                        depth -= 1
                        if depth == 0 {
                            end = k
                            break
                        }
                    }
                    k += 1
                }
                if let end {
                    out += String(c[i...end])
                    i = end + 1
                    continue
                }
            }
            if ch == "<", let end = find(">", from: i + 1), end > i + 1 {
                let inside = String(c[(i + 1)..<end])
                if inside.contains("://") || inside.contains("@"), !inside.contains(" ") {
                    out += String(c[i...end])
                    i = end + 1
                    continue
                }
            }
            if ch == "=", at(i + 1, "=") {
                if highlight {
                    out.append(highlightEnd)
                    highlight = false
                    i += 2
                    continue
                }
                var k = i + 2
                if k < c.count, !c[k].isWhitespace, let close = find("==", from: k + 1), !c[close - 1].isWhitespace {
                    var colour = 0
                    if let n = colours[c[k]] {
                        colour = n
                        k += 1
                    }
                    if k < close {
                        out.append(highlightStart[colour])
                        highlight = true
                        i = k
                        continue
                    }
                }
            }
            if ch == "#", wordStart(i) {
                var k = i + 1
                while k < c.count, c[k].isLetter || c[k].isNumber || c[k] == "_" || c[k] == "-" || c[k] == "/" { k += 1 }
                let tag = c[(i + 1)..<k]
                if !tag.isEmpty, tag.contains(where: { !$0.isNumber }) {
                    out.append(tagStart)
                    out += escaped("#" + String(tag))
                    out.append(tagEnd)
                    i = k
                    continue
                }
            }
            if ch == "h", wordStart(i) || (i > 0 && c[i - 1] == "("), at(i, "https://") || at(i, "http://") {
                var k = i
                while k < c.count, !c[k].isWhitespace, c[k] != "<", c[k] != ">", c[k] != "[", c[k] != "]" { k += 1 }
                // The words' own stop after an address is no part of it.
                while k > i, ".,;:!?'\"".contains(c[k - 1]) || (c[k - 1] == ")" && !c[i..<k].contains("(")) { k -= 1 }
                let address = String(c[i..<k])
                if address.count > 8 {
                    out += "[" + escaped(address) + "](" + address.replacingOccurrences(of: "(", with: "%28").replacingOccurrences(of: ")", with: "%29") + ")"
                    i = k
                    continue
                }
            }
            if ch == "<", at(i, "<br>") || at(i, "<br/>") || at(i, "<br />") {
                out += "\n"
                i = find(">", from: i)! + 1
                continue
            }
            out.append(ch)
            i += 1
        }
        if highlight { out.append(highlightEnd) }
        return out
    }

    /// The words a [[link]] shows without words of its own: the note as
    /// written, then > and its heading or block.
    static func display(_ link: String) -> String {
        let t = VaultLinks.target(link)
        let anchor = t.heading ?? t.block.map { "^" + $0 }
        guard let anchor else { return t.name }
        return t.name.isEmpty ? anchor : t.name + " > " + anchor
    }

    /// Words a link shows, with every ASCII mark Markdown might read
    /// escaped.
    static func escaped(_ s: String) -> String {
        var out = ""
        for ch in s {
            if ch.isASCII, ch.isPunctuation || ch.isSymbol { out.append("\\") }
            out.append(ch)
        }
        return out
    }

    /// A link's words as an address's: only ASCII letters, digits and
    /// -._~ as they are.
    static func encoded(_ s: String) -> String {
        s.addingPercentEncoding(withAllowedCharacters: plain) ?? s
    }

    private static let plain = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~")

    // MARK: Tasks

    /// Where a task's mark is in its line, in UTF-16 units, and whether it
    /// is done: after any > of a quote or callout, the list's marker and a
    /// space, between [ and ].
    static func checkbox(in line: String) -> (range: NSRange, done: Bool)? {
        let u = Array(line.utf16)
        var k = 0
        func spaces() { while k < u.count, u[k] == 32 || u[k] == 9 { k += 1 } }
        spaces()
        while k < u.count, u[k] == 62 { // >
            k += 1
            spaces()
        }
        if k < u.count, u[k] == 45 || u[k] == 42 || u[k] == 43 { // - * +
            k += 1
        } else {
            let start = k
            while k < u.count, (48...57).contains(u[k]) { k += 1 }
            guard k > start, k - start <= 9, k < u.count, u[k] == 46 || u[k] == 41 else { return nil } // . )
            k += 1
        }
        guard k < u.count, u[k] == 32 || u[k] == 9 else { return nil }
        spaces()
        guard k + 1 < u.count, u[k] == 91, u[k + 1] != 93 else { return nil } // [, and no ] at once
        let mark = (line as NSString).rangeOfComposedCharacterSequence(at: k + 1)
        guard NSMaxRange(mark) < u.count, u[NSMaxRange(mark)] == 93 else { return nil } // ]
        let after = NSMaxRange(mark) + 1
        guard after == u.count || u[after] == 32 || u[after] == 9 else { return nil }
        return (mark, !(mark.length == 1 && u[k + 1] == 32))
    }

    // MARK: Embeds

    /// What an embed of a note shows of it: the section under a heading,
    /// to the next as high, the block an ^id names, or the whole note
    /// without its properties. nil when the heading or block isn't there.
    static func section(_ t: VaultLinks.Target, of text: String) -> String? {
        var lines = text.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n")
        if let p = properties(lines) { lines = Array(lines[(p.end + 1)...]) }
        if let h = t.heading?.lowercased() {
            var fence: String?
            var start: Int?
            var level = 0
            for (i, l) in lines.enumerated() {
                if let f = fence {
                    if closes(l, f) { fence = nil }
                    continue
                }
                if let f = opens(l) {
                    fence = f
                    continue
                }
                guard let head = heading(l.trimmingCharacters(in: .whitespaces)) else { continue }
                if let s = start {
                    if head.level <= level { return lines[s..<i].joined(separator: "\n") }
                } else if head.text.lowercased() == h {
                    start = i
                    level = head.level
                }
            }
            return start.map { lines[$0...].joined(separator: "\n") }
        }
        if let b = t.block {
            guard let i = lines.firstIndex(where: { l in
                let s = l.trimmingCharacters(in: .whitespaces)
                return s.hasSuffix("^" + b) && withoutBlockID(s) != s
            }) else { return nil }
            let alone = withoutBlockID(lines[i]).trimmingCharacters(in: .whitespaces).isEmpty
            if item(lines[i]) != nil, !alone { return lines[i] }
            // A paragraph's lines, or the block before an ^id alone on its
            // line: back to the blank line before.
            var s = alone ? i - 1 : i
            while s > 0, s - 1 < lines.count, !lines[s - 1].trimmingCharacters(in: .whitespaces).isEmpty { s -= 1 }
            return lines[max(0, s)...i].joined(separator: "\n")
        }
        return lines.joined(separator: "\n")
    }

    /// The kinds of file an embed shows, by extension, as Obsidian's help
    /// lists its accepted file formats.
    enum Media: Equatable, Sendable {
        case note, image, audio, video, pdf, other
    }

    static func media(_ path: String) -> Media {
        switch (path as NSString).pathExtension.lowercased() {
        case "md": return .note
        case "avif", "bmp", "gif", "jpeg", "jpg", "png", "svg", "webp": return .image
        case "flac", "m4a", "mp3", "ogg", "wav", "3gp": return .audio
        case "mkv", "mov", "mp4", "ogv", "webm": return .video
        case "pdf": return .pdf
        default: return .other
        }
    }
}
