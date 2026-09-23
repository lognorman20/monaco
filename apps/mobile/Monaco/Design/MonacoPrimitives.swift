import SwiftUI

/// Flat paper canvas behind every screen. No gradient.
struct MonacoCanvasBackground: View {
    var body: some View {
        MonacoTheme.bgBase
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
            .foregroundStyle(MonacoTheme.fgPrimary)
    }
}

/// Capsule search field on the world's quiet fill, 44pt tall, with a clear button while there is
/// text. The caret is `controlTint`, not the brand accent — blue means tap, and a caret is not a
/// tap target.
struct MonacoSearchField: View {
    var placeholder: String
    @Binding var text: String
    var isEnabled: Bool = true

    @Environment(\.monacoPalette) private var palette

    var body: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            Image(systemName: "magnifyingglass")
                .font(.body.weight(.medium))
                .foregroundStyle(palette.fgMuted)
                .accessibilityHidden(true)
            TextField("", text: $text, prompt: Text(placeholder).foregroundStyle(palette.fgSubtle))
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(palette.fgPrimary)
                .tint(palette.controlTint)
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
                        .foregroundStyle(palette.fgSubtle)
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
        .background(palette.quietFill, in: Capsule())
        .opacity(isEnabled ? 1 : 0.6)
    }
}

/// E1 surface. Repointed at `.monacoElevation(.card)`, so the two screens still holding it get
/// the v3 treatment — shadow in light, stroke in dark, radius 20 — before their chunks land.
@available(*, deprecated, message: "Use MonacoGroupedList or .monacoElevation(.card). Last sites: CabalsPnLChartSection.swift:48 (Chunk D), ProfileNameEditor.swift:59 (Chunk F).")
struct MonacoCard<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        content
            .padding(MonacoTheme.Space.m)
            .frame(maxWidth: .infinity, alignment: .leading)
            .monacoElevation(.card)
    }
}

/// Filter / range pill. A `Capsule()`, never a radius approximation, on the world's quiet fill —
/// the inert raised control fill, not a stroked card surface. Both remaining call sites are
/// ranges, which is exactly what `MonacoSegmented` is for, so the type goes when they move.
@available(*, deprecated, message: "Use MonacoSegmented. Last sites: CabalsPnLChartSection.swift:64 (Chunk D), AssetDetailView.swift:138 (Chunk F).")
struct MonacoChip: View {
    let title: String
    var isSelected: Bool = false

    @Environment(\.monacoPalette) private var palette

    var body: some View {
        Text(title)
            .font(MonacoTheme.Typo.caption.weight(.semibold))
            .foregroundStyle(isSelected ? MonacoTheme.primaryButtonLabel : palette.fgPrimary)
            .padding(.horizontal, 14)
            .frame(minHeight: 36)
            .background(Capsule().fill(isSelected ? MonacoTheme.primaryButtonFill : palette.quietFill))
    }
}

/// Eyebrow over a large figure. The caption is the new `eyebrow` role — tracked uppercase, which
/// the app had none of before v3 — and the title is the display voice.
@available(*, deprecated, message: "Use .displayFont(.eyebrow) over .displayFont(.display). Last site: AssetDetailView.swift:100 (Chunk F).")
struct MonacoHeroHeader: View {
    let title: String
    let caption: String

    @Environment(\.monacoPalette) private var palette

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text(caption)
                .displayFont(.eyebrow)
                .foregroundStyle(palette.fgSubtle)
            Text(title)
                .displayFont(.display)
                .foregroundStyle(palette.fgPrimary)
                .lineLimit(2)
                .minimumScaleFactor(0.7)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// Image-or-mark + title + trailing metric, at E1.
@available(*, deprecated, message: "Use MonacoRow inside MonacoGroupedList. Last sites: CabalsTabView.swift:173,184 (Chunk D).")
struct MonacoRowCard<Leading: View>: View {
    let title: String
    let subtitle: String?
    let trailing: String?
    var subtitleColor: Color = MonacoTheme.fgMuted
    var trailingColor: Color = MonacoTheme.fgPrimary
    var trailingCaptionColor: Color = MonacoTheme.fgMuted
    var trailingCaptionAccessibilityIdentifier: String?
    /// Muted second line under `trailing` (e.g. percent under dollar P&L).
    let trailingCaption: String?
    let leading: Leading

    init(
        title: String,
        subtitle: String?,
        trailing: String?,
        trailingCaption: String? = nil,
        subtitleColor: Color = MonacoTheme.fgMuted,
        trailingColor: Color = MonacoTheme.fgPrimary,
        trailingCaptionColor: Color = MonacoTheme.fgMuted,
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
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.fgPrimary)
                if let subtitle, !subtitle.isEmpty {
                    Text(subtitle)
                        .font(MonacoTheme.Typo.caption)
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
        .monacoElevation(.card)
    }
}

/// SF Symbol tile used as the default `MonacoRowCard` leading mark.
@available(*, deprecated, message: "Use CabalMark or StockMark. Last use: the MonacoRowCard systemImage overload, via CabalsTabView.swift:173,184 (Chunk D).")
struct MonacoRowIcon: View {
    let systemImage: String

    var body: some View {
        Image(systemName: systemImage)
            .font(.title3)
            .foregroundStyle(MonacoTheme.brand)
            .symbolRenderingMode(.hierarchical)
            .frame(width: 44, height: 44)
            .background(
                MonacoTheme.fillQuiet,
                in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.tile, style: .continuous)
            )
    }
}

@available(*, deprecated, message: "Use MonacoRow inside MonacoGroupedList. Last sites: CabalsTabView.swift:173,184 (Chunk D).")
extension MonacoRowCard where Leading == MonacoRowIcon {
    init(
        systemImage: String,
        title: String,
        subtitle: String?,
        trailing: String?,
        trailingCaption: String? = nil,
        subtitleColor: Color = MonacoTheme.fgMuted,
        trailingColor: Color = MonacoTheme.fgPrimary,
        trailingCaptionColor: Color = MonacoTheme.fgMuted,
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
