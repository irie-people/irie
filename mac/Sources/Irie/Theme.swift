import AppKit
import SwiftUI

/// One of Irie's palettes: a light and a dark set of swatches. Rosewater is
/// the default; View > Theme picks Terracotta, Lilac or Evergreen, and the app keeps
/// the choice. In every palette and mode, each text colour keeps 4.5:1 and
/// each mark 3:1 on every surface it sits on (WCAG 2.2), and Increase
/// Contrast strengthens them further. The terminal sidebar has the same
/// palettes (internal/ui), and its tests check both the ratios and that the
/// two sets of values match.
struct Palette: Identifiable, Equatable {
    let id: String
    let name: String
    let light: Swatches
    let dark: Swatches

    static let all: [Palette] = [.rosewater, .terracotta, .lilac, .evergreen]
    static let standard = Palette.rosewater
    /// Where the app keeps the choice.
    static let defaultsKey = "theme"

    static func named(_ id: String?) -> Palette { all.first { $0.id == id } ?? standard }
}

/// One mode of a palette, as 0xRRGGBB.
struct Swatches: Equatable {
    // Solid grounds: the sidebar, the terminal's surround, cards, the
    // details panel and fields; a quiet card's hairline; the fills of a
    // hovered card, the selected one and one that needs you.
    var sidebar, ground, card, panel, field: UInt32
    var edge, hover, selection, needsTint: UInt32
    // Text, and quieter text.
    var ink, muted: UInt32
    // Irie's accent: the selected card's edge and focus rings, its text
    // form, and the start button with its arrow.
    var accent, accentInk, accentFill, onAccent: UInt32
    // The colour roles of DESIGN §5.3: red, green, yellow and cyan.
    var needs, done, paused, frozen: UInt32
    // The mark: the sun and the waves.
    var sun, waves: UInt32
}

extension Palette {
    static let rosewater = Palette(
        id: "rosewater", name: "Rosewater",
        light: Swatches(
            sidebar: 0xFBF1EE, ground: 0xF3E3DE, card: 0xFFF9F7, panel: 0xFCEFEB, field: 0xFFFCFB,
            edge: 0xEBD3CB, hover: 0xFFF3EF, selection: 0xF3DDEA, needsTint: 0xFEE4D8,
            ink: 0x3D2236, muted: 0x75566A,
            accent: 0xC4577A, accentInk: 0xA23E62, accentFill: 0xE58FA8, onAccent: 0x3D2236,
            needs: 0xB42A1E, done: 0x44694B, paused: 0x855700, frozen: 0x286873,
            sun: 0xF2B544, waves: 0xD9819C),
        dark: Swatches(
            sidebar: 0x251B22, ground: 0x1D151B, card: 0x30242C, panel: 0x2C2129, field: 0x33262F,
            edge: 0x4A3843, hover: 0x392B34, selection: 0x45293F, needsTint: 0x4A251C,
            ink: 0xF7E9EF, muted: 0xC2AAB7,
            accent: 0xEE8FAD, accentInk: 0xF4A7BF, accentFill: 0xEE8FAD, onAccent: 0x2A1A24,
            needs: 0xFF8070, done: 0x9BC9A2, paused: 0xEFC468, frozen: 0x7DC4CE,
            sun: 0xF2B544, waves: 0xEE8FAD))

    static let terracotta = Palette(
        id: "terracotta", name: "Terracotta",
        light: Swatches(
            sidebar: 0xF9F2EA, ground: 0xF0E4D6, card: 0xFFFBF6, panel: 0xFBF3EA, field: 0xFFFDF9,
            edge: 0xE7D6C3, hover: 0xFFF6EC, selection: 0xF8E0CF, needsTint: 0xFBE2E4,
            ink: 0x3A2629, muted: 0x735E58,
            accent: 0xC2603A, accentInk: 0xA24A26, accentFill: 0xE58A62, onAccent: 0x2B1A14,
            needs: 0xB3203F, done: 0x46694A, paused: 0x845600, frozen: 0x286773,
            sun: 0xF2B544, waves: 0x7FA383),
        dark: Swatches(
            sidebar: 0x231B17, ground: 0x1B1411, card: 0x2E241F, panel: 0x2A211C, field: 0x312722,
            edge: 0x48392F, hover: 0x372B24, selection: 0x4A2F22, needsTint: 0x46202A,
            ink: 0xF6EBE2, muted: 0xBFAA9B,
            accent: 0xEE9469, accentInk: 0xF4A983, accentFill: 0xEE9469, onAccent: 0x2A1810,
            needs: 0xFF7F96, done: 0x9CC79C, paused: 0xEFC468, frozen: 0x7EC3CC,
            sun: 0xF2B544, waves: 0x9CC79C))

