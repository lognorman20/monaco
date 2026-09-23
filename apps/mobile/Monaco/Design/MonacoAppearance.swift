import SwiftUI
import UIKit

/// Root appearance wiring and the handful of screen-level modifiers that survived v3.
///
/// The UIKit proxies are the one place display type has to be handed over as a `UIFont`: a nav
/// bar has no view tree to scale inside, so it takes `MonacoNavType`'s pre-scaled, point-capped
/// faces rather than `.displayFont(_:)`.
enum MonacoAppearance {
    static func configureUIKit() {
        let canvas = UIColor(MonacoTheme.bgBase)
        let surface = UIColor(MonacoTheme.bgRaised)
        let primaryText = UIColor(MonacoTheme.fgPrimary)
        let muted = UIColor(MonacoTheme.fgMuted)
        let hairline = UIColor(MonacoTheme.line)

        // SF Pro Expanded, capped: an uncapped large title at AX5 pushes the whole screen down
        // before the content has said anything. Tracking matches `DisplayRole`'s −0.01/−0.02em
        // and is measured against the *scaled* size, so it does not open into a gap at AX5.
        let inlineFont = MonacoNavType.inlineTitle
        let largeFont = MonacoNavType.largeTitle
        let titleAttributes: [NSAttributedString.Key: Any] = [
            .foregroundColor: primaryText,
            .font: inlineFont,
            .kern: MonacoNavType.inlineTracking(size: inlineFont.pointSize),
        ]
        let largeTitleAttributes: [NSAttributedString.Key: Any] = [
            .foregroundColor: primaryText,
            .font: largeFont,
            .kern: MonacoNavType.largeTracking(size: largeFont.pointSize),
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

        // At rest (scroll edge): transparent, no hairline, so an ink slab runs up under it.
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
        // Bar buttons are tap targets, so they take brand. The title is not, so it stays ink —
        // set on the title attributes above rather than inherited from the bar tint.
        navigationBar.tintColor = UIColor(MonacoTheme.brand)
        navigationBar.prefersLargeTitles = true

        // Tab bar: opaque surface, hairline top edge, brand on the selected item. Nothing sets a
        // SwiftUI `.tint` above this any more, so what is configured here is what ships. Before
        // v3 the two roots overrode it with ink and the app's one accent colour was absent from
        // its most persistent chrome while the segmented thumb was blue.
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

        // The two debug harnesses that keep a `Form` (see `monacoFormScreen`).
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
            .foregroundStyle(MonacoTheme.fgPrimary)
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

    /// E1 card. Repointed at `.monacoElevation(.card)` so the two screens still calling it get the
    /// v3 treatment — shadow in light, stroke in dark — before their own chunk lands.
    @available(*, deprecated, message: "Use .monacoElevation(.card). Last sites: PlatformBalanceCard.swift (Chunk C), CabalsLeaderboardSection.swift (Chunk D).")
    func monacoSurfaceCard() -> some View {
        padding(MonacoTheme.Space.m)
            .monacoElevation(.card)
    }

    /// The deep "money" card: ink in both schemes, with the shared off-centre radial highlight.
    /// Now built on `InkSurface` and declaring `\.monacoWorld`, so everything inside picks the ink
    /// pair instead of being told its colour twice.
    @available(*, deprecated, message: "Use .monacoInkBand() or .monacoInkSlab(). Last sites: HomeNetWorthSection.swift (Chunk C), GroupHeroSection.swift (Chunk D).")
    func monacoHeroCard(padding: CGFloat = MonacoInkBandMetrics.verticalPadding) -> some View {
        monacoWorld(.ink)
            .padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                InkSurface(
                    shape: AnyShape(
                        RoundedRectangle(cornerRadius: MonacoTheme.Radius.object, style: .continuous)
                    )
                )
            )
    }

    /// Toolbar / nav bar SF Symbol. Brand, because a toolbar glyph is a tap target and blue means tap.
    func monacoToolbarIcon() -> some View {
        font(.body.weight(.semibold))
            .foregroundStyle(MonacoTheme.brand)
            .symbolRenderingMode(.hierarchical)
    }

    /// Form screen root. **Debug harnesses only.** `DevBuyView` and `GroupNavSampleHarness` are
    /// the two screens the v3 form sweep exempts (§5.11); every product `Form` becomes a designed
    /// screen. A new product call site here is a bug, not a shortcut.
    func monacoFormScreen() -> some View {
        scrollContentBackground(.hidden)
            .monacoCanvas()
            .foregroundStyle(MonacoTheme.fgPrimary)
            .tint(MonacoTheme.controlTint)
            .listRowBackground(MonacoTheme.bgRaised)
            .listRowSeparatorTint(MonacoTheme.line)
    }

    /// Footnote / hint copy under a field.
    func monacoSecondaryCaption() -> some View {
        font(MonacoTheme.Typo.caption)
            .foregroundStyle(MonacoTheme.fgMuted)
    }

    /// Secondary action row inside a `Form`.
    @available(*, deprecated, message: "Use BottomCTA. Last site: DepositView.swift (Chunk C).")
    func monacoFormSecondaryAction() -> some View {
        buttonStyle(.monacoSecondary)
            .frame(maxWidth: .infinity)
            .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
            .listRowBackground(Color.clear)
    }
}
