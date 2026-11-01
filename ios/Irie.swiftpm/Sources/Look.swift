import SwiftUI
import UIKit

// How things look: the Evergreen palette of the Mac app (Theme.swift) and
// the web app (js/look.js, app.css), light and dark; each state's name,
// glyph and colour role (DESIGN §5.3, States.swift); the agents' own
// colours while they work; Irie's sun; and the short forms of ages,
// durations and counts.

// MARK: - Palette

enum Theme {
    private static func rgb(_ hex: UInt32, _ alpha: CGFloat = 1) -> UIColor {
        UIColor(red: CGFloat((hex >> 16) & 0xFF) / 255, green: CGFloat((hex >> 8) & 0xFF) / 255, blue: CGFloat(hex & 0xFF) / 255, alpha: alpha)
    }

    static func dyn(_ light: UInt32, _ dark: UInt32) -> Color {
        let l = rgb(light), d = rgb(dark)
        return Color(UIColor { $0.userInterfaceStyle == .dark ? d : l })
    }

    static func fixed(_ hex: UInt32) -> Color { Color(rgb(hex)) }

    static let sidebar = dyn(0xF4F4EC, 0x141C17)
    static let ground = dyn(0xE9EBDF, 0x0E1410)
    static let card = dyn(0xFBFBF6, 0x1C2620)
    static let panel = dyn(0xF1F2E8, 0x18221C)
    static let field = dyn(0xFDFDF9, 0x202B24)
    static let edge = dyn(0xD8DCC9, 0x34443A)
    static let hover = dyn(0xF6F7EF, 0x25322A)
    static let selection = dyn(0xDDE5D3, 0x2A3D31)
    static let needsTint = dyn(0xFBE5DC, 0x4A251C)
    static let ink = dyn(0x1E2B22, 0xECF0E6)
    static let muted = dyn(0x56635A, 0xB3BFB5)
    static let accent = dyn(0x2F5D46, 0x9CC5A1)
    static let accentInk = dyn(0x264D3A, 0xB3D6B8)
    static let accentFill = dyn(0x9DB8A3, 0x9CC5A1)
    static let onAccent = dyn(0x1E2B22, 0x12201A)
    /// Text on a primary button: the card's colour, or on-accent in dark mode.
    static let onPrimary = dyn(0xFBFBF6, 0x12201A)
    static let needs = dyn(0xB42A1E, 0xFF9585)
    static let onNeeds = dyn(0xFFFFFF, 0x2A0E09)
    static let done = dyn(0x3F6A5F, 0x8FC3B2)
    static let paused = dyn(0x855700, 0xEFC468)
    static let frozen = dyn(0x286873, 0x7DC4CE)
    static let sun = fixed(0xD9A93F)
    static let waves = dyn(0x5E8A6E, 0x9CC5A1)
    /// The bars, dark in both modes.
    static let bar = dyn(0x183F30, 0x0F1712)

    static func bar(_ scheme: ColorScheme) -> Color { fixed(scheme == .dark ? 0x0F1712 : 0x183F30) }
    static let barEdge = Color(UIColor { $0.userInterfaceStyle == .dark ? rgb(0x26332B) : .clear })
    static let termBack = fixed(0x14110D)
    static let termInk = fixed(0xEEE5D5)
    static let toastBack = dyn(0x1E2B22, 0xECF0E6)
    static let toastInk = dyn(0xECF0E6, 0x1E2B22)
    static let toastAction = dyn(0xB3D6B8, 0x264D3A)
    static let toastErrorBack = dyn(0x4A1E16, 0xFFE1DB)
    static let toastErrorInk = dyn(0xFFD9D2, 0x8C1D12)

    static func rounded(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        .system(size: size, weight: weight, design: .rounded)
    }

    static func mono(_ size: CGFloat) -> Font {
        .system(size: size, design: .monospaced)
    }
}

// MARK: - The agents' own colours

/// A colour for each of six agent slots, worked out once for both modes, as
/// js/look.js does it from Theme.swift's agentHue: a fan of hues opposite the
/// accent that skips the needs red, washed into the card so its text keeps
/// 4.6:1.
enum AgentColor {
    static let slots = 6