    static let lilac = Palette(
        id: "lilac", name: "Lilac",
        light: Swatches(
            sidebar: 0xF7F2F7, ground: 0xEEE5EF, card: 0xFFFCFF, panel: 0xF9F3F9, field: 0xFFFDFF,
            edge: 0xE2D5E4, hover: 0xFBF5FB, selection: 0xEBDDF1, needsTint: 0xFBE4E0,
            ink: 0x33223F, muted: 0x6C5A75,
            accent: 0x9A5CB0, accentInk: 0x7F4495, accentFill: 0xC39AD3, onAccent: 0x2A1838,
            needs: 0xB02E1C, done: 0x426A4F, paused: 0x845700, frozen: 0x276776,
            sun: 0xF2B544, waves: 0xB58BC9),
        dark: Swatches(
            sidebar: 0x1F1A26, ground: 0x18141E, card: 0x2A2332, panel: 0x26202E, field: 0x2D2636,
            edge: 0x433950, hover: 0x332B3C, selection: 0x3E2E4D, needsTint: 0x44221F,
            ink: 0xF2EAF6, muted: 0xB9A9C4,
            accent: 0xC79BE0, accentInk: 0xD2AEE8, accentFill: 0xC79BE0, onAccent: 0x241731,
            needs: 0xFF836F, done: 0x9CC9A6, paused: 0xEFC468, frozen: 0x80C3D0,
            sun: 0xF2B544, waves: 0xC79BE0))

    static let evergreen = Palette(
        id: "evergreen", name: "Evergreen",
        light: Swatches(
            sidebar: 0xF4F4EC, ground: 0xE9EBDF, card: 0xFBFBF6, panel: 0xF1F2E8, field: 0xFDFDF9,
            edge: 0xD8DCC9, hover: 0xF6F7EF, selection: 0xDDE5D3, needsTint: 0xFBE5DC,
            ink: 0x1E2B22, muted: 0x56635A,
            accent: 0x2F5D46, accentInk: 0x264D3A, accentFill: 0x9DB8A3, onAccent: 0x1E2B22,
            needs: 0xB42A1E, done: 0x3F6A5F, paused: 0x855700, frozen: 0x286873,
            sun: 0xD9A93F, waves: 0x5E8A6E),
        dark: Swatches(
            sidebar: 0x141C17, ground: 0x0E1410, card: 0x1C2620, panel: 0x18221C, field: 0x202B24,
            edge: 0x34443A, hover: 0x25322A, selection: 0x2A3D31, needsTint: 0x4A251C,
            ink: 0xECF0E6, muted: 0xB3BFB5,
            accent: 0x9CC5A1, accentInk: 0xB3D6B8, accentFill: 0x9CC5A1, onAccent: 0x12201A,
            needs: 0xFF9585, done: 0x8FC3B2, paused: 0xEFC468, frozen: 0x7DC4CE,
            sun: 0xD9A93F, waves: 0x9CC5A1))
}

/// Irie's colours, from the palette in use. Each follows the window's
/// appearance: light or dark, and stronger under Increase Contrast.
enum Theme {
    /// The palette in use. The app sets it from the defaults at launch and
    /// from View > Theme, and draws its views again when it changes.
    static var palette = Palette.named(UserDefaults.standard.string(forKey: Palette.defaultsKey)) {
        didSet { colors = Colors(palette) }
    }

