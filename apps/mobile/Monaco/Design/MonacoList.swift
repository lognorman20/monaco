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
                            .foregroundStyle(MonacoTheme.muted)
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

    var body: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            leading
                .frame(width: 44, height: 44)
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
                    .truncationMode(.tail)
                if let subtitle, !subtitle.isEmpty {
                    Text(subtitle)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(subtitleColor)
                        .lineLimit(1)
                        .truncationMode(.tail)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .layoutPriority(1)
            VStack(alignment: .trailing, spacing: 2) {
                trailing
            }
            .fixedSize(horizontal: true, vertical: false)
            if chevron {
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(MonacoTheme.tertiaryText)
                    .accessibilityHidden(true)
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, 8)
        .frame(minHeight: 60)
        .contentShape(Rectangle())
        .overlay(alignment: .bottom) {
            if !isLast {
                Rectangle()
                    .fill(MonacoTheme.hairline)
                    .frame(height: 1)
                    .padding(.leading, 72)
            }
        }
        .accessibilityElement(children: .combine)
    }
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
