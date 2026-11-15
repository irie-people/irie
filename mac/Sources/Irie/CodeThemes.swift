import AppKit

// The Code card's colour themes. Irie's own is worked out from the palette
// in use, so it changes with View > Theme: it wears the terminal's ground
// and ink in either appearance, as the terminal itself does, so a code card
// and a terminal card look like one family. Its tokens take the agents' six
// hues (Theme.agent(slot:)) at one low chroma, so they differ by hue alone
// and none glares.
//
// The others keep their makers' published values (PROVENANCE.md), mapped onto
// the token kinds from their own scope rules. Where a colour falls short of
// WCAG 2.2's 4.5:1 on the ground or the current line, it moves only as much
// lighter or darker as reaches it, its hue and chroma kept; a comment keeps
// the theme's own colour down to 3:1. Line numbers reach 4.5:1 too, and the
// caret 3:1, as a mark.

/// A code card's colours: its ground, text, caret, selection, current line
/// and line numbers, and a colour for each kind of token.
struct CodeTheme: Identifiable, Equatable {
    let id: String
    let name: String
    let dark: Bool
    let background, foreground, caret, selection, currentLine, gutterInk, gutterActiveInk: NSColor
    /// The tokens' colours, by CodeToken's raw value.
    fileprivate let inks: [NSColor]

    /// The colour of a kind of token; the text's own for plain.
    func color(_ t: CodeToken) -> NSColor {
        let i = Int(t.rawValue)
        return i < inks.count ? inks[i] : foreground
    }
}

/// A theme with a light and a dark set follows the app's appearance; one
/// with a single set always wears it.
struct CodeThemeFamily: Identifiable {
    let id: String
    let name: String
    let light: CodeTheme?
    let dark: CodeTheme?
}

enum CodeThemes {
    /// Where the app keeps the choice, a family's id.
    static let defaultsKey = "codeTheme"
    static let standard = Brand.slug

    /// Every family, Irie's first.
    static var families: [CodeThemeFamily] { [own()] + others }

    /// The family's set for the appearance; Irie's for an id there is not.
    static func theme(_ family: String, dark: Bool) -> CodeTheme {
        let f = family == standard ? own() : others.first { $0.id == family } ?? own()
        if let t = dark ? f.dark ?? f.light : f.light ?? f.dark { return t }
        return terminalSet()
    }

    // MARK: Irie's own

    /// Irie's family for the palette in use, made again when it changes.
    private static var made: (palette: Palette, terminal: TerminalColors, family: CodeThemeFamily)?

    private static func own() -> CodeThemeFamily {
        let p = Theme.palette, t = Theme.terminalColors
        if let m = made, m.palette == p, m.terminal == t { return m.family }
        let set = terminalSet()
        let f = CodeThemeFamily(id: standard, name: Brand.name, light: set, dark: set)
        made = (p, t, f)
        return f
    }

    /// The tokens' OKLCH chroma, and their lightness on a light ground and
    /// on a dark one, before contrast moves them.
    private static let chroma = 0.075
    private static let lightness = (light: 0.5, dark: 0.8)

    /// Irie's set, on the terminal's ground and ink, with the palette's
    /// colours for a ground that dark, or that light: a theme of yours may
    /// give the terminal a light ground, and then text goes darker.
    private static func terminalSet() -> CodeTheme {
        let p = Theme.palette
        let ground = Theme.terminalColors.ground
        let ink = Theme.terminalColors.ink
        let line = Theme.mix(ground, ink, 0.05)
        let grounds = [ground, line]
        let deep = Theme.luminance(ground) > 0.18
        let dark = !deep
        let s = dark ? p.dark : p.light
        let hues = (0..<Theme.agentSlots).map { Theme.oklch(Theme.agentHex(slot: $0, palette: p, dark: dark, high: false)).h }
        // A little over 4.5:1 leaves room for rounding.
        func hue(_ slot: Int) -> UInt32 {
            reach(Theme.hex(l: deep ? lightness.light : lightness.dark, c: chroma, h: hues[slot % hues.count]), 4.6, on: grounds)
        }
        // The agents' hues run from the needs red's one side round to its
        // other: amber, green, teal, blue, violet and pink in Rosewater.
        let amber = hue(0), green = hue(1), teal = hue(2), blue = hue(3), violet = hue(4), pink = hue(5)
        let quiet = toward(ink, ground, 4.6, on: grounds)
        let tokens: [CodeToken: UInt32] = [
            .comment: quiet, .keyword: violet, .type: teal, .function: blue, .string: green, .number: amber,
            .constant: amber, .attribute: amber, .variable: pink, .property: blue, .tag: violet, .heading: blue,
            .link: teal, .emphasis: pink, .code: green, .escape: teal, .inserted: green, .deleted: pink,
        ]
        return CodeTheme(id: standard + (dark ? "-dark" : "-light"), name: Brand.name + (dark ? " Dark" : " Light"),
                         dark: !deep, background: NSColor(hex: ground), foreground: NSColor(hex: ink),
                         caret: NSColor(hex: reach(s.accent, 3, on: grounds)), selection: highlight(dark: !deep),
                         currentLine: NSColor(hex: line), gutterInk: NSColor(hex: quiet),
                         gutterActiveInk: NSColor(hex: toward(ink, ground, 7, on: grounds)),
                         inks: CodeToken.allCases.map { NSColor(hex: tokens[$0] ?? ink) })
    }

