import MonacoCore
import SwiftUI

enum ProposalPickKind: String, Hashable, Identifiable {
    case buy
    case sell

    var id: String { rawValue }
}

/// Choose a joined cabal, then open propose buy or sell with the stock set.
struct GroupPickerForProposalView: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session
    let symbol: String
    let kind: ProposalPickKind

    private var cabals: [HomeGroupBoardRowDTO] {
        session.joinedCabals
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                if cabals.isEmpty {
                    MonacoEmptyStateCard(
                        message: "Join a cabal first to propose a buy or sell.",
                        systemImage: "person.3"
                    )
                } else {
                    ForEach(cabals) { cabal in
                        NavigationLink {
                            destination(for: cabal)
                        } label: {
                            MonacoRowCard(
                                systemImage: "person.3.fill",
                                title: cabal.name,
                                subtitle: nil,
                                trailing: UsdAmountFormatter.format(decimalString: cabal.potValueUsd)
                            )
                        }
                        .buttonStyle(.plain)
                        .accessibilityIdentifier("pick-cabal-\(cabal.groupId)")
                    }
                }
            }
            .padding(MonacoTheme.Space.m)
        }
        .monacoCanvas()
        .foregroundStyle(MonacoTheme.ink)
        .navigationTitle("Pick a cabal")
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("group-picker-root")
        .task {
            if session.home == nil {
                await session.refreshHomeBoards(accessToken: auth.accessToken)
            }
        }
    }

    @ViewBuilder
    private func destination(for cabal: HomeGroupBoardRowDTO) -> some View {
        switch kind {
        case .buy:
            ProposeBuyView(auth: auth, groupId: cabal.groupId, initialSymbol: symbol)
        case .sell:
            ProposeSellFromAssetView(auth: auth, groupId: cabal.groupId, symbol: symbol)
        }
    }
}

struct ProposeSellFromAssetView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let symbol: String

    private let apiClient = MonacoAPIClient()
    @State private var holdings: [PotRowDTO]?
    @State private var loadFailed = false

    var body: some View {
        Group {
            if let holdings {
                if holdings.isEmpty {
                    ScrollView {
                        MonacoEmptyStateCard(
                            message: "This cabal does not hold this stock.",
                            systemImage: "chart.line.downtrend.xyaxis"
                        )
                        .padding(MonacoTheme.Space.m)
                    }
                    .monacoCanvas()
                } else {
                    ProposeSellView(
                        auth: auth,
                        groupId: groupId,
                        holdings: holdings,
                        initialSymbol: symbol
                    )
                }
            } else if loadFailed {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
                    Text("Could not load this cabal.")
                        .font(MonacoTheme.TypeRole.body)
                        .foregroundStyle(MonacoTheme.destructive)
                    Button("Retry") {
                        Task { await loadHoldings() }
                    }
                    .buttonStyle(.monacoPrimary)
                }
                .padding(MonacoTheme.Space.m)
                .monacoCanvas()
            } else {
                ProgressView()
                    .tint(MonacoTheme.accent)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .monacoCanvas()
            }
        }
        .navigationTitle("Propose sell")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            await loadHoldings()
        }
    }

    private func loadHoldings() async {
        guard let token = auth.accessToken else { return }
        loadFailed = false
        do {
            let view = try await apiClient.getGroupView(accessToken: token, groupId: groupId)
            let needle = symbol.trimmingCharacters(in: .whitespacesAndNewlines)
            holdings = view.pot.filter { row in
                row.symbol.uppercased() != "USDC"
                    && row.symbol.caseInsensitiveCompare(needle) == .orderedSame
                    && (Int64(row.tokenAmount ?? "0") ?? 0) > 0
            }
        } catch {
            if error.isRequestCancellation { return }
            loadFailed = true
        }
    }
}
