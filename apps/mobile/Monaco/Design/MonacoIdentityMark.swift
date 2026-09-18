import SwiftUI

/// A typographic identity, not an avatar or an implied company logo.
struct MonacoIdentityMark: View {
    let title: String
    var size: CGFloat = 44

    private var initials: String {
        let words = title.split(separator: " ")
        return words.count > 1
            ? String(words.prefix(2).compactMap(\.first)).uppercased()
            : String(title.prefix(1)).uppercased()
    }
    private var color: Color {
        let index = title.utf8.reduce(0) { ($0 + Int($1)) % 3 }
        return [MonacoTheme.mint, MonacoTheme.peach, MonacoTheme.butter][index]
    }
    var body: some View {
        Text(initials)
            .font(MonacoTheme.display(size * 0.32))
            .lineLimit(1)
            .minimumScaleFactor(0.4)
            .foregroundStyle(MonacoTheme.primaryText)
            .frame(width: size, height: size)
            .background(color, in: RoundedRectangle(cornerRadius: size * 0.32))
            .accessibilityHidden(true)
    }
}
