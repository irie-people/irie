import SwiftUI

/// How the product's name (Brand.swift, from internal/brand) fits where
/// room is tight. A name may have up to 30 characters (DESIGN §2.10).
extension Brand {
    /// Whether the name is long enough that a tight label leaves it out,
    /// as Move In for Move into Irie.
    static var isLong: Bool { name.count > 12 }
}

/// The name on one line in a tight place: it shrinks a little, then trails
/// off, and never pushes what is beside it out of view.
struct BrandName: View {
    var font: Font
    var maxWidth: CGFloat

    var body: some View {
        Text(Brand.name)
            .font(font)
            .lineLimit(1)
            .minimumScaleFactor(0.75)
            .truncationMode(.tail)
            .frame(maxWidth: maxWidth, alignment: .leading)
            .help(Brand.name)
    }
}
