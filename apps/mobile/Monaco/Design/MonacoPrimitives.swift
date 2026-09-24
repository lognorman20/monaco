import SwiftUI

/// Flat paper canvas behind every screen. No gradient.
struct MonacoCanvasBackground: View {
    var body: some View {
        MonacoTheme.canvas
            .ignoresSafeArea()
    }
}

/// Canvas + ink for a tab root or pushed screen.
struct MonacoScreen<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .monacoCanvas()
            .foregroundStyle(MonacoTheme.ink)
    }
}

/// Capsule search field on `surfaceSunken`, 44pt tall, with a clear button while there is text.
struct MonacoSearchField: View {
    var placeholder: String
    @Binding var text: String
    var isEnabled: Bool = true

    var body: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            Image(systemName: "magnifyingglass")
                .font(.body.weight(.medium))
                .foregroundStyle(MonacoTheme.muted)
                .accessibilityHidden(true)
            TextField("", text: $text, prompt: Text(placeholder).foregroundStyle(MonacoTheme.disabledLabel))
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.ink)
                .tint(MonacoTheme.ink)
                .autocorrectionDisabled()
                .submitLabel(.search)
                .disabled(!isEnabled)
                .accessibilityLabel(placeholder)
                .accessibilityIdentifier("monaco-search-field")
            if !text.isEmpty, isEnabled {
                Button {
                    text = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundStyle(MonacoTheme.tertiaryText)
                        .frame(width: 44, height: 44)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Clear search")
            }
        }
        .padding(.leading, MonacoTheme.Space.m)
        .padding(.trailing, text.isEmpty ? MonacoTheme.Space.m : 0)
        .frame(minHeight: 44)
        .background(MonacoTheme.surfaceSunken, in: Capsule())
        .opacity(isEnabled ? 1 : 0.6)
    }
}

/// Large-radius surface. Hairline only — no decorative shadow.
@available(*, deprecated, message: "Use MonacoGroupedList, or a surface-filled VStack.")
struct MonacoCard<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        content
            .padding(MonacoTheme.Space.m)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                MonacoTheme.surface,
                in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                    .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
            }
    }
}

/// Filter / range pill.
@available(*, deprecated, message: "Use MonacoSegmented, or a 44pt chip built on surfaceSunken.")
struct MonacoChip: View {
    let title: String
    var isSelected: Bool = false

    var body: some View {
        Text(title)
            .font(MonacoTheme.TypeRole.caption.weight(.semibold))
            .foregroundStyle(isSelected ? MonacoTheme.primaryButtonLabel : MonacoTheme.ink)
            .padding(.horizontal, 14)
            .padding(.vertical, 8)
            .background(
                Capsule().fill(isSelected ? MonacoTheme.primaryButtonFill : MonacoTheme.surface)
            )
            .overlay {
                Capsule().strokeBorder(isSelected ? Color.clear : MonacoTheme.hairline, lineWidth: 1)
            }
    }
}

/// Large figure + caption (Home net worth / Profile name).
struct MonacoHeroHeader: View {
    let title: String
    let caption: String

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text(caption)
                .font(MonacoTheme.TypeRole.caption)
                .foregroundStyle(MonacoTheme.muted)
            Text(title)
                .font(MonacoTheme.TypeRole.display)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(2)
                .minimumScaleFactor(0.7)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// Image-or-mark + title + trailing metric.
@available(*, deprecated, message: "Use MonacoRow inside MonacoGroupedList.")
struct MonacoRowCard<Leading: View>: View {
    let title: String
    let subtitle: String?
    let trailing: String?
    var subtitleColor: Color = MonacoTheme.muted
    var trailingColor: Color = MonacoTheme.ink
    var trailingCaptionColor: Color = MonacoTheme.muted
    var trailingCaptionAccessibilityIdentifier: String?
    /// Muted second line under `trailing` (e.g. percent under dollar P&L).
    let trailingCaption: String?
    let leading: Leading

    init(
        title: String,
        subtitle: String?,
        trailing: String?,
        trailingCaption: String? = nil,
        subtitleColor: Color = MonacoTheme.muted,
        trailingColor: Color = MonacoTheme.ink,
        trailingCaptionColor: Color = MonacoTheme.muted,
        trailingCaptionAccessibilityIdentifier: String? = nil,
        @ViewBuilder leading: () -> Leading
    ) {
        self.title = title
        self.subtitle = subtitle
        self.trailing = trailing
        self.trailingCaption = trailingCaption
        self.subtitleColor = subtitleColor
        self.trailingColor = trailingColor
        self.trailingCaptionColor = trailingCaptionColor
        self.trailingCaptionAccessibilityIdentifier = trailingCaptionAccessibilityIdentifier
        self.leading = leading()
    }

    var body: some View {
        HStack(spacing: MonacoTheme.Space.m) {
            leading
                .frame(width: 44, height: 44)
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(MonacoTheme.TypeRole.title)
                    .foregroundStyle(MonacoTheme.ink)
                if let subtitle, !subtitle.isEmpty {
                    Text(subtitle)
                        .font(MonacoTheme.TypeRole.caption)
                        .foregroundStyle(subtitleColor)
                }
            }
            Spacer(minLength: 8)
            if let trailing, !trailing.isEmpty {
                VStack(alignment: .trailing, spacing: 2) {
                    Text(trailing)
                        .font(.subheadline.monospacedDigit())
                        .foregroundStyle(trailingColor)
                    if let trailingCaption, !trailingCaption.isEmpty {
                        Text(trailingCaption)
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(trailingCaptionColor)
                            .monacoOptionalAccessibilityIdentifier(trailingCaptionAccessibilityIdentifier)
                    }
                }
                .fixedSize()
            }
        }
        .padding(MonacoTheme.Space.m)
        .background(
            MonacoTheme.surface,
            in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
        }
    }
}

/// SF Symbol tile used as the default `MonacoRowCard` leading mark.
@available(*, deprecated, message: "Use CabalMark or StockMark.")
struct MonacoRowIcon: View {
    let systemImage: String

    var body: some View {
        Image(systemName: systemImage)
            .font(.title3)
            .foregroundStyle(MonacoTheme.accent)
            .symbolRenderingMode(.hierarchical)
            .frame(width: 44, height: 44)
            .background(MonacoTheme.canvas, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }
}

@available(*, deprecated, message: "Use MonacoRow inside MonacoGroupedList.")
extension MonacoRowCard where Leading == MonacoRowIcon {
    init(
        systemImage: String,
        title: String,
        subtitle: String?,
        trailing: String?,
        trailingCaption: String? = nil,
        subtitleColor: Color = MonacoTheme.muted,
        trailingColor: Color = MonacoTheme.ink,
        trailingCaptionColor: Color = MonacoTheme.muted,
        trailingCaptionAccessibilityIdentifier: String? = nil
    ) {
        self.init(
            title: title,
            subtitle: subtitle,
            trailing: trailing,
            trailingCaption: trailingCaption,
            subtitleColor: subtitleColor,
            trailingColor: trailingColor,
            trailingCaptionColor: trailingCaptionColor,
            trailingCaptionAccessibilityIdentifier: trailingCaptionAccessibilityIdentifier
        ) {
            MonacoRowIcon(systemImage: systemImage)
        }
    }
}

private extension View {
    @ViewBuilder
    func monacoOptionalAccessibilityIdentifier(_ identifier: String?) -> some View {
        if let identifier {
            accessibilityIdentifier(identifier)
        } else {
            self
        }
    }
}
