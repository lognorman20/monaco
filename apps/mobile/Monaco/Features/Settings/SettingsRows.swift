import SafariServices
import SwiftUI
import UIKit

/// Where a settings row's rule starts: under the text, past the mark (see `MonacoRow`).
private let settingsRuleInset = MonacoTheme.Space.m + 44 + MonacoTheme.Space.sm

/// A settings row on the ledger: a sunken glyph mark, a title over an optional caption, a
/// value at the trailing edge, and a chevron when the row goes somewhere. `MonacoRow` combines
/// its children for VoiceOver, which would swallow a switch or a menu, so settings rows are
/// their own view with the same metrics.
///
/// At accessibility text sizes the value drops under the title instead of squeezing it: a
/// masked phone number or "5 minutes" at that size is wider than what is left of the row.
struct SettingsRowLabel<Value: View>: View {
    let systemImage: String
    let title: String
    var subtitle: String?
    var titleColor: Color = MonacoTheme.ink
    var isMuted = false
    var chevron = false
    @ViewBuilder var value: Value

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var hasValue: Bool { Value.self != EmptyView.self }

    var body: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            SunkenGlyphMark(systemImage: systemImage, size: 40, isMuted: isMuted)
                .frame(width: 44, height: 44)
            if dynamicTypeSize.isAccessibilitySize, hasValue {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                    labels
                    value
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            } else {
                labels
                    .frame(maxWidth: .infinity, alignment: .leading)
                value
                    .layoutPriority(1)
            }
            if chevron {
                SettingsChevron()
            }
        }
    }

    private var labels: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title)
                .font(MonacoTheme.Typo.rowTitle)
                .foregroundStyle(isMuted ? MonacoTheme.tertiaryText : titleColor)
                .fixedSize(horizontal: false, vertical: true)
            if let subtitle, !subtitle.isEmpty {
                Text(subtitle)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }
}

extension SettingsRowLabel where Value == EmptyView {
    init(
        systemImage: String,
        title: String,
        subtitle: String? = nil,
        titleColor: Color = MonacoTheme.ink,
        isMuted: Bool = false,
        chevron: Bool = false
    ) {
        self.init(
            systemImage: systemImage,
            title: title,
            subtitle: subtitle,
            titleColor: titleColor,
            isMuted: isMuted,
            chevron: chevron
        ) {
            EmptyView()
        }
    }
}

/// Row metrics and the rule under it: 60pt tall, the rule inset to the text, none under the
/// last row (the list draws that one).
struct SettingsRowChrome: ViewModifier {
    var isLast = false

    func body(content: Content) -> some View {
        content
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.vertical, 8)
            .frame(minHeight: 60)
            .contentShape(Rectangle())
            .overlay(alignment: .bottom) {
                if !isLast {
                    MonacoRule().padding(.leading, settingsRuleInset)
                }
            }
    }
}

extension View {
    func settingsRow(isLast: Bool = false) -> some View {
        modifier(SettingsRowChrome(isLast: isLast))
    }
}

/// The chevron a row that goes somewhere carries.
struct SettingsChevron: View {
    var body: some View {
        Image(systemName: "chevron.right")
            .font(.footnote.weight(.semibold))
            .foregroundStyle(MonacoTheme.tertiaryText)
            .accessibilityHidden(true)
    }
}

/// A switch row. The whole row is the switch's label, so VoiceOver reads the title, the
/// caption and the state as one control, and a tap anywhere on the row flips it.
struct SettingsToggleRow: View {
    let systemImage: String
    let title: String
    var subtitle: String?
    @Binding var isOn: Bool
    var isLast = false

    var body: some View {
        Toggle(isOn: $isOn) {
            SettingsRowLabel(systemImage: systemImage, title: title, subtitle: subtitle)
        }
        .toggleStyle(MonacoSwitchStyle())
        .settingsRow(isLast: isLast)
    }
}

/// The switch in the brand's own pair: on is the button fill with the button's label colour
/// for the knob (ink and cream in light, green-soft and ink in dark), off is the sunken field
/// with a hairline. The system switch keeps a white knob in both schemes, which all but
/// vanishes on the dark scheme's light fill.
struct MonacoSwitchStyle: ToggleStyle {
    func makeBody(configuration: Configuration) -> some View {
        MonacoSwitch(configuration: configuration)
    }
}

private struct MonacoSwitch: View {
    let configuration: ToggleStyleConfiguration

    @Environment(\.isEnabled) private var isEnabled
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.colorScheme) private var colorScheme

    private var isOn: Bool { configuration.isOn }

    /// Off, the knob is white paper in light; in dark the lifted panel would vanish on the
    /// sunken track, so it takes the quiet text colour instead.
    private var offKnob: Color {
        colorScheme == .dark ? MonacoTheme.tertiaryText : MonacoTheme.surface
    }

    var body: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            configuration.label
            track
        }
        .contentShape(Rectangle())
        .onTapGesture(perform: flip)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isToggle)
        .accessibilityValue(isOn ? "On" : "Off")
        .accessibilityAction { flip() }
    }

    private func flip() {
        guard isEnabled else { return }
        Haptics.selection()
        configuration.isOn.toggle()
    }

    private var track: some View {
        Capsule()
            .fill(isOn ? MonacoTheme.brandFill : MonacoTheme.surfaceSunken)
            .overlay {
                Capsule().strokeBorder(isOn ? Color.clear : MonacoTheme.hairline, lineWidth: 1)
            }
            .frame(width: 51, height: 31)
            .overlay(alignment: isOn ? .trailing : .leading) {
                Circle()
                    .fill(isOn ? MonacoTheme.onBrand : offKnob)
                    .overlay {
                        Circle().strokeBorder(isOn ? Color.clear : MonacoTheme.hairline, lineWidth: 1)
                    }
                    .padding(3)
            }
            .opacity(isEnabled ? 1 : 0.45)
            .animation(reduceMotion ? nil : .spring(response: 0.25, dampingFraction: 0.85), value: isOn)
            .accessibilityHidden(true)
    }
}

/// A link that opens in Safari inside the app, for the terms and the privacy policy.
struct SafariSheet: UIViewControllerRepresentable {
    let url: URL

    func makeUIViewController(context: Context) -> SFSafariViewController {
        let controller = SFSafariViewController(url: url)
        controller.preferredControlTintColor = UIColor(MonacoTheme.brand)
        controller.dismissButtonStyle = .done
        return controller
    }

    func updateUIViewController(_ controller: SFSafariViewController, context: Context) {}
}

/// A URL a sheet can be presented for.
struct SheetURL: Identifiable {
    let url: URL
    var id: String { url.absoluteString }
}
