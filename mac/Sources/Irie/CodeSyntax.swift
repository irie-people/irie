import AppKit

// The Code card's syntax colours: a lexer for each kind of file, which reads
// a text a line at a time from the state the line before left, such as
// inside a comment, a string or a fence. The colorer keeps each line's start
// state and runs, so after an edit it lexes again from the first line the
// edit touched until a line starts as it did before, and no further.
//
// The lexers read UTF-16 code units, as NSString keeps them, through a
// pointer rather than Characters, which cost far more to walk. They keep to
// what stays quick in a build without optimisation, as the preview's is:
// plain comparisons in place of switches over ranges, no integer
// conversions in their loops, and words looked up as they are read, with
// no string made. A line longer than 4,000 units, as a minified file has,
// is lexed for its first 4,000 only; the rest stays plain.

// MARK: Tokens and languages

/// What a run of code is, which a theme gives its colour.
enum CodeToken: UInt8, CaseIterable {
    case plain, comment, keyword, type, function, string, number, constant,
         attribute, variable, property, tag, heading, link, emphasis, code,
         escape, inserted, deleted
}

/// A language the Code card knows: its name for the status line, what ⌘/
/// puts before a line, and how its lines are lexed.
struct CodeLanguage: Equatable {
    let id: String
    let name: String
    /// What ⌘/ puts before a line, such as "//"; nil for a language without.
    let lineComment: String?
    let blockStart: String?
    let blockEnd: String?
    /// Go and Makefiles indent with tabs.
    let usesTabs: Bool
    /// How its lines are lexed.
    fileprivate let rules: SyntaxRules

    static func == (a: CodeLanguage, b: CodeLanguage) -> Bool { a.id == b.id }
}

extension CodeLanguage {
    fileprivate init(_ id: String, _ name: String, line: String? = nil, block: (String, String)? = nil,
                     tabs: Bool = false, _ rules: SyntaxRules) {
        self.init(id: id, name: name, lineComment: line, blockStart: block?.0, blockEnd: block?.1,
                  usesTabs: tabs, rules: rules)
    }

    private static let slashes = ("/*", "*/")

    static let plain = CodeLanguage("plain", "Plain Text", SyntaxRules(.plain))
    static let go = CodeLanguage("go", "Go", line: "//", block: slashes, tabs: true,
                                 SyntaxRules(.code, .go, words: .go, slash: true, capitalTypes: true))
    static let goModule = CodeLanguage("gomod", "Go Module", line: "//", tabs: true,
                                       SyntaxRules(.code, words: .goModule, slash: true, versions: true))
    static let swift = CodeLanguage("swift", "Swift", line: "//", block: slashes,
                                    SyntaxRules(.code, .swift, words: .swift, slash: true, nests: true, decorators: true,
                                                dollar: true, capitalTypes: true, capitalCalls: true))
    static let python = CodeLanguage("python", "Python", line: "#",
                                     SyntaxRules(.code, .python, words: .python, hash: true, decorators: true, capsConstants: true))
    static let javascript = CodeLanguage("javascript", "JavaScript", line: "//", block: slashes,
                                         SyntaxRules(.code, .js, words: .javascript, slash: true, decorators: true, jsx: true,
                                                     dollar: true, capitalTypes: true, capitalCalls: true, capsConstants: true))
    static let typescript = CodeLanguage("typescript", "TypeScript", line: "//", block: slashes,
                                         SyntaxRules(.code, .js, words: .typescript, slash: true, decorators: true,
                                                     dollar: true, capitalTypes: true, capitalCalls: true, capsConstants: true))
    static let tsx = CodeLanguage("tsx", "TSX", line: "//", block: slashes,
                                  SyntaxRules(.code, .js, words: .typescript, slash: true, decorators: true, jsx: true,
                                              dollar: true, capitalTypes: true, capitalCalls: true, capsConstants: true))
    static let json = CodeLanguage("json", "JSON", SyntaxRules(.code, .json, words: .json, slash: true))
    static let jsonc = CodeLanguage("jsonc", "JSON with Comments", line: "//", block: slashes,
                                    SyntaxRules(.code, .json, words: .json, slash: true))
    static let yaml = CodeLanguage("yaml", "YAML", line: "#", SyntaxRules(.yaml))
    static let toml = CodeLanguage("toml", "TOML", line: "#", SyntaxRules(.toml))
    static let ini = CodeLanguage("ini", "INI", line: ";", SyntaxRules(.ini))
    static let conf = CodeLanguage("conf", "Config", line: "#", SyntaxRules(.ini))
    static let dotenv = CodeLanguage("dotenv", "Environment", line: "#", SyntaxRules(.ini))
    static let markdown = CodeLanguage("markdown", "Markdown", block: ("<!--", "-->"), SyntaxRules(.markdown))
    static let shell = CodeLanguage("shell", "Shell", line: "#", SyntaxRules(.shell, words: .shell))
    static let rust = CodeLanguage("rust", "Rust", line: "//", block: slashes,
                                   SyntaxRules(.code, .rust, words: .rust, slash: true, nests: true,
                                               capitalTypes: true, capitalCalls: true, capsConstants: true))
    private static let cRules = SyntaxRules(.code, .c, words: .c, slash: true, capitalTypes: true, capsConstants: true)
    static let c = CodeLanguage("c", "C", line: "//", block: slashes, cRules)
    static let cpp = CodeLanguage("cpp", "C++", line: "//", block: slashes, cRules)
    static let objc = CodeLanguage("objc", "Objective-C", line: "//", block: slashes, cRules)
    static let objcpp = CodeLanguage("objcpp", "Objective-C++", line: "//", block: slashes, cRules)
    static let java = CodeLanguage("java", "Java", line: "//", block: slashes,
                                   SyntaxRules(.code, .java, words: .java, slash: true, decorators: true, dollar: true,
                                               capitalTypes: true, capitalCalls: true, capsConstants: true))
    static let kotlin = CodeLanguage("kotlin", "Kotlin", line: "//", block: slashes,
                                     SyntaxRules(.code, .kotlin, words: .kotlin, slash: true, nests: true, decorators: true,
                                                 capitalTypes: true, capitalCalls: true, capsConstants: true))
    static let ruby = CodeLanguage("ruby", "Ruby", line: "#",
                                   SyntaxRules(.code, .ruby, words: .ruby, hash: true, capitalTypes: true, capitalCalls: true))
    private static let cssRules = SyntaxRules(.css)
    private static let scriptRules = SyntaxRules(.code, .js, words: .javascript, slash: true, dollar: true,
                                                 capitalTypes: true, capitalCalls: true, capsConstants: true)
    private static let htmlRules = SyntaxRules(.markup, script: scriptRules, style: cssRules)
    private static let xmlRules = SyntaxRules(.markup)
    private static let markup = ("<!--", "-->")
    static let html = CodeLanguage("html", "HTML", block: markup, htmlRules)
    static let xml = CodeLanguage("xml", "XML", block: markup, xmlRules)
    static let plist = CodeLanguage("plist", "Property List", block: markup, xmlRules)
    static let svg = CodeLanguage("svg", "SVG", block: markup, htmlRules)
    static let css = CodeLanguage("css", "CSS", block: slashes, cssRules)
    static let scss = CodeLanguage("scss", "SCSS", line: "//", block: slashes, SyntaxRules(.css, slash: true))
    static let less = CodeLanguage("less", "Less", line: "//", block: slashes, SyntaxRules(.css, slash: true))
    static let sql = CodeLanguage("sql", "SQL", line: "--", block: slashes,
                                  SyntaxRules(.code, .sql, words: .sql, slash: true, dash: true))
    static let makefile = CodeLanguage("makefile", "Makefile", line: "#", tabs: true,
                                       SyntaxRules(.make, words: .shell, heads: .make))
    static let dockerfile = CodeLanguage("dockerfile", "Dockerfile", line: "#",
                                         SyntaxRules(.docker, words: .shell, heads: .docker))
    static let lua = CodeLanguage("lua", "Lua", line: "--", block: ("--[[", "]]"),
                                  SyntaxRules(.code, .lua, words: .lua, dash: true))
    static let diff = CodeLanguage("diff", "Diff", SyntaxRules(.diff))
    static let ignore = CodeLanguage("ignore", "Ignore List", line: "#", SyntaxRules(.ignore))

    /// Every language, Plain Text first and the rest by name.
    static let all: [CodeLanguage] = [plain] + [
        go, goModule, swift, python, javascript, typescript, tsx, json, jsonc, yaml, toml, ini, conf, dotenv,
        markdown, shell, rust, c, cpp, objc, objcpp, java, kotlin, ruby, html, xml, plist, svg, css, scss, less,
        sql, makefile, dockerfile, lua, diff, ignore,
    ].sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }

    /// The language of a file: by its name or extension, then by a shebang or
    /// a first line such as <?xml; Plain Text for one it does not know.
    static func detect(path: String, firstLine: String?) -> CodeLanguage {
        let name = (path as NSString).lastPathComponent
        if let l = byName[name] ?? byName[name.lowercased()] { return l }
        let lower = name.lowercased()
        if lower.hasPrefix("dockerfile.") || lower.hasPrefix("containerfile.") { return dockerfile }
        if lower.hasPrefix(".env.") { return dotenv }
        if lower.hasPrefix("makefile.") || lower.hasPrefix("gnumakefile.") { return makefile }
        if path.hasSuffix(".git/config") || path.hasSuffix(".ssh/config") || path.contains(".ssh/config.d/") { return conf }
        let ext = (lower as NSString).pathExtension
        if ext == "config" {
            return firstLine?.trimmingCharacters(in: .whitespaces).hasPrefix("<") == true ? xml : conf
        }
        if ext == "json", lower.hasPrefix("tsconfig") || lower.hasPrefix("jsconfig") || path.contains("/.vscode/") {
            return jsonc
        }
        if !ext.isEmpty, let l = byExtension[ext] { return l }
        if let line = firstLine, let l = fromFirstLine(line) { return l }
        return plain
    }

    /// A language from a file's first line: its shebang's program, or a start
    /// such as <?xml or diff --git.
    private static func fromFirstLine(_ line: String) -> CodeLanguage? {
        if line.hasPrefix("#!") {
            var words = line.dropFirst(2).split(whereSeparator: { $0 == " " || $0 == "\t" }).map(String.init)
            guard !words.isEmpty else { return nil }
            var program = (words.removeFirst() as NSString).lastPathComponent
            if program == "env" {
                // env -S node --flag, env FOO=1 python3: the program is the first other word.
                while let w = words.first, w.hasPrefix("-") || w.contains("=") { words.removeFirst() }
                guard let w = words.first else { return nil }
                program = (w as NSString).lastPathComponent
            }
            // python3.12, lua5.4: the name without its version.
            while let last = program.last, last.isNumber || last == "." { program.removeLast() }
            switch program {
            case "sh", "bash", "zsh", "ksh", "mksh", "dash", "ash", "busybox": return shell
            case "python", "pypy", "uv": return python
            case "node", "nodejs", "bun": return javascript
            case "deno", "ts-node", "tsx": return typescript
            case "ruby", "jruby": return ruby
            case "lua", "luajit", "texlua": return lua
            case "swift": return swift
            case "make", "gmake": return makefile
            default: return nil
            }
        }
        let t = line.trimmingCharacters(in: .whitespaces)
        if t.hasPrefix("<?xml") { return xml }
        let l = t.lowercased()
        if l.hasPrefix("<!doctype html") || l.hasPrefix("<html") { return html }
        if t.hasPrefix("diff --git ") || t.hasPrefix("diff -") || t.hasPrefix("Index: ") { return diff }
        return nil
    }

    /// Files known by their whole name, as given and in lower case.
    private static let byName: [String: CodeLanguage] = {
        var m: [String: CodeLanguage] = [:]
        func add(_ l: CodeLanguage, _ names: String) {
            for n in names.split(separator: " ") { m[String(n)] = l }
        }
        add(makefile, "Makefile makefile GNUmakefile BSDmakefile Kbuild")
        add(dockerfile, "Dockerfile dockerfile Containerfile containerfile")
        add(goModule, "go.mod go.work")
        add(plain, "go.sum go.work.sum")
        add(ignore, ".gitignore .dockerignore .npmignore .hgignore .eslintignore .prettierignore .stylelintignore"
            + " .vscodeignore .gcloudignore .helmignore .slugignore .rgignore .ignore .fdignore .gitattributes CODEOWNERS")
        add(shell, ".bashrc .bash_profile .bash_login .bash_logout .bash_aliases .profile .zshrc .zshenv .zprofile"
            + " .zlogin .zlogout .kshrc .mkshrc .envrc .xprofile .xinitrc .xsession PKGBUILD APKBUILD")
        add(dotenv, ".env")
        add(conf, ".gitconfig .gitmodules .npmrc .yarnrc .pypirc .flake8 .pylintrc .coveragerc .wgetrc .curlrc"
            + " .inputrc .editorconfig .hgrc .htaccess .tmux.conf .ackrc")
        add(ruby, "Gemfile Rakefile Podfile Vagrantfile Brewfile Fastfile Appfile Guardfile Capfile Dangerfile"
            + " Berksfile Thorfile Steepfile .irbrc .pryrc")
        add(python, "BUILD BUILD.bazel WORKSPACE WORKSPACE.bazel SConstruct SConscript .pythonrc")
        add(toml, "Cargo.lock Pipfile poetry.lock uv.lock")
        add(jsonc, ".babelrc .eslintrc .jshintrc .swcrc devcontainer.json .devcontainer.json")
        add(yaml, ".clang-format .clang-tidy .yamllint")
        add(lua, ".luacheckrc")
        return m
    }()

    /// Files known by their extension, in lower case.
    private static let byExtension: [String: CodeLanguage] = {
        var m: [String: CodeLanguage] = [:]
        func add(_ l: CodeLanguage, _ exts: String) {
            for e in exts.split(separator: " ") { m[String(e)] = l }
        }
        add(go, "go")
        add(swift, "swift swiftinterface")
        add(python, "py pyw pyi bzl")
        add(javascript, "js mjs cjs jsx")
        add(typescript, "ts mts cts")
        add(tsx, "tsx")
        add(json, "json geojson webmanifest har ipynb jsonl ndjson")
        add(jsonc, "jsonc json5")
        add(yaml, "yaml yml cff")
        add(toml, "toml")
        add(ini, "ini cnf")
        add(conf, "cfg conf properties desktop service socket timer target mount automount path slice network netdev link")
        add(dotenv, "env")
        add(markdown, "md markdown mdown mkd mkdn mdwn mdx")
        add(shell, "sh bash zsh ksh command bats ebuild eclass")
        add(rust, "rs")
        add(c, "c h")
        add(cpp, "cc cpp cxx c++ hh hpp hxx h++ ipp tpp inl ino cu cuh metal")
        add(objc, "m")
        add(objcpp, "mm")
        add(java, "java")
        add(kotlin, "kt kts")
        add(ruby, "rb rake gemspec ru podspec rbi jbuilder")
        add(html, "html htm xhtml shtml vue svelte erb hbs handlebars mustache ejs njk")
        add(xml, "xml xsd xsl xslt wsdl rss atom xib storyboard csproj vbproj fsproj vcxproj props targets resx xaml"
            + " kml gpx opml fxml xliff xlf")
        add(plist, "plist entitlements mobileconfig")
        add(svg, "svg")
        add(css, "css pcss postcss")
        add(scss, "scss sass")
        add(less, "less")
        add(sql, "sql psql pgsql mysql ddl dml")
        add(makefile, "mk mak make")
        add(dockerfile, "dockerfile")
        add(lua, "lua rockspec wlua")
        add(diff, "diff patch rej debdiff")
        add(plain, "txt text log")
        return m
    }()
}

// MARK: The colorer

/// Where a line starts: inside a comment, a string or a fence, and the like,
/// as its lexer reads that.
struct SyntaxState: Hashable {
    /// What the line starts inside, 0 for nothing.
    var mode: UInt8 = 0
    /// Which of it, such as a string's quotes.
    var kind: UInt8 = 0
    /// A count: a comment's nesting, a raw string's hashes, a word's length.
    var count: UInt8 = 0
    /// For HTML, the script or style a line starts inside, whose own lexer's
    /// state the rest is; for Markdown, that the text's first line is past;
    /// for a Dockerfile, that the line goes on with the instruction before.
    var outer: UInt8 = 0
    /// A heredoc word's hash, a diff hunk's lines to go, CSS's nesting.
    var data: UInt32 = 0
}

/// A coloured run of a line, from the line's start.
struct SyntaxRun: Equatable {
    var start: UInt16
    var length: UInt16
    var kind: CodeToken
}