    /// The Mac's own colour for selected text, as anywhere else, in the
    /// appearance's form.
    private static func highlight(dark: Bool) -> NSColor {
        var color = NSColor(hex: dark ? 0x3F638B : 0xB3D7FF)
        NSAppearance(named: dark ? .darkAqua : .aqua)?.performAsCurrentDrawingAppearance {
            color = NSColor.selectedTextBackgroundColor.usingColorSpace(.sRGB) ?? color
        }
        return color
    }

    // MARK: Contrast

    /// c, or as little lighter or darker as reaches the ratio on every
    /// ground: its OKLCH lightness moves, its hue and chroma stay.
    static func reach(_ c: UInt32, _ want: Double, on grounds: [UInt32]) -> UInt32 {
        let ys = grounds.map(Theme.luminance)
        func ok(_ x: UInt32) -> Bool {
            let y = Theme.luminance(x)
            return ys.allSatisfy { Theme.ratio(y, $0) >= want }
        }
        if ok(c) { return c }
        let (l, ch, h) = Theme.oklch(c)
        // Away from the grounds: lighter on dark ones, darker on light ones.
        let lighter = ys.reduce(0, +) / Double(ys.count) < 0.18
        var lo = l, hi = lighter ? 1.0 : 0.0
        for _ in 0..<20 {
            let mid = (lo + hi) / 2
            if ok(Theme.hex(l: mid, c: ch, h: h)) { hi = mid } else { lo = mid }
        }
        let found = Theme.hex(l: hi, c: ch, h: h)
        return ok(found) ? found : (lighter ? 0xFFFFFF : 0x000000)
    }

    /// ink taken toward the ground as far as keeps the ratio on every ground.
    static func toward(_ ink: UInt32, _ ground: UInt32, _ want: Double, on grounds: [UInt32]) -> UInt32 {
        let ys = grounds.map(Theme.luminance)
        func ok(_ t: Double) -> Bool {
            let y = Theme.luminance(Theme.mix(ink, ground, t))
            return ys.allSatisfy { Theme.ratio(y, $0) >= want }
        }
        if !ok(0) { return reach(ink, want, on: grounds) }
        var lo = 0.0, hi = 1.0
        for _ in 0..<16 {
            let mid = (lo + hi) / 2
            if ok(mid) { lo = mid } else { hi = mid }
        }
        return Theme.mix(ink, ground, lo)
    }

    // MARK: Others

    /// The others, in the order the menu lists them.
    private static let others: [CodeThemeFamily] = [github, solarized, one, catppuccin, gruvbox, tokyoNight, rosePine,
                                                    dracula, nord]

    /// a over b at an alpha, as a theme's translucent colours are drawn.
    private static func over(_ b: UInt32, _ a: UInt32, _ alpha: Double) -> UInt32 { Theme.mix(b, a, alpha) }

