import AppKit
import SwiftUI

/// Irie's colours: the hues of the terminal sidebar's palette
/// (internal/ui/view.go) on warm ground, sand in light mode and cocoa in dark
/// mode. Each has a light and a dark value, and text colours have stronger
/// ones for Increase Contrast. Text colours keep 4.5:1 on every surface
/// they sit on.
enum Theme {
    static let sun = Color(hex: 0xF2B544)
    /// Sun for edges and focus rings: deeper in light mode, for 3:1 on sand.
    static let sunEdge = adaptive(light: 0xA87510, dark: 0xF2B544)
    static let sunInk = adaptive(light: 0x8A5A00, dark: 0xF4C35F, contrastLight: 0x6B4500, contrastDark: 0xFFD27A)
    static let deepGreen = Color(hex: 0x1E5A42)
    static let sea = adaptive(light: 0x1E8F68, dark: 0x48C894)
    static let needs = adaptive(light: 0xB42318, dark: 0xFF6F5B, contrastLight: 0x8E1A10, contrastDark: 0xFF8A7A)
    static let work = adaptive(light: 0x1F64A3, dark: 0x66B6F3, contrastLight: 0x164B7A, contrastDark: 0x8CCBFF)
    static let done = adaptive(light: 0x1A6E4C, dark: 0x48C894, contrastLight: 0x12543A, contrastDark: 0x6EE0B0)
    /// Ages, paths and other quiet text: readable, unlike `.tertiary`.
    static let muted = adaptive(light: 0x6B5C4B, dark: 0xB5A793, contrastLight: 0x4F4336, contrastDark: 0xD8CCBA)
    // Solid surfaces: nothing behind the window shows through. The ground is
    // what the terminal's frame sits on; the edge is the hairline round a
    // quiet card or a field.
    static let sidebar = adaptive(light: 0xF8F1E5, dark: 0x201A14)
    static let ground = adaptive(light: 0xEEE4D3, dark: 0x191510)
    static let card = adaptive(light: 0xFFFBF4, dark: 0x2A231B)
    static let panel = adaptive(light: 0xFBF4E8, dark: 0x272019)
    static let field = adaptive(light: 0xFFFDF8, dark: 0x2C251D)
    static let edge = adaptive(light: 0xE4D5BD, dark: 0x3E3429)
    // Card fills: the selected card, a hovered one, and one that needs you.
    static let selection = adaptive(light: 0xFCEBC8, dark: 0x3B2D1B)
    static let hover = adaptive(light: 0xFFF6E6, dark: 0x322A21)
    static let needsTint = adaptive(light: 0xFBE8E1, dark: 0x3C2019)
    static let terminalBackground = NSColor(hex: 0x14110D)
    static let terminalForeground = NSColor(hex: 0xEEE5D5)
    /// Corners: cards and fields, and the terminal's frame and the task bar.
    static let radius: CGFloat = 12
    static let frameRadius: CGFloat = 14

    static func adaptive(light: UInt32, dark: UInt32, contrastLight: UInt32? = nil, contrastDark: UInt32? = nil) -> Color {
        Color(nsColor: NSColor(name: nil) { appearance in
            let match = appearance.bestMatch(from: [.aqua, .darkAqua, .accessibilityHighContrastAqua, .accessibilityHighContrastDarkAqua])
            if match == .accessibilityHighContrastDarkAqua { return NSColor(hex: contrastDark ?? dark) }
            if match == .accessibilityHighContrastAqua { return NSColor(hex: contrastLight ?? light) }
            return NSColor(hex: match == .darkAqua ? dark : light)
        })
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

/// How a row's state looks, after the sidebar's glyphs: red when it needs
/// you, blue while it works, green when it is done.
enum Look {
    case needs, working, starting, done, idle, ended

    init(_ s: Session) {
        switch s.state {
        case "needs_you", "stalled", "failed": self = .needs
        case "working": self = .working
        case "starting": self = .starting
        case "done": self = .done
        case "idle": self = .idle
        default: self = .ended
        }
    }

    var group: Group {
        switch self {
        case .needs: return .needs
        case .working, .starting: return .working
        case .done, .idle, .ended: return .done
        }
    }

    /// The colour of its edge; none while it is quiet.
    var color: Color? {
        switch self {
        case .needs: return Theme.needs
        case .working, .starting: return Theme.work
        case .done: return Theme.done
        case .idle, .ended: return nil
        }
    }

    enum Group: CaseIterable {
        case needs, working, done

        var title: String {
            switch self {
            case .needs: return "Needs you"
            case .working: return "Working"
            case .done: return "Done"
            }
        }
    }
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
                ctx.stroke(wave, with: .color(Theme.sea), style: StrokeStyle(lineWidth: 2 * s, lineCap: .round))
            }
        }
        .accessibilityHidden(true)
    }
}
