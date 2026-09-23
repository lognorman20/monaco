import SwiftUI

/// Sentence-case section title with an optional trailing text button ("See all").
///
/// The title is the display voice at `.section` — SF Pro Expanded Semibold 18, scaling against
/// `.title3`. Eyebrows ("IN YOUR CABALS", "UP FOR VOTE") are a different role and are set by the
/// caller with `.displayFont(.eyebrow)`; this is the sentence-case header that sits over a run of
/// rows.
struct MonacoSectionHeader: View {
    private let title: String
    private let trailing: String?
    private let action: (() -> Void)?

    @Environment(\.monacoPalette) private var palette

    init(_ title: String, trailing: String? = nil, action: (() -> Void)? = nil) {
        self.title = title
        self.trailing = trailing
        self.action = action
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
            Text(title)
                .displayFont(.section)
                .foregroundStyle(palette.fgPrimary)
                .lineLimit(2)
                .accessibilityAddTraits(.isHeader)
            Spacer(minLength: MonacoTheme.Space.s)
            if let trailing {
                // The title is now `.displayFont(.section)` — SF Pro Expanded, 8-10% wider than
                // the face it replaced — so at AX5 the header and its action compete for a line
                // that was already tight. The action keeps `lineLimit(1)` (a wrapped "See all" is
                // worse than a slightly smaller one) and takes a scale floor and a layout
                // priority, so the *title* gives way first and the tap target never truncates.
                if let action {
                    Button(action: action) {
                        Text(trailing)
                            .font(MonacoTheme.Typo.callout.weight(.semibold))
                            .foregroundStyle(palette.accent)
                            .lineLimit(1)
                            .minimumScaleFactor(0.75)
                            .frame(minHeight: 44)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .layoutPriority(1)
                } else {
                    Text(trailing)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(palette.fgMuted)
                        .lineLimit(1)
                        .minimumScaleFactor(0.75)
                        .layoutPriority(1)
                }
            }
        }
        .frame(minHeight: action == nil ? nil : 44)
    }
}

/// One surface container for a run of `MonacoRow`s, at E1: the light-mode shadow and the
/// dark-mode stroke both come from `.monacoElevation(.card)`, so a list never grows a hairline of
/// its own. Children are clipped to the radius, and the shadow is drawn on the shape rather than
/// on the clipped content, so a long lazy list does not pay for offscreen rendering.
struct MonacoGroupedList<Content: View>: View {
    private let content: Content

    init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    var body: some View {
        VStack(spacing: 0) {
            content
        }
        .frame(maxWidth: .infinity)
        .clipShape(RoundedRectangle(cornerRadius: MonacoTheme.Radius.container, style: .continuous))
        .monacoElevation(.card)
    }
}

/// How tall a row stands. A plain row of labels and a figure is 60pt; a row carrying faces or a
/// sparkline needs the extra 4pt so the artwork is not squeezed against the separator.
enum MonacoRowDensity {
    case standard
    case tall

    var minimumHeight: CGFloat {
        switch self {
        case .standard: return 60
        case .tall: return 64
        }
    }
}

/// Leading 44pt mark, title over subtitle, trailing figures. Wrap in a `Button` or `NavigationLink`
/// with `.buttonStyle(.monacoRow)` for the pressed state.
struct MonacoRow<Leading: View, Trailing: View>: View {
    private let title: String
    private let subtitle: String?
    private let subtitleColor: Color?
    private let chevron: Bool
    private let isLast: Bool
    private let density: MonacoRowDensity
    private let leading: Leading
    private let trailing: Trailing