    private static var colors = Colors(palette)

    /// Makes the palette with this id the one in use, if it is not already.
    static func use(_ id: String) {
        if palette.id != id { palette = Palette.named(id) }
    }

    static var sidebar: Color { colors.sidebar }
    /// What the terminal's frame sits on.
    static var ground: Color { colors.ground }
    static var card: Color { colors.card }
    static var panel: Color { colors.panel }
    static var field: Color { colors.field }
    /// The hairline round a quiet card or a field.
    static var edge: Color { colors.edge }
    static var hover: Color { colors.hover }
    static var selection: Color { colors.selection }
    static var needsTint: Color { colors.needsTint }
    static var ink: Color { colors.ink }
    /// Ages, paths and other quiet text: readable, unlike `.tertiary`.
    static var muted: Color { colors.muted }
    static var accent: Color { colors.accent }
    static var accentInk: Color { colors.accentInk }
    static var accentFill: Color { colors.accentFill }
    static var onAccent: Color { colors.onAccent }
    static var needs: Color { colors.needs }
    static var done: Color { colors.done }
    static var paused: Color { colors.paused }
    static var frozen: Color { colors.frozen }
    static var sun: Color { colors.sun }
    static var waves: Color { colors.waves }
    /// Gentle shadows under cards and fields.
    static var shadow: Color { colors.shadow }

    // The names from before the palettes, which other views use.
    /// Working is drawn in ink: DESIGN §5.3 gives it the default colour.
    static var work: Color { ink }
    static var sunEdge: Color { accent }
    static var sunInk: Color { accentInk }
    static var deepGreen: Color { accent }
    static var sea: Color { waves }

    static let terminalBackground = NSColor(hex: 0x14110D)
    static let terminalForeground = NSColor(hex: 0xEEE5D5)
    /// Corners: cards and fields, and the terminal's frame and the task bar.
    static let radius: CGFloat = 14
    static let frameRadius: CGFloat = 16

    /// Rounded type, as the cards use.
    static func font(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        .system(size: size, weight: weight, design: .rounded)
    }

    /// A colour that takes the palette's light or dark swatch for the
    /// appearance it is drawn in. Under Increase Contrast a text colour moves
    /// further from the grounds and a hairline toward the ink.
    fileprivate static func dynamic(_ p: Palette, _ strength: Strength, alpha: @escaping (Bool) -> CGFloat = { _ in 1 },
                                    _ pick: @escaping (Swatches) -> UInt32) -> Color {
        Color(nsColor: NSColor(name: nil) { appearance in
            let match = appearance.bestMatch(from: [.aqua, .darkAqua, .accessibilityHighContrastAqua, .accessibilityHighContrastDarkAqua])
            let dark = match == .darkAqua || match == .accessibilityHighContrastDarkAqua
            let high = match == .accessibilityHighContrastAqua || match == .accessibilityHighContrastDarkAqua
            let s = dark ? p.dark : p.light
            var hex = pick(s)
            if high {
                switch strength {
                case .text: hex = luminance(hex) < 0.18 ? mix(hex, 0x000000, 0.35) : mix(hex, 0xFFFFFF, 0.45)
                case .line: hex = mix(hex, s.ink, 0.4)
                case .none: break
                }
            }
            return NSColor(hex: hex).withAlphaComponent(alpha(dark))
        })
    }

    fileprivate enum Strength { case none, text, line }

    /// WCAG 2.2's relative luminance.
    static func luminance(_ hex: UInt32) -> Double {
        func channel(_ v: UInt32) -> Double {
            let c = Double(v & 0xFF) / 255
            return c <= 0.04045 ? c / 12.92 : pow((c + 0.055) / 1.055, 2.4)
        }
        return 0.2126 * channel(hex >> 16) + 0.7152 * channel(hex >> 8) + 0.0722 * channel(hex)
    }