    /// A theme from its maker's 0xRRGGBB values, brought to the contrast the
    /// card keeps: 4.5:1 for text and line numbers on the ground and the
    /// current line, the theme's own comment colour down to 3:1, and 3:1
    /// for the caret.
    private static func make(_ id: String, _ name: String, dark: Bool, ground: UInt32, ink: UInt32, caret: UInt32,
                             selection: UInt32, line: UInt32, gutter: UInt32, gutterActive: UInt32,
                             _ tokens: [CodeToken: UInt32]) -> CodeTheme {
        let grounds = [ground, line]
        func text(_ c: UInt32) -> NSColor { NSColor(hex: reach(c, 4.5, on: grounds)) }
        let inks: [NSColor] = CodeToken.allCases.map { t in
            guard let c = tokens[t] else { return text(ink) }
            return t == .comment ? NSColor(hex: reach(c, 3, on: grounds)) : text(c)
        }
        return CodeTheme(id: id, name: name, dark: dark, background: NSColor(hex: ground), foreground: text(ink),
                         caret: NSColor(hex: reach(caret, 3, on: [ground])), selection: NSColor(hex: selection),
                         currentLine: NSColor(hex: line), gutterInk: text(gutter), gutterActiveInk: text(gutterActive),
                         inks: inks)
    }

    /// GitHub's Light Default and Dark Default (primer/github-vscode-theme,
    /// MIT). It leaves the selection to the editor, so it takes VS Code's.
    static let github = CodeThemeFamily(
        id: "github", name: "GitHub",
        light: make("github-light", "GitHub Light", dark: false, ground: 0xFFFFFF, ink: 0x1F2328, caret: 0x0969DA,
                    selection: 0xADD6FF, line: over(0xFFFFFF, 0xEAEEF2, 0x80 / 255.0), gutter: 0x8C959F,
                    gutterActive: 0x1F2328, [
                        .comment: 0x6E7781, .keyword: 0xCF222E, .type: 0x953800, .function: 0x8250DF,
                        .string: 0x0A3069, .number: 0x0550AE, .constant: 0x0550AE, .attribute: 0x8250DF,
                        .variable: 0x953800, .property: 0x0550AE, .tag: 0x116329, .heading: 0x0550AE,
                        .link: 0x0A3069, .emphasis: 0x1F2328, .code: 0x0550AE, .escape: 0xCF222E,
                        .inserted: 0x116329, .deleted: 0x82071E,
                    ]),
        dark: make("github-dark", "GitHub Dark", dark: true, ground: 0x0D1117, ink: 0xE6EDF3, caret: 0x2F81F7,
                   selection: 0x264F78, line: over(0x0D1117, 0x6E7681, 0x1A / 255.0), gutter: 0x6E7681,
                   gutterActive: 0xE6EDF3, [
                       .comment: 0x8B949E, .keyword: 0xFF7B72, .type: 0xFFA657, .function: 0xD2A8FF,
                       .string: 0xA5D6FF, .number: 0x79C0FF, .constant: 0x79C0FF, .attribute: 0xD2A8FF,
                       .variable: 0xFFA657, .property: 0x79C0FF, .tag: 0x7EE787, .heading: 0x79C0FF,
                       .link: 0xA5D6FF, .emphasis: 0xE6EDF3, .code: 0x79C0FF, .escape: 0xFF7B72,
                       .inserted: 0x7EE787, .deleted: 0xFFA198,
                   ]))

    /// Solarized (MIT), as its Vim scheme maps the palette, whose base
    /// tones swap between light and dark. The selection is VS Code's
    /// Solarized's; the light one has no current-line band, since VS Code
    /// gives the band and the selection one colour.
    static let solarized = CodeThemeFamily(
        id: "solarized", name: "Solarized",
        light: make("solarized-light", "Solarized Light", dark: false, ground: 0xFDF6E3, ink: 0x657B83, caret: 0x657B83,
                    selection: 0xEEE8D5, line: 0xFDF6E3, gutter: 0x93A1A1, gutterActive: 0x586E75, [
                        .comment: 0x93A1A1, .keyword: 0x859900, .type: 0xB58900, .function: 0x268BD2,
                        .string: 0x2AA198, .number: 0x2AA198, .constant: 0x2AA198, .attribute: 0xCB4B16,
                        .variable: 0x268BD2, .property: 0x268BD2, .tag: 0x268BD2, .heading: 0xCB4B16,
                        .link: 0x6C71C4, .emphasis: 0xD33682, .code: 0x2AA198, .escape: 0xDC322F,
                        .inserted: 0x859900, .deleted: 0xDC322F,
                    ]),
        dark: make("solarized-dark", "Solarized Dark", dark: true, ground: 0x002B36, ink: 0x839496, caret: 0x839496,
                   selection: 0x274642, line: 0x073642, gutter: 0x586E75, gutterActive: 0x93A1A1, [
                       .comment: 0x586E75, .keyword: 0x859900, .type: 0xB58900, .function: 0x268BD2,
                       .string: 0x2AA198, .number: 0x2AA198, .constant: 0x2AA198, .attribute: 0xCB4B16,
                       .variable: 0x268BD2, .property: 0x268BD2, .tag: 0x268BD2, .heading: 0xCB4B16,
                       .link: 0x6C71C4, .emphasis: 0xD33682, .code: 0x2AA198, .escape: 0xDC322F,
                       .inserted: 0x859900, .deleted: 0xDC322F,
                   ]))

