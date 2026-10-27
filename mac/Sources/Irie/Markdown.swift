import SwiftUI

/// Markdown as the agent writes it: headings, lists, code blocks, quotes,
/// tables and rules, with bold, italics, code and links inside a line
/// (Foundation's inline Markdown). A line break stays a line break, as in
/// the terminal.
struct MarkdownText: View {
    let source: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(MarkdownBlock.parse(source).enumerated()), id: \.offset) { _, block in
                view(block)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder private func view(_ block: MarkdownBlock) -> some View {
        switch block {
        case let .paragraph(text):
            inline(text).font(.body).textSelection(.enabled)
        case let .heading(level, text):
            inline(text)
                .font(level == 1 ? .title2.weight(.semibold) : level == 2 ? .title3.weight(.semibold) : .headline)
                .padding(.top, 4)
        case let .item(depth, marker, text):
            HStack(alignment: .firstTextBaseline, spacing: 7) {
                Text(marker).foregroundStyle(Theme.muted).monospacedDigit()
                inline(text).textSelection(.enabled)
            }
            .font(.body)
            .padding(.leading, CGFloat(depth) * 18)
        case let .code(text):
            Text(text)
                .font(.system(.callout, design: .monospaced))
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, Theme.inset)
                .padding(.vertical, 10)
                .background(RoundedRectangle(cornerRadius: Theme.radius - 2, style: .continuous).fill(Theme.panel))
                .overlay(RoundedRectangle(cornerRadius: Theme.radius - 2, style: .continuous).strokeBorder(Theme.edge, lineWidth: 1))
        case let .quote(text):
            HStack(alignment: .top, spacing: 10) {
                RoundedRectangle(cornerRadius: 1.5).fill(Theme.edge).frame(width: 3)
                inline(text).font(.body).foregroundStyle(Theme.muted).textSelection(.enabled)
            }
            .fixedSize(horizontal: false, vertical: true)
        case let .table(rows):
            Grid(alignment: .leading, horizontalSpacing: 14, verticalSpacing: 5) {
                ForEach(Array(rows.enumerated()), id: \.offset) { i, row in
                    GridRow {
                        ForEach(Array(row.enumerated()), id: \.offset) { _, cell in
                            inline(cell).font(i == 0 ? .callout.weight(.semibold) : .callout)
                        }
                    }
                    if i == 0 {
                        Rectangle().fill(Theme.edge).frame(height: 1).gridCellUnsizedAxes(.horizontal)
                    }
                }
            }
            .padding(.horizontal, Theme.inset)
            .padding(.vertical, 10)
            .background(RoundedRectangle(cornerRadius: Theme.radius - 2, style: .continuous).fill(Theme.panel))
        case .rule:
            Rectangle().fill(Theme.edge).frame(height: 1).padding(.vertical, 2)
        }
    }

    private func inline(_ s: String) -> Text {
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        if let a = try? AttributedString(markdown: s, options: options) {
            return Text(a)
        }
        return Text(s)
    }
}

/// One block of Markdown.
enum MarkdownBlock: Equatable {
    case paragraph(String)
    case heading(Int, String)
    case item(Int, String, String) // depth, marker, text
    case code(String)
    case quote(String)
    case table([[String]])
    case rule

    static func parse(_ source: String) -> [MarkdownBlock] {
        var out: [MarkdownBlock] = []
        var paragraph: [String] = []
        var quote: [String] = []
        var table: [[String]] = []
        var fence: (marker: String, lines: [String])?

        func flush() {
            if !paragraph.isEmpty { out.append(.paragraph(paragraph.joined(separator: "\n"))) }
            if !quote.isEmpty { out.append(.quote(quote.joined(separator: "\n"))) }
            if !table.isEmpty { out.append(.table(table)) }
            paragraph = []
            quote = []
            table = []
        }

        for raw in source.components(separatedBy: "\n") {
            let line = raw.replacingOccurrences(of: "\t", with: "    ")
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if var f = fence {
                if trimmed.hasPrefix(f.marker) && trimmed.trimmingCharacters(in: CharacterSet(charactersIn: "`~")).isEmpty {
                    out.append(.code(f.lines.joined(separator: "\n")))
                    fence = nil
                } else {
                    f.lines.append(raw)
                    fence = f
                }
                continue
            }
            if trimmed.hasPrefix("```") || trimmed.hasPrefix("~~~") {
                flush()
                let marker = String(trimmed.prefix { $0 == "`" || $0 == "~" })
                fence = (marker, [])
                continue
            }
            if trimmed.isEmpty {
                flush()
                continue
            }
            if let h = heading(trimmed) {
                flush()
                out.append(h)
                continue
            }
            if isRule(trimmed) {
                flush()
                out.append(.rule)
                continue
            }
            if trimmed.hasPrefix(">") {
                if !paragraph.isEmpty || !table.isEmpty { flush() }
                quote.append(String(trimmed.dropFirst()).trimmingCharacters(in: .whitespaces))
                continue
            }
            if trimmed.hasPrefix("|") {
                if !paragraph.isEmpty || !quote.isEmpty { flush() }
                if !isTableRule(trimmed) { table.append(cells(trimmed)) }
                continue
            }
            if let it = item(line) {
                flush()
                out.append(it)
                continue
            }
            if !quote.isEmpty || !table.isEmpty { flush() }
            paragraph.append(trimmed)
        }
        if let f = fence { out.append(.code(f.lines.joined(separator: "\n"))) }
        flush()
        return out
    }

    private static func heading(_ s: String) -> MarkdownBlock? {
        let hashes = s.prefix { $0 == "#" }.count
        guard (1...6).contains(hashes), s.dropFirst(hashes).first == " " else { return nil }
        return .heading(hashes, String(s.dropFirst(hashes)).trimmingCharacters(in: .whitespaces))
    }

    private static func isRule(_ s: String) -> Bool {
        let chars = s.filter { $0 != " " }
        guard chars.count >= 3, let c = chars.first, "-*_".contains(c) else { return false }
        return chars.allSatisfy { $0 == c }
    }

    private static func isTableRule(_ s: String) -> Bool {
        s.allSatisfy { "|-: ".contains($0) } && s.contains("-")
    }

    private static func cells(_ s: String) -> [String] {
        var t = s
        if t.hasPrefix("|") { t.removeFirst() }
        if t.hasSuffix("|") { t.removeLast() }
        return t.components(separatedBy: "|").map { $0.trimmingCharacters(in: .whitespaces) }
    }

    /// A list item: "- text", "* text", "+ text" or "1. text", indented two
    /// spaces a level.
    private static func item(_ line: String) -> MarkdownBlock? {
        let indent = line.prefix { $0 == " " }.count
        let rest = line.dropFirst(indent)
        if let first = rest.first, "-*+".contains(first), rest.dropFirst().first == " " {
            return .item(indent / 2, "•", String(rest.dropFirst(2)).trimmingCharacters(in: .whitespaces))
        }
        let digits = rest.prefix { $0.isNumber }
        if !digits.isEmpty, digits.count <= 3 {
            let after = rest.dropFirst(digits.count)
            if let mark = after.first, mark == "." || mark == ")", after.dropFirst().first == " " {
                return .item(indent / 2, digits + ".", String(after.dropFirst(2)).trimmingCharacters(in: .whitespaces))
            }
        }
        return nil
    }
}
