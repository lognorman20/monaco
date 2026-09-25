import MonacoCore
import SwiftUI

/// "News" on the stock screen: the newest three headlines about this stock, and the way
/// into the rest.
///
/// It sits after the stock-vs-token comparison and before About: the numbers, then what
/// happened to them today, then what the thing is.
struct AssetNewsSection: View {
    let model: NewsFeedModel
    /// "Alphabet": what the empty line names.
    let companyName: String
    let seeAll: () -> Void
    var open: (URL) -> Void = { NewsArticleReader.open($0) }

    var body: some View {
        AssetDetailCard(title: NewsCopy.sectionTitle, identifier: "asset-detail-news") {
            switch model.phase {
            case .loading:
                skeleton
            case .failed:
                NewsRetryLine(identifier: "asset-news-failed") {
                    Task { await model.load() }
                }
                .padding(.bottom, MonacoTheme.Space.sm)
            case .answered:
                if model.headlines.isEmpty {
                    EmptyState(title: NewsCopy.empty(companyName))
                        .accessibilityIdentifier("asset-news-empty")
                } else {
                    rows
                }
            }
        }
    }

    private var preview: [NewsHeadline] {
        Array(model.headlines.prefix(NewsCopy.previewCount))
    }

    private var rows: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(preview.enumerated()), id: \.element.id) { index, headline in
                if index > 0 { AssetCardDivider() }
                NewsHeadlineRow(headline: headline, layout: .section, isFirst: index == 0, open: open)
                    .accessibilityIdentifier("asset-news-row-\(index)")
            }
            if model.headlines.count > NewsCopy.previewCount {
                // Like the activity section's "Show more": under the last line, with no
                // room of its own below — the next section's break is already there.
                AssetSectionTextButton(title: NewsCopy.seeAll, action: seeAll)
                    .padding(.top, MonacoTheme.Space.xs)
                    .accessibilityIdentifier("asset-news-see-all")
            }
        }
    }

    private var skeleton: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(0..<NewsCopy.previewCount, id: \.self) { index in
                if index > 0 { AssetCardDivider() }
                NewsHeadlineRowSkeleton(layout: .section, isFirst: index == 0, index: index)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(NewsCopy.loading)
        .accessibilityIdentifier("asset-news-loading")
    }
}

/// The quiet failure: one muted line and a text button, not an empty state. News is the
/// least important thing on either screen it appears on, and a read that failed should
/// not take more room than the headlines would have.
struct NewsRetryLine: View {
    let identifier: String
    let retry: () -> Void

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
            Text(NewsCopy.failed)
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityIdentifier(identifier)
            Spacer(minLength: MonacoTheme.Space.s)
            AssetSectionTextButton(title: NewsCopy.retry, action: retry)
                .padding(.trailing, -MonacoTheme.Space.m)
                .accessibilityIdentifier("\(identifier)-retry")
        }
        .padding(.top, MonacoTheme.Space.xs)
    }
}
