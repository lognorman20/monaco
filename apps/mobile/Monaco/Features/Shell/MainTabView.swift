import MonacoCore
import SwiftUI
import UIKit

/// The four tab roots. Account actions (withdraw, advanced, sign out) live on Profile.
enum MainTab: Hashable {
    case home, cabals, stocks, profile
}

/// Post-auth frame. Tab chrome only — screens live in their feature folders.
///
/// `house` / `person.3` / `chart.line.uptrend.xyaxis` / `person.crop.circle` is verbatim the set
/// every generated fintech app ships, so two of the four are drawn rather than picked: Home takes
/// the Monaco mark, and Profile takes the member's own monogram. Both are rendered once into
/// template images, because a `UITabBar` item is a UIKit image and there is no view tree in there
/// to put a SwiftUI mark into.
struct MainTabView: View {
    @ObservedObject var auth: DynamicAuthService
    @Environment(AppSessionStore.self) private var session: AppSessionStore?
    @State private var selectedTab: MainTab = .home

    /// Votes still open on the dashboard the app already loads. Nothing new is fetched and nothing
    /// is invented: a row whose `expiresAt` has passed is not counted, and zero shows no badge.
    private var openVoteCount: Int {
        guard let rows = session?.dashboard?.missedProposals else { return 0 }
        return HomeMissedVotes.open(rows, now: Date()).count
    }

    private var monogramSource: String? {
        let name = session?.me?.displayName.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if !name.isEmpty { return name }
        #if DEBUG
        // The tab-shell harness has no session; it passes the name it wants the glyph drawn from.
        return TabShellSample.displayName
        #else
        return nil
        #endif
    }

    var body: some View {
        TabView(selection: $selectedTab) {
            NavigationStack {
                HomeView(auth: auth, selectedTab: $selectedTab)
                    .tint(MonacoTheme.controlTint)
            }
            .tabItem {
                Label {
                    Text("Home")
                } icon: {
                    Image(uiImage: MonacoTabGlyph.mark)
                }
                .accessibilityIdentifier("tab-home")
            }
            .tag(MainTab.home)
            .environment(\.hostMainTab, .home)

            NavigationStack {
                CabalsTabView(auth: auth)
                    .tint(MonacoTheme.controlTint)
            }
            .tabItem {
                Label("Cabals", systemImage: "person.2.fill")
                    .accessibilityIdentifier("tab-cabals")
            }
            .badge(openVoteCount)
            .tag(MainTab.cabals)
            .environment(\.hostMainTab, .cabals)

            NavigationStack {
                AssetsTabView(auth: auth)
                    .tint(MonacoTheme.controlTint)
            }
            .tabItem {
                Label("Stocks", systemImage: "chart.xyaxis.line")
                    .accessibilityIdentifier("tab-assets")
            }
            .tag(MainTab.stocks)
            .environment(\.hostMainTab, .stocks)

            NavigationStack {
                ProfileTabView(auth: auth)
                    .tint(MonacoTheme.controlTint)
            }
            .tabItem {
                Label {
                    Text("Profile")
                } icon: {
                    Image(uiImage: MonacoTabGlyph.monogram(for: monogramSource))
                }
                .accessibilityIdentifier("tab-profile")
            }
            .tag(MainTab.profile)
            .environment(\.hostMainTab, .profile)
        }
        // Brand blue is the selected tab, said once and out loud. `MonacoAppearance` configures
        // the same colour on `UITabBarAppearance`, but SwiftUI's tint beats the proxy and the
        // iOS 26 tab bar reads the tint rather than the proxy at all — so leaving it unset would
        // put the app back on the undefined behaviour that made the accent disappear in the first
        // place. Each stack's content takes `controlTint` above, so this reaches the tab bar and
        // nothing else; a control inside a screen that wants brand asks for it at its own site.
        .tint(MonacoTheme.brand)
        // Each stack knows its tab (`hostMainTab`) and which one is showing, so screens in a tab
        // the member switched away from stop polling. See `pollWhileVisible`.
        .environment(\.selectedMainTab, selectedTab)
        .onChange(of: selectedTab) { _, _ in
            Haptics.selection()
        }
    }
}

/// The two drawn tab glyphs, rendered once into template images.
///
/// Both are drawn as *alpha masks* on purpose. A `UITabBarItem` image is tinted — selected takes
/// brand, unselected takes `fgMuted` — so anything with its own colours (a photo, a filled avatar)
/// would flatten to a silhouette. A knocked-out mark and a ringed monogram are shapes, so they
/// survive tinting intact and pick up the selected state for free.
enum MonacoTabGlyph {
    /// Matches the tab bar's own symbol sizing, so the drawn glyphs sit on the same baseline as
    /// the two SF Symbols beside them.
    static let side: CGFloat = 26