    /// Atom's One Light and One Dark (MIT): their HSL values in hex, mapped
    /// as their syntax variables and base styles do.
    static let one = CodeThemeFamily(
        id: "one", name: "One",
        light: make("one-light", "One Light", dark: false, ground: 0xFAFAFA, ink: 0x383A42, caret: 0x526FFF,
                    selection: 0xE5E5E6, line: over(0xFAFAFA, 0x383A42, 0.05), gutter: 0x9D9D9F, gutterActive: 0x383A42, [
                        .comment: 0xA0A1A7, .keyword: 0xA626A4, .type: 0xCB7701, .function: 0x4078F2,
                        .string: 0x50A14F, .number: 0xB76B01, .constant: 0xB76B01, .attribute: 0x4078F2,
                        .variable: 0xE45649, .property: 0xE45649, .tag: 0xE45649, .heading: 0xE45649,
                        .link: 0x0184BC, .emphasis: 0xA626A4, .code: 0x50A14F, .escape: 0x0184BC,
                        .inserted: 0x50A14F, .deleted: 0xE45649,
                    ]),
        dark: make("one-dark", "One Dark", dark: true, ground: 0x282C34, ink: 0xABB2BF, caret: 0x528BFF,
                   selection: 0x3E4451, line: over(0x282C34, 0x99BBFF, 0.04), gutter: 0x636D83, gutterActive: 0xABB2BF, [
                       .comment: 0x5C6370, .keyword: 0xC678DD, .type: 0xE5C07B, .function: 0x61AFEF,
                       .string: 0x98C379, .number: 0xD19A66, .constant: 0xD19A66, .attribute: 0x61AFEF,
                       .variable: 0xE06C75, .property: 0xE06C75, .tag: 0xE06C75, .heading: 0xE06C75,
                       .link: 0x56B6C2, .emphasis: 0xC678DD, .code: 0x98C379, .escape: 0x56B6C2,
                       .inserted: 0x98C379, .deleted: 0xE06C75,
                   ]))

    /// Catppuccin's Latte and Mocha (MIT), as its style guide gives code
    /// editors their colours.
    static let catppuccin = CodeThemeFamily(
        id: "catppuccin", name: "Catppuccin",
        light: make("catppuccin-latte", "Catppuccin Latte", dark: false, ground: 0xEFF1F5, ink: 0x4C4F69, caret: 0xDC8A78,
                    selection: over(0xEFF1F5, 0x7C7F93, 0.25), line: over(0xEFF1F5, 0x4C4F69, 0.1), gutter: 0x8C8FA1,
                    gutterActive: 0x7287FD, [
                        .comment: 0x7C7F93, .keyword: 0x8839EF, .type: 0xDF8E1D, .function: 0x1E66F5,
                        .string: 0x40A02B, .number: 0xFE640B, .constant: 0xFE640B, .attribute: 0xDF8E1D,
                        .variable: 0xE64553, .property: 0x1E66F5, .tag: 0x8839EF, .heading: 0xD20F39,
                        .link: 0x1E66F5, .emphasis: 0xE64553, .code: 0x40A02B, .escape: 0xEA76CB,
                        .inserted: 0x40A02B, .deleted: 0xD20F39,
                    ]),
        dark: make("catppuccin-mocha", "Catppuccin Mocha", dark: true, ground: 0x1E1E2E, ink: 0xCDD6F4, caret: 0xF5E0DC,
                   selection: over(0x1E1E2E, 0x9399B2, 0.25), line: over(0x1E1E2E, 0xCDD6F4, 0.1), gutter: 0x7F849C,
                   gutterActive: 0xB4BEFE, [
                       .comment: 0x9399B2, .keyword: 0xCBA6F7, .type: 0xF9E2AF, .function: 0x89B4FA,
                       .string: 0xA6E3A1, .number: 0xFAB387, .constant: 0xFAB387, .attribute: 0xF9E2AF,
                       .variable: 0xEBA0AC, .property: 0x89B4FA, .tag: 0xCBA6F7, .heading: 0xF38BA8,
                       .link: 0x89B4FA, .emphasis: 0xEBA0AC, .code: 0xA6E3A1, .escape: 0xF5C2E7,
                       .inserted: 0xA6E3A1, .deleted: 0xF38BA8,
                   ]))