/// Colours a text a line at a time, keeping each line's start state and
/// tokens, so an edit re-lexes only the lines it reaches.
final class SyntaxColorer {
    let language: CodeLanguage
    /// Each line's first unit, the state it starts in, and its runs.
    private(set) var starts: [Int] = [0]
    private(set) var states: [SyntaxState] = [SyntaxState()]
    private(set) var runs: [[SyntaxRun]] = [[]]
    /// The text's length when last seen.
    private(set) var length = 0
    /// Where a line's runs are written while it is lexed.
    private let scratch: UnsafeMutablePointer<SyntaxToken>
    private var reader = SyntaxReader()

    /// How much of a line is lexed.
    static let longest = 4000

    init(language: CodeLanguage) {
        self.language = language
        scratch = .allocate(capacity: SyntaxColorer.longest + 8)
    }

    deinit {
        scratch.deallocate()
        reader.free()
    }

    /// The whole text afresh.
    func load(_ text: NSString) {
        length = text.length
        reader.reset(text)
        var starts: [Int] = [], states: [SyntaxState] = [], runs: [[SyntaxRun]] = []
        let guess = length / 32 + 1
        starts.reserveCapacity(guess)
        states.reserveCapacity(guess)
        runs.reserveCapacity(guess)
        var st = SyntaxState()
        var p = 0
        while true {
            let (end, next) = reader.line(p)
            starts.append(p)
            states.append(st)
            runs.append(lex(p, end, &st))
            if end >= length { break }
            p = next
        }
        self.starts = starts
        self.states = states
        self.runs = runs
        reader.reset(nil)
    }

    /// After an edit, NSTextStorage-style: text is the text now, editedRange
    /// is in the new text, delta is its change in length. Re-lexes from the
    /// first line touched until a line's start state matches the cached one
    /// past the edit. Returns the range (whole lines) whose tokens may have
    /// changed, so the caller recolours just that.
    @discardableResult
    func edit(_ text: NSString, editedRange: NSRange, delta: Int) -> NSRange {
        let newLength = text.length
        let p = editedRange.location
        let newEnd = p + editedRange.length
        guard newLength - delta == length, p >= 0, editedRange.length >= max(delta, 0), newEnd <= newLength,
              newEnd - delta <= length, !starts.isEmpty else {
            load(text)
            return NSRange(location: 0, length: newLength)
        }
        // The line before the edit may end in a \r that a \n put in now joins,
        // so lexing starts at the line holding the unit before it.
        let first = line(containing: max(p - 1, 0))
        let from = starts[first]
        reader.reset(text)
        length = newLength
        var st = states[first]
        var newStarts: [Int] = [], newStates: [SyntaxState] = [], newRuns: [[SyntaxRun]] = []
        var q = from
        var j = first + 1
        let count = starts.count
        var until = count   // the first old line kept
        while true {
            let (end, next) = reader.line(q)
            newStarts.append(q)
            newStates.append(st)
            newRuns.append(lex(q, end, &st))
            if end >= newLength { break }
            q = next
            // Past the edit, a line that starts where an old one did, in the
            // state that one did, lexes as it did, and so does every line after.
            if q >= newEnd {
                let old = q - delta
                while j < count && starts[j] < old { j += 1 }
                if j < count && starts[j] == old && states[j] == st {
                    until = j
                    break
                }
            }
        }
        let kept = count - until
        starts.replaceSubrange(first..<until, with: newStarts)
        states.replaceSubrange(first..<until, with: newStates)
        runs.replaceSubrange(first..<until, with: newRuns)
        if kept > 0 && delta != 0 {
            let at = starts.count - kept
            starts.withUnsafeMutableBufferPointer { b in
                var k = at
                while k < b.count {
                    b[k] += delta
                    k += 1
                }
            }
        }
        reader.reset(nil)
        let end = kept > 0 ? starts[starts.count - kept] : newLength
        return NSRange(location: from, length: end - from)
    }

    /// The tokens in a range, absolute UTF-16 ranges clipped to it; plain
    /// tokens are skipped.
    func tokens(in range: NSRange, _ body: (NSRange, CodeToken) -> Void) {
        let lo = max(0, range.location), hi = min(length, range.location + range.length)
        guard lo < hi else { return }
        var li = line(containing: lo)
        while li < starts.count {
            let base = starts[li]
            if base >= hi { break }
            for r in runs[li] {
                let a = base + Int(truncatingIfNeeded: r.start)
                let b = a + Int(truncatingIfNeeded: r.length)
                if b <= lo { continue }
                if a >= hi { break }
                let s = max(a, lo)
                body(NSRange(location: s, length: min(b, hi) - s), r.kind)
            }
            li += 1
        }
    }

    /// The last line starting at or before unit x.
    private func line(containing x: Int) -> Int {
        var lo = 0, hi = starts.count - 1
        while lo < hi {
            let mid = (lo + hi + 1) / 2
            if starts[mid] <= x { lo = mid } else { hi = mid - 1 }
        }
        return lo
    }

    /// Lexes the line from unit p to end, from state st, which it moves on.
    private func lex(_ p: Int, _ end: Int, _ st: inout SyntaxState) -> [SyntaxRun] {
        var n = end - p
        if n > SyntaxColorer.longest { n = SyntaxColorer.longest }
        let s = reader.hold(p, p + n)
        // A cut never splits a surrogate pair.
        if n == SyntaxColorer.longest && s[n - 1] >= 0xD800 && s[n - 1] <= 0xDBFF { n -= 1 }
        var out = SyntaxSink(buf: scratch)
        Lex.line(s, 0, n, &st, language.rules, &out)
        let count = out.count
        if count == 0 { return [] }
        let buf = scratch
        return [SyntaxRun](unsafeUninitializedCapacity: count) { b, made in
            var k = 0
            while k < count {
                let t = buf[k]
                b[k] = SyntaxRun(start: UInt16(truncatingIfNeeded: t.start),
                                 length: UInt16(truncatingIfNeeded: t.end - t.start), kind: t.kind)
                k += 1
            }
            made = count
        }
    }
}

/// Reads an NSString's units a window at a time, for the lexers to walk
/// through a pointer.
private struct SyntaxReader {
    static let window = 1 << 16
    private var text: NSString?
    private var length = 0
    private var buf: UnsafeMutablePointer<UInt16>
    private var cap = SyntaxReader.window
    private var lo = 0, hi = 0

    init() { buf = .allocate(capacity: SyntaxReader.window) }

    mutating func free() { buf.deallocate() }

    mutating func reset(_ t: NSString?) {
        text = t
        length = t?.length ?? 0
        lo = 0
        hi = 0
    }

    /// Units a to b in hand, at the pointer returned.
    mutating func hold(_ a: Int, _ b: Int) -> UnsafePointer<UInt16> {
        if a < lo || b > hi {
            let want = max(b - a, SyntaxReader.window)
            if want > cap {
                buf.deallocate()
                cap = want
                buf = .allocate(capacity: cap)
            }
            lo = a
            hi = min(length, a + want)
            if hi > lo { text?.getCharacters(buf, range: NSRange(location: lo, length: hi - lo)) }
        }
        return UnsafePointer(buf + (a - lo))
    }

    /// The line from unit p: where its text ends, and where the next line
    /// starts, past its \n, \r\n or \r. The last line ends at the length.
    mutating func line(_ p: Int) -> (end: Int, next: Int) {
        var q = p
        while q < length {
            if q < lo || q >= hi { _ = hold(q, min(length, q + SyntaxReader.window)) }
            var k = q - lo
            let m = hi - lo
            while k < m {
                let c = buf[k]
                if c == 10 { return (lo + k, lo + k + 1) }
                if c == 13 {
                    let e = lo + k
                    if e + 1 >= length { return (e, e + 1) }
                    if k + 1 < m { return (e, buf[k + 1] == 10 ? e + 2 : e + 1) }
                    _ = hold(e + 1, min(length, e + 1 + SyntaxReader.window))
                    return (e, buf[0] == 10 ? e + 2 : e + 1)
                }
                k += 1
            }
            q = hi
        }
        return (length, length)
    }
}

/// A run being made: where it starts and ends in the line, and its kind.
struct SyntaxToken {
    var start: Int
    var end: Int
    var kind: CodeToken
}

/// Where a lexer writes a line's runs, in order, joining a run to the one
/// before when they touch and are of one kind.
private struct SyntaxSink {
    let buf: UnsafeMutablePointer<SyntaxToken>
    var count = 0

    init(buf: UnsafeMutablePointer<SyntaxToken>) { self.buf = buf }

    mutating func add(_ a: Int, _ b: Int, _ kind: CodeToken) {
        if b <= a || kind == .plain { return }
        if count > 0 {
            let last = buf + (count - 1)
            if last.pointee.end == a && last.pointee.kind == kind {
                last.pointee.end = b
                return
            }
        }
        buf[count] = SyntaxToken(start: a, end: b, kind: kind)
        count += 1
    }

    /// Makes the runs written since mark one run of this kind.
    mutating func retag(since mark: Int, _ kind: CodeToken) {
        guard count > mark else { return }
        let a = buf[mark].start, b = buf[count - 1].end
        count = mark
        add(a, b, kind)
    }
}

// MARK: Rules and words

/// How a language's lines are lexed: by which lexer, with which words, and
/// what the code lexer looks for.
private final class SyntaxRules {
    enum Lexer { case plain, code, shell, make, docker, markup, css, markdown, yaml, toml, ini, diff, ignore }
    /// The code lexer's dialects, for their strings and other quirks.
    enum Dialect { case generic, go, swift, python, js, rust, c, java, kotlin, ruby, lua, sql, json }

    let lexer: Lexer
    let dialect: Dialect
    /// Keywords, built-in types, constants and the like.
    let words: SyntaxWords
    /// Words only a line's first can be, as a Dockerfile's FROM, or a
    /// Makefile's directives and functions.
    let heads: SyntaxWords?
    /// // and /* */ comments; # comments; -- comments; /* */ that nest.
    let slash, hash, dash, nests: Bool
    /// @name is an attribute; JSX tags; $ goes in names.
    let decorators, jsx, dollar: Bool
    /// A capitalised name is a type; and so before (; a name in capitals is
    /// a constant; v1.2.3 is a number, as in go.mod.
    let capitalTypes, capitalCalls, capsConstants, versions: Bool
    /// HTML's lexers for its scripts and styles.
    let script, style: SyntaxRules?

    init(_ lexer: Lexer, _ dialect: Dialect = .generic, words: SyntaxWords = .none, heads: SyntaxWords? = nil,
         slash: Bool = false, hash: Bool = false, dash: Bool = false, nests: Bool = false,
         decorators: Bool = false, jsx: Bool = false, dollar: Bool = false,
         capitalTypes: Bool = false, capitalCalls: Bool = false, capsConstants: Bool = false, versions: Bool = false,
         script: SyntaxRules? = nil, style: SyntaxRules? = nil) {
        self.lexer = lexer
        self.dialect = dialect
        self.words = words
        self.heads = heads
        self.slash = slash
        self.hash = hash
        self.dash = dash
        self.nests = nests
        self.decorators = decorators
        self.jsx = jsx
        self.dollar = dollar
        self.capitalTypes = capitalTypes
        self.capitalCalls = capitalCalls
        self.capsConstants = capsConstants
        self.versions = versions
        self.script = script
        self.style = style
    }
}

/// A language's words in a table of their own. Each word is packed seven
/// bits a letter, nine letters to a 64-bit key and two keys to a word, so a
/// lexer looks a name up as it reads it, with no string made.
private final class SyntaxWords {
    /// Three per slot: the word's two keys, then its value.
    private let slots: UnsafeMutablePointer<UInt64>
    private let mask: Int
    /// Whether case does not count, as in SQL.
    let folds: Bool

    /// The value's kind is in its low five bits; these mark a word that names
    /// a function or a type next, and one that stays itself after a dot.
    static let namesFunction: UInt8 = 32
    static let namesType: UInt8 = 64
    static let afterDot: UInt8 = 128

    static let none = SyntaxWords([])

    init(folds: Bool = false, _ kinds: [(CodeToken, String)], functions: String = "", types: String = "",
         afterDot: String = "") {
        self.folds = folds
        var values: [String: UInt8] = [:]
        for (kind, words) in kinds {
            for w in words.split(separator: " ") { values[String(w)] = kind.rawValue }
        }
        for (flag, words) in [(SyntaxWords.namesFunction, functions), (SyntaxWords.namesType, types),
                              (SyntaxWords.afterDot, afterDot)] {
            for w in words.split(separator: " ") { values[String(w), default: CodeToken.keyword.rawValue] |= flag }
        }
        var size = 16
        while size < values.count * 2 { size *= 2 }
        mask = size - 1
        slots = .allocate(capacity: size * 3)
        slots.initialize(repeating: 0, count: size * 3)
        for (w, v) in values {
            guard let (k0, k1) = SyntaxWords.key(w, folds: folds) else { continue }
            var i = SyntaxWords.slot(k0, k1, mask)
            while slots[i * 3] != 0 { i = (i + 1) & mask }
            slots[i * 3] = k0
            slots[i * 3 + 1] = k1
            slots[i * 3 + 2] = UInt64(v)
        }
    }

    /// A word's keys, as a lexer packs them while it reads: up to 18 ASCII
    /// letters, folded to lower case where case does not count.
    static func key(_ word: String, folds: Bool) -> (UInt64, UInt64)? {
        var k0: UInt64 = 0, k1: UInt64 = 0, n = 0
        for u in word.utf16 {
            guard u < 128, n < 18 else { return nil }
            var c = u
            if folds && c >= 65 && c <= 90 { c += 32 }
            if n < 9 { k0 = k0 << 7 | UInt64(c) } else { k1 = k1 << 7 | UInt64(c) }
            n += 1
        }
        return n == 0 ? nil : (k0, k1)
    }

    /// The keys of units a to b, or nil when they cannot be a word here.
    static func key(_ s: UnsafePointer<UInt16>, _ a: Int, _ b: Int, folds: Bool) -> (UInt64, UInt64)? {
        if b - a > 18 || b <= a { return nil }
        var k0: UInt64 = 0, k1: UInt64 = 0, i = a
        while i < b {
            var c = s[i]
            if c >= 128 { return nil }
            if folds && c >= 65 && c <= 90 { c += 32 }
            if i - a < 9 { k0 = k0 << 7 | UInt64(truncatingIfNeeded: c) } else { k1 = k1 << 7 | UInt64(truncatingIfNeeded: c) }
            i += 1
        }
        return (k0, k1)
    }

    private static func slot(_ k0: UInt64, _ k1: UInt64, _ mask: Int) -> Int {
        let h = (k0 &* 0x9E37_79B9_7F4A_7C15) ^ (k1 &* 0xC2B2_AE3D_27D4_EB4F)
        return Int(truncatingIfNeeded: h >> 40) & mask
    }

    /// The word's value, 0 for none.
    func find(_ k0: UInt64, _ k1: UInt64) -> UInt8 {
        var i = SyntaxWords.slot(k0, k1, mask)
        while true {
            let p = slots + i * 3
            let a = p[0]
            if a == 0 { return 0 }
            if a == k0 && p[1] == k1 { return UInt8(truncatingIfNeeded: p[2]) }
            i = (i + 1) & mask
        }
    }

    /// The value of the word from a to b, 0 for none.
    func find(_ s: UnsafePointer<UInt16>, _ a: Int, _ b: Int) -> UInt8 {
        guard let (k0, k1) = SyntaxWords.key(s, a, b, folds: folds) else { return 0 }
        return find(k0, k1)
    }
}

extension SyntaxWords {
    static let go = SyntaxWords([
        (.keyword, "break case chan const continue default defer else fallthrough for func go goto if import interface"
            + " map package range return select struct switch type var"),
        (.type, "any bool byte comparable complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune"
            + " string uint uint8 uint16 uint32 uint64 uintptr"),
        (.constant, "true false nil iota"),
    ], functions: "func", types: "type")

    static let goModule = SyntaxWords([
        (.keyword, "module go require replace exclude retract toolchain godebug use tool ignore"),
    ])

    static let swift = SyntaxWords([
        (.keyword, "actor any as associatedtype async await borrowing break case catch class consuming continue"
            + " convenience default defer deinit didSet do dynamic else enum extension fallthrough fileprivate final for"
            + " func get guard if import in indirect infix init inout internal is isolated lazy let macro mutating"
            + " nonisolated nonmutating open operator optional override package postfix precedencegroup prefix private"
            + " protocol public repeat required rethrows return self Self set some static struct subscript super switch"
            + " throw throws try typealias unowned var weak where while willSet"),
        (.constant, "true false nil"),
    ], functions: "func", types: "actor associatedtype class enum extension protocol struct typealias",
       afterDot: "self init")