    init(
        title: String,
        subtitle: String? = nil,
        subtitleColor: Color? = nil,
        chevron: Bool = false,
        isLast: Bool = false,
        density: MonacoRowDensity = .standard,
        @ViewBuilder leading: () -> Leading,
        @ViewBuilder trailing: () -> Trailing
    ) {
        self.title = title
        self.subtitle = subtitle
        self.subtitleColor = subtitleColor
        self.chevron = chevron
        self.isLast = isLast
        self.density = density
        self.leading = leading()
        self.trailing = trailing()
    }

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.monacoPalette) private var palette
    @ScaledMetric(relativeTo: .body)
    private var titleWidthFloor: CGFloat = MonacoRowLayout.baseMinimumTitleWidth

    /// True for the `Trailing == EmptyView` overload, where there are no figures to lay out.
    private var hasTrailing: Bool { Trailing.self != EmptyView.self }

    private var layout: MonacoRowLayout {
        MonacoRowLayout(dynamicTypeSize: dynamicTypeSize, scaledTitleWidthFloor: titleWidthFloor)
    }

    private var labels: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title)
                .font(MonacoTheme.Typo.rowTitle)
                .foregroundStyle(palette.fgPrimary)
                .lineLimit(layout.titleLineLimit)
                .truncationMode(.tail)
            if let subtitle, !subtitle.isEmpty {
                Text(subtitle)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(subtitleColor ?? palette.fgMuted)
                    .lineLimit(layout.subtitleLineLimit)
                    .truncationMode(.tail)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var chevronGlyph: some View {
        Image(systemName: "chevron.right")
            .font(.footnote.weight(.semibold))
            .foregroundStyle(palette.fgSubtle)
            .accessibilityHidden(true)
    }

    /// Mark, then title over subtitle, then the figures. At accessibility text sizes the figures
    /// drop below the labels instead of squeezing them out of the row.
    private var content: some View {
        Group {
            if layout.isStacked {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    HStack(spacing: MonacoTheme.Space.sm) {
                        leading
                            .frame(width: 44, height: 44)
                        labels
                        if chevron { chevronGlyph }
                    }
                    // Chevron-only rows have no second line to drop below the labels; an empty
                    // column would still spend the stack's spacing.
                    if hasTrailing {
                        VStack(alignment: .leading, spacing: 2) {
                            trailing
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            } else {
                HStack(spacing: MonacoTheme.Space.sm) {
                    leading
                        .frame(width: 44, height: 44)
                    // The labels keep a floor and truncate; the figures take what is left and
                    // shrink through MoneyText's minimumScaleFactor before they ever truncate.
                    labels
                        .frame(minWidth: layout.minimumTitleWidth, alignment: .leading)
                    VStack(alignment: .trailing, spacing: 2) {
                        trailing
                    }
                    .layoutPriority(1)
                    if chevron { chevronGlyph }
                }
            }
        }
    }

    var body: some View {
        content
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.vertical, 8)
            .frame(minHeight: density.minimumHeight)
            .contentShape(Rectangle())
            .overlay(alignment: .bottom) {
                if !isLast {
                    Rectangle()
                        .fill(palette.line)
                        .frame(height: 1)
                        .padding(.leading, layout.separatorLeadingInset)
                }
            }
            .accessibilityElement(children: .combine)
    }
}

/// How a `MonacoRow` arranges itself for the current text size.
///
/// The trailing column used to be `fixedSize`, so it was always offered its ideal width. At
/// accessibility sizes a figure like "$12,480.55" is wider than the whole row, which left the
/// title zero width (an ellipsis) and stopped `MoneyText`'s `minimumScaleFactor` from ever
/// applying. Rows now stack at accessibility sizes, and the figures shrink at normal sizes.
struct MonacoRowLayout: Equatable {
    /// Floor for the label column at the default text size.
    static let baseMinimumTitleWidth: CGFloat = 96

    let isStacked: Bool
    private let scaledTitleWidthFloor: CGFloat

    init(
        dynamicTypeSize: DynamicTypeSize,
        scaledTitleWidthFloor: CGFloat = MonacoRowLayout.baseMinimumTitleWidth
    ) {
        isStacked = dynamicTypeSize.isAccessibilitySize
        self.scaledTitleWidthFloor = scaledTitleWidthFloor
    }

    /// A stacked row gives the title room for two lines; an inline row still truncates at one.
    var titleLineLimit: Int { isStacked ? 2 : 1 }

    var subtitleLineLimit: Int { isStacked ? 2 : 1 }

    /// Floor for the label column in the inline layout, so the figures give way first. It scales
    /// with the title: the title grows up to xxxLarge while the row is still inline, and a fixed
    /// 96pt is about five characters at that size, so a row with a wide figure went on truncating.
    var minimumTitleWidth: CGFloat? { isStacked ? nil : scaledTitleWidthFloor }

    /// The separator lines up under the labels in the inline layout, and runs the full width
    /// of a stacked row, where the figures sit below the mark.
    var separatorLeadingInset: CGFloat { isStacked ? MonacoTheme.Space.m : 72 }
}

extension MonacoRow where Trailing == EmptyView {
    init(
        title: String,
        subtitle: String? = nil,
        subtitleColor: Color? = nil,
        chevron: Bool = false,
        isLast: Bool = false,
        density: MonacoRowDensity = .standard,
        @ViewBuilder leading: () -> Leading
    ) {
        self.init(
            title: title,
            subtitle: subtitle,
            subtitleColor: subtitleColor,
            chevron: chevron,
            isLast: isLast,
            density: density,
            leading: leading,
            trailing: { EmptyView() }
        )
    }
}

/// Pressed state for a tappable `MonacoRow`: a quiet fill, no scale, and no animation — a row
/// that springs under a thumb reads as a card, not a list.
struct MonacoRowButtonStyle: ButtonStyle {
    @Environment(\.monacoPalette) private var palette

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(configuration.isPressed ? palette.quietFill : Color.clear)
    }
}