    /// Gruvbox (MIT), at its medium contrast, as its Vim scheme maps the
    /// palette.
    static let gruvbox = CodeThemeFamily(
        id: "gruvbox", name: "Gruvbox",
        light: make("gruvbox-light", "Gruvbox Light", dark: false, ground: 0xFBF1C7, ink: 0x3C3836, caret: 0x3C3836,
                    selection: 0xBDAE93, line: 0xEBDBB2, gutter: 0xA89984, gutterActive: 0xB57614, [
                        .comment: 0x928374, .keyword: 0x9D0006, .type: 0xB57614, .function: 0x79740E,
                        .string: 0x79740E, .number: 0x8F3F71, .constant: 0x8F3F71, .attribute: 0x427B58,
                        .variable: 0x076678, .property: 0x076678, .tag: 0x427B58, .heading: 0x79740E,
                        .link: 0x8F3F71, .emphasis: 0x665C54, .code: 0x427B58, .escape: 0xAF3A03,
                        .inserted: 0x79740E, .deleted: 0x9D0006,
                    ]),
        dark: make("gruvbox-dark", "Gruvbox Dark", dark: true, ground: 0x282828, ink: 0xEBDBB2, caret: 0xEBDBB2,
                   selection: 0x665C54, line: 0x3C3836, gutter: 0x7C6F64, gutterActive: 0xFABD2F, [
                       .comment: 0x928374, .keyword: 0xFB4934, .type: 0xFABD2F, .function: 0xB8BB26,
                       .string: 0xB8BB26, .number: 0xD3869B, .constant: 0xD3869B, .attribute: 0x8EC07C,
                       .variable: 0x83A598, .property: 0x83A598, .tag: 0x8EC07C, .heading: 0xB8BB26,
                       .link: 0xD3869B, .emphasis: 0xBDAE93, .code: 0x8EC07C, .escape: 0xFE8019,
                       .inserted: 0xB8BB26, .deleted: 0xFB4934,
                   ]))

    /// Tokyo Night's Day and Night (tokyonight.nvim, Apache-2.0), from its
    /// Helix theme and its Neovim editor groups.
    static let tokyoNight = CodeThemeFamily(
        id: "tokyo-night", name: "Tokyo Night",
        light: make("tokyo-night-day", "Tokyo Night Day", dark: false, ground: 0xE1E2E7, ink: 0x3760BF, caret: 0x3760BF,
                    selection: 0xB7C1E3, line: 0xC4C8DA, gutter: 0xA8AECB, gutterActive: 0xB15C00, [
                        .comment: 0x848CB5, .keyword: 0x9854F1, .type: 0x188092, .function: 0x2E7DE9,
                        .string: 0x587539, .number: 0xB15C00, .constant: 0xB15C00, .attribute: 0x007197,
                        .variable: 0xF52A65, .property: 0x387068, .tag: 0x9854F1, .heading: 0x2E7DE9,
                        .link: 0x118C74, .emphasis: 0x3760BF, .code: 0x587539, .escape: 0x9854F1,
                        .inserted: 0x4197A4, .deleted: 0xC47981,
                    ]),
        dark: make("tokyo-night", "Tokyo Night", dark: true, ground: 0x1A1B26, ink: 0xC0CAF5, caret: 0xC0CAF5,
                   selection: 0x283457, line: 0x292E42, gutter: 0x3B4261, gutterActive: 0xFF9E64, [
                       .comment: 0x565F89, .keyword: 0xBB9AF7, .type: 0x2AC3DE, .function: 0x7AA2F7,
                       .string: 0x9ECE6A, .number: 0xFF9E64, .constant: 0xFF9E64, .attribute: 0x7DCFFF,
                       .variable: 0xF7768E, .property: 0x73DACA, .tag: 0xBB9AF7, .heading: 0x7AA2F7,
                       .link: 0x1ABC9C, .emphasis: 0xC0CAF5, .code: 0x9ECE6A, .escape: 0xBB9AF7,
                       .inserted: 0x449DAB, .deleted: 0x914C54,
                   ]))

