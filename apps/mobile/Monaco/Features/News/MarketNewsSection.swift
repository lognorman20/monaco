import MonacoCore
import SwiftUI

/// "Market today" on the Stocks tab: the three newest market headlines, under the movers.
///
/// A glance, not a feed. The section is left out entirely when the market pulse has
/// nothing, the way "Up for vote" is on a quiet day; a read that failed keeps its title
/// and says so in one line.
struct MarketNewsSection: View {
    let model: NewsFeedModel
    var open: (URL) -> Void = { NewsArticleReader.open($0) }

    private static let count = 3

    var body: some View {
        switch model.phase {
        case .loading:
            section {
                MonacoGroupedList {
                    ForEach(0..<Self.count, id: \.self) { index in
                        NewsHeadlineRowSkeleton(layout: .list, isLast: index == Self.count - 1, index: index)
                    }
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(NewsCopy.loading)
            }
        case .failed:
            section {
                NewsRetryLine(identifier: "assets-market-news-failed") {
                    Task { await model.load() }
                }
                .padding(.horizontal, MonacoTheme.Space.m)
            }
        case .answered:
            if !model.headlines.isEmpty {
                section {
                    let headlines = Array(model.headlines.prefix(Self.count))
                    MonacoGroupedList {
                        ForEach(Array(headlines.enumerated()), id: \.element.id) { index, headline in
                            NewsHeadlineRow(
                                headline: headline,
                                layout: .list,
                                isLast: index == headlines.count - 1,
                                open: open
                            )
                            .accessibilityIdentifier("assets-market-news-row-\(index)")
                        }
                    }
                }
            }
        }
    }

    /// The tab's section shape: a title on the page's inset, then the ruled rows.
    private func section<Content: View>(@ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            MonacoSectionHeader(NewsCopy.marketSectionTitle)
                .padding(.horizontal, MonacoTheme.Space.m)
            content()
        }
        // `.contain` so the identifier lands on the section, not on every row inside it.
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("assets-market-news")
    }
}