extension ButtonStyle where Self == MonacoRowButtonStyle {
    static var monacoRow: MonacoRowButtonStyle { MonacoRowButtonStyle() }
}

/// Empty state: one title, one muted line, an optional secondary action, and — on a first-run
/// surface only — an optional 56pt mark disc above it.
///
/// There are 42 of these in the app and they are **not** `ContentUnavailableView`s: that would
/// make 42 screens look like stock iOS and throw away copy that is better than the frame it would
/// go in ("The first one to fund takes the top spot"). `mark` is one optional parameter on the
/// component that already exists, used on the three first-run empties and left off everywhere
/// else. A parameter is not a new component.
struct EmptyState: View {
    private let title: String
    private let message: String?
    private let mark: String?
    private let actionTitle: String?
    private let action: (() -> Void)?

    @Environment(\.cabalTint) private var cabalTint
    @Environment(\.monacoPalette) private var palette
    @ScaledMetric(relativeTo: .title2) private var markSize: CGFloat = 56

    init(
        title: String,
        message: String? = nil,
        mark: String? = nil,
        actionTitle: String? = nil,
        action: (() -> Void)? = nil
    ) {
        self.title = title
        self.message = message
        self.mark = mark
        self.actionTitle = actionTitle
        self.action = action
    }

    /// A cabal surface tints its own disc; everywhere else the disc is the world's brand wash.
    /// Every pair is measured in the contrast table — `fgPrimary` on `soft`, `brandOnWash` on
    /// `brandWash`, `Ink.accent` on `brandWashOnInk` at 6.27:1 — so the glyph never falls below
    /// AA, whichever of the seven tints the cabal drew and whichever world the empty landed in.
    ///
    /// The world read matters because `EmptyState` is the one component here with 42 call sites:
    /// the first empty that lands inside an ink band would otherwise draw a paper wash and a
    /// near-invisible glyph, and nobody would go looking for the reason.
    private var discFill: Color {
        if let cabalTint { return cabalTint.soft }
        return palette.world == .ink ? MonacoTheme.brandWashOnInk : MonacoTheme.brandWash
    }

    private var glyphColor: Color {
        guard cabalTint == nil else { return palette.fgPrimary }
        return palette.world == .ink ? MonacoTheme.Ink.accent : MonacoTheme.brandOnWash
    }

    var body: some View {
        VStack(spacing: MonacoTheme.Space.s) {
            if let mark {
                Image(systemName: mark)
                    .font(.system(size: markSize * 0.42, weight: .semibold))
                    .foregroundStyle(glyphColor)
                    .frame(width: markSize, height: markSize)
                    .background(Circle().fill(discFill))
                    .padding(.bottom, MonacoTheme.Space.xs)
                    .accessibilityHidden(true)
            }
            Text(title)
                .font(.system(.body, weight: .semibold))
                .foregroundStyle(palette.fgPrimary)
                .multilineTextAlignment(.center)
            if let message, !message.isEmpty {
                Text(message)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(palette.fgMuted)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if let actionTitle, let action {
                Button(actionTitle, action: action)
                    .buttonStyle(.monacoSecondary)
                    .padding(.top, MonacoTheme.Space.s)
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.horizontal, MonacoTheme.Space.l)
        .padding(.vertical, MonacoTheme.Space.l)
    }
}
