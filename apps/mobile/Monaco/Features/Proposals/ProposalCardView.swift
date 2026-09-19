import MonacoCore
import SwiftUI

/// Feed card: what the cabal would buy, who proposed it, where the vote stands, and inline yes/no.
/// Used by `ProposalFeedView` (summary links to detail) and `ProposalDetailView` (header, no link).
struct ProposalCardView<Destination: View>: View {
    let proposal: ProposalDTO
    var isVoting = false
    var onVote: (ProposalVoteChoice) -> Void = { _ in }
    /// Detail screen pushed when the summary is tapped; nil when the card is the detail header.
    var destination: (() -> Destination)?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if let destination {
                NavigationLink {
                    destination()
                } label: {
                    summary
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("proposal-card-open-\(proposal.id)")
            } else {
                summary
            }

            if proposal.showsVoteActions {
                voteButtons
            }
        }
        .monacoSurfaceCard()
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("proposal-card-\(proposal.id)")
    }

    private var summary: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline) {
                Text(ProposalFeedCopy.title(for: proposal))
                    .font(.title3.weight(.bold))
                    .foregroundStyle(MonacoTheme.primaryText)
                Spacer()
                ProposalStatusChip(status: proposal.status, kind: proposal.resolvedKind)
            }

            Text(ProposalFeedCopy.headline(for: proposal))
            .font(.body.weight(.semibold))
            .foregroundStyle(MonacoTheme.primaryText)
            .accessibilityIdentifier("proposal-card-amount-\(proposal.id)")

            if let byline {
                Text(byline)
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.secondaryText)
            }

            if let summary = proposal.voteSummary {
                ProposalVoteBar(progress: ProposalVoteProgress(summary: summary))
                    .accessibilityIdentifier("proposal-card-votes-\(proposal.id)")
            }

            HStack(spacing: 12) {
                if proposal.isOpen, let expiresAt = proposal.expiresAt,
                   let closes = ProposalTimeFormatter.closesLabel(expiresAt: expiresAt) {
                    Label(closes, systemImage: "clock")
                        .foregroundStyle(MonacoTheme.accent)
                }
                Spacer(minLength: 0)
                if let count = proposal.commentCount {
                    Label(ProposalFeedCopy.commentCount(count), systemImage: "bubble.left")
                        .foregroundStyle(MonacoTheme.secondaryText)
                        .accessibilityIdentifier("proposal-card-comment-count-\(proposal.id)")
                }
            }
            .font(.caption.weight(.medium))
        }
    }

    private var byline: String? {
        guard let name = proposal.proposerName, !name.isEmpty else { return nil }
        var text = ProposalFeedCopy.proposedBy(name)
        if let createdAt = proposal.createdAt {
            let age = ProposalTimeFormatter.ageLabel(createdAt)
            if !age.isEmpty { text += " · \(age)" }
        }
        return text
    }

    private var voteButtons: some View {
        HStack(spacing: 10) {
            Button {
                onVote(.yes)
            } label: {
                Label(ProposalFeedCopy.voteYes, systemImage: "hand.thumbsup")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.monacoPrimary)
            .accessibilityIdentifier("proposal-card-vote-yes-\(proposal.id)")

            Button {
                onVote(.no)
            } label: {
                Label(ProposalFeedCopy.voteNo, systemImage: "hand.thumbsdown")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.monacoSecondary)
            .accessibilityIdentifier("proposal-card-vote-no-\(proposal.id)")
        }
        .disabled(isVoting)
        .opacity(isVoting ? 0.6 : 1)
    }
}

extension ProposalCardView where Destination == EmptyView {
    /// Card without a detail link, used as the header of `ProposalDetailView`.
    init(proposal: ProposalDTO, isVoting: Bool = false, onVote: @escaping (ProposalVoteChoice) -> Void = { _ in }) {
        self.init(proposal: proposal, isVoting: isVoting, onVote: onVote, destination: nil)
    }
}

/// Two-tone bar: yes fills from the left, no from the right, the gap is members still to vote.
struct ProposalVoteBar: View {
    let progress: ProposalVoteProgress

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            GeometryReader { proxy in
                let width = proxy.size.width
                ZStack(alignment: .leading) {
                    Capsule().fill(MonacoTheme.border.opacity(0.5))
                    HStack(spacing: 0) {
                        Rectangle()
                            .fill(MonacoTheme.success)
                            .frame(width: width * progress.yesFraction)
                        Spacer(minLength: 0)
                        Rectangle()
                            .fill(MonacoTheme.destructive)
                            .frame(width: width * progress.noFraction)
                    }
                    .clipShape(Capsule())
                }
            }
            .frame(height: 6)
            .accessibilityHidden(true)

            Text(progress.caption)
                .font(.caption)
                .foregroundStyle(MonacoTheme.secondaryText)
        }
        .accessibilityElement(children: .combine)
    }
}
