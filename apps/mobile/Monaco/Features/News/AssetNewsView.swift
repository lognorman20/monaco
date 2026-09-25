import MonacoCore
import SwiftUI

/// Every headline the server has for one stock, or for the market: "See all" from the
/// stock screen. It reads the same model the section does, so opening it fetches nothing
/// the member has not already waited for.
struct AssetNewsView: View {
    let model: NewsFeedModel
    /// "Alphabet": the title and the empty line name it.
    let companyName: String
    var open: (URL) -> Void = { NewsArticleReader.open($0) }

    var body: some View {
        ScrollView {
            content
                .padding(.vertical, MonacoTheme.Space.m)
        }
        .refreshable { await model.load() }
        .monacoCanvas()
        .foregroundStyle(MonacoTheme.ink)
        .navigationTitle(NewsCopy.listTitle(companyName))
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("asset-news-root")
    }

    @ViewBuilder
    private var content: some View {
        switch model.phase {
        case .loading:
            MonacoGroupedList {
                ForEach(0..<6, id: \.self) { index in
                    NewsHeadlineRowSkeleton(layout: .list, isLast: index == 5, index: index)
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(NewsCopy.loading)
            .accessibilityIdentifier("asset-news-list-loading")
        case .failed:
            // Here the list is the whole screen, so the failure gets the full empty state.
            EmptyState(
                title: NewsCopy.failed,
                actionTitle: NewsCopy.retry,
                action: { Task { await model.load() } }
            )
            .accessibilityIdentifier("asset-news-list-failed")
        case .answered:
            if model.headlines.isEmpty {
                EmptyState(title: NewsCopy.empty(companyName))
                    .accessibilityIdentifier("asset-news-list-empty")
            } else {
                MonacoGroupedList {
                    ForEach(Array(model.headlines.enumerated()), id: \.element.id) { index, headline in
                        NewsHeadlineRow(
                            headline: headline,
                            layout: .list,
                            isLast: index == model.headlines.count - 1,
                            open: open
                        )
                        .accessibilityIdentifier("asset-news-list-row-\(index)")
                    }
                }
            }
        }
    }
}