    struct Tints {
        var mark: Color
        var wash: Color
        var wash2: Color
        var ink: Color
    }

    /// The slot for a session, the same one every time: FNV-1a of its id's UTF-8 bytes.
    static func slot(_ id: String) -> Int {
        var h: UInt32 = 0x811C_9DC5
        for b in id.utf8 {
            h ^= UInt32(b)
            h = h &* 0x0100_0193
        }
        return Int(h % UInt32(slots))
    }

    static func colors(_ id: String) -> Tints { table[slot(id)] }

    private struct Swatches {
        var sidebar, ground, card, panel, selection, muted, accent, needs: UInt32
    }

    private static let light = Swatches(sidebar: 0xF4F4EC, ground: 0xE9EBDF, card: 0xFBFBF6, panel: 0xF1F2E8, selection: 0xDDE5D3, muted: 0x56635A, accent: 0x2F5D46, needs: 0xB42A1E)
    private static let dark = Swatches(sidebar: 0x141C17, ground: 0x0E1410, card: 0x1C2620, panel: 0x18221C, selection: 0x2A3D31, muted: 0xB3BFB5, accent: 0x9CC5A1, needs: 0xFF9585)

    private static let table: [Tints] = (0..<slots).map { i in
        let l = (mark: mark(i, light, false), wash: wash(i, light, false), ink: ink(i, light, false))
        let d = (mark: mark(i, dark, true), wash: wash(i, dark, true), ink: ink(i, dark, true))
        return Tints(mark: Theme.dyn(l.mark, d.mark), wash: Theme.dyn(l.wash, d.wash),
                   wash2: Theme.dyn(mix(light.card, l.wash, 0.5), mix(dark.card, d.wash, 0.5)),
                   ink: Theme.dyn(l.ink, d.ink))
    }

    private static func ch(_ hex: UInt32, _ shift: UInt32) -> Double { Double((hex >> shift) & 0xFF) }

    static func mix(_ a: UInt32, _ b: UInt32, _ t: Double) -> UInt32 {
        func m(_ s: UInt32) -> UInt32 { UInt32((ch(a, s) + (ch(b, s) - ch(a, s)) * t).rounded()) << s }
        return m(16) | m(8) | m(0)
    }

    static func luminance(_ hex: UInt32) -> Double {
        func lin(_ v: Double) -> Double {
            let c = v / 255
            return c <= 0.04045 ? c / 12.92 : pow((c + 0.055) / 1.055, 2.4)
        }
        return 0.2126 * lin(ch(hex, 16)) + 0.7152 * lin(ch(hex, 8)) + 0.0722 * lin(ch(hex, 0))
    }

    private static func ratio(_ a: Double, _ b: Double) -> Double { (max(a, b) + 0.05) / (min(a, b) + 0.05) }

    private static func hue(_ hex: UInt32) -> Double {
        let r = ch(hex, 16) / 255, g = ch(hex, 8) / 255, b = ch(hex, 0) / 255
        let hi = max(r, g, b), lo = min(r, g, b)
        if hi == lo { return 0 }
        var h: Double
        if hi == r { h = (g - b) / (hi - lo) } else if hi == g { h = 2 + (b - r) / (hi - lo) } else { h = 4 + (r - g) / (hi - lo) }
        h *= 60
        return h < 0 ? h + 360 : h
    }

    private static func fromHSB(_ hDeg: Double, _ s: Double, _ v: Double) -> UInt32 {
        let h = ((hDeg.truncatingRemainder(dividingBy: 360) + 360).truncatingRemainder(dividingBy: 360)) / 60
        let i = Int(h.rounded(.down)), f = h - Double(i)
        let p = v * (1 - s), q = v * (1 - s * f), t = v * (1 - s * (1 - f))
        let rgb: [(Double, Double, Double)] = [(v, t, p), (q, v, p), (p, v, t), (p, q, v), (t, p, v), (v, p, q)]
        let (r, g, b) = rgb[i % 6]
        func c(_ x: Double) -> UInt32 { UInt32((x * 255).rounded()) }
        return (c(r) << 16) | (c(g) << 8) | c(b)
    }

