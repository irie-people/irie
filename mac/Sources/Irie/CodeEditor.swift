import AppKit
import SwiftUI

// The editor of a code card (CodePane.swift): a plain-text NSTextView on its
// document's text (CodeDocument.swift), coloured by SyntaxColorer in the
// code theme picked (CodeThemes.swift), with line numbers down its side,
// the lines that came from outside marked in the card's colour, and the
// keys a code editor has.

/// Where the caret is, for the card's foot.
@MainActor
@Observable
final class CodeCaret {
    var line = 1
    var column = 1
    var selected = 0
}

/// A line of a file to go to, and a serial bumped to go to it again.
struct CodeGoTo: Equatable {
    var path: String
    var line: Int
    var column = 1
    var serial = 0
}

/// What the editor asks of its card.
@MainActor
struct CodeActions {
    var save: () -> Void = {}
    var quickOpen: () -> Void = {}
    var goToLine: () -> Void = {}
    var closeFile: () -> Void = {}
    /// The editor went where a CodeGoTo asked.
    var went: (CodeGoTo) -> Void = { _ in }
    /// Ask the session's agent about these lines, 1-based.
    var ask: ((ClosedRange<Int>) -> Void)?
    var copyReference: (ClosedRange<Int>) -> Void = { _ in }
}

/// The text view, with the keys and touches a code editor has.
final class CodeTextView: NSTextView {
    var indent = "    "
    var lineComment: String?
    var currentLine = NSColor.clear
    var actions = CodeActions()

    // MARK: Keys

    /// The keys a code editor has, which the window's key router hands it
    /// before the menus see them, while it has the keyboard: ⌘F find, ⌥⌘F
    /// replace, ⌘G and ⇧⌘G the next and last match, ⌘E find the selection,
    /// ⌘S save now, ⌘P open a file, ⌘L go to a line, ⌘/ comment lines, ⌘]
    /// and ⌘[ indent and outdent, and ⌘W close the file.
    func command(_ event: NSEvent) -> Bool {
        let mods = event.modifierFlags.intersection([.command, .control, .option, .shift])
        guard mods.contains(.command), let c = event.charactersIgnoringModifiers?.lowercased() else { return false }
        switch (mods, c) {
        case (.command, "f"): find(.showFindInterface)
        case ([.command, .option], "f"): find(.showReplaceInterface)
        case (.command, "g"): find(.nextMatch)
        case ([.command, .shift], "g"): find(.previousMatch)
        case (.command, "e"): find(.setSearchString)
        case (.command, "s"): actions.save()
        case (.command, "p"): actions.quickOpen()
        case (.command, "l"): actions.goToLine()
        case (.command, "/"): toggleComment()
        case (.command, "]"): shift(right: true)
        case (.command, "["): shift(right: false)
        case (.command, "w"): actions.closeFile()
        default: return false
        }
        return true
    }

    private func find(_ action: NSTextFinder.Action) {
        let item = NSMenuItem()
        item.tag = action.rawValue
        performTextFinderAction(item)
    }

    /// Return keeps the line's indent, and a line that opens a block
    /// indents the next one more; between a pair, the closer goes on a line
    /// of its own.
    override func insertNewline(_ sender: Any?) {
        let ns = string as NSString
        let sel = selectedRange()
        let start = ns.lineRange(for: NSRange(location: sel.location, length: 0)).location
        let before = ns.substring(with: NSRange(location: start, length: sel.location - start))
        let lead = String(before.prefix { $0 == " " || $0 == "\t" })
        let last = before.trimmingCharacters(in: .whitespaces).last
        let opens = last == "{" || last == "[" || last == "(" || (last == ":" && indentsAfterColon)
        let next = NSMaxRange(sel) < ns.length ? Character(UnicodeScalar(ns.character(at: NSMaxRange(sel))) ?? " ") : nil
        let pairs: [Character: Character] = ["{": "}", "[": "]", "(": ")"]
        let closes = opens && last.flatMap { pairs[$0] } == next && next != nil
        let first = "\n" + lead + (opens ? indent : "")
        insertText(first + (closes ? "\n" + lead : ""), replacementRange: sel)
        if closes { setSelectedRange(NSRange(location: sel.location + (first as NSString).length, length: 0)) }
    }