    static let python = SyntaxWords([
        (.keyword, "and as assert async await break class continue def del elif else except finally for from global"
            + " if import in is lambda nonlocal not or pass raise return try while with yield"),
        (.type, "bool bytearray bytes complex dict float frozenset int list memoryview object range set slice str"
            + " tuple type"),
        (.constant, "True False None NotImplemented Ellipsis __debug__"),
        (.variable, "self cls"),
    ], functions: "def", types: "class")

    private static let jsKeywords = "as async await break case catch class const continue debugger default delete do"
        + " else export extends finally for from function get if import in instanceof let new of return set static"
        + " super switch this throw try typeof var void while with yield"

    static let javascript = SyntaxWords([
        (.keyword, jsKeywords),
        (.constant, "true false null undefined NaN Infinity"),
    ], functions: "function", types: "class")

    static let typescript = SyntaxWords([
        (.keyword, jsKeywords + " abstract accessor asserts declare enum implements infer interface is keyof module"
            + " namespace out override private protected public readonly satisfies type unique"),
        (.type, "any bigint boolean never number object string symbol unknown void"),
        (.constant, "true false null undefined NaN Infinity"),
    ], functions: "function", types: "class enum interface namespace type")

    static let json = SyntaxWords([(.constant, "true false null")])

    static let rust = SyntaxWords([
        (.keyword, "as async await break const continue crate dyn else enum extern fn for if impl in let loop"
            + " macro_rules match mod move mut pub ref return self Self static struct super trait type union unsafe"
            + " use where while yield"),
        (.type, "bool char f32 f64 i8 i16 i32 i64 i128 isize str u8 u16 u32 u64 u128 usize"),
        (.constant, "true false None"),
    ], functions: "fn", types: "enum struct trait type union", afterDot: "await")

    /// C, C++ and Objective-C in one, since a header could be any of them.
    static let c = SyntaxWords([
        (.keyword, "auto break case const continue default do else enum extern for goto if inline register restrict"
            + " return sizeof static struct switch typedef union volatile while _Alignas _Alignof _Atomic _Generic"
            + " _Noreturn _Static_assert _Thread_local alignas alignof static_assert thread_local typeof"
            + " and and_eq asm bitand bitor catch class co_await co_return co_yield compl concept const_cast consteval"
            + " constexpr constinit decltype delete dynamic_cast explicit export final friend mutable namespace new"
            + " noexcept not not_eq operator or or_eq override private protected public reinterpret_cast requires"
            + " static_cast template this throw try typeid typename using virtual xor xor_eq self super in"),
        (.type, "bool char char8_t char16_t char32_t double float int long short signed unsigned void wchar_t size_t"
            + " ssize_t ptrdiff_t intptr_t uintptr_t int8_t int16_t int32_t int64_t uint8_t uint16_t uint32_t"
            + " uint64_t FILE va_list _Bool _Complex id instancetype BOOL SEL IMP Class NSInteger NSUInteger CGFloat"),
        (.constant, "true false NULL nullptr nil Nil YES NO"),
    ], types: "class enum namespace struct union")

    static let java = SyntaxWords([
        (.keyword, "abstract assert break case catch class const continue default do else enum extends final finally"
            + " for goto if implements import instanceof interface native new package permits private protected"
            + " public record return sealed static strictfp super switch synchronized this throw throws transient try"
            + " var volatile while yield"),
        (.type, "boolean byte char double float int long short void"),
        (.constant, "true false null"),
    ], types: "class enum interface record")

    static let kotlin = SyntaxWords([
        (.keyword, "abstract actual annotation as break by catch class companion const constructor continue"
            + " crossinline data do else enum expect external final finally for fun get if import in infix init inline"
            + " inner interface internal is lateinit noinline object open operator out override package private"
            + " protected public reified return sealed set super suspend tailrec this throw try typealias typeof val"
            + " var vararg when where while"),
        (.constant, "true false null"),
    ], functions: "fun", types: "class interface object typealias")

    static let ruby = SyntaxWords([
        (.keyword, "BEGIN END alias and begin break case class def defined? do else elsif end ensure for if in module"
            + " next not or redo rescue retry return self super then undef unless until when while yield __method__"
            + " __FILE__ __LINE__ __ENCODING__ __dir__ require require_relative include extend prepend attr_reader"
            + " attr_writer attr_accessor private protected public module_function raise loop lambda proc"),
        (.constant, "true false nil"),
    ], functions: "def", types: "class module")

    static let lua = SyntaxWords([
        (.keyword, "and break do else elseif end for function goto if in local not or repeat return then until while"),
        (.constant, "true false nil"),
        (.variable, "self"),
    ], functions: "function")

    static let sql = SyntaxWords(folds: true, [
        (.keyword, "add all alter analyze and as asc begin between by cascade case check column commit constraint"
            + " create cross database declare default delete desc distinct do drop each else end except execute"
            + " exists explain extension fetch first for foreign from full function grant group having if ilike in"
            + " index inner insert intersect into is join key language last lateral left like limit loop materialized"
            + " natural next not nulls of offset on only or order outer over partition precision primary procedure"
            + " recursive references replace return returning returns revoke right rollback row rows schema select"
            + " sequence set table temp temporary then to transaction trigger truncate union unique unlogged update"
            + " using values view when where while window with"),
        (.type, "array bigint bigserial binary bit blob bool boolean bytea char character cidr clob date datetime"
            + " decimal double enum float inet int int2 int4 int8 integer interval json jsonb money nchar numeric"
            + " nvarchar real serial smallint smallserial text time timestamp timestamptz tinyint uuid varbinary varchar"
            + " xml"),
        (.constant, "null true false"),
    ])

    static let shell = SyntaxWords([
        (.keyword, "if then else elif fi case esac for select while until do done in function time coproc return"
            + " break continue exit"),
        (.function, "alias bg bind builtin caller cd command compgen complete declare dirs disown echo enable eval exec"
            + " export fc fg getopts hash help history jobs kill let local logout mapfile popd printf pushd pwd read"
            + " readarray readonly set shift shopt source suspend test times trap type typeset ulimit umask unalias"
            + " unset wait autoload bindkey compdef emulate setopt unsetopt zle zmodload zstyle print whence true false"),
    ], functions: "function")

    static let docker = SyntaxWords(folds: true, [
        (.keyword, "from run cmd label maintainer expose env add copy entrypoint volume user workdir arg onbuild"
            + " stopsignal healthcheck shell"),
    ])

    /// A Makefile's directives, and the functions $( ) calls.
    static let make = SyntaxWords([
        (.keyword, "include -include sinclude ifeq ifneq ifdef ifndef else endif define endef export unexport override"
            + " vpath private undefine"),
        (.function, "abspath addprefix addsuffix and basename call dir error eval file filter filter-out findstring"
            + " firstword flavor foreach guile if info intcmp join lastword let notdir or origin patsubst realpath"
            + " shell sort strip subst suffix value warning wildcard word wordlist words"),
    ])

    static let yaml = SyntaxWords(folds: true, [(.constant, "true false yes no on off null ~")])
    static let toml = SyntaxWords([(.constant, "true false"), (.number, "inf nan +inf -inf +nan -nan")])
    static let ini = SyntaxWords(folds: true, [(.constant, "true false yes no on off null none")])
}

// MARK: Lexers

/// The lexers, one per kind of file, each reading a line from `from` to `n`
/// at s and moving its state on.
private enum Lex {
    /// What a line starts inside, in SyntaxState.mode.
    enum Mode {
        static let block: UInt8 = 1      // a /* */ comment; count: its nesting
        static let long: UInt8 = 2       // Lua's --[[ ]] comment; count: its level
        static let doc: UInt8 = 3        // Ruby's =begin and =end
        static let string: UInt8 = 4     // kind: Str; count and data: its delimiter
        static let heredoc: UInt8 = 5    // kind: 1 for an indented end, 2 for no variables; count, data: the word
        static let jsx: UInt8 = 6        // a JSX tag's attributes
        static let define: UInt8 = 7     // a Makefile's define, to endef
        static let comment: UInt8 = 8    // <!-- -->
        static let tag: UInt8 = 9        // a tag's attributes; kind: script 1, style 2; count: a quote open
        static let cdata: UInt8 = 10     // <![CDATA[ ]]>
        static let fence: UInt8 = 11     // Markdown's ``` or ~~~; kind: 1 or 2; data: its length
        static let front: UInt8 = 12     // Markdown's front matter
        static let scalar: UInt8 = 13    // YAML's | and >; data: the indent its lines pass
    }

    /// String kinds, in SyntaxState.kind.
    enum Str {
        static let dq: UInt8 = 1         // "…" on one line, with \ escapes
        static let sq: UInt8 = 2         // '…' on one line, with \ escapes
        static let dqLines: UInt8 = 3    // "…" over lines, with \ escapes
        static let swift: UInt8 = 4      // "…" with \( ); count: its #s
        static let swiftLines: UInt8 = 5 // """…""" with \( ); count: its #s
        static let py: UInt8 = 6         // '…' or "…"; count: 1 raw, 2 f-string, 4 single quote
        static let pyLines: UInt8 = 7    // ''' or """, the same
        static let template: UInt8 = 8   // JavaScript's `…` with ${ }
        static let raw: UInt8 = 9        // Go's `…`
        static let rustRaw: UInt8 = 10   // r#"…"#; count: its #s
        static let lua: UInt8 = 11       // [==[…]==]; count: its level
        static let cppRaw: UInt8 = 12    // R"d(…)d"; count, data: d's length and hash
        static let kotlin: UInt8 = 13    // "…" with $name and ${ }
        static let kotlinLines: UInt8 = 14 // """…""" with $name and ${ }, no escapes
        static let textBlock: UInt8 = 15 // """…""" with \ escapes, as Java and TOML have
        static let ruby: UInt8 = 16      // "…" over lines with #{ }; count 1 for `…`
        static let rubySq: UInt8 = 17    // '…' over lines, where only \\ and \' escape
        static let sql: UInt8 = 18       // '…' over lines, '' for a quote
        static let literal: UInt8 = 19   // '…' on one line, no escapes
        static let literalLines: UInt8 = 20 // '''…''', no escapes
        static let shSq: UInt8 = 21      // the shell's '…' over lines
        static let shDq: UInt8 = 22      // the shell's "…" over lines, with $variables
        static let shAnsi: UInt8 = 23    // the shell's $'…', with \ escapes
    }

    /// Lexes a line with the language's lexer.
    static func line(_ s: UnsafePointer<UInt16>, _ from: Int, _ n: Int, _ st: inout SyntaxState,
                     _ r: SyntaxRules, _ out: inout SyntaxSink) {
        switch r.lexer {
        case .plain: return
        case .code: code(s, from, n, &st, r, &out)
        case .shell, .docker: shell(s, from, n, &st, r, &out, make: false)
        case .make: make(s, n, &st, r, &out)
        case .markup: markup(s, from, n, &st, r, &out)
        case .css: css(s, from, n, &st, r, &out)
        case .markdown: markdown(s, n, &st, &out)
        case .yaml: yaml(s, n, &st, &out)
        case .toml: toml(s, n, &st, &out)
        case .ini: ini(s, n, &out)
        case .diff: diff(s, n, &st, &out)
        case .ignore: ignore(s, n, &out)
        }
    }

    // MARK: Characters

    static func isDigit(_ c: UInt16) -> Bool { c >= 48 && c <= 57 }
    static func isLetter(_ c: UInt16) -> Bool { (c >= 97 && c <= 122) || (c >= 65 && c <= 90) }
    static func isUpper(_ c: UInt16) -> Bool { c >= 65 && c <= 90 }
    /// A name's first character: a letter, _, or anything past ASCII.
    static func isWordStart(_ c: UInt16) -> Bool { isLetter(c) || c == 95 || c >= 128 }
    static func isWord(_ c: UInt16) -> Bool { isLetter(c) || isDigit(c) || c == 95 || c >= 128 }
    static func isAlnum(_ c: UInt16) -> Bool { isLetter(c) || isDigit(c) || c >= 128 }
    static func isHex(_ c: UInt16) -> Bool { isDigit(c) || (c >= 97 && c <= 102) || (c >= 65 && c <= 70) }
    static func isSpace(_ c: UInt16) -> Bool { c == 32 || c == 9 || c == 12 }
    static func isHigh(_ c: UInt16) -> Bool { c >= 0xD800 && c <= 0xDBFF }

    /// Whether the units at i spell lit, an ASCII literal.
    static func has(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int, _ lit: StaticString) -> Bool {
        let count = lit.utf8CodeUnitCount
        if i + count > n { return false }
        let p = lit.utf8Start
        var k = 0
        while k < count {
            if s[i + k] != UInt16(truncatingIfNeeded: p[k]) { return false }
            k += 1
        }
        return true
    }

    /// The same, with case not counting; lit is in lower case.
    static func hasFolded(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int, _ lit: StaticString) -> Bool {
        let count = lit.utf8CodeUnitCount
        if i + count > n { return false }
        let p = lit.utf8Start
        var k = 0
        while k < count {
            var c = s[i + k]
            if isUpper(c) { c += 32 }
            if c != UInt16(truncatingIfNeeded: p[k]) { return false }
            k += 1
        }
        return true
    }

    /// Where lit next starts from i, or -1.
    static func find(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int, _ lit: StaticString) -> Int {
        var j = i
        let first = UInt16(truncatingIfNeeded: lit.utf8Start[0])
        while j < n {
            if s[j] == first && has(s, j, n, lit) { return j }
            j += 1
        }
        return -1
    }

    /// FNV-1a of units a to b, for a heredoc's word.
    static func hash(_ s: UnsafePointer<UInt16>, _ a: Int, _ b: Int) -> UInt32 {
        var h: UInt32 = 2_166_136_261
        var i = a
        while i < b {
            h = (h ^ UInt32(truncatingIfNeeded: s[i])) &* 16_777_619
            i += 1
        }
        return h
    }

    /// The bracket matching the one at i, skipping quoted strings inside, or
    /// -1 when the line ends first.
    static func closing(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int, _ open: UInt16, _ close: UInt16) -> Int {
        var depth = 0
        var j = i
        while j < n {
            let c = s[j]
            if c == open {
                depth += 1
            } else if c == close {
                depth -= 1
                if depth == 0 { return j }
            } else if c == 34 || c == 39 {
                var k = j + 1
                while k < n && s[k] != c {
                    if s[k] == 92 { k += 1 }
                    k += 1
                }
                j = k
            }
            j += 1
        }
        return -1
    }

    /// A number from i: decimal, hex, octal or binary, with a fraction, an
    /// exponent and a suffix such as u32, f or px.
    static func number(_ s: UnsafePointer<UInt16>, _ i0: Int, _ n: Int) -> Int {
        var i = i0
        if s[i] == 48 && i + 1 < n && (s[i + 1] == 120 || s[i + 1] == 88) {
            i += 2
            while i < n && (isHex(s[i]) || s[i] == 95) { i += 1 }
            if i + 1 < n && (s[i] == 112 || s[i] == 80) && (isDigit(s[i + 1]) || s[i + 1] == 43 || s[i + 1] == 45) {
                i += 2
                while i < n && isDigit(s[i]) { i += 1 }
            }
        } else {
            while i < n && (isDigit(s[i]) || s[i] == 95) { i += 1 }
            if i + 1 < n && s[i] == 46 && isDigit(s[i + 1]) {
                i += 1
                while i < n && (isDigit(s[i]) || s[i] == 95) { i += 1 }
            }
            if i < n && (s[i] == 101 || s[i] == 69) {
                var e = i + 1
                if e < n && (s[e] == 43 || s[e] == 45) { e += 1 }
                if e < n && isDigit(s[e]) {
                    i = e
                    while i < n && isDigit(s[i]) { i += 1 }
                }
            }
        }
        while i < n && (isLetter(s[i]) || isDigit(s[i]) || s[i] == 95) { i += 1 }
        return i
    }

    /// The end of the escape whose backslash is just before b.
    static func escapeEnd(_ s: UnsafePointer<UInt16>, _ b: Int, _ n: Int) -> Int {
        let c = s[b]
        var e = b + 1
        if c == 120 {          // \xHH
            while e < n && e < b + 3 && isHex(s[e]) { e += 1 }
        } else if c == 117 {   // \uHHHH or \u{…}
            if e < n && s[e] == 123 {
                while e < n && s[e] != 125 { e += 1 }
                e = min(e + 1, n)
            } else {
                while e < n && e < b + 5 && isHex(s[e]) { e += 1 }
            }
        } else if c == 85 {    // \UHHHHHHHH
            while e < n && e < b + 9 && isHex(s[e]) { e += 1 }
        } else if c >= 48 && c <= 55 {
            while e < n && e < b + 3 && s[e] >= 48 && s[e] <= 55 { e += 1 }
        } else if isHigh(c) && e < n {
            e += 1
        }
        return e
    }

    // MARK: Comments and strings

