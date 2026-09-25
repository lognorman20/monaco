import MonacoCore
import SwiftUI

/// The cabal behind a typed or pasted code, under the field on the join screen: its mark,
/// name, members and pot, and what joining means for its policy. A skeleton in the same
/// shape while the preview loads; nothing for a dead code, whose line under the field says so.
struct InviteCodePreviewSection: View {
    let preview: InviteCodeEntryModel.Preview
    var onRetry: () -> Void = {}

    var body: some View {
        switch preview {
        case .idle, .notFound:
            EmptyView()
        case .loading:
            skeleton
        case .loaded(let cabal):
            header(cabal)
        case .unavailable:
            Button(action: onRetry) {
                Text(InviteEntryCopy.retryPreview)
                    .font(MonacoTheme.Typo.calloutStrong)
                    .foregroundStyle(MonacoTheme.brand)
                    .frame(minHeight: 44)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("join-group-preview-retry")
        }
    }

    private func header(_ cabal: InvitePreviewDTO) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
            HStack(spacing: MonacoTheme.Space.sm) {
                CabalMark(groupId: cabal.groupId, name: cabal.name, size: 56, pictureUrl: cabal.pictureUrl)
                VStack(alignment: .leading, spacing: 2) {
                    // Its own element, labelled with the name alone, as on the row route.
                    Text(cabal.name)
                        .font(MonacoTheme.Typo.title)
                        .foregroundStyle(MonacoTheme.ink)
                        .lineLimit(2)
                        .minimumScaleFactor(0.8)
                        .accessibilityAddTraits(.isHeader)
                        .accessibilityIdentifier("join-group-name")
                    let facts = InviteEntryCopy.facts(for: cabal)
                    if !facts.isEmpty {
                        Text(facts)
                            .font(MonacoTheme.Typo.caption)
                            .foregroundStyle(MonacoTheme.muted)
                            .accessibilityIdentifier("join-group-facts")
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            Text(JoinCabalScreenCopy.explanation(joinMode: cabal.joinPolicy))
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)
        }
        .transition(.opacity)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("join-group-preview")
    }

    private var skeleton: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            SkeletonBlock(width: 56, height: 56, radius: MonacoTheme.Radius.tile)
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                SkeletonBlock(width: 180, height: 22)
                SkeletonBlock(width: 140, height: 13)
            }
        }
        .accessibilityElement()
        .accessibilityLabel("Looking up the cabal")
        .accessibilityIdentifier("join-group-preview-loading")
    }
}