    /// The Monaco mark: three equal circles rising left to right, overlaps knocked out. Same unit
    /// geometry as `MonacoMark` and the app icon — the brand survives past the login screen.
    static let mark: UIImage = renderMark()

    private static let monogramCache = NSCache<NSString, UIImage>()

    /// The member's own initial in a ring. Falls back to `person.crop.circle` before the profile
    /// has loaded a name — the fallback is a real state, not a placeholder face.
    static func monogram(for displayName: String?) -> UIImage {
        guard let initial = initial(from: displayName) else {
            let configuration = UIImage.SymbolConfiguration(pointSize: side * 0.86, weight: .regular)
            return UIImage(systemName: "person.crop.circle", withConfiguration: configuration)?
                .withRenderingMode(.alwaysTemplate)
                ?? UIImage()
        }
        let key = initial as NSString
        if let cached = monogramCache.object(forKey: key) { return cached }
        let rendered = renderMonogram(initial)
        monogramCache.setObject(rendered, forKey: key)
        return rendered
    }

    /// First letter of the first word that has one. Uppercased for display; a name that is only
    /// punctuation or emoji has no initial and takes the fallback rather than a box glyph.
    static func initial(from displayName: String?) -> String? {
        guard let displayName else { return nil }
        let letter = displayName
            .trimmingCharacters(in: .whitespacesAndNewlines)
            .first { $0.isLetter || $0.isNumber }
        guard let letter else { return nil }
        return String(letter).uppercased()
    }

    // MARK: Drawing

    /// Unit geometry, shared with `MonacoMark`: diameter 0.27, centres (0.33, 0.60) (0.50, 0.50)
    /// (0.67, 0.40), a 14/1024 knockout gap, and 2% breathing room each side.
    private static let diameter: CGFloat = 0.27
    private static let centres = [CGPoint(x: 0.33, y: 0.60), CGPoint(x: 0.50, y: 0.50), CGPoint(x: 0.67, y: 0.40)]
    private static let gap: CGFloat = 14.0 / 1024.0
    private static let markWidth: CGFloat = (0.67 - 0.33 + diameter) * 1.04

    private static func renderMark() -> UIImage {
        let format = UIGraphicsImageRendererFormat.preferred()
        format.opaque = false
        let size = CGSize(width: side, height: side)
        let image = UIGraphicsImageRenderer(size: size, format: format).image { context in
            let cgContext = context.cgContext
            let unit = side / markWidth
            let origin = CGPoint(x: side / 2 - unit / 2, y: side / 2 - unit / 2)
            let d = diameter * unit
            let g = gap * unit
            cgContext.setFillColor(UIColor.black.cgColor)
            for (index, centre) in centres.enumerated() {
                let point = CGPoint(x: origin.x + centre.x * unit, y: origin.y + centre.y * unit)
                if index > 0 {
                    // Knock the gap out of what is already drawn, so the circles read as three
                    // separate discs rather than one blob once they are tinted a single colour.
                    cgContext.setBlendMode(.clear)
                    cgContext.fillEllipse(in: CGRect(
                        x: point.x - d / 2 - g,
                        y: point.y - d / 2 - g,
                        width: d + 2 * g,
                        height: d + 2 * g
                    ))
                    cgContext.setBlendMode(.normal)
                }
                cgContext.fillEllipse(in: CGRect(x: point.x - d / 2, y: point.y - d / 2, width: d, height: d))
            }
        }
        return image.withRenderingMode(.alwaysTemplate)
    }

    private static func renderMonogram(_ initial: String) -> UIImage {
        let format = UIGraphicsImageRendererFormat.preferred()
        format.opaque = false
        let size = CGSize(width: side, height: side)
        let lineWidth: CGFloat = 1.6
        let image = UIGraphicsImageRenderer(size: size, format: format).image { context in
            let ring = CGRect(x: lineWidth / 2, y: lineWidth / 2, width: side - lineWidth, height: side - lineWidth)
            context.cgContext.setStrokeColor(UIColor.black.cgColor)
            context.cgContext.setLineWidth(lineWidth)
            context.cgContext.strokeEllipse(in: ring)

            // SF Pro Expanded Bold, the display voice, at the ratio `CabalMark` uses for initials.
            let font = UIFont.systemFont(ofSize: side * 0.42, weight: .bold, width: .expanded)
            let attributes: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: UIColor.black]
            let text = initial as NSString
            let bounds = text.size(withAttributes: attributes)
            text.draw(
                at: CGPoint(x: (side - bounds.width) / 2, y: (side - bounds.height) / 2),
                withAttributes: attributes
            )
        }
        return image.withRenderingMode(.alwaysTemplate)
    }
}
