import SwiftUI
import UIKit

/// Root appearance wiring and reusable styles for sibling screen agents.
enum MonacoAppearance {
    static func configureUIKit() {
        let canvas = UIColor(MonacoTheme.canvas)
        let surface = UIColor(MonacoTheme.surface)
        let primaryText = UIColor(MonacoTheme.primaryText)
        let muted = UIColor(MonacoTheme.muted)
        let hairline = UIColor(MonacoTheme.hairline)
        let titleFont = UIFont(name: "AvenirNext-DemiBold", size: 17) ?? .systemFont(ofSize: 17, weight: .semibold)
        let largeTitleFont = UIFont(name: "AvenirNext-Bold", size: 32) ?? .systemFont(ofSize: 32, weight: .bold)
        let titleAttributes: [NSAttributedString.Key: Any] = [
            .foregroundColor: primaryText,
            .font: UIFontMetrics(forTextStyle: .headline).scaledFont(for: titleFont, maximumPointSize: 22),
        ]
        let largeTitleAttributes: [NSAttributedString.Key: Any] = [
            .foregroundColor: primaryText,
            .font: UIFontMetrics(forTextStyle: .largeTitle).scaledFont(for: largeTitleFont, maximumPointSize: 44),
        ]

        // Chevron-only back button: the title is drawn clear and at a near-zero size so it takes no width.
        let backButton = UIBarButtonItemAppearance(style: .plain)
        backButton.normal.titleTextAttributes = [.foregroundColor: UIColor.clear, .font: UIFont.systemFont(ofSize: 0.1)]
        backButton.highlighted.titleTextAttributes = [.foregroundColor: UIColor.clear, .font: UIFont.systemFont(ofSize: 0.1)]
        let backImage = UIImage(systemName: "chevron.left", withConfiguration: UIImage.SymbolConfiguration(weight: .semibold))

        // Scrolled: opaque canvas with a hairline, so content never slides under the title.
        let standard = UINavigationBarAppearance()
        standard.configureWithOpaqueBackground()
        standard.backgroundColor = canvas
        standard.shadowColor = hairline
        standard.titleTextAttributes = titleAttributes
        standard.largeTitleTextAttributes = largeTitleAttributes
        standard.backButtonAppearance = backButton
        standard.setBackIndicatorImage(backImage, transitionMaskImage: backImage)

        // At rest (scroll edge): transparent, no hairline.
        let scrollEdge = UINavigationBarAppearance()
        scrollEdge.configureWithTransparentBackground()
        scrollEdge.titleTextAttributes = titleAttributes
        scrollEdge.largeTitleTextAttributes = largeTitleAttributes
        scrollEdge.backButtonAppearance = backButton
        scrollEdge.setBackIndicatorImage(backImage, transitionMaskImage: backImage)

        let navigationBar = UINavigationBar.appearance()
        navigationBar.standardAppearance = standard
        navigationBar.compactAppearance = standard
        navigationBar.scrollEdgeAppearance = scrollEdge
        navigationBar.compactScrollEdgeAppearance = scrollEdge
        navigationBar.tintColor = primaryText
        navigationBar.prefersLargeTitles = true

        // Tab bar: opaque surface, hairline top edge.
        let tabBar = UITabBarAppearance()
        tabBar.configureWithOpaqueBackground()
        tabBar.backgroundColor = surface
        tabBar.shadowColor = hairline
        let brand = UIColor(MonacoTheme.brand)
        let tabItem = UITabBarItemAppearance()
        tabItem.normal.iconColor = muted
        tabItem.normal.titleTextAttributes = [.foregroundColor: muted]
        tabItem.selected.iconColor = brand
        tabItem.selected.titleTextAttributes = [.foregroundColor: brand]
        tabBar.stackedLayoutAppearance = tabItem
        tabBar.inlineLayoutAppearance = tabItem
        tabBar.compactInlineLayoutAppearance = tabItem
        UITabBar.appearance().standardAppearance = tabBar
        UITabBar.appearance().scrollEdgeAppearance = tabBar
        UITabBar.appearance().tintColor = brand
        UITabBar.appearance().unselectedItemTintColor = muted

        // Legacy Form / List screens until they migrate to MonacoGroupedList.
        UITableView.appearance().backgroundColor = .clear
        UITableView.appearance().separatorColor = hairline
        UITableViewCell.appearance().backgroundColor = surface

        // No global UITextField / UISegmentedControl appearance: it leaked into auth SDK sheets.
        // Use MonacoTextField and MonacoSegmented instead.
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
    /// Flat paper canvas. Apply once at a screen root.
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

    /// The premium "money" card: deep ink in both schemes, with a very subtle top-left
    /// radial highlight. Everything inside draws in `onHero` / `onHeroMuted`.
    func monacoHeroCard(padding: CGFloat = 24) -> some View {
        self
            .padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(MonacoHeroCardBackground())
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
@available(*, deprecated, message: "Use EmptyState (no icon).")
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

/// Deep ink money card. One flat ink base plus a soft off-centre highlight — no glass, no glow.
struct MonacoHeroCardBackground: View {
    var radius: CGFloat = MonacoTheme.Radius.hero

    var body: some View {
        RoundedRectangle(cornerRadius: radius, style: .continuous)
            .fill(MonacoTheme.heroInk)
            .overlay {
                RoundedRectangle(cornerRadius: radius, style: .continuous)
                    .fill(
                        RadialGradient(
                            colors: [MonacoTheme.heroInkHighlight, .clear],
                            center: UnitPoint(x: 0.08, y: -0.05),
                            startRadius: 0,
                            endRadius: 340
                        )
                    )
            }
            .overlay {
                RoundedRectangle(cornerRadius: radius, style: .continuous)
                    .strokeBorder(Color.white.opacity(0.07), lineWidth: 1)
            }
    }
}