    private static func agentHue(_ slot: Int, _ s: Swatches) -> Double {
        let start = hue(s.accent) + 90
        var red = (hue(s.needs) - start).truncatingRemainder(dividingBy: 360)
        if red < 0 { red += 360 }
        if red >= 270 { red -= 360 }
        let from = max(0, red - 30), gap = max(0, min(180, red + 30) - from)
        var along = Double(slot % slots) * (180 - gap) / Double(slots - 1)
        if gap > 0, along >= from { along += gap }
        return (start + along).truncatingRemainder(dividingBy: 360)
    }

    private static func wash(_ slot: Int, _ s: Swatches, _ dark: Bool) -> UInt32 {
        let h = agentHue(slot, s)
        let vivid = fromHSB(h, 0.62, dark ? 0.6 : 1)
        let muted = luminance(s.muted), brightest = luminance(fromHSB(h, 0.62, 1))
        func fits(_ t: Double) -> Bool {
            let l = luminance(mix(s.card, vivid, t))
            return ratio(l, muted) >= 4.6 && (!dark || ratio(l, brightest) >= 3.2)
        }
        var lo = 0.0, hi = dark ? 0.45 : 0.2
        if fits(hi) { return mix(s.card, vivid, hi) }
        for _ in 0..<14 {
            let mid = (lo + hi) / 2
            if fits(mid) { lo = mid } else { hi = mid }
        }
        return mix(s.card, vivid, lo)
    }

    private static func search(_ h: Double, _ grounds: [Double], _ want: Double, _ dark: Bool) -> UInt32 {
        func at(_ x: Double) -> UInt32 { x <= 1 ? fromHSB(h, 0.62, x) : fromHSB(h, 0.62 * (2 - x), 1) }
        var lo = 0.0, hi = dark ? 2.0 : 1.0
        for _ in 0..<14 {
            let mid = (lo + hi) / 2
            let l = luminance(at(mid))
            if grounds.allSatisfy({ ratio(l, $0) >= want }) {
                if dark { hi = mid } else { lo = mid }
            } else if dark { lo = mid } else { hi = mid }
        }
        return at(dark ? hi : lo)
    }

    private static func mark(_ slot: Int, _ s: Swatches, _ dark: Bool) -> UInt32 {
        search(agentHue(slot, s), [wash(slot, s, dark), s.selection].map(luminance), 3.2, dark)
    }

    private static func ink(_ slot: Int, _ s: Swatches, _ dark: Bool) -> UInt32 {
        search(agentHue(slot, s), [s.sidebar, s.ground, s.card, s.panel].map(luminance), 4.6, dark)
    }
}

// MARK: - States

/// A row's state as DESIGN §5.3 draws it (States.swift's Look).
enum Look: Equatable {
    case failed, exited, exitedWithError, frozen, paused, needs, stalled, pausing, working, starting, done, idle

    init(_ s: Session) {
        switch s.state {
        case "failed": self = .failed
        case "exited": self = (s.exitCode ?? 0) == 0 ? .exited : .exitedWithError
        case "frozen": self = .frozen
        case "paused": self = .paused
        case "needs_you": self = .needs
        case "stalled": self = .stalled
        case "pausing": self = .pausing
        case "starting": self = .starting
        case "done": self = .done
        case "idle": self = .idle
        // The core takes a live session it has no events for to be working.
        default: self = .working
        }
    }

    var label: String {
        switch self {
        case .failed: return "Failed"
        case .exited: return "Exited"
        case .exitedWithError: return "Exited with an error"
        case .frozen: return "Frozen"
        case .paused: return "Paused"
        case .needs: return "Needs you"
        case .stalled: return "Stalled"
        case .pausing: return "Pausing"
        case .working: return "Working"
        case .starting: return "Starting"
        case .done: return "To verify"
        case .idle: return "Idle"
        }
    }