    /// A /* */ comment from i, past its opening, to its end or the line's.
    static func blockComment(_ s: UnsafePointer<UInt16>, _ i0: Int, _ n: Int, _ from: Int, _ depth0: UInt8,
                             _ nests: Bool, _ st: inout SyntaxState, _ out: inout SyntaxSink) -> Int {
        var i = i0
        var depth = depth0
        while i < n {
            let c = s[i]
            if c == 42 && i + 1 < n && s[i + 1] == 47 {
                i += 2
                if depth <= 1 {
                    out.add(from, i, .comment)
                    st.mode = 0
                    st.count = 0
                    return i
                }
                depth -= 1
                continue
            }
            if nests && c == 47 && i + 1 < n && s[i + 1] == 42 {
                i += 2
                if depth < 255 { depth += 1 }
                continue
            }
            i += 1
        }
        out.add(from, n, .comment)
        st.mode = Mode.block
        st.count = depth
        return n
    }

    /// Lua's [==[ at j: its level, the number of =s, or -1.
    static func longOpen(_ s: UnsafePointer<UInt16>, _ j: Int, _ n: Int) -> Int {
        guard j < n && s[j] == 91 else { return -1 }
        var k = j + 1
        while k < n && s[k] == 61 { k += 1 }
        return k < n && s[k] == 91 && k - j - 1 < 255 ? k - j - 1 : -1
    }

    /// Lua's long string or comment from i, past its opening, to its ]==] or
    /// the line's end.
    static func longBracket(_ s: UnsafePointer<UInt16>, _ i0: Int, _ n: Int, _ from: Int, _ level: UInt8,
                            _ kind: CodeToken, _ st: inout SyntaxState, _ out: inout SyntaxSink) -> Int {
        let lv = Int(truncatingIfNeeded: level)
        var i = i0
        while i < n {
            if s[i] == 93 {
                var k = 0
                while k < lv && i + 1 + k < n && s[i + 1 + k] == 61 { k += 1 }
                if k == lv && i + 1 + lv < n && s[i + 1 + lv] == 93 {
                    let e = i + lv + 2
                    out.add(from, e, kind)
                    st.mode = 0
                    st.kind = 0
                    st.count = 0
                    return e
                }
            }
            i += 1
        }
        out.add(from, n, kind)
        st.mode = kind == .comment ? Mode.long : Mode.string
        st.kind = kind == .comment ? 0 : Str.lua
        st.count = level
        return n
    }

    /// C++'s raw string from i, past R"d(, to )d" or the line's end.
    static func cppRaw(_ s: UnsafePointer<UInt16>, _ i0: Int, _ n: Int, _ from: Int, _ len: Int, _ h: UInt32,
                       _ st: inout SyntaxState, _ out: inout SyntaxSink) -> Int {
        var i = i0
        while i < n {
            if s[i] == 41 && i + 1 + len < n && s[i + 1 + len] == 34 && hash(s, i + 1, i + 1 + len) == h {
                let e = i + len + 2
                out.add(from, e, .string)
                st.mode = 0
                st.kind = 0
                st.count = 0
                st.data = 0
                return e
            }
            i += 1
        }
        out.add(from, n, .string)
        st.mode = Mode.string
        st.kind = Str.cppRaw
        st.count = UInt8(truncatingIfNeeded: len)
        st.data = h
        return n
    }

    /// A string from i, just past its opening, to its end or the line's: its
    /// text as string, its escapes and interpolations as escape. One that can
    /// go on past the line leaves the state there.
    static func string(_ s: UnsafePointer<UInt16>, _ i0: Int, _ n: Int, _ from: Int, _ kind: UInt8, _ count: UInt8,
                       _ data: UInt32, _ st: inout SyntaxState, _ out: inout SyntaxSink) -> Int {
        if kind == Str.lua { return longBracket(s, i0, n, from, count, .string, &st, &out) }
        if kind == Str.cppRaw { return cppRaw(s, i0, n, from, Int(truncatingIfNeeded: count), data, &st, &out) }
        var q: UInt16 = 34       // the quote that ends it
        var triple = false       // three of them
        var hashes = 0           // and so many #s after
        var esc = true           // \ escapes
        var lines = false        // it can go on past the line
        var interp: UInt8 = 0    // 1 \( ), 2 ${ }, 3 $name and ${ }, 4 #{ }, 5 { }, 6 the shell's $
        switch kind {
        case Str.sq: q = 39
        case Str.dqLines: lines = true
        case Str.swift: hashes = Int(truncatingIfNeeded: count); interp = 1
        case Str.swiftLines: hashes = Int(truncatingIfNeeded: count); interp = 1; triple = true; lines = true
        case Str.py, Str.pyLines:
            q = count & 4 != 0 ? 39 : 34
            esc = count & 1 == 0
            interp = count & 2 != 0 ? 5 : 0
            if kind == Str.pyLines { triple = true; lines = true }
        case Str.template: q = 96; interp = 2; lines = true
        case Str.raw: q = 96; esc = false; lines = true
        case Str.rustRaw: hashes = Int(truncatingIfNeeded: count); esc = false; lines = true
        case Str.kotlin: interp = 3
        case Str.kotlinLines: triple = true; esc = false; interp = 3; lines = true
        case Str.textBlock: triple = true; lines = true
        case Str.ruby: q = count == 1 ? 96 : 34; interp = 4; lines = true
        case Str.rubySq: q = 39; lines = true
        case Str.sql: q = 39; esc = false; lines = true
        case Str.literal: q = 39; esc = false
        case Str.literalLines: q = 39; esc = false; triple = true; lines = true
        case Str.shSq: q = 39; esc = false; lines = true
        case Str.shDq: interp = 6; lines = true
        case Str.shAnsi: q = 39; lines = true
        default: break
        }
        var i = i0
        var seg = from
        while i < n {
            let c = s[i]
            if c == q {
                var e = i + 1
                var ends = true
                if triple {
                    if i + 2 < n && s[i + 1] == q && s[i + 2] == q { e = i + 3 } else { ends = false }
                }
                if ends && hashes > 0 {
                    var k = 0
                    while k < hashes && e + k < n && s[e + k] == 35 { k += 1 }
                    if k == hashes { e += hashes } else { ends = false }
                }
                if ends && kind == Str.sql && e < n && s[e] == 39 {
                    out.add(seg, i, .string)
                    out.add(i, i + 2, .escape)
                    i += 2
                    seg = i
                    continue
                }
                if ends {
                    out.add(seg, e, .string)
                    st.mode = 0
                    st.kind = 0
                    st.count = 0
                    return e
                }
                i += 1
                continue
            }
            if c == 92 && esc {
                var b = i + 1
                // A raw Swift string escapes with \ and as many #s.
                if hashes > 0 && (kind == Str.swift || kind == Str.swiftLines) {
                    var k = 0
                    while k < hashes && b < n && s[b] == 35 { k += 1; b += 1 }
                    if k < hashes { i += 1; continue }
                }
                if b >= n {
                    out.add(seg, i, .string)
                    out.add(i, n, .escape)
                    seg = n
                    i = n
                    break
                }
                if interp == 1 && s[b] == 40 {
                    let m = closing(s, b, n, 40, 41)
                    let e = m < 0 ? n : m + 1
                    out.add(seg, i, .string)
                    out.add(i, e, .escape)
                    i = e
                    seg = e
                    continue
                }
                if kind == Str.rubySq && s[b] != 92 && s[b] != 39 { i += 1; continue }
                let e = escapeEnd(s, b, n)
                out.add(seg, i, .string)
                out.add(i, e, .escape)
                i = e
                seg = e
                continue
            }
            if interp != 0 {
                var e = -1
                var paint: CodeToken = .escape
                switch interp {
                case 2:
                    if c == 36 && i + 1 < n && s[i + 1] == 123 { let m = closing(s, i + 1, n, 123, 125); e = m < 0 ? n : m + 1 }
                case 3:
                    if c == 36 && i + 1 < n {
                        if s[i + 1] == 123 {
                            let m = closing(s, i + 1, n, 123, 125)
                            e = m < 0 ? n : m + 1
                        } else if isWordStart(s[i + 1]) {
                            e = i + 2
                            while e < n && isWord(s[e]) { e += 1 }
                        }
                    }
                case 4:
                    if c == 35 && i + 1 < n && s[i + 1] == 123 { let m = closing(s, i + 1, n, 123, 125); e = m < 0 ? n : m + 1 }
                case 5:
                    if c == 123 {
                        if i + 1 < n && s[i + 1] == 123 {
                            e = i + 2
                        } else {
                            let m = closing(s, i, n, 123, 125)
                            e = m < 0 ? n : m + 1
                        }
                    } else if c == 125 && i + 1 < n && s[i + 1] == 125 {
                        e = i + 2
                    }
                case 6:
                    if c == 36 {
                        let v = shellVariable(s, i, n)
                        if v > i + 1 { e = v; paint = .variable }
                    }
                default: break
                }
                if e > i {
                    out.add(seg, i, .string)
                    out.add(i, e, paint)
                    i = e
                    seg = e
                    continue
                }
            }
            i += 1
        }
        out.add(seg, n, .string)
        if lines {
            st.mode = Mode.string
            st.kind = kind
            st.count = count
            st.data = data
        } else {
            st.mode = 0
            st.kind = 0
            st.count = 0
        }
        return n
    }

    /// A heredoc's word after << at i, as the shell and Ruby write them:
    /// where it ends, 1 for an end that may be indented, its length and hash.
    static func heredocWord(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int, ruby: Bool) -> (end: Int, kind: UInt8, count: UInt8, data: UInt32)? {
        var j = i + 2
        var kind: UInt8 = 0
        if j < n && (s[j] == 45 || (ruby && s[j] == 126)) { kind = 1; j += 1 }
        if !ruby { while j < n && isSpace(s[j]) { j += 1 } }
        var q: UInt16 = 0
        if j < n && (s[j] == 39 || s[j] == 34) { q = s[j]; j += 1 }
        let w = j
        while j < n && (isLetter(s[j]) || isDigit(s[j]) || s[j] == 95) { j += 1 }
        let e = j
        if e == w || e - w > 255 { return nil }
        if q != 0 {
            guard j < n && s[j] == q else { return nil }
            j += 1
            if q == 39 { kind |= 2 }
        } else if ruby && kind == 0 && !isUpper(s[w]) {
            return nil   // a << before a lowercase name is a shift
        }
        return (j, kind, UInt8(truncatingIfNeeded: e - w), hash(s, w, e))
    }

    /// A line inside a heredoc: its text as string, or the word that ends it.
    static func heredocLine(_ s: UnsafePointer<UInt16>, _ n: Int, _ st: inout SyntaxState, _ out: inout SyntaxSink,
                            variables: Bool) {
        var j = 0
        if st.kind & 1 != 0 { while j < n && isSpace(s[j]) { j += 1 } }
        if n - j == Int(truncatingIfNeeded: st.count) && hash(s, j, n) == st.data {
            out.add(j, n, .keyword)
            st.mode = 0
            st.kind = 0
            st.count = 0
            st.data = 0
            return
        }
        if !variables || st.kind & 2 != 0 {
            out.add(0, n, .string)
            return
        }
        var i = 0, seg = 0
        while i < n {
            if s[i] == 36 {
                let v = shellVariable(s, i, n)
                if v > i + 1 {
                    out.add(seg, i, .string)
                    out.add(i, v, .variable)
                    i = v
                    seg = v
                    continue
                }
            }
            i += 1
        }
        out.add(seg, n, .string)
    }

    /// The end of a shell variable at the $ at i: $name, ${…}, $1, $@ and
    /// the like, or the $( that starts a command; i + 1 for none.
    static func shellVariable(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int) -> Int {
        guard i + 1 < n else { return i + 1 }
        let d = s[i + 1]
        if d == 123 {
            let m = closing(s, i + 1, n, 123, 125)
            return m < 0 ? n : m + 1
        }
        if d == 40 { return i + 2 }
        if isLetter(d) || d == 95 {
            var e = i + 2
            while e < n && (isLetter(s[e]) || isDigit(s[e]) || s[e] == 95) { e += 1 }
            return e
        }
        if isDigit(d) || d == 64 || d == 42 || d == 35 || d == 63 || d == 36 || d == 33 || d == 45 { return i + 2 }
        return i + 1
    }

    // MARK: Code

