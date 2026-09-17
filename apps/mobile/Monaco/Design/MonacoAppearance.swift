import SwiftUI
import UIKit

/// Root appearance wiring and reusable styles for sibling screen agents.
enum MonacoAppearance {
    static func configureUIKit() {
        let surface = UIColor(MonacoTheme.surface)
        let background = UIColor(MonacoTheme.background)
        let primaryText = UIColor(MonacoTheme.primaryText)
        let border = UIColor(MonacoTheme.border)

        let navigationBar = UINavigationBarAppearance()
        navigationBar.configureWithOpaqueBackground()
        navigationBar.backgroundColor = surface
        navigationBar.shadowColor = border
        navigationBar.titleTextAttributes = [.foregroundColor: primaryText]
        navigationBar.largeTitleTextAttributes = [.foregroundColor: primaryText]

        UINavigationBar.appearance().standardAppearance = navigationBar
        UINavigationBar.appearance().scrollEdgeAppearance = navigationBar
        UINavigationBar.appearance().compactAppearance = navigationBar
        UINavigationBar.appearance().tintColor = UIColor(MonacoTheme.accent)

        UITableView.appearance().backgroundColor = background
        UITableView.appearance().separatorColor = border
        UITableViewCell.appearance().backgroundColor = surface

        UITextField.appearance().backgroundColor = UIColor(MonacoTheme.surface)
        UITextField.appearance().textColor = primaryText
        UITextField.appearance().tintColor = UIColor(MonacoTheme.accent)

        let segmented = UISegmentedControl.appearance()
        segmented.selectedSegmentTintColor = UIColor(MonacoTheme.primaryButtonFill)
        segmented.backgroundColor = UIColor(MonacoTheme.background)
        segmented.setTitleTextAttributes(
            [.foregroundColor: UIColor(MonacoTheme.primaryButtonLabel)],
            for: .selected
        )
        segmented.setTitleTextAttributes(
            [.foregroundColor: UIColor(MonacoTheme.primaryText)],
            for: .normal
        )
    }
}

struct MonacoRootAppearanceModifier: ViewModifier {
    func body(content: Content) -> some View {
        content
            .background(MonacoTheme.background)
            .foregroundStyle(MonacoTheme.primaryText)
    }
}

extension View {
    /// Apply at screen root (or rely on `ContentView` / `MonacoApp` wiring).
    func monacoRootAppearance() -> some View {
        modifier(MonacoRootAppearanceModifier())
    }

    /// Card-style container on the app canvas.
    func monacoSurfaceCard() -> some View {
        self
            .padding()
            .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .strokeBorder(MonacoTheme.border, lineWidth: 1)
            }
    }

    /// Inset grouped list on the app canvas — hides default scroll chrome.
    func monacoInsetList() -> some View {
        listStyle(.insetGrouped)
            .scrollContentBackground(.hidden)
            .background(MonacoTheme.background)
    }

    /// High-contrast segmented control strip for board tabs.
    func monacoSegmentedBoardPicker() -> some View {
        padding(.horizontal, 16)
            .padding(.vertical, 12)
            .background(MonacoTheme.surface)
            .overlay(alignment: .bottom) {
                Rectangle()
                    .fill(MonacoTheme.border)
                    .frame(height: 1)
            }
    }

    /// Toolbar / nav bar SF Symbol — accent tint, readable weight.
    func monacoToolbarIcon() -> some View {
        font(.body.weight(.semibold))
            .foregroundStyle(MonacoTheme.accent)
            .symbolRenderingMode(.hierarchical)
    }

    /// Form screen root — canvas background, visible rows/separators, readable fields.
    func monacoFormScreen() -> some View {
        scrollContentBackground(.hidden)
            .background(MonacoTheme.background)
            .foregroundStyle(MonacoTheme.primaryText)
            .tint(MonacoTheme.accent)
            .listRowBackground(MonacoTheme.surface)
            .listRowSeparatorTint(MonacoTheme.border)
    }

    /// Footnote / hint copy inside forms.
    func monacoSecondaryCaption() -> some View {
        font(.footnote)
            .foregroundStyle(MonacoTheme.secondaryText)
    }

    /// Text field inside a Form section — ink text + accent caret.
    func monacoFormTextField() -> some View {
        foregroundStyle(MonacoTheme.primaryText)
            .tint(MonacoTheme.accent)
    }

    /// Primary action button row inside a Form.
    func monacoFormPrimaryAction() -> some View {
        buttonStyle(.monacoPrimary)
            .frame(maxWidth: .infinity)
            .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
            .listRowBackground(Color.clear)
    }

    /// Secondary action button row inside a Form.
    func monacoFormSecondaryAction() -> some View {
        buttonStyle(.monacoSecondary)
            .frame(maxWidth: .infinity)
            .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
            .listRowBackground(Color.clear)
    }

    /// Destructive action button row inside a Form (e.g. sign out).
    func monacoFormDestructiveAction() -> some View {
        buttonStyle(.monacoDestructive)
            .frame(maxWidth: .infinity)
            .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
            .listRowBackground(Color.clear)
    }
}

/// Empty list placeholder with bordered surface card.
struct MonacoEmptyStateCard: View {
    let message: String
    let systemImage: String

    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: systemImage)
                .font(.title2)
                .foregroundStyle(MonacoTheme.accent)
                .symbolRenderingMode(.hierarchical)
            Text(message)
                .font(.subheadline)
                .foregroundStyle(MonacoTheme.secondaryText)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .monacoSurfaceCard()
        .listRowInsets(EdgeInsets(top: 12, leading: 16, bottom: 12, trailing: 16))
        .listRowBackground(Color.clear)
        .listRowSeparator(.hidden)
    }
}

struct MonacoPrimaryButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .foregroundStyle(isEnabled ? MonacoTheme.primaryButtonLabel : MonacoTheme.disabled)
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
            .background(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .fill(isEnabled ? MonacoTheme.primaryButtonFill : MonacoTheme.disabled.opacity(0.35))
            )
            .opacity(configuration.isPressed ? 0.85 : 1)
    }
}

struct MonacoSecondaryButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .foregroundStyle(isEnabled ? MonacoTheme.secondaryButtonLabel : MonacoTheme.disabled)
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
            .background(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .fill(MonacoTheme.secondaryButtonFill)
            )
            .overlay {
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .strokeBorder(isEnabled ? MonacoTheme.border : MonacoTheme.disabled.opacity(0.5), lineWidth: 1)
            }
            .opacity(configuration.isPressed ? 0.85 : 1)
    }
}

extension ButtonStyle where Self == MonacoPrimaryButtonStyle {
    static var monacoPrimary: MonacoPrimaryButtonStyle { MonacoPrimaryButtonStyle() }
}

extension ButtonStyle where Self == MonacoSecondaryButtonStyle {
    static var monacoSecondary: MonacoSecondaryButtonStyle { MonacoSecondaryButtonStyle() }
}

struct MonacoDestructiveButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .foregroundStyle(isEnabled ? MonacoTheme.destructive : MonacoTheme.disabled)
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
            .background(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .fill(MonacoTheme.surface)
            )
            .overlay {
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .strokeBorder(isEnabled ? MonacoTheme.destructive : MonacoTheme.disabled.opacity(0.5), lineWidth: 1)
            }
            .opacity(configuration.isPressed ? 0.85 : 1)
    }
}

extension ButtonStyle where Self == MonacoDestructiveButtonStyle {
    static var monacoDestructive: MonacoDestructiveButtonStyle { MonacoDestructiveButtonStyle() }
}
