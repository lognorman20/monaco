import SwiftUI
import UIKit

/// Root appearance wiring and the handful of screen-level modifiers that survived v3.
///
/// The UIKit proxies are the one place display type has to be handed over as a `UIFont`: a nav
/// bar has no view tree to scale inside, so it takes `MonacoNavType`'s pre-scaled, point-capped
/// faces rather than `.displayFont(_:)`.
enum MonacoAppearance {
    /// Chevron-only back button: the title is drawn clear and at a near-zero size so it takes no width.
    private static var backButtonAppearance: UIBarButtonItemAppearance {
        let backButton = UIBarButtonItemAppearance(style: .plain)
        let hidden: [NSAttributedString.Key: Any] = [
            .foregroundColor: UIColor.clear,
            .font: UIFont.systemFont(ofSize: 0.1),
        ]
        backButton.normal.titleTextAttributes = hidden
        backButton.highlighted.titleTextAttributes = hidden
        return backButton
    }

    private static let backChevron = UIImage(
        systemName: "chevron.left",
        withConfiguration: UIImage.SymbolConfiguration(weight: .semibold)
    )

    /// Title and large-title attributes in a given foreground.
    ///
    /// SF Pro Expanded, capped: an uncapped large title at AX5 pushes the whole screen down
    /// before the content has said anything. Tracking matches `DisplayRole`'s −0.01/−0.02em and
    /// is measured against the *scaled* size, so it does not open into a gap at AX5.
    private static func titleAttributes(_ colour: UIColor) -> (inline: [NSAttributedString.Key: Any], large: [NSAttributedString.Key: Any]) {
        let inlineFont = MonacoNavType.inlineTitle
        let largeFont = MonacoNavType.largeTitle
        return (
            [
                .foregroundColor: colour,
                .font: inlineFont,
                .kern: MonacoNavType.inlineTracking(size: inlineFont.pointSize),
            ],
            [
                .foregroundColor: colour,
                .font: largeFont,
                .kern: MonacoNavType.largeTracking(size: largeFont.pointSize),
            ]
        )
    }

    /// The transparent scroll-edge bar, in a given foreground pair.
    ///
    /// The back chevron is pre-tinted `.alwaysOriginal` rather than left to the bar's `tintColor`:
    /// `tintColor` is one value for the whole bar, and the ink variant has to differ from it on one
    /// screen without reaching across to every other.
    private static func scrollEdgeAppearance(title: UIColor, chevron: UIColor) -> UINavigationBarAppearance {
        let appearance = UINavigationBarAppearance()
        appearance.configureWithTransparentBackground()
        let attributes = titleAttributes(title)
        appearance.titleTextAttributes = attributes.inline
        appearance.largeTitleTextAttributes = attributes.large
        appearance.backButtonAppearance = backButtonAppearance
        let image = backChevron?.withTintColor(chevron, renderingMode: .alwaysOriginal)
        appearance.setBackIndicatorImage(image, transitionMaskImage: backChevron)
        return appearance
    }

    /// The scroll-edge bar in the **ink** pair, installed per screen by `.monacoInkNavBar()`.
    ///
    /// White title (18.72:1 on `Ink.base`) and an `Ink.accent` chevron (7.98:1). The paper pair
    /// the proxy configures measures 1.0:1 and 3.12:1 over a slab in light mode.
    static let inkScrollEdge: UINavigationBarAppearance = scrollEdgeAppearance(
        title: UIColor(MonacoTheme.Ink.fgPrimary),
        chevron: UIColor(MonacoTheme.Ink.accent)
    )

