import MonacoCore
import SafariServices
import SwiftUI
import UIKit

/// One headline: the title in the words' voice, then "Reuters · 3h" in the market's.
///
/// No thumbnail and no chevron. The feeds carry no reliable images, and a chevron means
/// "push a screen" everywhere else in the app; this row leaves for the publisher's page.
struct NewsHeadlineRow: View {
    enum Layout {
        /// Inside a stock-screen section, which insets its content and draws the rules
        /// between rows itself.
        case section
        /// A ruled list that runs edge to edge (`MonacoGroupedList`): the row insets
        /// itself and draws its own rule under the text.
        case list
    }

    let headline: NewsHeadline
    var layout: Layout = .section
    /// The first line of a section starts on the title's own spacing, the way every
    /// other stock-screen section's first line does, rather than 12pt further down.
    var isFirst = false
    var isLast = false
    let open: (URL) -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        styled(Button {
            Haptics.selection()
            open(headline.url)
        } label: {
            content
        })
        .accessibilityLabel(headline.spoken)
        .accessibilityHint(NewsCopy.opensArticle)
        .accessibilityAddTraits(.isLink)
    }

    /// A list row takes the sunken pressed fill every ruled list uses. Inside an inset
    /// section that fill would stop 16pt short of both edges, so it stays plain there,
    /// the way the activity lines do.
    @ViewBuilder
    private func styled<Label: View>(_ button: Button<Label>) -> some View {
        switch layout {
        case .list: button.buttonStyle(.monacoRow)
        case .section: button.buttonStyle(.plain)
        }
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
            Text(headline.title)
                .font(MonacoTheme.Typo.bodyStrong)
                .foregroundStyle(MonacoTheme.ink)
                // Two lines is a headline; at the accessibility sizes two lines is four
                // words, so it gets room to say what happened.
                .lineLimit(dynamicTypeSize.isAccessibilitySize ? 5 : 2)
                .multilineTextAlignment(.leading)
                .fixedSize(horizontal: false, vertical: true)
            if !headline.stamp.isEmpty {
                Text(headline.stamp)
                    .font(MonacoTheme.Typo.stamp)
                    .foregroundStyle(MonacoTheme.tertiaryText)
                    .lineLimit(dynamicTypeSize.isAccessibilitySize ? 2 : 1)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.top, layout == .section && isFirst ? 0 : MonacoTheme.Space.sm)
        .padding(.bottom, MonacoTheme.Space.sm)
        .padding(.horizontal, layout == .list ? MonacoTheme.Space.m : 0)
        .frame(minHeight: 44)
        .contentShape(Rectangle())
        .overlay(alignment: .bottom) {
            if layout == .list, !isLast {
                MonacoRule().padding(.leading, MonacoTheme.Space.m)
            }
        }
    }
}

/// A headline's shape while the feed is read: two lines of title, one of byline.
struct NewsHeadlineRowSkeleton: View {
    var layout: NewsHeadlineRow.Layout = .section
    var isFirst = false
    var isLast = false
    /// Varies the second line so three skeleton rows do not read as one repeated block.
    var index = 0

    private static let secondLineWidths: [CGFloat] = [188, 232, 156]

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            SkeletonBlock(height: 14, radius: 3)
            SkeletonBlock(width: Self.secondLineWidths[index % Self.secondLineWidths.count], height: 14, radius: 3)
            SkeletonBlock(width: 112, height: 10, radius: 3)
                .padding(.top, 2)
        }
        .padding(.top, layout == .section && isFirst ? 0 : MonacoTheme.Space.sm)
        .padding(.bottom, MonacoTheme.Space.sm)
        .padding(.horizontal, layout == .list ? MonacoTheme.Space.m : 0)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) {
            if layout == .list, !isLast {
                MonacoRule().padding(.leading, MonacoTheme.Space.m)
            }
        }
        .accessibilityHidden(true)
    }
}

/// Opens an article in Safari's in-app reader, in reader mode when the page offers it.
///
/// Presented from UIKit rather than a SwiftUI sheet: the rows live in lazy stacks and in
/// sections that may not be on screen for long, and the reader should belong to the window,
/// not to whichever row asked for it.
@MainActor
enum NewsArticleReader {
    static func open(_ url: URL) {
        // SFSafariViewController traps on anything but a web page.
        guard let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https",
              let presenter = topViewController()
        else { return }
        let configuration = SFSafariViewController.Configuration()
        configuration.entersReaderIfAvailable = true
        let reader = SFSafariViewController(url: url, configuration: configuration)
        reader.preferredControlTintColor = UIColor(MonacoTheme.brand)
        reader.dismissButtonStyle = .close
        presenter.present(reader, animated: true)
    }

    private static func topViewController() -> UIViewController? {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        let scene = scenes.first { $0.activationState == .foregroundActive } ?? scenes.first
        var top = scene?.keyWindow?.rootViewController
        while let presented = top?.presentedViewController, !presented.isBeingDismissed {
            top = presented
        }
        return top
    }
}
