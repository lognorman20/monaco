import MonacoCore
import SwiftUI

/// "Needs your vote": plain rows, not compact proposal cards. `HomeMissedProposalRowDTO`
/// carries no amount or tally, so this only ever routes to the real detail screen.
/// The section is not rendered at all when `rows` is empty — no "caught up" card.
struct HomeMissedVotesSection: View {
    @ObservedObject var auth: PrivyAuthService
    let rows: [HomeMissedProposalRowDTO]

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text("Needs your vote")
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.ink)

            VStack(spacing: 1) {
                ForEach(rows, id: \.proposalId) { (row: HomeMissedProposalRowDTO) in
                    NavigationLink {
                        ProposalDetailView(auth: auth, proposalId: row.proposalId)
                    } label: {
                        HomeMissedVoteRow(
                            title: AssetSymbolFormatter.format(row.symbol),
                            subtitle: "\(row.groupName) · \(closesInLabel(row.expiresAt))",
                            isLast: row.proposalId == rows.last?.proposalId
                        )
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("home-missed-\(row.proposalId)")
                }
            }
            .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous))
        }
    }

    /// Interim countdown copy. Phase B may fold this into a shared formatter once WP1
    /// lands `RelativeTimeFormatter` (which handles past timestamps, not this "closes in").
    private func closesInLabel(_ expiresAt: Date, now: Date = Date()) -> String {
        let remaining = expiresAt.timeIntervalSince(now)
        guard remaining > 0 else { return "closing" }
        let hours = Int(remaining / 3600)
        if hours >= 1 { return "closes in \(hours)h" }
        let minutes = max(1, Int(remaining / 60))
        return "closes in \(minutes)m"
    }
}

private struct HomeMissedVoteRow: View {
    let title: String
    let subtitle: String
    let isLast: Bool

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: MonacoTheme.Space.m) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(.body.weight(.semibold))
                        .foregroundStyle(MonacoTheme.ink)
                        .lineLimit(1)
                    Text(subtitle)
                        .font(MonacoTheme.TypeRole.caption)
                        .foregroundStyle(MonacoTheme.muted)
                        .lineLimit(1)
                }
                Spacer(minLength: MonacoTheme.Space.s)
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(MonacoTheme.muted)
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            .frame(minHeight: 60)
            .contentShape(Rectangle())

            if !isLast {
                Rectangle()
                    .fill(MonacoTheme.hairline)
                    .frame(height: 1)
                    .padding(.leading, MonacoTheme.Space.m)
            }
        }
    }
}