    static func configureUIKit() {
        let canvas = UIColor(MonacoTheme.bgBase)
        let surface = UIColor(MonacoTheme.bgRaised)
        let primaryText = UIColor(MonacoTheme.fgPrimary)
        let muted = UIColor(MonacoTheme.fgMuted)
        let hairline = UIColor(MonacoTheme.line)

        let attributes = titleAttributes(primaryText)

        // Scrolled: opaque canvas with a hairline, so content never slides under the title.
        let standard = UINavigationBarAppearance()
        standard.configureWithOpaqueBackground()
        standard.backgroundColor = canvas
        standard.shadowColor = hairline
        standard.titleTextAttributes = attributes.inline
        standard.largeTitleTextAttributes = attributes.large
        standard.backButtonAppearance = backButtonAppearance
        standard.setBackIndicatorImage(backChevron, transitionMaskImage: backChevron)

        // At rest (scroll edge): transparent, no hairline, so an ink slab runs up under it.
        //
        // The title and chevron here are the *paper* pair, which is right for the paper canvas and
        // wrong over a slab: `fgPrimary` on `Ink.base` in light measures 1.0:1 and `brand`
        // measures 3.12:1. A screen that opens on a slab applies `.monacoInkNavBar()` (below),
        // which swaps in `inkScrollEdge` for that one screen. Nothing else compensates, so a slab
        // without that modifier ships an unreadable bar.
        let scrollEdge = scrollEdgeAppearance(title: primaryText, chevron: UIColor(MonacoTheme.brand))

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
        // Badge: amber, not systemRed. §1.6 reserves the danger ramp for "this went wrong with
        // your money" — a failed transfer, a refused withdrawal. A vote still waiting on you has
        // not gone wrong; it is `warning`. The label is composed from the two tokens that clear
        // AA against the amber pair: white on `#9A5B13` is 5.41:1, `Ink.base` on `#E0A458` is
        // 8.57:1. White on the dark amber would be 2.18:1, which is why this flips with the scheme.
        let warningFill = UIColor(MonacoTheme.warning)
        let onWarning = UIColor { traits in
            traits.userInterfaceStyle == .dark
                ? UIColor(MonacoTheme.Ink.base)
                : UIColor(MonacoTheme.Ink.fgPrimary)
        }
        for state in [tabItem.normal, tabItem.selected, tabItem.focused, tabItem.disabled] {
            state.badgeBackgroundColor = warningFill
            state.badgeTextAttributes = [.foregroundColor: onWarning]
        }
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

    /// Nav chrome for a screen that opens on an ink slab (§5.5, §6.1) — the hook the ink fold
    /// needs, and the reason the scroll-edge appearance above can afford to be transparent.
    ///
    /// The bar at rest is transparent so the slab runs up under it, but its title and chevron come
    /// from the paper pair. Measured over `Ink.base`, in **light mode**: title `fgPrimary`
    /// `#0B1220` on `#0B1220` is **1.0:1** — invisible — and the chevron `brand` `#1652F0` is
    /// **3.12:1**, under the 4.5:1 text bar. `MonacoPalette.ink` already states that brand cannot
    /// carry a tappable label on ink, which is why `Ink.accent` exists.
    ///
    /// **Why this reaches into UIKit.** `.toolbarColorScheme(.dark, for: .navigationBar)` is the
    /// only per-screen bar hook SwiftUI offers, and on the iOS 18 deployment target it does not
    /// override a `titleTextAttributes` foreground set on the appearance proxy — verified on
    /// 18.3, where the title stayed `#0B1220` over the slab. (It does work on iOS 26, which is
    /// why it is still applied here.) `UINavigationItem.scrollEdgeAppearance` is the supported
    /// per-screen escape hatch and UIKit scopes it to the one screen and unwinds it on its own,
    /// so there is no appearance to restore and no state to get wrong. It lives here, once,
    /// rather than in every chunk that opens on a slab.
    ///
    /// **Apply it at the screen root, above the scroll view.** The tint is an environment value,
    /// so the paper content below the fold inherits `Ink.accent` for *system* controls unless it
    /// re-declares its own; Monaco's own components set explicit colours and are unaffected. A
    /// screen with a system control on its paper half wraps that section in
    /// `.tint(MonacoTheme.controlTint)`, and a toolbar glyph over the slab takes
    /// `.monacoToolbarIcon(onInk: true)`.
    ///
    /// Callers: Home (Chunk C) and stock detail (Chunk F). See the cross-chunk contract.
    func monacoInkNavBar() -> some View {
        toolbarColorScheme(.dark, for: .navigationBar)
            .tint(MonacoTheme.Ink.accent)
            .background(MonacoInkNavBarInstaller().frame(width: 0, height: 0).accessibilityHidden(true))
    }

    /// Toolbar / nav bar SF Symbol. Brand, because a toolbar glyph is a tap target and blue means
    /// tap — `Ink.accent` when the bar is sitting over a slab, where brand is 3.12:1.
    func monacoToolbarIcon(onInk: Bool = false) -> some View {
        font(.body.weight(.semibold))
            .foregroundStyle(onInk ? MonacoTheme.Ink.accent : MonacoTheme.brand)
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


/// Installs `MonacoAppearance.inkScrollEdge` on the enclosing screen's own `navigationItem`.
///
/// A zero-sized background view is the cheapest way to reach the `UIHostingController` SwiftUI
/// put this screen in. Setting the appearance on the *item* rather than on the bar is what makes
/// this safe: UIKit applies it while this screen is on top and puts the bar back to the proxy's
/// paper appearance on its own when the screen is popped or covered, so there is nothing to
/// restore and nothing to leak onto the next screen.
private struct MonacoInkNavBarInstaller: UIViewRepresentable {
    func makeUIView(context: Context) -> UIView {
        InstallerView()
    }

    func updateUIView(_ uiView: UIView, context: Context) {
        (uiView as? InstallerView)?.install()
    }

    private final class InstallerView: UIView {
        override func didMoveToWindow() {
            super.didMoveToWindow()
            install()
        }

        func install() {
            guard let item = owningViewController?.navigationItem else { return }
            item.scrollEdgeAppearance = MonacoAppearance.inkScrollEdge
            item.compactScrollEdgeAppearance = MonacoAppearance.inkScrollEdge
        }

        private var owningViewController: UIViewController? {
            var responder: UIResponder? = self
            while let next = responder?.next {
                if let controller = next as? UIViewController { return controller }
                responder = next
            }
            return nil
        }
    }
}