    /// a moved toward b by t, channel by channel.
    static func mix(_ a: UInt32, _ b: UInt32, _ t: Double) -> UInt32 {
        func ch(_ shift: UInt32) -> UInt32 {
            let x = Double((a >> shift) & 0xFF), y = Double((b >> shift) & 0xFF)
            return UInt32((x + (y - x) * t).rounded()) << shift
        }
        return ch(16) | ch(8) | ch(0)
    }
}

/// The palette's swatches as colours, made once per palette.
private struct Colors {
    let sidebar, ground, card, panel, field, edge, hover, selection, needsTint: Color
    let ink, muted, accent, accentInk, accentFill, onAccent: Color
    let needs, done, paused, frozen, sun, waves, shadow: Color

    init(_ p: Palette) {
        func surface(_ k: KeyPath<Swatches, UInt32>) -> Color { Theme.dynamic(p, .none) { $0[keyPath: k] } }
        func text(_ k: KeyPath<Swatches, UInt32>) -> Color { Theme.dynamic(p, .text) { $0[keyPath: k] } }
        sidebar = surface(\.sidebar)
        ground = surface(\.ground)
        card = surface(\.card)
        panel = surface(\.panel)
        field = surface(\.field)
        edge = Theme.dynamic(p, .line) { $0.edge }
        hover = surface(\.hover)
        selection = surface(\.selection)
        needsTint = surface(\.needsTint)
        ink = text(\.ink)
        muted = text(\.muted)
        accent = text(\.accent)
        accentInk = text(\.accentInk)
        accentFill = surface(\.accentFill)
        onAccent = text(\.onAccent)
        needs = text(\.needs)
        done = text(\.done)
        paused = text(\.paused)
        frozen = text(\.frozen)
        sun = surface(\.sun)
        waves = surface(\.waves)
        // The ink, faint, in light mode; black, deeper, in dark mode.
        shadow = Theme.dynamic(p, .none, alpha: { $0 ? 0.38 : 0.08 }) { s in s.ink < 0x808080 ? s.ink : 0x000000 }
    }
}

extension NSColor {
    convenience init(hex: UInt32) {
        self.init(srgbRed: CGFloat((hex >> 16) & 0xFF) / 255, green: CGFloat((hex >> 8) & 0xFF) / 255,
                  blue: CGFloat(hex & 0xFF) / 255, alpha: 1)
    }
}

extension Color {
    init(hex: UInt32) { self.init(nsColor: NSColor(hex: hex)) }
}

/// The sun rising over two waves: Irie's mark.
struct SunMark: View {
    var body: some View {
        Canvas { ctx, size in
            let s = min(size.width, size.height) / 32
            func p(_ x: CGFloat, _ y: CGFloat) -> CGPoint { CGPoint(x: x * s, y: y * s) }
            let ends: [((CGFloat, CGFloat), (CGFloat, CGFloat))] = [
                ((16, 2.5), (16, 5)), ((8, 5.6), (9.6, 7.4)), ((24, 5.6), (22.4, 7.4)), ((4.2, 13), (6.6, 13)), ((25.4, 13), (27.8, 13)),
            ]
            var rays = Path()
            for (a, b) in ends {
                rays.move(to: p(a.0, a.1))
                rays.addLine(to: p(b.0, b.1))
            }
            ctx.stroke(rays, with: .color(Theme.sun), style: StrokeStyle(lineWidth: 2 * s, lineCap: .round))
            ctx.fill(Path(ellipseIn: CGRect(x: 10.5 * s, y: 8 * s, width: 11 * s, height: 11 * s)), with: .color(Theme.sun))
            for (y, from, to) in [(21.6, 3.0, 29.0), (26.6, 6.5, 26.0)] {
                var wave = Path()
                wave.move(to: p(from, y))
                var x = from
                var up = true
                while x < to {
                    let next = min(x + 4.3, to)
                    wave.addQuadCurve(to: p(next, y), control: p((x + next) / 2, up ? y - 2.2 : y + 0.6))
                    x = next
                    up.toggle()
                }
                ctx.stroke(wave, with: .color(Theme.waves), style: StrokeStyle(lineWidth: 2 * s, lineCap: .round))
            }
        }
        .accessibilityHidden(true)
    }
}
