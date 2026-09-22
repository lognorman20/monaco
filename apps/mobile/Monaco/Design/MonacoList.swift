import SwiftUI

/// Sentence-case section title with an optional trailing text button ("See all").
struct MonacoSectionHeader: View {
    private let title: String
    private let trailing: String?
    private let action: (() -> Void)?

    init(_ title: String, trailing: String? = nil, action: (() -> Void)? = nil) {
        self.title = title
        self.trailing = trailing
        self.action = action
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
            Text(title)
                .font(MonacoTheme.Typo.section)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(2)
                .accessibilityAddTraits(.isHeader)
            Spacer(minLength: MonacoTheme.Space.s)
            if let trailing {
                if let action {
                    Button(action: action) {
                        Text(trailing)
                            .font(MonacoTheme.Typo.callout.weight(.semibold))
                            .foregroundStyle(MonacoTheme.brand)
                            .lineLimit(1)
                            .frame(minHeight: 44)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                } else {
                    Text(trailing)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.muted)
                        .lineLimit(1)
                }
            }
        }
        .frame(minHeight: action == nil ? nil : 44)
    }
}

/// One surface container for a run of `MonacoRow`s. No stroke; children are clipped to the radius.
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
        .background(MonacoTheme.surface)
        .clipShape(RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous))
    }
}

/// Leading 44pt mark, title over subtitle, trailing figures. Wrap in a `Button` or `NavigationLink`
/// with `.buttonStyle(.monacoRow)` for the pressed state.
struct MonacoRow<Leading: View, Trailing: View>: View {
    private let title: String
    private let subtitle: String?
    private let subtitleColor: Color
    private let chevron: Bool
    private let isLast: Bool
    private let leading: Leading
    private let trailing: Trailing

    init(
        title: String,
        subtitle: String? = nil,
        subtitleColor: Color = MonacoTheme.muted,
        chevron: Bool = false,
        isLast: Bool = false,
        @ViewBuilder leading: () -> Leading,
        @ViewBuilder trailing: () -> Trailing
    ) {
        self.title = title
        self.subtitle = subtitle
        self.subtitleColor = subtitleColor
        self.chevron = chevron
        self.isLast = isLast
        self.leading = leading()
        self.trailing = trailing()
    }

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
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
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(layout.titleLineLimit)
                .truncationMode(.tail)
            if let subtitle, !subtitle.isEmpty {
                Text(subtitle)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(subtitleColor)
                    .lineLimit(layout.subtitleLineLimit)
                    .truncationMode(.tail)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var chevronGlyph: some View {
        Image(systemName: "chevron.right")
            .font(.footnote.weight(.semibold))
            .foregroundStyle(MonacoTheme.tertiaryText)
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
            .frame(minHeight: 60)
            .contentShape(Rectangle())
            .overlay(alignment: .bottom) {
                if !isLast {
                    Rectangle()
                        .fill(MonacoTheme.hairline)
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
        subtitleColor: Color = MonacoTheme.muted,
        chevron: Bool = false,
        isLast: Bool = false,
        @ViewBuilder leading: () -> Leading
    ) {
        self.init(
            title: title,
            subtitle: subtitle,
            subtitleColor: subtitleColor,
            chevron: chevron,
            isLast: isLast,
            leading: leading,
            trailing: { EmptyView() }
        )
    }
}

/// Pressed state for a tappable `MonacoRow`: sunken fill, no scale.
struct MonacoRowButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(configuration.isPressed ? MonacoTheme.surfaceSunken : Color.clear)
    }
}

extension ButtonStyle where Self == MonacoRowButtonStyle {
    static var monacoRow: MonacoRowButtonStyle { MonacoRowButtonStyle() }
}

/// Empty state without an icon: one title, one muted line, an optional secondary action.
struct EmptyState: View {
    private let title: String
    private let message: String?
    private let actionTitle: String?
    private let action: (() -> Void)?

    init(title: String, message: String? = nil, actionTitle: String? = nil, action: (() -> Void)? = nil) {
        self.title = title
        self.message = message
        self.actionTitle = actionTitle
        self.action = action
    }

    var body: some View {
        VStack(spacing: MonacoTheme.Space.s) {
            Text(title)
                .font(.system(.body, weight: .semibold))
                .foregroundStyle(MonacoTheme.ink)
                .multilineTextAlignment(.center)
            if let message, !message.isEmpty {
                Text(message)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.muted)
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