    /// The lexer for Go, Swift, Python, JavaScript, Rust, the C family,
    /// Java, Kotlin, Ruby, Lua, SQL and JSON.
    static func code(_ s: UnsafePointer<UInt16>, _ from: Int, _ n: Int, _ st: inout SyntaxState,
                     _ r: SyntaxRules, _ out: inout SyntaxSink) {
        var i = from
        var lead = from == 0 && st.mode == 0
        switch st.mode {
        case 0: break
        case Mode.block: i = blockComment(s, i, n, i, st.count, r.nests, &st, &out)
        case Mode.long: i = longBracket(s, i, n, i, st.count, .comment, &st, &out)
        case Mode.doc:
            out.add(i, n, .comment)
            if has(s, i, n, "=end") && (n == i + 4 || isSpace(s[i + 4])) { st.mode = 0 }
            return
        case Mode.string: i = string(s, i, n, i, st.kind, st.count, st.data, &st, &out)
        case Mode.heredoc:
            heredocLine(s, n, &st, &out, variables: false)
            return
        case Mode.jsx: i = jsxAttributes(s, i, n, r, &st, &out)
        default: st = SyntaxState()
        }
        var regex = true            // a / here starts a regex, not a division
        var def: UInt8 = 0          // the next name is a function's or a type's
        var dot = false             // the next name is a member's, after a .
        var carry: UInt8 = 0        // a def for the name after the next dot
        var heredoc: (kind: UInt8, count: UInt8, data: UInt32)?
        let words = r.words
        let folds = words.folds
        while i < n {
            let c = s[i]
            if c == 32 || c == 9 || c == 12 { i += 1; continue }
            let atLead = lead
            lead = false
            if isWordStart(c) || (c == 36 && r.dollar) {
                // A name, packed for the word table as it is read.
                let a = i
                var k0: UInt64 = 0, k1: UInt64 = 0
                var len = 0
                var ascii = true, upper = false, lower = false
                while i < n {
                    var d = s[i]
                    if d >= 128 {
                        ascii = false
                    } else {
                        if !(isLetter(d) || isDigit(d) || d == 95 || (d == 36 && r.dollar)) { break }
                        if d >= 65 && d <= 90 {
                            upper = true
                            if folds { d += 32 }
                        } else if d >= 97 {
                            lower = true
                        }
                        if len < 9 {
                            k0 = k0 << 7 | UInt64(truncatingIfNeeded: d)
                        } else if len < 18 {
                            k1 = k1 << 7 | UInt64(truncatingIfNeeded: d)
                        }
                    }
                    len += 1
                    i += 1
                }
                // Ruby's names may end in ? or !.
                if r.dialect == .ruby && i < n && (s[i] == 63 || s[i] == 33) && !(i + 1 < n && (s[i + 1] == 61 || s[i + 1] == 58)) {
                    if len < 9 { k0 = k0 << 7 | UInt64(truncatingIfNeeded: s[i]) } else if len < 18 { k1 = k1 << 7 | UInt64(truncatingIfNeeded: s[i]) }
                    len += 1
                    i += 1
                }
                // A prefix before a quote: r"", b'', f"", u8"", R"(…)", N''.
                if i < n && (s[i] == 34 || s[i] == 39 || (s[i] == 35 && r.dialect == .rust)) && len <= 3 {
                    let e = prefixed(s, a, i, n, r, &st, &out)
                    if e > i {
                        i = e
                        regex = false
                        dot = false
                        def = 0
                        continue
                    }
                }
                var w: UInt8 = 0
                if ascii && len <= 18 {
                    w = words.find(k0, k1)
                    if dot && w & SyntaxWords.afterDot == 0 { w = 0 }
                }
                var kind = CodeToken.plain
                let nextUnit: UInt16 = i < n ? s[i] : 0
                // In function M.greet and def self.name, the name is the last.
                if def != 0 && (nextUnit == 46 || (nextUnit == 58 && r.dialect == .lua)) {
                    carry = def
                    def = 0
                }
                if w != 0 {
                    kind = CodeToken(rawValue: w & 31) ?? .plain
                } else if def == SyntaxWords.namesFunction {
                    kind = .function
                } else if def == SyntaxWords.namesType {
                    kind = .type
                } else {
                    let next: UInt16 = i < n ? s[i] : 0
                    let capital = isUpper(s[a])
                    if next == 40 {
                        kind = capital && r.capitalCalls ? .type : .function
                    } else if next == 33 && r.dialect == .rust && i + 1 < n && (s[i + 1] == 40 || s[i + 1] == 91 || s[i + 1] == 123) {
                        i += 1
                        kind = .function
                    } else if r.capsConstants && upper && !lower && len > 1 {
                        kind = .constant
                    } else if r.capitalTypes && capital {
                        kind = .type
                    } else if r.dialect == .ruby && next == 58 && i + 1 < n && s[i + 1] == 32 {
                        i += 1
                        kind = .constant
                    } else if r.versions && s[a] == 118 && len > 1 && isDigit(s[a + 1]) {
                        while i < n && (isAlnum(s[i]) || s[i] == 46 || s[i] == 45 || s[i] == 43) { i += 1 }
                        kind = .number
                    } else if r.dialect == .c && len > 2 && s[i - 2] == 95 && s[i - 1] == 116 {
                        kind = .type   // size_t, node_t
                    }
                }
                out.add(a, i, kind)
                regex = kind == .keyword
                def = w & (SyntaxWords.namesFunction | SyntaxWords.namesType)
                dot = false
                continue
            }
            if isDigit(c) || (c == 46 && i + 1 < n && isDigit(s[i + 1]))
                || (c == 45 && r.dialect == .json && i + 1 < n && isDigit(s[i + 1])) {
                let e = number(s, c == 45 ? i + 1 : i, n)
                out.add(i, e, .number)
                i = e
                regex = false
                dot = false
                def = 0
                continue
            }
            switch c {
            case 34, 39, 96:
                i = quote(s, i, n, c, r, &st, &out)
                regex = false
                dot = false
                def = 0
                continue
            case 47:
                if r.slash && i + 1 < n {
                    if s[i + 1] == 47 {
                        out.add(i, n, .comment)
                        i = n
                        continue
                    }
                    if s[i + 1] == 42 {
                        i = blockComment(s, i + 2, n, i, 1, r.nests, &st, &out)
                        continue
                    }
                }
                if regex && (r.dialect == .js || r.dialect == .ruby) {
                    let e = regexEnd(s, i, n)
                    if e > i {
                        out.add(i, e, .string)
                        i = e
                        regex = false
                        dot = false
                        def = 0
                        continue
                    }
                }
            case 35:
                if r.hash {
                    out.add(i, n, .comment)
                    i = n
                    continue
                }
                if r.dialect == .c && atLead {
                    i = directive(s, i, n, &out)
                    regex = true
                    continue
                }
                if r.dialect == .swift {
                    var h = i
                    while h < n && s[h] == 35 { h += 1 }
                    if h < n && s[h] == 34 {
                        let triple = h + 2 < n && s[h + 1] == 34 && s[h + 2] == 34
                        let count = UInt8(truncatingIfNeeded: min(h - i, 255))
                        i = string(s, h + (triple ? 3 : 1), n, i, triple ? Str.swiftLines : Str.swift, count, 0, &st, &out)
                        regex = false
                        continue
                    }
                    if i + 1 < n && isWordStart(s[i + 1]) {
                        var e = i + 1
                        while e < n && isWord(s[e]) { e += 1 }
                        out.add(i, e, .attribute)
                        i = e
                        continue
                    }
                }
                if r.dialect == .rust && i + 1 < n && (s[i + 1] == 91 || (s[i + 1] == 33 && i + 2 < n && s[i + 2] == 91)) {
                    let m = closing(s, s[i + 1] == 91 ? i + 1 : i + 2, n, 91, 93)
                    let e = m < 0 ? n : m + 1
                    out.add(i, e, .attribute)
                    i = e
                    continue
                }
            case 64:
                if i + 1 < n {
                    let d = s[i + 1]
                    if r.dialect == .ruby && (d == 64 || isWordStart(d)) {
                        var e = i + (d == 64 ? 2 : 1)
                        while e < n && isWord(s[e]) { e += 1 }
                        out.add(i, e, .variable)
                        i = e
                        regex = false
                        continue
                    }
                    if r.dialect == .c && d == 34 {
                        i = string(s, i + 2, n, i, Str.dq, 0, 0, &st, &out)
                        regex = false
                        continue
                    }
                    if r.dialect == .sql && isWordStart(d) {
                        var e = i + 1
                        while e < n && isWord(s[e]) { e += 1 }
                        out.add(i, e, .variable)
                        i = e
                        regex = false
                        continue
                    }
                    if (r.decorators || r.dialect == .c) && isWordStart(d) {
                        var e = i + 1
                        while e < n && (isWord(s[e]) || (s[e] == 46 && r.dialect != .c && e + 1 < n && isWordStart(s[e + 1]))) { e += 1 }
                        out.add(i, e, r.dialect == .c ? .keyword : .attribute)
                        i = e
                        continue
                    }
                }
            case 36:
                if (r.dialect == .ruby || r.dialect == .sql) && i + 1 < n && (isWord(s[i + 1]) || r.dialect == .ruby) {
                    var e = i + 1
                    if e < n && !isWord(s[e]) { e += 1 } else { while e < n && isWord(s[e]) { e += 1 } }
                    out.add(i, e, .variable)
                    i = e
                    regex = false
                    continue
                }
            case 45:
                if r.dash && i + 1 < n && s[i + 1] == 45 {
                    if r.dialect == .lua {
                        let level = longOpen(s, i + 2, n)
                        if level >= 0 {
                            i = longBracket(s, i + 4 + level, n, i, UInt8(truncatingIfNeeded: level), .comment, &st, &out)
                            continue
                        }
                    }
                    out.add(i, n, .comment)
                    i = n
                    continue
                }
            case 91:
                if r.dialect == .lua {
                    let level = longOpen(s, i, n)
                    if level >= 0 {
                        i = longBracket(s, i + 2 + level, n, i, UInt8(truncatingIfNeeded: level), .string, &st, &out)
                        regex = false
                        continue
                    }
                }
            case 60:
                if r.jsx && i + 1 < n {
                    let d = s[i + 1]
                    let closer = d == 47 && i + 2 < n && (isLetter(s[i + 2]) || s[i + 2] == 62)
                    if closer || (regex && (isLetter(d) || d == 62)) {
                        i = jsxTag(s, i, n, r, &st, &out)
                        regex = false
                        continue
                    }
                }
                if r.dialect == .ruby && i + 1 < n && s[i + 1] == 60, let h = heredocWord(s, i, n, ruby: true) {
                    out.add(i, h.end, .keyword)
                    if heredoc == nil { heredoc = (h.kind, h.count, h.data) }
                    i = h.end
                    regex = false
                    continue
                }
            case 61:
                if r.dialect == .ruby && i == 0 && from == 0 && has(s, 0, n, "=begin") && (n == 6 || isSpace(s[6])) {
                    out.add(0, n, .comment)
                    st.mode = Mode.doc
                    return
                }
            case 58:
                if i + 1 < n && s[i + 1] == 58 {
                    i += 2
                    regex = true
                    dot = true
                    continue
                }
                if r.dialect == .ruby && i + 1 < n && isWordStart(s[i + 1]) {
                    var e = i + 1
                    while e < n && isWord(s[e]) { e += 1 }
                    if e < n && (s[e] == 63 || s[e] == 33 || s[e] == 61) && !(e + 1 < n && s[e + 1] == 62) { e += 1 }
                    out.add(i, e, .constant)
                    i = e
                    regex = false
                    continue
                }
                if r.dialect == .sql && i + 1 < n && isWordStart(s[i + 1]) {
                    var e = i + 1
                    while e < n && isWord(s[e]) { e += 1 }
                    out.add(i, e, .variable)
                    i = e
                    continue
                }
                if carry != 0 {
                    i += 1
                    def = carry
                    carry = 0
                    continue
                }
            case 46:
                if i + 1 < n && s[i + 1] == 46 {
                    i += 2
                    regex = true
                    dot = false
                    def = 0
                    continue
                }
                i += 1
                regex = true
                dot = true
                def = carry
                carry = 0
                continue
            default:
                break
            }
            // Anything else: an operator or a bracket.
            regex = !(c == 41 || c == 93 || c == 125)
            dot = false
            def = 0
            carry = 0
            i += 1
        }
        if let h = heredoc, st.mode == 0 {
            st.mode = Mode.heredoc
            st.kind = h.kind
            st.count = h.count
            st.data = h.data
        }
    }

    /// A quoted run from the quote at i, as the dialect reads it: a string, a
    /// character, a JSON key, a lifetime or a quoted name.
    static func quote(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int, _ q: UInt16, _ r: SyntaxRules,
                      _ st: inout SyntaxState, _ out: inout SyntaxSink) -> Int {
        let triple = i + 2 < n && s[i + 1] == q && s[i + 2] == q
        switch r.dialect {
        case .python:
            if q == 96 { return i + 1 }
            return string(s, i + (triple ? 3 : 1), n, i, triple ? Str.pyLines : Str.py, q == 39 ? 4 : 0, 0, &st, &out)
        case .swift:
            if q == 34 { return string(s, i + (triple ? 3 : 1), n, i, triple ? Str.swiftLines : Str.swift, 0, 0, &st, &out) }
            if q == 96 { return quotedName(s, i, n) }
            return i + 1
        case .kotlin:
            if q == 34 { return string(s, i + (triple ? 3 : 1), n, i, triple ? Str.kotlinLines : Str.kotlin, 0, 0, &st, &out) }
            if q == 96 { return quotedName(s, i, n) }
            return string(s, i + 1, n, i, Str.sq, 0, 0, &st, &out)
        case .java:
            if q == 34 { return string(s, i + (triple ? 3 : 1), n, i, triple ? Str.textBlock : Str.dq, 0, 0, &st, &out) }
            if q == 39 { return string(s, i + 1, n, i, Str.sq, 0, 0, &st, &out) }
            return i + 1
        case .go:
            if q == 96 { return string(s, i + 1, n, i, Str.raw, 0, 0, &st, &out) }
            return string(s, i + 1, n, i, q == 34 ? Str.dq : Str.sq, 0, 0, &st, &out)
        case .js:
            if q == 96 { return string(s, i + 1, n, i, Str.template, 0, 0, &st, &out) }
            return string(s, i + 1, n, i, q == 34 ? Str.dq : Str.sq, 0, 0, &st, &out)
        case .rust:
            if q == 34 { return string(s, i + 1, n, i, Str.dqLines, 0, 0, &st, &out) }
            if q == 96 { return i + 1 }
            return rustQuote(s, i, n, &st, &out)
        case .ruby:
            if q == 39 { return string(s, i + 1, n, i, Str.rubySq, 0, 0, &st, &out) }
            return string(s, i + 1, n, i, Str.ruby, q == 96 ? 1 : 0, 0, &st, &out)
        case .sql:
            if q == 39 { return string(s, i + 1, n, i, Str.sql, 0, 0, &st, &out) }
            return quotedName(s, i, n)
        case .json:
            let mark = out.count
            let e = string(s, i + 1, n, i, q == 39 ? Str.sq : Str.dq, 0, 0, &st, &out)
            // A key: a string with a colon after it.
            var j = e
            while j < n && isSpace(s[j]) { j += 1 }
            if j < n && s[j] == 58 { out.retag(since: mark, .property) }
            return e
        case .lua, .c, .generic:
            if q == 96 { return i + 1 }
            return string(s, i + 1, n, i, q == 34 ? Str.dq : Str.sq, 0, 0, &st, &out)
        }
    }

    /// A name in quotes, as Swift's `default` or SQL's "user": plain.
    static func quotedName(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int) -> Int {
        let q = s[i]
        var e = i + 1
        while e < n && s[e] != q { e += 1 }
        return min(e + 1, n)
    }

    /// Rust's ': a character, 'x' or '\n', or a lifetime, 'a.
    static func rustQuote(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int, _ st: inout SyntaxState,
                          _ out: inout SyntaxSink) -> Int {
        guard i + 1 < n else { return i + 1 }
        let d = s[i + 1]
        if d == 92 { return string(s, i + 1, n, i, Str.sq, 0, 0, &st, &out) }
        if i + 2 < n && s[i + 2] == 39 {
            out.add(i, i + 3, .string)
            return i + 3
        }
        if isHigh(d) && i + 3 < n && s[i + 3] == 39 {
            out.add(i, i + 4, .string)
            return i + 4
        }
        if isWordStart(d) {
            var e = i + 2
            while e < n && isWord(s[e]) { e += 1 }
            out.add(i, e, .keyword)
            return e
        }
        return i + 1
    }

    /// A string after its prefix from a to the quote at i: Python's r, b and
    /// f, Rust's b, r and c, the C family's L, u, U, u8 and R, SQL's N and E.
    /// i itself when the name is no prefix.
    static func prefixed(_ s: UnsafePointer<UInt16>, _ a: Int, _ i: Int, _ n: Int, _ r: SyntaxRules,
                         _ st: inout SyntaxState, _ out: inout SyntaxSink) -> Int {
        let q = s[i]
        let len = i - a
        func lower(_ k: Int) -> UInt16 { let c = s[a + k]; return isUpper(c) ? c + 32 : c }
        switch r.dialect {
        case .python:
            if q == 35 || len > 2 { return i }
            var flags: UInt8 = q == 39 ? 4 : 0
            var k = 0
            while k < len {
                let c = lower(k)
                if c == 114 { flags |= 1 } else if c == 102 { flags |= 2 } else if c != 98 && c != 117 { return i }
                k += 1
            }
            let triple = i + 2 < n && s[i + 1] == q && s[i + 2] == q
            return string(s, i + (triple ? 3 : 1), n, a, triple ? Str.pyLines : Str.py, flags, 0, &st, &out)
        case .rust:
            let first = s[a]
            let raw = (len == 1 && first == 114) || (len == 2 && (first == 98 || first == 99) && s[a + 1] == 114)
            if raw && (q == 34 || q == 35) {
                var h = i
                while h < n && s[h] == 35 { h += 1 }
                guard h < n && s[h] == 34 else { return i }
                return string(s, h + 1, n, a, Str.rustRaw, UInt8(truncatingIfNeeded: min(h - i, 255)), 0, &st, &out)
            }
            if len == 1 && (first == 98 || first == 99) && q == 34 { return string(s, i + 1, n, a, Str.dqLines, 0, 0, &st, &out) }
            if len == 1 && first == 98 && q == 39 { return string(s, i + 1, n, a, Str.sq, 0, 0, &st, &out) }
            return i
        case .c:
            if q == 35 { return i }
            var k = 0
            let first = s[a]
            if first == 76 || first == 85 { k = 1 } else if first == 117 { k = len >= 2 && s[a + 1] == 56 ? 2 : 1 }
            let rawR = k < len && s[a + k] == 82
            if k + (rawR ? 1 : 0) != len { return i }
            if rawR && q == 34 {
                // R"delim( … )delim"
                var j = i + 1
                while j < n && j - i - 1 <= 16 && s[j] != 40 && s[j] != 34 && !isSpace(s[j]) { j += 1 }
                guard j < n && s[j] == 40 else { return i }
                return cppRaw(s, j + 1, n, a, j - i - 1, hash(s, i + 1, j), &st, &out)
            }
            if k == 0 { return i }
            return string(s, i + 1, n, a, q == 34 ? Str.dq : Str.sq, 0, 0, &st, &out)
        case .sql:
            if len != 1 || q != 39 { return i }
            let c = lower(0)
            guard c == 110 || c == 101 || c == 120 || c == 98 else { return i }
            return string(s, i + 1, n, a, Str.sql, 0, 0, &st, &out)
        default:
            return i
        }
    }

    /// The end of a regex literal from the / at i, or i when the line has no
    /// end for it.
    static func regexEnd(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int) -> Int {
        var j = i + 1
        if j < n && (s[j] == 47 || s[j] == 42 || isSpace(s[j])) { return i }
        var klass = false
        while j < n {
            let c = s[j]
            if c == 92 {
                j += 2
                continue
            }
            if c == 91 { klass = true } else if c == 93 { klass = false } else if c == 47 && !klass {
                j += 1
                while j < n && isLetter(s[j]) { j += 1 }
                return j
            }
            j += 1
        }
        return i
    }

    /// A C preprocessor line's # and directive, and an #include's <file>.
    static func directive(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int, _ out: inout SyntaxSink) -> Int {
        var e = i + 1
        while e < n && isSpace(s[e]) { e += 1 }
        let w = e
        while e < n && (isLetter(s[e]) || s[e] == 95) { e += 1 }
        out.add(i, e, .attribute)
        if (e - w == 7 && has(s, w, n, "include")) || (e - w == 6 && has(s, w, n, "import")) {
            var j = e
            while j < n && isSpace(s[j]) { j += 1 }
            if j < n && s[j] == 60 {
                var k = j + 1
                while k < n && s[k] != 62 { k += 1 }
                let end = min(k + 1, n)
                out.add(j, end, .string)
                return end
            }
        }
        return e
    }