    /// Python and YAML open a block after a colon.
    var indentsAfterColon = false

    /// Tab indents the lines picked, or puts an indent in; ⇧Tab outdents.
    override func insertTab(_ sender: Any?) {
        let ns = string as NSString
        let sel = selectedRange()
        if sel.length > 0, ns.substring(with: sel).contains("\n") {
            shift(right: true)
        } else {
            insertText(indent, replacementRange: sel)
        }
    }

    override func insertBacktab(_ sender: Any?) { shift(right: false) }

    /// The whole lines the selection touches; a selection that ends at a
    /// line's start leaves that line out.
    private func pickedLines() -> NSRange {
        let ns = string as NSString
        var sel = selectedRange()
        if sel.length > 0, sel.location + sel.length <= ns.length, ns.character(at: NSMaxRange(sel) - 1) == 10 {
            sel.length -= 1
        }
        return ns.lineRange(for: sel)
    }

    /// Indents or outdents the lines picked by one indent.
    func shift(right: Bool) {
        let lines = pickedLines()
        let ns = string as NSString
        let text = ns.substring(with: lines)
        var parts = text.components(separatedBy: "\n")
        let trailing = parts.last == ""
        if trailing { parts.removeLast() }
        let out = parts.map { line -> String in
            if right { return line.isEmpty ? line : indent + line }
            if line.hasPrefix("\t") { return String(line.dropFirst()) }
            let spaces = line.prefix { $0 == " " }.count
            return String(line.dropFirst(min(spaces, max(indent == "\t" ? 4 : indent.count, 1))))
        }.joined(separator: "\n") + (trailing ? "\n" : "")
        replace(lines, with: out)
    }

    /// ⌘/: comments the lines picked out with the language's line comment,
    /// or takes it off when every one has it.
    func toggleComment() {
        guard let token = lineComment else {
            NSSound.beep()
            return
        }
        let lines = pickedLines()
        let text = (string as NSString).substring(with: lines)
        var parts = text.components(separatedBy: "\n")
        let trailing = parts.last == ""
        if trailing { parts.removeLast() }
        let blank: (String) -> Bool = { $0.allSatisfy { $0 == " " || $0 == "\t" } }
        let lead: (String) -> Substring = { $0.prefix { $0 == " " || $0 == "\t" } }
        let written = parts.filter { !blank($0) }
        guard !written.isEmpty else { return }
        let off = written.allSatisfy { $0.dropFirst(lead($0).count).hasPrefix(token) }
        let depth = written.map { lead($0).count }.min() ?? 0
        let out = parts.map { line -> String in
            if blank(line) { return line }
            if off {
                let l = lead(line)
                var rest = line.dropFirst(l.count).dropFirst(token.count)
                if rest.first == " " { rest = rest.dropFirst() }
                return String(l) + rest
            }
            let i = line.index(line.startIndex, offsetBy: depth)
            return String(line[..<i]) + token + " " + line[i...]
        }.joined(separator: "\n") + (trailing ? "\n" : "")
        replace(lines, with: out)
    }

    /// Replaces a range as typing does, so ⌘Z takes it back, and picks what
    /// was put in.
    private func replace(_ range: NSRange, with text: String) {
        guard shouldChangeText(in: range, replacementString: text) else { return }
        textStorage?.replaceCharacters(in: range, with: text)
        didChangeText()
        setSelectedRange(NSRange(location: range.location, length: (text as NSString).length))
    }

    // MARK: Drawing

    /// The caret's line, marked across the whole width.
    override func drawBackground(in rect: NSRect) {
        super.drawBackground(in: rect)
        guard selectedRange().length == 0, let line = caretLineRect() else { return }
        currentLine.setFill()
        line.intersection(rect).fill()
    }

