import AppKit
import SwiftUI

/// Irie's colours (the sidebar's palette in internal/ui/view.go), each with a
/// light and a dark value, and stronger ones for Increase Contrast. Text
/// colours keep 4.5:1 on the surfaces they sit on.
enum Theme {
    static let sun = Color(hex: 0xF2B544)
    static let sunInk = adaptive(light: 0x8A5A00, dark: 0xF4C35F, contrastLight: 0x6B4500, contrastDark: 0xFFD27A)
    static let deepGreen = Color(hex: 0x1E5A42)
    static let sea = adaptive(light: 0x1E8F68, dark: 0x48C894)
    static let needs = adaptive(light: 0xB42318, dark: 0xFF6F5B, contrastLight: 0x8E1A10, contrastDark: 0xFF8A7A)
    static let work = adaptive(light: 0x1F64A3, dark: 0x66B6F3, contrastLight: 0x164B7A, contrastDark: 0x8CCBFF)
    static let done = adaptive(light: 0x1A6E4C, dark: 0x48C894, contrastLight: 0x12543A, contrastDark: 0x6EE0B0)
    /// Ages, paths and other quiet text: readable, unlike `.tertiary`.
    static let muted = adaptive(light: 0x5B6A62, dark: 0x9FB3A8, contrastLight: 0x3E4A44, contrastDark: 0xC3D3CA)
    // Solid surfaces: nothing behind the window shows through.
    static let sidebar = adaptive(light: 0xF2F6F3, dark: 0x0E1D17)
    static let panel = adaptive(light: 0xE8EFEB, dark: 0x0B1813)
    static let field = adaptive(light: 0xFFFFFF, dark: 0x132820)
    // Card fills: the selected card, a hovered one, and one that needs you.
    static let selection = adaptive(light: 0xDDEBE3, dark: 0x173C2E)
    static let hover = Color.primary.opacity(0.06)
    static let needsTint = adaptive(light: 0xFBEAE7, dark: 0x2A1714)
    static let terminalBackground = NSColor(hex: 0x0A1813)
    static let terminalForeground = NSColor(hex: 0xD6E7DE)

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