    /// A JSX tag from the < at i: its name as tag, then its attributes.
    static func jsxTag(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int, _ r: SyntaxRules,
                       _ st: inout SyntaxState, _ out: inout SyntaxSink) -> Int {
        var j = i + 1
        if j < n && s[j] == 47 { j += 1 }
        let a = j
        while j < n && (isWord(s[j]) || s[j] == 46 || s[j] == 45 || s[j] == 58) { j += 1 }
        out.add(a, j, .tag)
        return jsxAttributes(s, j, n, r, &st, &out)
    }

    /// A JSX tag's attributes to its > or the line's end: names as property,
    /// strings as string, and code in braces lexed as code.
    static func jsxAttributes(_ s: UnsafePointer<UInt16>, _ i0: Int, _ n: Int, _ r: SyntaxRules,
                              _ st: inout SyntaxState, _ out: inout SyntaxSink) -> Int {
        var i = i0
        var value = false
        while i < n {
            let c = s[i]
            if c == 62 {
                st.mode = 0
                return i + 1
            }
            if c == 47 && i + 1 < n && s[i + 1] == 62 {
                st.mode = 0
                return i + 2
            }
            if c == 34 || c == 39 {
                var e = i + 1
                while e < n && s[e] != c { e += 1 }
                e = min(e + 1, n)
                out.add(i, e, .string)
                i = e
                value = false
                continue
            }
            if c == 123 {
                let m = closing(s, i, n, 123, 125)
                var inner = SyntaxState()
                code(s, i + 1, m < 0 ? n : m, &inner, r, &out)
                i = m < 0 ? n : m + 1
                value = false
                continue
            }
            if c == 61 {
                value = true
                i += 1
                continue
            }
            if isWordStart(c) {
                var e = i + 1
                while e < n && (isWord(s[e]) || s[e] == 45 || s[e] == 58) { e += 1 }
                out.add(i, e, value ? .string : .property)
                value = false
                i = e
                continue
            }
            i += 1
        }
        st.mode = Mode.jsx
        st.kind = 0
        st.count = 0
        return n
    }

    // MARK: Shell, Makefile and Dockerfile

    /// The shell's lexer, which a Dockerfile's lines and a Makefile's
    /// recipes use too.
    static func shell(_ s: UnsafePointer<UInt16>, _ from: Int, _ n: Int, _ st: inout SyntaxState,
                      _ r: SyntaxRules, _ out: inout SyntaxSink, make: Bool) {
        var i = from
        let docker = r.lexer == .docker
        var head = docker && from == 0 && st.outer == 0 && st.mode == 0
        switch st.mode {
        case 0: break
        case Mode.string: i = string(s, i, n, i, st.kind, st.count, st.data, &st, &out)
        case Mode.heredoc:
            heredocLine(s, n, &st, &out, variables: true)
            return
        default: st.mode = 0
        }
        var word = true             // at a word's start
        var fromLine = false        // a Dockerfile's FROM, where AS is a keyword
        var def = false
        var heredoc: (kind: UInt8, count: UInt8, data: UInt32)?
        let words = r.words
        while i < n {
            let c = s[i]
            if c == 32 || c == 9 {
                i += 1
                word = true
                continue
            }
            if c == 35 && word {
                out.add(i, n, .comment)
                i = n
                break
            }
            if c == 39 {
                i = string(s, i + 1, n, i, Str.shSq, 0, 0, &st, &out)
                word = false
                head = false
                continue
            }
            if c == 34 {
                i = string(s, i + 1, n, i, Str.shDq, 0, 0, &st, &out)
                word = false
                head = false
                continue
            }
            if c == 36 {
                if i + 1 < n && s[i + 1] == 39 {
                    i = string(s, i + 2, n, i, Str.shAnsi, 0, 0, &st, &out)
                } else if make {
                    i = makeVariable(s, i, n, r, &out)
                } else {
                    let e = shellVariable(s, i, n)
                    out.add(i, e, e > i + 1 ? .variable : .plain)
                    i = e
                }
                word = false
                head = false
                continue
            }
            if c == 92 {
                i = min(i + 2, n)
                word = false
                continue
            }
            if c == 60 && i + 1 < n && s[i + 1] == 60 && !(i + 2 < n && s[i + 2] == 60) {
                if let h = heredocWord(s, i, n, ruby: false) {
                    out.add(i, h.end, .keyword)
                    if heredoc == nil { heredoc = (h.kind, h.count, h.data) }
                    i = h.end
                    word = true
                    continue
                }
            }
            if c == 59 || c == 38 || c == 124 || c == 40 || c == 41 || c == 60 || c == 62 || c == 96 {
                i += 1
                word = true
                continue
            }
            // A word, to a space, an operator or a quote.
            let a = i
            var eq = -1
            var digits = true
            while i < n {
                let d = s[i]
                if d == 32 || d == 9 || d == 59 || d == 38 || d == 124 || d == 40 || d == 41 || d == 60 || d == 62
                    || d == 34 || d == 39 || d == 96 || d == 36 || d == 92 { break }
                if d == 61 && eq < 0 { eq = i }
                if !isDigit(d) { digits = false }
                i += 1
            }
            // NAME=value: the name is a variable, and the value goes on.
            if eq > a && (isLetter(s[a]) || s[a] == 95) {
                var e = a + 1
                while e < eq && (isLetter(s[e]) || isDigit(s[e]) || s[e] == 95) { e += 1 }
                if e == eq || (e == eq - 1 && s[e] == 43) {
                    out.add(a, e, .variable)
                    i = eq + 1
                    word = false
                    head = false
                    def = false
                    continue
                }
            }
            var kind = CodeToken.plain
            if head, let heads = r.heads, heads.find(s, a, i) != 0 {
                kind = .keyword
                fromLine = i - a == 4 && hasFolded(s, a, n, "from")
            } else if fromLine && i - a == 2 && hasFolded(s, a, n, "as") {
                kind = .keyword
            } else if word {
                let w = words.find(s, a, i)
                if w != 0 {
                    kind = CodeToken(rawValue: w & 31) ?? .plain
                    def = w & SyntaxWords.namesFunction != 0
                    out.add(a, i, kind)
                    head = false
                    word = false
                    continue
                }
            }
            if kind == .plain {
                if def {
                    kind = .function
                } else if digits {
                    kind = .number
                } else if i + 1 < n && s[i] == 40 && s[i + 1] == 41 {
                    kind = .function
                }
            }
            out.add(a, i, kind)
            def = false
            head = false
            word = false
        }
        if let h = heredoc, st.mode == 0 {
            st.mode = Mode.heredoc
            st.kind = h.kind
            st.count = h.count
            st.data = h.data
        }
        if docker {
            // A line that ends in \ goes on with its instruction.
            var e = n
            while e > from && isSpace(s[e - 1]) { e -= 1 }
            st.outer = e > from && s[e - 1] == 92 ? 1 : 0
        }
    }

    /// A Makefile's $(name), ${name}, $@ and the like as variable, or a
    /// function's name, as in $(patsubst …), as function; returns where its
    /// lexer goes on.
    static func makeVariable(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int, _ r: SyntaxRules,
                             _ out: inout SyntaxSink) -> Int {
        guard i + 1 < n else { return i + 1 }
        let d = s[i + 1]
        if d == 40 || d == 123 {
            var e = i + 2
            while e < n && (isLetter(s[e]) || s[e] == 45) { e += 1 }
            if e > i + 2 && e < n && isSpace(s[e]), let heads = r.heads,
               heads.find(s, i + 2, e) & 31 == CodeToken.function.rawValue {
                out.add(i + 2, e, .function)
                return e
            }
            let m = closing(s, i + 1, n, d, d == 40 ? 41 : 125)
            let end = m < 0 ? n : m + 1
            out.add(i, end, .variable)
            return end
        }
        out.add(i, i + 2, .variable)
        return i + 2
    }

    /// Text in a Makefile: variables, and a comment after #.
    static func makeText(_ s: UnsafePointer<UInt16>, _ from: Int, _ n: Int, _ r: SyntaxRules,
                         _ out: inout SyntaxSink) {
        var i = from
        while i < n {
            let c = s[i]
            if c == 92 {
                i += 2
                continue
            }
            if c == 35 {
                out.add(i, n, .comment)
                return
            }
            if c == 36 {
                i = makeVariable(s, i, n, r, &out)
                continue
            }
            i += 1
        }
    }

    /// A Makefile's lines: directives, assignments, rules and recipes.
    static func make(_ s: UnsafePointer<UInt16>, _ n: Int, _ st: inout SyntaxState, _ r: SyntaxRules,
                     _ out: inout SyntaxSink) {
        if st.mode == Mode.define {
            var j = 0
            while j < n && isSpace(s[j]) { j += 1 }
            if has(s, j, n, "endef") && (j + 5 == n || isSpace(s[j + 5])) {
                out.add(j, j + 5, .keyword)
                st.mode = 0
                makeText(s, j + 5, n, r, &out)
                return
            }
            makeText(s, 0, n, r, &out)
            return
        }
        if n > 0 && s[0] == 9 {
            // A recipe: a shell command, after @, - or +.
            var i = 1
            while i < n && (s[i] == 64 || s[i] == 45 || s[i] == 43 || isSpace(s[i])) {
                if !isSpace(s[i]) { out.add(i, i + 1, .keyword) }
                i += 1
            }
            var inner = SyntaxState()
            shell(s, i, n, &inner, r, &out, make: true)
            return
        }
        var i = 0
        while i < n && isSpace(s[i]) { i += 1 }
        if i < n && s[i] == 35 {
            out.add(i, n, .comment)
            return
        }
        // A directive: include, ifeq, define, export and the like.
        var e = i
        while e < n && (isLetter(s[e]) || s[e] == 45) { e += 1 }
        if e > i, let heads = r.heads, heads.find(s, i, e) & 31 == CodeToken.keyword.rawValue {
            out.add(i, e, .keyword)
            if e - i == 6 && has(s, i, n, "define") { st.mode = Mode.define }
            i = e
            while i < n && isSpace(s[i]) { i += 1 }
        }
        // An assignment or a rule: the first = or :, outside $( ).
        var j = i, depth = 0, sep = -1
        var assign = false
        while j < n {
            let c = s[j]
            if c == 36 && j + 1 < n && (s[j + 1] == 40 || s[j + 1] == 123) {
                depth += 1
                j += 2
                continue
            }
            if (c == 41 || c == 125) && depth > 0 {
                depth -= 1
            } else if depth == 0 {
                if c == 35 { break }
                if c == 61 {
                    sep = j
                    assign = true
                    break
                }
                if c == 58 {
                    sep = j
                    assign = (j + 1 < n && s[j + 1] == 61) || (j + 2 < n && s[j + 1] == 58 && s[j + 2] == 61)
                    break
                }
            }
            j += 1
        }
        if assign {
            var t = sep
            if s[sep] == 61 && t > i && (s[t - 1] == 63 || s[t - 1] == 43 || s[t - 1] == 33) { t -= 1 }
            while t > i && isSpace(s[t - 1]) { t -= 1 }
            out.add(i, t, .variable)
            var v = sep
            while v < n && (s[v] == 58 || s[v] == 61) { v += 1 }
            makeText(s, v, n, r, &out)
            return
        }
        if sep >= 0 {
            // A rule: its targets, then what they need, then perhaps ; and a recipe.
            var k = i
            while k < sep {
                if isSpace(s[k]) {
                    k += 1
                    continue
                }
                if s[k] == 36 {
                    k = makeVariable(s, k, sep, r, &out)
                    continue
                }
                let a = k
                while k < sep && !isSpace(s[k]) && s[k] != 36 { k += 1 }
                out.add(a, k, s[a] == 46 && a + 1 < sep && isUpper(s[a + 1]) ? .keyword : .function)
            }
            var p = sep + 1
            if p < n && s[p] == 58 { p += 1 }
            var semi = p
            while semi < n && s[semi] != 59 && s[semi] != 35 { semi += 1 }
            makeText(s, p, semi, r, &out)
            if semi < n && s[semi] == 59 {
                var inner = SyntaxState()
                shell(s, semi + 1, n, &inner, r, &out, make: true)
            } else if semi < n {
                out.add(semi, n, .comment)
            }
            return
        }
        makeText(s, i, n, r, &out)
    }

    // MARK: HTML and XML

    /// HTML's and XML's lines, with HTML's scripts and styles lexed as
    /// JavaScript and CSS.
    static func markup(_ s: UnsafePointer<UInt16>, _ from: Int, _ n: Int, _ st: inout SyntaxState,
                       _ r: SyntaxRules, _ out: inout SyntaxSink) {
        var i = from
        while true {
            if st.outer != 0 {
                let script = st.outer == 1
                let close = closingTag(s, i, n, script)
                var inner = st
                inner.outer = 0
                if let sub = script ? r.script : r.style { line(s, i, close, &inner, sub, &out) }
                if close >= n {
                    st = inner
                    st.outer = script ? 1 : 2
                    return
                }
                st = SyntaxState()
                i = close
            }
            if st.mode == Mode.comment {
                let e = find(s, i, n, "-->")
                if e < 0 {
                    out.add(i, n, .comment)
                    return
                }
                out.add(i, e + 3, .comment)
                st.mode = 0
                i = e + 3
                continue
            }
            if st.mode == Mode.cdata {
                let e = find(s, i, n, "]]>")
                if e < 0 {
                    out.add(i, n, .string)
                    return
                }
                out.add(i, e + 3, .string)
                st.mode = 0
                i = e + 3
                continue
            }
            if st.mode == Mode.tag {
                i = attributes(s, i, n, &st, &out)
                if st.mode == Mode.tag { return }
                continue
            }
            while i < n && s[i] != 60 && s[i] != 38 { i += 1 }
            if i >= n { return }
            if s[i] == 38 {
                i = entity(s, i, n, &out)
                continue
            }
            i = open(s, i, n, r, &st, &out)
        }
    }

    /// What starts at a <: a comment, CDATA, a doctype, a processing
    /// instruction, or a tag with its name as tag.
    static func open(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int, _ r: SyntaxRules,
                     _ st: inout SyntaxState, _ out: inout SyntaxSink) -> Int {
        if has(s, i, n, "<!--") {
            let e = find(s, i + 4, n, "-->")
            if e < 0 {
                out.add(i, n, .comment)
                st.mode = Mode.comment
                return n
            }
            out.add(i, e + 3, .comment)
            return e + 3
        }
        if has(s, i, n, "<![CDATA[") {
            let e = find(s, i + 9, n, "]]>")
            if e < 0 {
                out.add(i, n, .string)
                st.mode = Mode.cdata
                return n
            }
            out.add(i, e + 3, .string)
            return e + 3
        }
        guard i + 1 < n else { return i + 1 }
        if s[i + 1] == 33 {
            var e = i + 2
            while e < n && s[e] != 62 { e += 1 }
            e = min(e + 1, n)
            out.add(i, e, .attribute)
            return e
        }
        if s[i + 1] == 63 {
            var e = i + 2
            while e < n && (isWord(s[e]) || s[e] == 45) { e += 1 }
            out.add(i, e, .attribute)
            st.kind = 0
            return attributes(s, e, n, &st, &out)
        }
        var j = i + 1
        let closer = s[j] == 47
        if closer { j += 1 }
        guard j < n && (isLetter(s[j]) || s[j] == 95 || s[j] == 58) else { return i + 1 }
        let a = j
        while j < n && (isWord(s[j]) || s[j] == 45 || s[j] == 46 || s[j] == 58) { j += 1 }
        out.add(a, j, .tag)
        var kind: UInt8 = 0
        if !closer && r.script != nil {
            if j - a == 6 && hasFolded(s, a, n, "script") { kind = 1 } else if j - a == 5 && hasFolded(s, a, n, "style") { kind = 2 }
        }
        st.kind = kind
        return attributes(s, j, n, &st, &out)
    }