    /// Rosé Pine's Dawn and Main (MIT), as its Neovim theme maps them.
    static let rosePine = CodeThemeFamily(
        id: "rose-pine", name: "Rosé Pine",
        light: make("rose-pine-dawn", "Rosé Pine Dawn", dark: false, ground: 0xFAF4ED, ink: 0x464261, caret: 0xCECACD,
                    selection: over(0xFAF4ED, 0x907AA9, 0.15), line: 0xF2E9E1, gutter: 0x9893A5, gutterActive: 0x464261, [
                        .comment: 0x797593, .keyword: 0x286983, .type: 0x56949F, .function: 0xD7827E,
                        .string: 0xEA9D34, .number: 0xEA9D34, .constant: 0xEA9D34, .attribute: 0x907AA9,
                        .variable: 0xB4637A, .property: 0x56949F, .tag: 0x56949F, .heading: 0x56949F,
                        .link: 0x907AA9, .emphasis: 0x464261, .code: 0xEA9D34, .escape: 0x286983,
                        .inserted: 0x56949F, .deleted: 0xB4637A,
                    ]),
        dark: make("rose-pine", "Rosé Pine", dark: true, ground: 0x191724, ink: 0xE0DEF4, caret: 0x524F67,
                   selection: over(0x191724, 0xC4A7E7, 0.15), line: 0x26233A, gutter: 0x6E6A86, gutterActive: 0xE0DEF4, [
                       .comment: 0x908CAA, .keyword: 0x31748F, .type: 0x9CCFD8, .function: 0xEBBCBA,
                       .string: 0xF6C177, .number: 0xF6C177, .constant: 0xF6C177, .attribute: 0xC4A7E7,
                       .variable: 0xEB6F92, .property: 0x9CCFD8, .tag: 0x9CCFD8, .heading: 0x9CCFD8,
                       .link: 0xC4A7E7, .emphasis: 0xE0DEF4, .code: 0xF6C177, .escape: 0x31748F,
                       .inserted: 0x9CCFD8, .deleted: 0xEB6F92,
                   ]))

    /// Dracula (MIT), as its VS Code theme maps the palette.
    static let dracula = CodeThemeFamily(
        id: "dracula", name: "Dracula", light: nil,
        dark: make("dracula", "Dracula", dark: true, ground: 0x282A36, ink: 0xF8F8F2, caret: 0xF8F8F2,
                   selection: 0x44475A, line: over(0x282A36, 0x44475A, 0x75 / 255.0), gutter: 0x6272A4,
                   gutterActive: 0xF8F8F2, [
                       .comment: 0x6272A4, .keyword: 0xFF79C6, .type: 0x8BE9FD, .function: 0x50FA7B,
                       .string: 0xF1FA8C, .number: 0xBD93F9, .constant: 0xBD93F9, .attribute: 0x50FA7B,
                       .variable: 0xBD93F9, .property: 0x8BE9FD, .tag: 0xFF79C6, .heading: 0xBD93F9,
                       .link: 0x8BE9FD, .emphasis: 0xF1FA8C, .code: 0x50FA7B, .escape: 0xFF79C6,
                       .inserted: 0x50FA7B, .deleted: 0xFF5555,
                   ]))

    /// Nord (MIT), as its VS Code theme maps the palette.
    static let nord = CodeThemeFamily(
        id: "nord", name: "Nord", light: nil,
        dark: make("nord", "Nord", dark: true, ground: 0x2E3440, ink: 0xD8DEE9, caret: 0xD8DEE9,
                   selection: over(0x2E3440, 0x434C5E, 0xCC / 255.0), line: 0x3B4252, gutter: 0x4C566A,
                   gutterActive: 0xD8DEE9, [
                       .comment: 0x616E88, .keyword: 0x81A1C1, .type: 0x8FBCBB, .function: 0x88C0D0,
                       .string: 0xA3BE8C, .number: 0xB48EAD, .constant: 0x81A1C1, .attribute: 0xD08770,
                       .variable: 0x81A1C1, .property: 0x8FBCBB, .tag: 0x81A1C1, .heading: 0x88C0D0,
                       .link: 0x88C0D0, .emphasis: 0xD8DEE9, .code: 0x8FBCBB, .escape: 0xEBCB8B,
                       .inserted: 0xA3BE8C, .deleted: 0xBF616A,
                   ]))
}
