import SwiftUI
import UIKit

/// Root appearance wiring and reusable styles for sibling screen agents.
enum MonacoAppearance {
    static func configureUIKit() {
        let surface = UIColor(MonacoTheme.surface)
        let primaryText = UIColor(MonacoTheme.primaryText)
        let muted = UIColor(MonacoTheme.muted)
        let border = UIColor(MonacoTheme.border)
        let titleFont = UIFont(name: "AvenirNext-DemiBold", size: 17) ?? .systemFont(ofSize: 17, weight: .semibold)

        let navigationBar = UINavigationBarAppearance()
        navigationBar.configureWithTransparentBackground()
        navigationBar.backgroundColor = .clear
        navigationBar.shadowColor = .clear
        navigationBar.titleTextAttributes = [
            .foregroundColor: primaryText,
            .font: titleFont,
        ]
        navigationBar.largeTitleTextAttributes = [
            .foregroundColor: primaryText,
            .font: UIFont(name: "AvenirNext-Bold", size: 28) ?? .systemFont(ofSize: 28, weight: .bold),
        ]

        UINavigationBar.appearance().standardAppearance = navigationBar
        UINavigationBar.appearance().scrollEdgeAppearance = navigationBar
        UINavigationBar.appearance().compactAppearance = navigationBar
        UINavigationBar.appearance().tintColor = UIColor(MonacoTheme.ink)
        UINavigationBar.appearance().prefersLargeTitles = false

        let tabBar = UITabBarAppearance()
        tabBar.configureWithOpaqueBackground()
        tabBar.backgroundColor = surface
        tabBar.shadowColor = border
        let tabItem = UITabBarItemAppearance()
        tabItem.normal.iconColor = muted
        tabItem.normal.titleTextAttributes = [.foregroundColor: muted]
        tabItem.selected.iconColor = primaryText
        tabItem.selected.titleTextAttributes = [.foregroundColor: primaryText]
        tabBar.stackedLayoutAppearance = tabItem
        tabBar.inlineLayoutAppearance = tabItem
        tabBar.compactInlineLayoutAppearance = tabItem
        UITabBar.appearance().standardAppearance = tabBar
        UITabBar.appearance().scrollEdgeAppearance = tabBar
        UITabBar.appearance().tintColor = primaryText
        UITabBar.appearance().unselectedItemTintColor = muted

        UITableView.appearance().backgroundColor = .clear
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
            .monacoCanvas()
            .foregroundStyle(MonacoTheme.primaryText)
    }
}

extension View {
    /// Paper white + top peach wash. Apply once at a screen root.
    func monacoCanvas() -> some View {
        background { MonacoCanvasBackground() }
    }

    /// Apply at screen root (or rely on `ContentView` / `MonacoApp` wiring).
    func monacoRootAppearance() -> some View {
        modifier(MonacoRootAppearanceModifier())
    }

    /// Card-style container on the app canvas.
    func monacoSurfaceCard() -> some View {
        self
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

    /// Inset grouped list on the app canvas — hides default scroll chrome.
    func monacoInsetList() -> some View {
        listStyle(.insetGrouped)
            .scrollContentBackground(.hidden)
            .background(Color.clear)
    }

    /// High-contrast segmented control strip for board tabs.
    func monacoSegmentedBoardPicker() -> some View {
        padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, 12)
        .background(MonacoTheme.surface)
        .overlay(alignment: .bottom) {
            Rectangle()
                .fill(MonacoTheme.hairline)
                .frame(height: 1)
        }
    }

    /// Toolbar / nav bar SF Symbol — ink tint, readable weight.
    func monacoToolbarIcon() -> some View {
        font(.body.weight(.semibold))
            .foregroundStyle(MonacoTheme.ink)
            .symbolRenderingMode(.hierarchical)
    }

    /// Form screen root — canvas background, visible rows/separators, readable fields.
    func monacoFormScreen() -> some View {
        scrollContentBackground(.hidden)
            .monacoCanvas()
            .foregroundStyle(MonacoTheme.primaryText)
            .tint(MonacoTheme.accent)
            .listRowBackground(MonacoTheme.surface)
            .listRowSeparatorTint(MonacoTheme.border)
    }

    /// Footnote / hint copy inside forms.
    func monacoSecondaryCaption() -> some View {
        font(MonacoTheme.TypeRole.caption)
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
                .font(MonacoTheme.TypeRole.body)
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
            .font(MonacoTheme.TypeRole.body.weight(.semibold))
            .foregroundStyle(isEnabled ? MonacoTheme.primaryButtonLabel : MonacoTheme.disabled)
            .padding(.horizontal, 20)
            .padding(.vertical, 14)
            .background(
                Capsule()
                    .fill(isEnabled ? MonacoTheme.primaryButtonFill : MonacoTheme.disabled.opacity(0.35))
            )
            .opacity(configuration.isPressed ? 0.85 : 1)
    }
}

struct MonacoSecondaryButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(MonacoTheme.TypeRole.body.weight(.semibold))
            .foregroundStyle(isEnabled ? MonacoTheme.secondaryButtonLabel : MonacoTheme.disabled)
            .padding(.horizontal, 20)
            .padding(.vertical, 14)
            .background(Capsule().fill(MonacoTheme.secondaryButtonFill))
            .overlay {
                Capsule()
                    .strokeBorder(isEnabled ? MonacoTheme.hairline : MonacoTheme.disabled.opacity(0.5), lineWidth: 1)
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
            .font(MonacoTheme.TypeRole.body.weight(.semibold))
            .foregroundStyle(isEnabled ? MonacoTheme.destructive : MonacoTheme.disabled)
            .padding(.horizontal, 20)
            .padding(.vertical, 14)
            .background(Capsule().fill(MonacoTheme.surface))
            .overlay {
                Capsule()
                    .strokeBorder(isEnabled ? MonacoTheme.destructive : MonacoTheme.disabled.opacity(0.5), lineWidth: 1)
            }
            .opacity(configuration.isPressed ? 0.85 : 1)
    }
}

extension ButtonStyle where Self == MonacoDestructiveButtonStyle {
    static var monacoDestructive: MonacoDestructiveButtonStyle { MonacoDestructiveButtonStyle() }
}
