import SwiftUI

/// The frosted ground of a panel that floats over the window, as the
/// look-ahead at keys (KeyPeek.swift) and the Teach tile (TeachTile.swift)
/// wear it: frosted, in the palette's own field colour, with no line round
/// it, its ground running on past its edge and blurring into the window, so
/// the edge is soft all round. Reduce Transparency draws it solid, and
/// Increase Contrast gives it an edge.
struct FrostedPanel: ViewModifier {
    var radius: CGFloat = 16
    @Environment(\.accessibilityReduceTransparency) private var solid
    @Environment(\.colorSchemeContrast) private var contrast

    func body(content: Content) -> some View {
        let shape = RoundedRectangle(cornerRadius: radius, style: .continuous)
        return content
            .background {
                if solid {
                    shape.fill(Theme.field)
                } else {
                    ZStack {
                        shape.fill(Theme.field.opacity(0.6))
                            .padding(-10)
                            .blur(radius: 14)
                        shape.fill(.regularMaterial)
                            .overlay(shape.fill(Theme.field.opacity(0.6)))
                            .mask { shape.inset(by: 2).blur(radius: 3) }
                    }
                    .shadow(color: Theme.shadow, radius: 24, y: 8)
                }
            }
            .overlay { if solid || contrast == .increased { shape.strokeBorder(Theme.edge, lineWidth: 1) } }
    }
}

extension View {
    /// The frosted ground with the soft edge (FrostedPanel).
    func frostedPanel(radius: CGFloat = 16) -> some View { modifier(FrostedPanel(radius: radius)) }
}