    /// Red: it needs you.
    var loud: Bool { self == .failed || self == .needs || self == .stalled }
    /// Its name is drawn muted.
    var quiet: Bool { self == .exited || self == .exitedWithError || self == .starting || self == .idle }
    /// Pausing is dim yellow, an exit with an error code dim red.
    var dimmed: Bool { self == .pausing || self == .exitedWithError }
    var ended: Bool { self == .exited || self == .exitedWithError || self == .failed }

    /// The mark's colour, by its colour role; a working one takes its agent's.
    func color(redOnDone: Bool, id: String) -> Color {
        switch self {
        case .failed, .needs, .stalled, .exitedWithError: return Theme.needs
        case .frozen: return Theme.frozen
        case .paused, .pausing: return Theme.paused
        case .working: return AgentColor.colors(id).mark
        case .done: return redOnDone ? Theme.needs : Theme.done
        case .exited, .starting, .idle: return Theme.muted
        }
    }

    /// The state's words in its colour, away from its card.
    func textColor(redOnDone: Bool, id: String) -> Color {
        self == .working ? AgentColor.colors(id).ink : color(redOnDone: redOnDone, id: id)
    }

    /// A session's state in plain words; a parked finished turn is Parked.
    static func label(_ s: Session) -> String {
        let look = Look(s)
        return look == .done && s.isParked ? "Parked" : look.label
    }
}

/// A state's glyph from DESIGN §5.3: ✕ failed, · exited, ◆ frozen, ‖ paused,
/// ● needs you, ! stalled, ◐ working, ◌ starting, ✓ done and ○ idle.
@MainActor struct StateMark: View {
    let look: Look
    var color: Color
    var size: CGFloat = 16

    var body: some View {
        Canvas { ctx, box in
            let k = min(box.width, box.height) / 16
            ctx.scaleBy(x: k, y: k)
            let ink = GraphicsContext.Shading.color(color)
            func line(_ runs: [(CGFloat, CGFloat)]...) -> Path {
                var p = Path()
                for run in runs {
                    p.move(to: CGPoint(x: run[0].0, y: run[0].1))
                    for q in run.dropFirst() { p.addLine(to: CGPoint(x: q.0, y: q.1)) }
                }
                return p
            }
            func circle(_ r: CGFloat, _ y: CGFloat = 8) -> Path {
                Path(ellipseIn: CGRect(x: 8 - r, y: y - r, width: 2 * r, height: 2 * r))
            }
            func stroke(_ p: Path, _ width: CGFloat = 1.7, dash: [CGFloat] = []) {
                ctx.stroke(p, with: ink, style: StrokeStyle(lineWidth: width, lineCap: .round, lineJoin: .round, dash: dash))
            }
            switch look {
            case .failed, .exitedWithError:
                stroke(line([(4.5, 4.5), (11.5, 11.5)], [(11.5, 4.5), (4.5, 11.5)]), 2)
            case .exited:
                ctx.fill(circle(2.1), with: ink)
            case .frozen:
                ctx.fill(line([(8, 2.6), (13.4, 8), (8, 13.4), (2.6, 8)]), with: ink)
            case .paused, .pausing:
                stroke(line([(5.8, 3.5), (5.8, 12.5)], [(10.2, 3.5), (10.2, 12.5)]), 2.1)
            case .needs:
                ctx.fill(circle(7), with: .color(color.opacity(0.22)))
                ctx.fill(circle(4.2), with: ink)
            case .stalled:
                stroke(line([(8, 3.2), (8, 9.2)]), 2.1)
                ctx.fill(circle(1.25, 12.6), with: ink)
            case .working:
                stroke(circle(5.6))
                var half = ctx
                half.clip(to: circle(5.6))
                half.fill(Path(CGRect(x: 0, y: 0, width: 8, height: 16)), with: ink)
            case .starting:
                stroke(circle(5.6), dash: [1.7, 2.3])
            case .done:
                stroke(line([(3.6, 8.4), (6.6, 11.3), (12.4, 4.9)]), 2.1)
            case .idle:
                stroke(circle(4.6))
            }
        }
        .frame(width: size, height: size)
        .opacity(look.dimmed ? 0.7 : 1)
        .accessibilityHidden(true)
    }
}