    /// A tag's attributes to its > or the line's end: names as property and
    /// values as string. A script's or style's tag hands on to its lexer.
    static func attributes(_ s: UnsafePointer<UInt16>, _ i0: Int, _ n: Int, _ st: inout SyntaxState,
                           _ out: inout SyntaxSink) -> Int {
        var i = i0
        let kind = st.kind
        if st.mode == Mode.tag && st.count != 0 {
            let q: UInt16 = st.count == 1 ? 34 : 39
            var e = i
            while e < n && s[e] != q { e += 1 }
            if e >= n {
                out.add(i, n, .string)
                return n
            }
            out.add(i, e + 1, .string)
            i = e + 1
            st.count = 0
        }
        var value = false
        while i < n {
            let c = s[i]
            if c == 62 {
                st.mode = 0
                st.kind = 0
                st.count = 0
                st.outer = kind
                return i + 1
            }
            if (c == 47 || c == 63) && i + 1 < n && s[i + 1] == 62 {
                st.mode = 0
                st.kind = 0
                st.count = 0
                return i + 2
            }
            if c == 34 || c == 39 {
                var e = i + 1
                while e < n && s[e] != c { e += 1 }
                if e >= n {
                    out.add(i, n, .string)
                    st.mode = Mode.tag
                    st.kind = kind
                    st.count = c == 34 ? 1 : 2
                    return n
                }
                out.add(i, e + 1, .string)
                i = e + 1
                value = false
                continue
            }
            if c == 61 {
                value = true
                i += 1
                continue
            }
            if isSpace(c) {
                i += 1
                continue
            }
            var e = i + 1
            while e < n && !isSpace(s[e]) && s[e] != 62 && s[e] != 61 && s[e] != 34 && s[e] != 39
                && !((s[e] == 47 || s[e] == 63) && e + 1 < n && s[e + 1] == 62) { e += 1 }
            out.add(i, e, value ? .string : .property)
            value = false
            i = e
        }
        st.mode = Mode.tag
        st.kind = kind
        st.count = 0
        return n
    }

    /// Where </script or </style starts, from i, or n.
    static func closingTag(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int, _ script: Bool) -> Int {
        var j = i
        while j + 1 < n {
            if s[j] == 60 && s[j + 1] == 47 && (script ? hasFolded(s, j + 2, n, "script") : hasFolded(s, j + 2, n, "style")) {
                return j
            }
            j += 1
        }
        return n
    }

    /// An entity, &amp; &#123; &#x1F;, as escape.
    static func entity(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int, _ out: inout SyntaxSink) -> Int {
        var e = i + 1
        if e < n && s[e] == 35 { e += 1 }
        while e < n && e - i < 32 && isAlnum(s[e]) { e += 1 }
        if e < n && s[e] == 59 && e > i + 1 {
            out.add(i, e + 1, .escape)
            return e + 1
        }
        return i + 1
    }

    // MARK: CSS

    static func isCSSName(_ c: UInt16) -> Bool { isWord(c) || c == 45 }

    /// CSS, SCSS and Less: selectors as tag and attribute, declarations'
    /// names as property, values as constant, number and function.
    static func css(_ s: UnsafePointer<UInt16>, _ from: Int, _ n: Int, _ st: inout SyntaxState,
                    _ r: SyntaxRules, _ out: inout SyntaxSink) {
        var i = from
        var depth = Int(truncatingIfNeeded: st.data)
        var value = st.kind == 1
        if st.mode == Mode.block { i = blockComment(s, i, n, i, st.count, false, &st, &out) }
        while i < n {
            let c = s[i]
            if isSpace(c) {
                i += 1
                continue
            }
            if c == 47 && i + 1 < n && s[i + 1] == 42 {
                i = blockComment(s, i + 2, n, i, 1, false, &st, &out)
                continue
            }
            if c == 47 && i + 1 < n && s[i + 1] == 47 && r.slash && !(i > 0 && s[i - 1] == 58) {
                out.add(i, n, .comment)
                break
            }
            if c == 34 || c == 39 {
                i = string(s, i + 1, n, i, c == 34 ? Str.dq : Str.sq, 0, 0, &st, &out)
                continue
            }
            if c == 123 {
                if depth < 255 { depth += 1 }
                value = false
                i += 1
                continue
            }
            if c == 125 {
                if depth > 0 { depth -= 1 }
                value = false
                i += 1
                continue
            }
            if c == 59 {
                value = false
                i += 1
                continue
            }
            if c == 64 || (c == 36 && i + 1 < n && isWordStart(s[i + 1])) {
                // @media (…), @import url(…), $gap: 8px and Less's @gap: 8px
                // go on as values.
                var e = i + 1
                while e < n && isCSSName(s[e]) { e += 1 }
                out.add(i, e, c == 64 ? .keyword : .variable)
                var t = e
                while t < n && isSpace(s[t]) { t += 1 }
                if c == 64 || (t < n && s[t] == 58) { value = true }
                i = c == 36 && t < n && s[t] == 58 ? t + 1 : e
                continue
            }
            if c == 35 && i + 1 < n && s[i + 1] == 123 {
                let m = closing(s, i + 1, n, 123, 125)
                let e = m < 0 ? n : m + 1
                out.add(i, e, .escape)
                i = e
                continue
            }
            if c == 33 && i + 1 < n && isLetter(s[i + 1]) {
                var e = i + 1
                while e < n && isLetter(s[e]) { e += 1 }
                out.add(i, e, .keyword)
                i = e
                continue
            }
            if value {
                if c == 35 {
                    var e = i + 1
                    while e < n && isWord(s[e]) { e += 1 }
                    out.add(i, e, .number)
                    i = e
                    continue
                }
                if isDigit(c) || ((c == 46 || c == 45 || c == 43) && i + 1 < n && (isDigit(s[i + 1]) || (s[i + 1] == 46 && i + 2 < n && isDigit(s[i + 2])))) {
                    var e = i + 1
                    while e < n && (isDigit(s[e]) || s[e] == 46) { e += 1 }
                    while e < n && (isLetter(s[e]) || s[e] == 37) { e += 1 }
                    out.add(i, e, .number)
                    i = e
                    continue
                }
                if c == 45 && i + 1 < n && s[i + 1] == 45 {
                    var e = i + 2
                    while e < n && isCSSName(s[e]) { e += 1 }
                    out.add(i, e, .variable)
                    i = e
                    continue
                }
                if isWordStart(c) || c == 45 {
                    var e = i + 1
                    while e < n && isCSSName(s[e]) { e += 1 }
                    if e < n && s[e] == 40 {
                        out.add(i, e, .function)
                        // url(…) without quotes holds a string.
                        if e - i == 3 && hasFolded(s, i, n, "url") {
                            var j = e + 1
                            while j < n && isSpace(s[j]) { j += 1 }
                            if j < n && s[j] != 34 && s[j] != 39 {
                                var k = j
                                while k < n && s[k] != 41 { k += 1 }
                                out.add(j, k, .string)
                                i = k
                                continue
                            }
                        }
                        i = e + 1
                        continue
                    }
                    out.add(i, e, .constant)
                    i = e
                    continue
                }
                i += 1
                continue
            }
            if (c == 46 || c == 35) && i + 1 < n && (isWordStart(s[i + 1]) || s[i + 1] == 45) {
                var e = i + 1
                while e < n && isCSSName(s[e]) { e += 1 }
                out.add(i, e, .attribute)
                i = e
                continue
            }
            if c == 58 {
                var e = i + 1
                if e < n && s[e] == 58 { e += 1 }
                let a = e
                while e < n && isCSSName(s[e]) { e += 1 }
                if e > a {
                    out.add(i, e, .attribute)
                    i = e
                } else {
                    i += 1
                }
                continue
            }
            if c == 45 && i + 1 < n && s[i + 1] == 45 {
                var e = i + 2
                while e < n && isCSSName(s[e]) { e += 1 }
                out.add(i, e, .variable)
                var t = e
                while t < n && isSpace(s[t]) { t += 1 }
                if t < n && s[t] == 58 {
                    value = true
                    i = t + 1
                } else {
                    i = e
                }
                continue
            }
            if isWordStart(c) || c == 45 {
                var e = i + 1
                while e < n && isCSSName(s[e]) { e += 1 }
                if depth > 0 && declares(s, e, n) {
                    out.add(i, e, .property)
                    var t = e
                    while t < n && s[t] != 58 { t += 1 }
                    value = true
                    i = t + 1
                    continue
                }
                if e < n && s[e] == 40 {
                    out.add(i, e, .function)
                    i = e + 1
                    continue
                }
                out.add(i, e, .tag)
                i = e
                continue
            }
            i += 1
        }
        st.data = UInt32(truncatingIfNeeded: depth)
        st.kind = value ? 1 : 0
    }

    /// Whether a name ending at e starts a declaration, name: value, rather
    /// than a selector such as a:hover {.
    static func declares(_ s: UnsafePointer<UInt16>, _ e: Int, _ n: Int) -> Bool {
        var t = e
        while t < n && isSpace(s[t]) { t += 1 }
        guard t < n && s[t] == 58 && !(t + 1 < n && s[t + 1] == 58) else { return false }
        var j = t + 1
        while j < n {
            let c = s[j]
            if c == 123 { return false }
            if c == 59 || c == 125 { return true }
            j += 1
        }
        return true
    }

    // MARK: Markdown

    /// Markdown: headings, emphasis, code, links, lists and quotes, fenced
    /// code, HTML comments and front matter.
    static func markdown(_ s: UnsafePointer<UInt16>, _ n: Int, _ st: inout SyntaxState, _ out: inout SyntaxSink) {
        let first = st.outer == 0
        st.outer = 1
        var ind = 0
        while ind < n && isSpace(s[ind]) { ind += 1 }
        if st.mode == Mode.fence {
            // It ends at a run of its character as long, with nothing after.
            let ch: UInt16 = st.kind == 1 ? 96 : 126
            var e = ind
            while e < n && s[e] == ch { e += 1 }
            var t = e
            while t < n && isSpace(s[t]) { t += 1 }
            out.add(ind, n, .code)
            if e - ind >= Int(truncatingIfNeeded: st.data) && t == n {
                st.mode = 0
                st.kind = 0
                st.data = 0
            }
            return
        }
        if st.mode == Mode.comment {
            let e = find(s, 0, n, "-->")
            if e < 0 {
                out.add(0, n, .comment)
                return
            }
            out.add(0, e + 3, .comment)
            st.mode = 0
            inline(s, e + 3, n, &st, &out)
            return
        }
        if st.mode == Mode.front {
            if n == 3 && ((s[0] == 45 && s[1] == 45 && s[2] == 45) || (s[0] == 46 && s[1] == 46 && s[2] == 46)) {
                out.add(0, 3, .keyword)
                st.mode = 0
                return
            }
            if ind < n && s[ind] == 35 {
                out.add(ind, n, .comment)
                return
            }
            var k = ind
            while k < n && s[k] != 58 { k += 1 }
            if k < n && k > ind { out.add(ind, k, .property) }
            return
        }
        if ind >= n { return }
        let c = s[ind]
        if first && ind == 0 && n == 3 && c == 45 && s[1] == 45 && s[2] == 45 {
            out.add(0, 3, .keyword)
            st.mode = Mode.front
            return
        }
        if c == 96 || c == 126 {
            var e = ind
            while e < n && s[e] == c { e += 1 }
            var info = e
            while c == 96 && info < n && s[info] != 96 { info += 1 }
            if e - ind >= 3 && info == n || (c == 126 && e - ind >= 3) {
                out.add(ind, n, .code)
                st.mode = Mode.fence
                st.kind = c == 96 ? 1 : 2
                st.data = UInt32(truncatingIfNeeded: e - ind)
                return
            }
        }
        if c == 35 && ind < 4 {
            var e = ind
            while e < n && s[e] == 35 { e += 1 }
            if e - ind <= 6 && (e == n || isSpace(s[e])) {
                out.add(ind, n, .heading)
                return
            }
        }
        if (c == 61 || c == 45 || c == 42 || c == 95) && ind < 4 {
            // === under a heading, and the rules --- *** ___.
            var k = ind, marks = 0
            while k < n && (s[k] == c || isSpace(s[k])) {
                if s[k] == c { marks += 1 }
                k += 1
            }
            if k == n && (c == 61 ? marks >= 1 : marks >= 3) {
                out.add(ind, n, c == 61 ? .heading : .keyword)
                return
            }
        }
        var i = ind
        while i < n && s[i] == 62 {
            out.add(i, i + 1, .keyword)
            i += 1
            while i < n && isSpace(s[i]) { i += 1 }
        }
        if i < n {
            let d = s[i]
            if (d == 45 || d == 42 || d == 43) && i + 1 < n && isSpace(s[i + 1]) {
                out.add(i, i + 1, .keyword)
                i += 2
            } else if isDigit(d) {
                var e = i
                while e < n && isDigit(s[e]) && e - i < 9 { e += 1 }
                if e < n && (s[e] == 46 || s[e] == 41) && (e + 1 == n || isSpace(s[e + 1])) {
                    out.add(i, e + 1, .keyword)
                    i = e + 1
                }
            }
            while i < n && isSpace(s[i]) { i += 1 }
            if i + 2 < n && s[i] == 91 && (s[i + 1] == 32 || s[i + 1] == 120 || s[i + 1] == 88) && s[i + 2] == 93 {
                out.add(i, i + 3, .keyword)
                i += 3
            }
        }
        // A link's definition: [label]: url
        if i == ind && i < n && s[i] == 91 {
            var e = i + 1
            while e < n && s[e] != 93 { e += 1 }
            if e + 1 < n && s[e + 1] == 58 {
                out.add(i, n, .link)
                return
            }
        }
        inline(s, i, n, &st, &out)
    }

    /// Markdown's inline elements from i: code, emphasis, links, autolinks,
    /// HTML and escapes. A search that finds no closer to the line's end
    /// is not made again for that kind, so a line stays linear.
    static func inline(_ s: UnsafePointer<UInt16>, _ from: Int, _ n: Int, _ st: inout SyntaxState,
                       _ out: inout SyntaxSink) {
        var i = from
        var none: UInt32 = 0   // the kinds with no closer left: bits by character and run
        while i < n {
            let c = s[i]
            if c == 92 {
                if i + 1 < n && s[i + 1] < 128 && !isAlnum(s[i + 1]) && !isSpace(s[i + 1]) {
                    out.add(i, i + 2, .escape)
                    i += 2
                } else {
                    i += 1
                }
                continue
            }
            if c == 96 {
                var e = i
                while e < n && s[e] == 96 { e += 1 }
                let run = e - i
                let bit: UInt32 = run <= 4 ? 1 << UInt32(truncatingIfNeeded: run - 1) : 0
                if none & bit != 0 {
                    i = e
                    continue
                }
                var j = e, found = -1
                while j < n {
                    if s[j] == 96 {
                        var k = j
                        while k < n && s[k] == 96 { k += 1 }
                        if k - j == run {
                            found = k
                            break
                        }
                        j = k
                    } else {
                        j += 1
                    }
                }
                if found < 0 {
                    none |= bit
                    i = e
                    continue
                }
                out.add(i, found, .code)
                i = found
                continue
            }
            if c == 42 || c == 95 || c == 126 {
                var e = i
                while e < n && s[e] == c { e += 1 }
                let run = e - i
                let shift: UInt32 = c == 42 ? 4 : (c == 95 ? 8 : 12)
                let bit: UInt32 = run <= 3 ? 1 << (shift + UInt32(truncatingIfNeeded: run - 1)) : 0
                if run > 3 || (c == 126 && run != 2) || e >= n || isSpace(s[e]) || (c == 95 && i > 0 && isAlnum(s[i - 1]))
                    || none & bit != 0 {
                    i = e
                    continue
                }
                var j = e, found = -1
                while j < n {
                    if s[j] == c {
                        var k = j
                        while k < n && s[k] == c { k += 1 }
                        if k - j == run && !isSpace(s[j - 1]) && !(c == 95 && k < n && isAlnum(s[k])) {
                            found = k
                            break
                        }
                        j = k
                    } else {
                        j += 1
                    }
                }
                if found < 0 {
                    none |= bit
                    i = e
                    continue
                }
                out.add(i, found, .emphasis)
                i = found
                continue
            }
            if c == 91 || (c == 33 && i + 1 < n && s[i + 1] == 91) {
                let e = link(s, c == 33 ? i + 1 : i, n, &none)
                if e > i {
                    out.add(i, e, .link)
                    i = e
                } else {
                    i += 1
                }
                continue
            }
            if c == 60 {
                i = angle(s, i, n, &st, &out)
                continue
            }
            if c == 104 && (i == 0 || !isAlnum(s[i - 1])) && (has(s, i, n, "https://") || has(s, i, n, "http://")) {
                var e = i
                while e < n && !isSpace(s[e]) && s[e] != 60 && s[e] != 62 && s[e] != 34 { e += 1 }
                while e > i && (s[e - 1] == 46 || s[e - 1] == 44 || s[e - 1] == 41 || s[e - 1] == 59 || s[e - 1] == 58) { e -= 1 }
                out.add(i, e, .link)
                i = e
                continue
            }
            if c == 38 {
                i = entity(s, i, n, &out)
                continue
            }
            i += 1
        }
    }