    private func caretLineRect() -> NSRect? {
        guard let lm = layoutManager, let storage = textStorage else { return nil }
        let at = selectedRange().location
        var r: NSRect
        if at >= storage.length, storage.length == 0 || (storage.string as NSString).character(at: storage.length - 1) == 10 {
            r = lm.extraLineFragmentRect
        } else {
            let g = lm.glyphIndexForCharacter(at: min(at, storage.length - 1))
            r = lm.lineFragmentRect(forGlyphAt: g, effectiveRange: nil)
        }
        guard r.height > 0 else { return nil }
        return NSRect(x: 0, y: r.minY + textContainerOrigin.y, width: bounds.width, height: r.height)
    }

    override func setSelectedRanges(_ ranges: [NSValue], affinity: NSSelectionAffinity, stillSelecting: Bool) {
        let was = caretLineRect()
        super.setSelectedRanges(ranges, affinity: affinity, stillSelecting: stillSelecting)
        if let was { setNeedsDisplay(was) }
        if let now = caretLineRect() { setNeedsDisplay(now) }
    }

    // MARK: The menu

    override func menu(for event: NSEvent) -> NSMenu? {
        let menu = NSMenu()
        let lines = pickedLineNumbers()
        if actions.ask != nil {
            menu.addItem(item(lines.count > 1 ? "Ask the Agent About These Lines" : "Ask the Agent About This Line") { [weak self] in
                self?.actions.ask?(lines)
            })
        }
        menu.addItem(item("Copy Path and Lines") { [weak self] in self?.actions.copyReference(lines) })
        menu.addItem(.separator())
        menu.addItem(withTitle: "Cut", action: #selector(cut(_:)), keyEquivalent: "")
        menu.addItem(withTitle: "Copy", action: #selector(copy(_:)), keyEquivalent: "")
        menu.addItem(withTitle: "Paste", action: #selector(paste(_:)), keyEquivalent: "")
        menu.addItem(.separator())
        menu.addItem(item("Find…") { [weak self] in self?.find(.showFindInterface) })
        menu.addItem(item("Go to Line…") { [weak self] in self?.actions.goToLine() })
        if lineComment != nil {
            menu.addItem(item("Comment Lines") { [weak self] in self?.toggleComment() })
        }
        return menu
    }

    /// The 1-based lines the selection covers.
    func pickedLineNumbers() -> ClosedRange<Int> {
        let ns = string as NSString
        let lines = pickedLines()
        let first = lineNumber(at: lines.location, in: ns)
        let last = lineNumber(at: max(lines.location, NSMaxRange(lines) - 1), in: ns)
        return first...max(first, last)
    }

    private func lineNumber(at offset: Int, in ns: NSString) -> Int {
        var n = 1
        let end = min(offset, ns.length)
        var buffer = [unichar](repeating: 0, count: 8192)
        var at = 0
        while at < end {
            let k = min(buffer.count, end - at)
            buffer.withUnsafeMutableBufferPointer { b in
                ns.getCharacters(b.baseAddress!, range: NSRange(location: at, length: k))
                for i in 0..<k where b[i] == 10 { n += 1 }
            }
            at += k
        }
        return n
    }

    private func item(_ title: String, _ run: @escaping () -> Void) -> NSMenuItem {
        let i = NSMenuItem(title: title, action: #selector(MenuRun.run(_:)), keyEquivalent: "")
        let target = MenuRun(run)
        i.target = target
        i.representedObject = target
        return i
    }
}

/// A menu item's action as a closure; the item keeps it alive.
private final class MenuRun: NSObject {
    let body: () -> Void
    init(_ body: @escaping () -> Void) { self.body = body }
    @objc func run(_: Any?) { body() }
}

/// The line numbers down the editor's side, the caret's line brighter, and a
/// bar in the card's colour by each line that came from outside; a tick
/// where lines were taken out.
final class CodeRuler: NSRulerView {
    weak var text: CodeTextView?
    weak var doc: CodeDocument?
    var ground = NSColor.textBackgroundColor
    var ink = NSColor.secondaryLabelColor
    var bright = NSColor.labelColor
    var mark = NSColor.systemGreen
    var digitFont = NSFont.monospacedDigitSystemFont(ofSize: 11, weight: .regular)

    override var isOpaque: Bool { true }

    /// As wide as the line count's digits, at least three.
    func fit(lines: Int) {
        let digits = max(3, String(lines).count)
        let w = ceil(CGFloat(digits) * ("8" as NSString).size(withAttributes: [.font: digitFont]).width + 22)
        if abs(ruleThickness - w) > 0.5 { ruleThickness = w }
    }

    override func drawHashMarksAndLabels(in rect: NSRect) {
        ground.setFill()
        bounds.fill()
        MainActor.assumeIsolated { drawLines() }
    }

    @MainActor private func drawLines() {
        guard let tv = text, let lm = tv.layoutManager, let tc = tv.textContainer, let doc, let clip = scrollView?.contentView else { return }
        let storage = doc.storage
        let starts = doc.starts
        let visible = clip.bounds.offsetBy(dx: -tv.textContainerOrigin.x, dy: -tv.textContainerOrigin.y)
        let glyphs = lm.glyphRange(forBoundingRect: visible, in: tc)
        let chars = lm.characterRange(forGlyphRange: glyphs, actualGlyphRange: nil)
        let caret = doc.lineIndex(tv.selectedRange().location) ?? -1
        // The marked lines, and the lines a cut stands before.
        var marked = IndexSet(), cut = IndexSet()
        for m in doc.marks {
            guard let a = doc.lineIndex(m.location) else { continue }
            if m.length == 0 {
                cut.insert(a)
            } else if let b = doc.lineIndex(max(m.location, NSMaxRange(m) - 1)) {
                marked.insert(integersIn: a...max(a, b))
            }
        }
        var line = doc.lineIndex(chars.location) ?? 0
        while line < starts.count, starts[line] <= NSMaxRange(chars) {
            let at = starts[line]
            var frag: NSRect
            if at >= storage.length {
                frag = lm.extraLineFragmentRect
                if frag.height == 0 { break }
            } else {
                frag = lm.lineFragmentRect(forGlyphAt: lm.glyphIndexForCharacter(at: at), effectiveRange: nil)
            }
            // The line's top in the ruler, which the text view's scroll
            // carries.
            let y = convert(NSPoint(x: 0, y: frag.minY + tv.textContainerOrigin.y), from: tv).y
            let label = "\(line + 1)" as NSString
            let attrs: [NSAttributedString.Key: Any] = [.font: digitFont, .foregroundColor: line == caret ? bright : ink]
            let size = label.size(withAttributes: attrs)
            label.draw(at: NSPoint(x: ruleThickness - size.width - 10, y: y + (frag.height - size.height) / 2), withAttributes: attrs)
            if marked.contains(line) {
                mark.setFill()
                NSBezierPath(roundedRect: NSRect(x: ruleThickness - 5, y: y + 1, width: 3, height: frag.height - 2), xRadius: 1.5, yRadius: 1.5).fill()
            }
            if cut.contains(line) {
                mark.setFill()
                let p = NSBezierPath()
                p.move(to: NSPoint(x: ruleThickness - 7, y: y - 3))
                p.line(to: NSPoint(x: ruleThickness - 1, y: y))
                p.line(to: NSPoint(x: ruleThickness - 7, y: y + 3))
                p.close()
                p.fill()
            }
            line += 1
        }
    }
}

/// A code card's editor, on a document's text.
@MainActor
struct CodeEditorView: NSViewRepresentable {
    let doc: CodeDocument
    let theme: CodeTheme
    let fontSize: CGFloat
    /// The card's colour, for the marks by lines from outside.
    let mark: NSColor
    let focusToken: Int
    let takesKeysOnStart: Bool
    let shows: Bool
    let goTo: CodeGoTo?
    let caret: CodeCaret
    let actions: CodeActions

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> NSScrollView {
        let scroll = NSScrollView()
        scroll.hasVerticalScroller = true
        scroll.hasHorizontalScroller = true
        scroll.autohidesScrollers = true
        scroll.borderType = .noBorder
        scroll.scrollerStyle = .overlay
        scroll.findBarPosition = .aboveContent

        let lm = NSLayoutManager()
        lm.allowsNonContiguousLayout = true
        doc.storage.addLayoutManager(lm)
        let tc = NSTextContainer(size: NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude))
        tc.widthTracksTextView = false
        tc.lineFragmentPadding = 4
        lm.addTextContainer(tc)
        let tv = CodeTextView(frame: NSRect(origin: .zero, size: scroll.contentSize), textContainer: tc)
        tv.minSize = NSSize(width: 0, height: 0)
        tv.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        tv.isVerticallyResizable = true
        tv.isHorizontallyResizable = true
        tv.autoresizingMask = [.width, .height]
        tv.textContainerInset = NSSize(width: 4, height: 8)
        tv.isRichText = false
        tv.importsGraphics = false
        tv.usesFontPanel = false
        tv.allowsUndo = true
        tv.usesFindBar = true
        tv.isIncrementalSearchingEnabled = true
        tv.isAutomaticQuoteSubstitutionEnabled = false
        tv.isAutomaticDashSubstitutionEnabled = false
        tv.isAutomaticTextReplacementEnabled = false
        tv.isAutomaticSpellingCorrectionEnabled = false
        tv.isAutomaticLinkDetectionEnabled = false
        tv.isAutomaticDataDetectionEnabled = false
        tv.isAutomaticTextCompletionEnabled = false
        tv.isContinuousSpellCheckingEnabled = false
        tv.isGrammarCheckingEnabled = false
        tv.smartInsertDeleteEnabled = false
        tv.delegate = context.coordinator
        scroll.documentView = tv

        let ruler = CodeRuler(scrollView: scroll, orientation: .verticalRuler)
        ruler.clientView = tv
        ruler.text = tv
        ruler.doc = doc
        scroll.verticalRulerView = ruler
        scroll.hasVerticalRuler = true
        scroll.rulersVisible = true

        let c = context.coordinator
        c.text = tv
        c.ruler = ruler
        c.layout = lm
        c.doc = doc
        c.caret = caret
        c.watch(scroll)
        doc.views.add(tv)
        dress(tv, ruler: ruler, scroll: scroll, c)
        doc.recolourAll(in: lm)
        c.token = focusToken
        if takesKeysOnStart, shows {
            DispatchQueue.main.async { tv.window?.makeFirstResponder(tv) }
        }
        goIfAsked(c)
        // The card's foot says where this file's caret is, not the last one's.
        DispatchQueue.main.async { c.noteCaret() }
        return scroll
    }

    func updateNSView(_ scroll: NSScrollView, context: Context) {
        let c = context.coordinator
        guard let tv = c.text, let ruler = c.ruler else { return }
        dress(tv, ruler: ruler, scroll: scroll, c)
        // The keyboard comes as the card asks for it, and goes as it hides.
        if c.token != focusToken {
            c.token = focusToken
            if shows { DispatchQueue.main.async { tv.window?.makeFirstResponder(tv) } }
        }
        if !shows, tv.window?.firstResponder === tv {
            tv.window?.makeFirstResponder(nil)
        }
        goIfAsked(c)
    }

    /// Goes to the line asked of this file, once; an ask for another file
    /// waits for its own editor.
    private func goIfAsked(_ c: Coordinator) {
        guard let g = goTo, g.path == doc.path, g != c.wentTo else { return }
        c.wentTo = g
        let done = actions.went
        DispatchQueue.main.async {
            c.go(to: g)
            done(g)
        }
    }

    static func dismantleNSView(_ scroll: NSScrollView, coordinator c: Coordinator) {
        c.unwatch()
        if let lm = c.layout, let doc = c.doc { doc.storage.removeLayoutManager(lm) }
    }

    /// The theme, the size and the card's colour, on every part.
    private func dress(_ tv: CodeTextView, ruler: CodeRuler, scroll: NSScrollView, _ c: Coordinator) {
        let font = NSFont.monospacedSystemFont(ofSize: fontSize, weight: .regular)
        doc.dress(theme: theme, font: font)
        tv.actions = actions
        tv.indent = doc.indent
        tv.lineComment = doc.language.lineComment
        tv.indentsAfterColon = ["python", "yaml"].contains(doc.language.id)
        guard c.dressed?.theme != theme || c.dressed?.size != fontSize || c.dressed?.mark != mark else { return }
        c.dressed = (theme, fontSize, mark)
        tv.font = font
        tv.textColor = theme.foreground
        tv.defaultParagraphStyle = doc.paragraph
        tv.typingAttributes = [.font: font, .foregroundColor: theme.foreground, .paragraphStyle: doc.paragraph]
        tv.backgroundColor = theme.background
        tv.insertionPointColor = theme.caret
        tv.selectedTextAttributes = [.backgroundColor: theme.selection]
        tv.currentLine = theme.currentLine
        scroll.backgroundColor = theme.background
        scroll.drawsBackground = true
        ruler.ground = theme.background
        ruler.ink = theme.gutterInk
        ruler.bright = theme.gutterActiveInk
        ruler.mark = mark
        ruler.digitFont = NSFont.monospacedDigitSystemFont(ofSize: max(9, fontSize - 1.5), weight: .regular)
        ruler.fit(lines: doc.lineCount)
        ruler.needsDisplay = true
        tv.needsDisplay = true
    }

    @MainActor
    final class Coordinator: NSObject, NSTextViewDelegate {
        weak var text: CodeTextView?
        weak var ruler: CodeRuler?
        weak var layout: NSLayoutManager?
        weak var doc: CodeDocument?
        weak var caret: CodeCaret?
        var token = 0
        var wentTo: CodeGoTo?
        /// What the parts were last dressed in.
        var dressed: (theme: CodeTheme, size: CGFloat, mark: NSColor)?
        private var observers: [NSObjectProtocol] = []

        /// The document's own undo, so each file keeps its history.
        func undoManager(for _: NSTextView) -> UndoManager? { doc?.undo }

        func textViewDidChangeSelection(_: Notification) {
            ruler?.needsDisplay = true
            noteCaret()
        }

        func textDidChange(_: Notification) {
            if let doc { ruler?.fit(lines: doc.lineCount) }
            ruler?.needsDisplay = true
        }

        func noteCaret() {
            guard let tv = text, let doc, let caret else { return }
            let sel = tv.selectedRange()
            let line = doc.lineIndex(sel.location) ?? 0
            let col = sel.location - doc.lineRange(line).location + 1
            if caret.line != line + 1 { caret.line = line + 1 }
            if caret.column != col { caret.column = col }
            if caret.selected != sel.length { caret.selected = sel.length }
        }

        /// The ruler follows scrolls and any change of the text, such as one
        /// from the disk.
        func watch(_ scroll: NSScrollView) {
            scroll.contentView.postsBoundsChangedNotifications = true
            observers.append(NotificationCenter.default.addObserver(forName: NSView.boundsDidChangeNotification, object: scroll.contentView, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.ruler?.needsDisplay = true }
            })
            if let doc {
                observers.append(NotificationCenter.default.addObserver(forName: NSTextStorage.didProcessEditingNotification, object: doc.storage, queue: .main) { [weak self] _ in
                    MainActor.assumeIsolated {
                        guard let self, let doc = self.doc else { return }
                        self.ruler?.fit(lines: doc.lineCount)
                        self.ruler?.needsDisplay = true
                        self.noteCaret()
                    }
                })
            }
        }

        func unwatch() {
            observers.forEach(NotificationCenter.default.removeObserver)
            observers = []
        }

        /// Puts the caret on a line, scrolls it into the middle and flashes
        /// it; while you type in it, only scrolls and flashes.
        func go(to g: CodeGoTo) {
            guard let tv = text, let doc else { return }
            let range = doc.lineRange(max(0, g.line - 1))
            let at = range.location + min(max(0, g.column - 1), range.length)
            if tv.window?.firstResponder !== tv { tv.setSelectedRange(NSRange(location: at, length: 0)) }
            tv.scrollRangeToVisible(range)
            if let lm = tv.layoutManager, let tc = tv.textContainer, let clip = tv.enclosingScrollView?.contentView {
                let rect = lm.boundingRect(forGlyphRange: lm.glyphRange(forCharacterRange: range, actualCharacterRange: nil), in: tc)
                let y = max(0, rect.midY + tv.textContainerOrigin.y - clip.bounds.height / 2)
                clip.scroll(to: NSPoint(x: 0, y: y))
                tv.enclosingScrollView?.reflectScrolledClipView(clip)
            }
            if range.length > 0 { tv.showFindIndicator(for: range) }
        }
    }
}