/// The sun rising over two waves: Irie's mark (Theme.swift's SunMark).
@MainActor struct SunMark: View {
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
            for (y, from, to) in [(21.6, 3.0, 29.0), (26.6, 6.5, 26.0)] as [(CGFloat, CGFloat, CGFloat)] {
                var wave = Path()
                wave.move(to: p(from, y))
                var x = from
                var up = true
                while x < to - 1e-9 {
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

/// A call's mark: done, failed, stopped, or running.
@MainActor struct CallMark: View {
    var state: String?
    var size: CGFloat = 17

    var body: some View {
        switch state {
        case "ok":
            Image(systemName: "checkmark.circle.fill").font(.system(size: size - 1)).foregroundStyle(Theme.done)
                .frame(width: size, height: size)
        case "error":
            Image(systemName: "xmark.circle.fill").font(.system(size: size - 1)).foregroundStyle(Theme.needs)
                .frame(width: size, height: size)
        case "stopped":
            Image(systemName: "minus.circle").font(.system(size: size - 1)).foregroundStyle(Theme.muted)
                .frame(width: size, height: size)
        default:
            ProgressView().controlSize(.mini).tint(Theme.muted).frame(width: size, height: size)
        }
    }
}

// MARK: - Short forms

enum Fmt {
    /// An age: now, 12m, 3h, 2d.
    static func age(_ date: Date?, now: Date = Date()) -> String {
        guard let t = date.real else { return "" }
        let s = max(0, Int(now.timeIntervalSince(t)))
        if s < 60 { return "now" }
        if s < 3600 { return "\(s / 60)m" }
        if s < 86400 { return "\(s / 3600)h" }
        return "\(s / 86400)d"
    }

    /// An age in words, for VoiceOver and banners.
    static func ageSpoken(_ date: Date?, now: Date = Date()) -> String {
        guard let t = date.real else { return "" }
        let s = max(0, Int(now.timeIntervalSince(t)))
        func n(_ v: Int, _ unit: String) -> String { "\(v) \(unit)\(v == 1 ? "" : "s") ago" }
        if s < 60 { return "just now" }
        if s < 3600 { return n(s / 60, "minute") }
        if s < 86400 { return n(s / 3600, "hour") }
        return n(s / 86400, "day")
    }

    /// How long a call took: 0.4 s, 40 s, 3 min, 1 h 5 min.
    static func took(_ ms: Int64) -> String {
        let s = Double(ms) / 1000
        if s < 10 { return String(format: "%.1f s", s) }
        if s < 60 { return "\(Int(s.rounded())) s" }
        if s < 3600 { return "\(Int(s / 60)) min" }
        let m = Int(s / 60)
        return m % 60 == 0 ? "\(m / 60) h" : "\(m / 60) h \(m % 60) min"
    }

    /// A count in short: 940, 4.2k, 48k, 1.3M.
    static func count(_ n: Int64) -> String {
        switch n {
        case ..<1000: return "\(n)"
        case ..<1_000_000: return String(format: n < 10000 ? "%.1fk" : "%.0fk", Double(n) / 1000)
        default: return String(format: n < 10_000_000 ? "%.1fM" : "%.0fM", Double(n) / 1_000_000)
        }
    }

    /// A path's last part, from its first line.
    static func base(_ path: String) -> String {
        var first = String(path.split(separator: "\n", omittingEmptySubsequences: false).first ?? "")
        while first.count > 1, first.hasSuffix("/") { first.removeLast() }
        return first.split(separator: "/").last.map(String.init) ?? first
    }

    /// A clock time, or a weekday and time when it is not today.
    static func clock(_ t: Date, now: Date = Date()) -> String {
        let cal = Calendar.current
        if cal.isDate(t, inSameDayAs: now) { return time.string(from: t) }
        if abs(now.timeIntervalSince(t)) < 6 * 86400 { return weekday.string(from: t) }
        return day.string(from: t)
    }

    private static func formatter(_ template: String) -> DateFormatter {
        let f = DateFormatter()
        f.setLocalizedDateFormatFromTemplate(template)
        return f
    }

    private static let time = formatter("jmm")
    private static let weekday = formatter("EEEjmm")
    private static let day = formatter("MMMdjmm")
}
