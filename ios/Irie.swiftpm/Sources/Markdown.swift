import SwiftUI

// A small Markdown renderer for the agent's replies, as the web app's
// js/md.js reads them: paragraphs, headings, fenced code, lists, quotes,
// rules and simple tables are blocks; bold, italic, strikethrough, code and
// links within a line are Foundation's inline Markdown. Only http(s) links
// stay links.

enum MD {
    enum Align { case left, center, right }

    struct ListItem: Hashable {
        var depth: Int
        var ordered: Bool
        var number: Int
        var text: String
        /// nil for a plain item, false for [ ] and true for [x].
        var task: Bool?
    }

    indirect enum Block: Hashable {
        case paragraph(String)
        /// 1 for # and ##, then 2 and 3: a phone has little room.
        case heading(Int, String)
        case code(String, String)
        case quote([Block])
        case list([ListItem])
        case rule
        case table([String], [Align], [[String]])
    }

    private static func re(_ p: String) -> NSRegularExpression { try! NSRegularExpression(pattern: p) }
    private static let fence = re(#"^ {0,3}(`{3,}|~{3,})\s*([^`\s]*)"#)
    private static let heading = re(#"^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$"#)
    private static let rule = re(#"^ {0,3}([-*_])(?:\s*\1){2,}\s*$"#)
    private static let quote = re(#"^ {0,3}>"#)
    private static let item = re(#"^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$"#)
    private static let tableRule = re(#"^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?\s*$"#)
    private static let indented = re(#"^\s{2,}\S"#)
    private static let taskBox = re(#"^\[( |x|X)\]\s+(.*)$"#)

    private static func groups(_ r: NSRegularExpression, _ s: String) -> [String]? {
        let ns = s as NSString
        guard let m = r.firstMatch(in: s, range: NSRange(location: 0, length: ns.length)) else { return nil }
        return (0..<m.numberOfRanges).map { i in
            let range = m.range(at: i)
            return range.location == NSNotFound ? "" : ns.substring(with: range)
        }
    }

    private static func test(_ r: NSRegularExpression, _ s: String) -> Bool { groups(r, s) != nil }

    static func parse(_ source: String) -> [Block] {
        let text = source.replacingOccurrences(of: "\r\n", with: "\n").replacingOccurrences(of: "\r", with: "\n")
        return blocks(text.components(separatedBy: "\n"))
    }

    private static func blocks(_ lines: [String]) -> [Block] {
        var out: [Block] = []
        var para: [String] = []
        func flush() {
            if !para.isEmpty { out.append(.paragraph(para.joined(separator: "\n"))) }
            para = []
        }
        var i = 0
        while i < lines.count {
            let line = lines[i]
            if let m = groups(fence, line) {
                flush()
                let mark = m[1], lang = m[2]
                var body: [String] = []
                i += 1
                while i < lines.count, !closes(lines[i], mark) {
                    body.append(lines[i])
                    i += 1
                }
                i += 1
                out.append(.code(lang, body.joined(separator: "\n")))
                continue
            }
            if line.trimmingCharacters(in: .whitespaces).isEmpty {
                flush()
                i += 1
                continue
            }
            if let m = groups(heading, line) {
                flush()
                let n = m[1].count
                out.append(.heading(n <= 2 ? 1 : n == 3 ? 2 : 3, m[2]))
                i += 1
                continue
            }
            if test(rule, line) {
                flush()
                out.append(.rule)
                i += 1
                continue
            }
            if test(quote, line) {
                flush()
                var q: [String] = []
                while i < lines.count, test(quote, lines[i]) {
                    q.append(lines[i].replacingOccurrences(of: #"^ {0,3}> ?"#, with: "", options: .regularExpression))
                    i += 1
                }
                out.append(.quote(blocks(q)))
                continue
            }
            if line.contains("|"), i + 1 < lines.count, lines[i + 1].contains("-"), test(tableRule, lines[i + 1]) {
                flush()
                let head = cells(line)
                let aligns = cells(lines[i + 1]).map { r -> Align in
                    let l = r.hasPrefix(":"), rt = r.hasSuffix(":")
                    return l && rt ? .center : rt ? .right : .left
                }
                i += 2
                var rows: [[String]] = []
                while i < lines.count, lines[i].contains("|"), !lines[i].trimmingCharacters(in: .whitespaces).isEmpty {
                    rows.append(cells(lines[i]))
                    i += 1
                }
                out.append(.table(head, aligns, rows))
                continue
            }
            if test(item, line) {
                flush()
                let (items, next) = list(lines, i)
                out.append(.list(items))
                i = next
                continue
            }
            para.append(line.trimmingCharacters(in: .whitespaces))
            i += 1
        }
        flush()
        return out
    }

    private static func closes(_ line: String, _ mark: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        guard let c = mark.first else { return false }
        return t.count >= mark.count && t.allSatisfy { $0 == c }
    }

    private static func cells(_ line: String) -> [String] {
        var t = line.trimmingCharacters(in: .whitespaces)
        if t.hasPrefix("|") { t.removeFirst() }
        if t.hasSuffix("|"), !t.hasSuffix("\\|") { t.removeLast() }
        var out: [String] = []
        var cell = ""
        var chars = Array(t)[...]
        while let c = chars.popFirst() {
            if c == "\\", chars.first == "|" {
                cell.append("|")
                chars.removeFirst()
            } else if c == "|" {
                out.append(cell.trimmingCharacters(in: .whitespaces))
                cell = ""
            } else {
                cell.append(c)
            }
        }
        out.append(cell.trimmingCharacters(in: .whitespaces))
        return out
    }

    private static func list(_ lines: [String], _ start: Int) -> ([ListItem], Int) {
        struct Raw {
            var indent: Int
            var ordered: Bool
            var number: Int
            var text: [String]
        }
        var raws: [Raw] = []
        var i = start
        while i < lines.count {
            let line = lines[i]
            if let m = groups(item, line) {
                let indent = m[1].replacingOccurrences(of: "\t", with: "    ").count
                let ordered = m[2].first?.isNumber ?? false
                raws.append(Raw(indent: indent, ordered: ordered, number: Int(m[2].filter(\.isNumber)) ?? 1, text: [m[3]]))
                i += 1
                continue
            }
            if line.trimmingCharacters(in: .whitespaces).isEmpty {
                // A blank line goes on with the list when an item or an indented line follows.
                if i + 1 < lines.count, test(item, lines[i + 1]) || test(indented, lines[i + 1]) {
                    i += 1
                    continue
                }
                break
            }
            if test(indented, line), !raws.isEmpty {
                raws[raws.count - 1].text.append(line.trimmingCharacters(in: .whitespaces))
                i += 1
                continue
            }
            break
        }
        // Depth by indent, as nested lists.
        var stack: [Int] = []
        var items: [ListItem] = []
        for r in raws {
            while let top = stack.last, r.indent < top { stack.removeLast() }
            if stack.last.map({ r.indent > $0 }) ?? true { stack.append(r.indent) }
            var first = r.text[0]
            var task: Bool?
            if let m = groups(taskBox, first) {
                task = m[1] != " "
                first = m[2]
            }
            items.append(ListItem(depth: stack.count - 1, ordered: r.ordered, number: r.number,
                                  text: ([first] + r.text.dropFirst()).joined(separator: "\n"), task: task))
        }
        return (items, i)
    }

    // MARK: Inline

    private static let detector = try? NSDataDetector(types: NSTextCheckingResult.CheckingType.link.rawValue)

    /// One block's inline Markdown, styled.
    static func inline(_ source: String, codeSize: CGFloat = 14) -> AttributedString {
        var a = (try? AttributedString(markdown: source, options: .init(allowsExtendedAttributes: false, interpretedSyntax: .inlineOnlyPreservingWhitespace,
                                                                        failurePolicy: .returnPartiallyParsedIfPossible))) ?? AttributedString(source)
        for run in a.runs {
            if let intent = run.inlinePresentationIntent, intent.contains(.code) {
                a[run.range].font = Theme.mono(codeSize)
                a[run.range].backgroundColor = Theme.panel
            }
            if let link = run.link, !["http", "https"].contains(link.scheme?.lowercased() ?? "") {
                a[run.range].link = nil
            }
        }
        // Bare addresses become links too.
        let plain = String(a.characters)
        if plain.contains("http"), let detector {
            let ns = plain as NSString
            for m in detector.matches(in: plain, range: NSRange(location: 0, length: ns.length)) {
                guard let url = m.url, ["http", "https"].contains(url.scheme?.lowercased() ?? ""),
                      ns.substring(with: m.range).lowercased().hasPrefix("http"),
                      let r = Range(m.range, in: a), a[r].runs.allSatisfy({ $0.link == nil && !($0.inlinePresentationIntent?.contains(.code) ?? false) }) else { continue }
                a[r].link = url
            }
        }
        return a
    }
}

/// The agent's Markdown, drawn.
@MainActor struct MarkdownView: View {
    let blocks: [MD.Block]
    var size: CGFloat = 16

    init(_ text: String, size: CGFloat = 16) {
        blocks = MD.parse(text)
        self.size = size
    }

    init(blocks: [MD.Block], size: CGFloat) {
        self.blocks = blocks
        self.size = size
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, b in
                block(b)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .tint(Theme.accentInk)
    }

    @ViewBuilder
    private func block(_ b: MD.Block) -> some View {
        switch b {
        case .paragraph(let text):
            Text(MD.inline(text, codeSize: size - 2))
                .font(.system(size: size))
                .lineSpacing(3)
                .foregroundStyle(Theme.ink)
                .fixedSize(horizontal: false, vertical: true)
        case .heading(let level, let text):
            Text(MD.inline(text))
                .font(Theme.rounded(level == 1 ? size + 3 : level == 2 ? size + 1 : size - 0.5, .bold))
                .foregroundStyle(level == 3 ? Theme.muted : Theme.ink)
                .padding(.top, 4)
                .fixedSize(horizontal: false, vertical: true)
        case .code(let lang, let code):
            VStack(alignment: .leading, spacing: 0) {
                if !lang.isEmpty {
                    Text(lang.lowercased()).font(.system(size: 11.5, weight: .semibold)).foregroundStyle(Theme.muted)
                        .padding(.horizontal, 12).padding(.top, 6)
                }
                ScrollView(.horizontal, showsIndicators: false) {
                    Text(code)
                        .font(Theme.mono(13))
                        .lineSpacing(3)
                        .foregroundStyle(Theme.ink)
                        .fixedSize()
                        .padding(.horizontal, 12).padding(.vertical, 9)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.panel, in: RoundedRectangle(cornerRadius: 13, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 13, style: .continuous).strokeBorder(Theme.edge, lineWidth: 0.5))
        case .quote(let inner):
            HStack(alignment: .top, spacing: 10) {
                RoundedRectangle(cornerRadius: 1.5).fill(Theme.accentFill).frame(width: 3)
                MarkdownView(blocks: inner, size: size).opacity(0.8)
            }
            .fixedSize(horizontal: false, vertical: true)
        case .list(let items):
            VStack(alignment: .leading, spacing: 4) {
                ForEach(Array(items.enumerated()), id: \.offset) { _, it in
                    HStack(alignment: .firstTextBaseline, spacing: 7) {
                        if let done = it.task {
                            Image(systemName: done ? "checkmark.square.fill" : "square")
                                .font(.system(size: size - 2))
                                .foregroundStyle(done ? Theme.done : Theme.muted)
                        } else {
                            Text(it.ordered ? "\(it.number)." : "•")
                                .font(.system(size: size).monospacedDigit())
                                .foregroundStyle(Theme.muted)
                        }
                        Text(MD.inline(it.text, codeSize: size - 2))
                            .font(.system(size: size))
                            .lineSpacing(3)
                            .foregroundStyle(Theme.ink)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    .padding(.leading, CGFloat(it.depth) * 18 + 4)
                }
            }
        case .rule:
            Rectangle().fill(Theme.edge).frame(height: 1).padding(.vertical, 4)
        case .table(let head, let aligns, let rows):
            ScrollView(.horizontal, showsIndicators: false) {
                Grid(alignment: .leading, horizontalSpacing: 0, verticalSpacing: 0) {
                    GridRow {
                        ForEach(Array(head.enumerated()), id: \.offset) { k, c in
                            cell(c, aligns, k).font(.system(size: 14, weight: .semibold))
                        }
                    }
                    .background(Theme.panel)
                    ForEach(Array(rows.enumerated()), id: \.offset) { _, r in
                        Divider().gridCellUnsizedAxes(.horizontal)
                        GridRow {
                            ForEach(0..<head.count, id: \.self) { k in
                                cell(k < r.count ? r[k] : "", aligns, k).font(.system(size: 14))
                            }
                        }
                    }
                }
            }
            .background(Theme.card)
            .clipShape(RoundedRectangle(cornerRadius: 13, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 13, style: .continuous).strokeBorder(Theme.edge, lineWidth: 0.5))
        }
    }

    private func cell(_ text: String, _ aligns: [MD.Align], _ k: Int) -> some View {
        let a = k < aligns.count ? aligns[k] : .left
        return Text(MD.inline(text, codeSize: 13))
            .foregroundStyle(Theme.ink)
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, 12).padding(.vertical, 7)
            .gridColumnAlignment(a == .right ? .trailing : a == .center ? .center : .leading)
    }
}