    /// A link from the [ at i: [text](url) or [text][ref], to its end; i
    /// when it is none.
    static func link(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int, _ none: inout UInt32) -> Int {
        if none & (1 << 16) != 0 { return i }
        var e = i + 1
        var depth = 1
        while e < n {
            if s[e] == 92 { e += 2; continue }
            if s[e] == 91 { depth += 1 } else if s[e] == 93 {
                depth -= 1
                if depth == 0 { break }
            }
            e += 1
        }
        if e >= n {
            none |= 1 << 16
            return i
        }
        guard e + 1 < n else { return i }
        let close: UInt16 = s[e + 1] == 40 ? 41 : (s[e + 1] == 91 ? 93 : 0)
        if close == 0 { return i }
        let bit: UInt32 = close == 41 ? 1 << 17 : 1 << 18
        if none & bit != 0 { return i }
        var k = e + 2
        while k < n && s[k] != close { k += 1 }
        if k >= n {
            none |= bit
            return i
        }
        return k + 1
    }

    /// What starts at a < in Markdown: an autolink, an HTML comment or a tag.
    static func angle(_ s: UnsafePointer<UInt16>, _ i: Int, _ n: Int, _ st: inout SyntaxState,
                      _ out: inout SyntaxSink) -> Int {
        if has(s, i, n, "<!--") {
            let e = find(s, i + 4, n, "-->")
            if e < 0 {
                out.add(i, n, .comment)
                st.mode = Mode.comment
                return n
            }
            out.add(i, e + 3, .comment)
            return e + 3
        }
        var j = i + 1
        if j < n && s[j] == 47 { j += 1 }
        guard j < n && isLetter(s[j]) else { return i + 1 }
        let a = j
        while j < n && (isAlnum(s[j]) || s[j] == 45) { j += 1 }
        // <https://…> and <alice@example.com>
        if j < n && (s[j] == 58 || s[j] == 64 || s[j] == 46) {
            var e = j
            while e < n && s[e] != 62 && !isSpace(s[e]) { e += 1 }
            if e < n && s[e] == 62 {
                out.add(i, e + 1, .link)
                return e + 1
            }
        }
        out.add(a, j, .tag)
        var e = j
        var value = false
        while e < n && s[e] != 62 {
            let c = s[e]
            if c == 34 || c == 39 {
                var k = e + 1
                while k < n && s[k] != c { k += 1 }
                k = min(k + 1, n)
                out.add(e, k, .string)
                e = k
                value = false
                continue
            }
            if c == 61 {
                value = true
                e += 1
                continue
            }
            if isWordStart(c) {
                var k = e + 1
                while k < n && (isWord(s[k]) || s[k] == 45) { k += 1 }
                out.add(e, k, value ? .string : .property)
                value = false
                e = k
                continue
            }
            e += 1
        }
        return min(e + 1, n)
    }

    // MARK: YAML, TOML and INI

    /// YAML: keys as property, values as string, number or constant,
    /// anchors, tags, comments, and | and > blocks.
    static func yaml(_ s: UnsafePointer<UInt16>, _ n: Int, _ st: inout SyntaxState, _ out: inout SyntaxSink) {
        var ind = 0
        while ind < n && s[ind] == 32 { ind += 1 }
        var i = ind
        if st.mode == Mode.scalar {
            if ind >= n || ind > Int(truncatingIfNeeded: st.data) {
                out.add(ind, n, .string)
                return
            }
            st.mode = 0
            st.data = 0
        } else if st.mode == Mode.string {
            i = string(s, 0, n, 0, st.kind, st.count, st.data, &st, &out)
            if st.mode != 0 { return }
        }
        if i == 0 && n >= 3 && ((s[0] == 45 && s[1] == 45 && s[2] == 45) || (s[0] == 46 && s[1] == 46 && s[2] == 46))
            && (n == 3 || isSpace(s[3])) {
            out.add(0, 3, .keyword)
            i = 3
        }
        var parent = ind      // what a | block's lines must be indented past
        var flow = 0          // depth in [ ] and { }
        var key = true        // a key may come here
        var block = false
        while i < n {
            let c = s[i]
            if isSpace(c) {
                i += 1
                continue
            }
            if c == 35 && (i == 0 || isSpace(s[i - 1])) {
                out.add(i, n, .comment)
                break
            }
            if c == 45 && key && (i + 1 == n || isSpace(s[i + 1])) {
                parent = i
                i += 1
                continue
            }
            if c == 91 || c == 123 {
                flow += 1
                key = c == 123
                i += 1
                continue
            }
            if c == 93 || c == 125 {
                if flow > 0 { flow -= 1 }
                key = false
                i += 1
                continue
            }
            if c == 44 {
                key = flow > 0
                i += 1
                continue
            }
            if c == 38 || c == 42 || c == 33 {
                var e = i + 1
                while e < n && !isSpace(s[e]) && s[e] != 44 && s[e] != 93 && s[e] != 125 { e += 1 }
                out.add(i, e, c == 33 ? .type : .variable)
                i = e
                continue
            }
            if c == 124 || c == 62 {
                var e = i + 1
                while e < n && (s[e] == 43 || s[e] == 45 || isDigit(s[e])) { e += 1 }
                var t = e
                while t < n && isSpace(s[t]) { t += 1 }
                if t == n || s[t] == 35 {
                    out.add(i, e, .keyword)
                    block = true
                    if t < n { out.add(t, n, .comment) }
                    break
                }
            }
            if c == 34 || c == 39 {
                let mark = out.count
                let e = string(s, i + 1, n, i, c == 34 ? Str.dqLines : Str.sql, 0, 0, &st, &out)
                if st.mode != 0 { return }
                var t = e
                while t < n && isSpace(s[t]) { t += 1 }
                if key && t < n && s[t] == 58 && (t + 1 == n || isSpace(s[t + 1])) {
                    out.retag(since: mark, .property)
                    parent = i
                    i = t + 1
                    key = false
                    continue
                }
                i = e
                key = false
                continue
            }
            // A plain scalar, to ": ", " #" or the line's end; , ] } end it in flow.
            let a = i
            var e = i
            var isKey = false
            while e < n {
                let d = s[e]
                if d == 58 && (e + 1 == n || isSpace(s[e + 1]) || (flow > 0 && (s[e + 1] == 44 || s[e + 1] == 93 || s[e + 1] == 125))) {
                    isKey = true
                    break
                }
                if d == 35 && e > a && isSpace(s[e - 1]) { break }
                if flow > 0 && (d == 44 || d == 93 || d == 125) { break }
                e += 1
            }
            var t = e
            while t > a && isSpace(s[t - 1]) { t -= 1 }
            if isKey && key {
                out.add(a, t, .property)
                parent = a
                i = e + 1
                key = false
                continue
            }
            out.add(a, t, scalar(s, a, t))
            key = false
            i = max(e, a + 1)
        }
        if block {
            st.mode = Mode.scalar
            st.data = UInt32(truncatingIfNeeded: parent)
        }
    }

    /// A YAML or TOML value: a number, a constant such as true, or else a
    /// string.
    static func scalar(_ s: UnsafePointer<UInt16>, _ a: Int, _ b: Int) -> CodeToken {
        if SyntaxWords.yaml.find(s, a, b) != 0 { return .constant }
        if isNumber(s, a, b) { return .number }
        return .string
    }

    /// Whether units a to b are a number: 12, -3.5e2, 0x1F, 0o17, .inf, 1_000.
    static func isNumber(_ s: UnsafePointer<UInt16>, _ a: Int, _ b: Int) -> Bool {
        var i = a
        if i < b && (s[i] == 43 || s[i] == 45) { i += 1 }
        if i + 1 < b && s[i] == 46 && (hasFolded(s, i + 1, b, "inf") || hasFolded(s, i + 1, b, "nan")) { return i + 4 == b }
        guard i < b && (isDigit(s[i]) || (s[i] == 46 && i + 1 < b && isDigit(s[i + 1]))) else { return false }
        if s[i] == 48 && i + 1 < b && (s[i + 1] == 120 || s[i + 1] == 111 || s[i + 1] == 98) {
            i += 2
            while i < b && (isHex(s[i]) || s[i] == 95) { i += 1 }
            return i == b
        }
        while i < b && (isDigit(s[i]) || s[i] == 95) { i += 1 }
        if i < b && s[i] == 46 {
            i += 1
            while i < b && (isDigit(s[i]) || s[i] == 95) { i += 1 }
        }
        if i < b && (s[i] == 101 || s[i] == 69) {
            i += 1
            if i < b && (s[i] == 43 || s[i] == 45) { i += 1 }
            while i < b && isDigit(s[i]) { i += 1 }
        }
        return i == b
    }

    /// TOML: [tables] as heading, keys as property, strings, numbers, dates
    /// and booleans.
    static func toml(_ s: UnsafePointer<UInt16>, _ n: Int, _ st: inout SyntaxState, _ out: inout SyntaxSink) {
        var i = 0
        if st.mode == Mode.string {
            i = string(s, 0, n, 0, st.kind, st.count, st.data, &st, &out)
            if st.mode != 0 { return }
        } else {
            while i < n && isSpace(s[i]) { i += 1 }
            if i < n && s[i] == 91 {
                var e = i
                while e < n && s[e] != 93 && s[e] != 35 { e += 1 }
                while e < n && s[e] == 93 { e += 1 }
                if e > i + 1 && s[e - 1] == 93 {
                    out.add(i, e, .heading)
                    i = e
                }
            }
        }
        while i < n {
            let c = s[i]
            if isSpace(c) {
                i += 1
                continue
            }
            if c == 35 {
                out.add(i, n, .comment)
                return
            }
            if c == 34 || c == 39 {
                let triple = i + 2 < n && s[i + 1] == c && s[i + 2] == c
                let kind = c == 34 ? (triple ? Str.textBlock : Str.dq) : (triple ? Str.literalLines : Str.literal)
                let mark = out.count
                let e = string(s, i + (triple ? 3 : 1), n, i, kind, 0, 0, &st, &out)
                if st.mode != 0 { return }
                if !triple && keyed(s, e, n) { out.retag(since: mark, .property) }
                i = e
                continue
            }
            if isAlnum(c) || c == 95 || c == 45 || c == 43 {
                var e = i + 1
                while e < n && (isAlnum(s[e]) || s[e] == 95 || s[e] == 45 || s[e] == 43 || s[e] == 46 || s[e] == 58) { e += 1 }
                if keyed(s, e, n) {
                    out.add(i, e, .property)
                } else if SyntaxWords.toml.find(s, i, e) & 31 == CodeToken.constant.rawValue {
                    out.add(i, e, .constant)
                } else if isDigit(c) || c == 43 || c == 45 || SyntaxWords.toml.find(s, i, e) != 0 {
                    out.add(i, e, .number)
                }
                i = e
                continue
            }
            i += 1
        }
    }

    /// Whether a TOML key ends at e: an = follows.
    static func keyed(_ s: UnsafePointer<UInt16>, _ e: Int, _ n: Int) -> Bool {
        var t = e
        while t < n && isSpace(s[t]) { t += 1 }
        return t < n && s[t] == 61
    }

    /// INI, .env and config files: [sections] as heading, keys as property,
    /// values, and ; and # comments.
    static func ini(_ s: UnsafePointer<UInt16>, _ n: Int, _ out: inout SyntaxSink) {
        var i = 0
        while i < n && isSpace(s[i]) { i += 1 }
        if i >= n { return }
        let c = s[i]
        if c == 59 || c == 35 {
            out.add(i, n, .comment)
            return
        }
        if c == 91 {
            var e = i + 1
            while e < n && s[e] != 93 { e += 1 }
            e = min(e + 1, n)
            out.add(i, e, .heading)
            iniValue(s, e, n, &out)
            return
        }
        let indented = i > 0
        if has(s, i, n, "export") && i + 6 < n && isSpace(s[i + 6]) {
            out.add(i, i + 6, .keyword)
            i += 7
            while i < n && isSpace(s[i]) { i += 1 }
        }
        var sep = i
        while sep < n && s[sep] != 61 && s[sep] != 58 { sep += 1 }
        if sep >= n {
            if indented {
                iniValue(s, i, n, &out)
                return
            }
            var e = i
            while e < n && !isSpace(s[e]) { e += 1 }
            out.add(i, e, .property)
            iniValue(s, e, n, &out)
            return
        }
        var t = sep
        while t > i && isSpace(s[t - 1]) { t -= 1 }
        out.add(i, t, .property)
        iniValue(s, sep + 1, n, &out)
    }

    /// An INI value: a quoted string, a number, a constant, or text with
    /// $variables, and a comment after a space and ; or #.
    static func iniValue(_ s: UnsafePointer<UInt16>, _ from: Int, _ n: Int, _ out: inout SyntaxSink) {
        var i = from
        while i < n && isSpace(s[i]) { i += 1 }
        let a = i
        var e = n
        var j = i
        while j < n {
            if (s[j] == 59 || s[j] == 35) && j > a && isSpace(s[j - 1]) {
                e = j
                break
            }
            j += 1
        }
        var t = e
        while t > a && isSpace(s[t - 1]) { t -= 1 }
        if t > a && (SyntaxWords.ini.find(s, a, t) != 0 || isNumber(s, a, t)) {
            out.add(a, t, SyntaxWords.ini.find(s, a, t) != 0 ? .constant : .number)
        } else {
            var k = a
            while k < t {
                let c = s[k]
                if c == 34 || c == 39 {
                    var m = k + 1
                    while m < t && s[m] != c {
                        if s[m] == 92 && c == 34 { m += 1 }
                        m += 1
                    }
                    m = min(m + 1, t)
                    out.add(k, m, .string)
                    k = m
                    continue
                }
                if c == 36 {
                    let v = shellVariable(s, k, t)
                    if v > k + 1 { out.add(k, v, .variable) }
                    k = v
                    continue
                }
                k += 1
            }
        }
        if e < n { out.add(e, n, .comment) }
    }

    // MARK: Diff and lists

    /// A diff: added and removed lines, hunks' @@ headers, and file headers.
    /// The state counts a hunk's lines down, so a removed line that starts
    /// with -- is never taken for a file's header.
    static func diff(_ s: UnsafePointer<UInt16>, _ n: Int, _ st: inout SyntaxState, _ out: inout SyntaxSink) {
        var old = st.data >> 16, new = st.data & 0xFFFF
        if old > 0 || new > 0 {
            let c: UInt16 = n > 0 ? s[0] : 32
            var ok = true
            if c == 43 {
                out.add(0, n, .inserted)
                if new > 0 { new -= 1 }
            } else if c == 45 {
                out.add(0, n, .deleted)
                if old > 0 { old -= 1 }
            } else if c == 32 {
                if old > 0 { old -= 1 }
                if new > 0 { new -= 1 }
            } else if c == 92 {
                out.add(0, n, .comment)
            } else {
                ok = false
            }
            if ok {
                st.data = old << 16 | new
                return
            }
        }
        st.data = 0
        if has(s, 0, n, "@@") {
            // @@ -a,b +c,d @@: b lines of the old file and d of the new to come.
            var i = 2
            func count(_ sign: UInt16) -> UInt32 {
                while i < n && s[i] != sign { i += 1 }
                i += 1
                var start: UInt32 = 0
                while i < n && isDigit(s[i]) { start = start &* 10 &+ UInt32(truncatingIfNeeded: s[i] - 48); i += 1 }
                if i < n && s[i] == 44 {
                    i += 1
                    var lines: UInt32 = 0
                    while i < n && isDigit(s[i]) { lines = min(lines &* 10 &+ UInt32(truncatingIfNeeded: s[i] - 48), 0xFFFF); i += 1 }
                    return lines
                }
                return 1
            }
            let a = count(45), b = count(43)
            let close = find(s, min(i, n), n, "@@")
            out.add(0, close < 0 ? n : close + 2, .attribute)
            st.data = a << 16 | b
            return
        }
        if has(s, 0, n, "+++") {
            out.add(0, n, .inserted)
        } else if has(s, 0, n, "---") {
            out.add(0, n, .deleted)
        } else if has(s, 0, n, "diff ") || has(s, 0, n, "index ") || has(s, 0, n, "new file") || has(s, 0, n, "deleted file")
                    || has(s, 0, n, "similarity ") || has(s, 0, n, "rename ") || has(s, 0, n, "old mode")
                    || has(s, 0, n, "new mode") || has(s, 0, n, "Binary files") || has(s, 0, n, "Only in") {
            out.add(0, n, .keyword)
        } else if n > 0 && s[0] == 92 {
            out.add(0, n, .comment)
        } else if n > 0 && (s[0] == 43 || s[0] == 62) {
            out.add(0, n, .inserted)
        } else if n > 0 && (s[0] == 45 || s[0] == 60) {
            out.add(0, n, .deleted)
        }
    }

    /// .gitignore and the like: comments, and ! before a pattern.
    static func ignore(_ s: UnsafePointer<UInt16>, _ n: Int, _ out: inout SyntaxSink) {
        var i = 0
        while i < n && isSpace(s[i]) { i += 1 }
        if i < n && s[i] == 35 { out.add(i, n, .comment) } else if i < n && s[i] == 33 { out.add(i, i + 1, .keyword) }
    }
}
