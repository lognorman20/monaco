import MonacoCore
import SwiftUI

enum ProposalPickKind: String, Hashable, Identifiable {
    case buy
    case sell

    var id: String { rawValue }
}

/// Choose a cabal, then go straight to the amount step with the stock already set.
///
/// Sell lists only the cabals that hold the stock, so picking one can never dead-end on
/// "this cabal does not hold this stock".
struct GroupPickerForProposalView: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session
    let symbol: String
    let stock: ProposeStock
    var onProposed: ((_ cabalName: String) -> Void)?

    @State private var kind: ProposalPickKind
    @State private var holdings: CabalHoldingsModel
    @State private var service: ProposeService
    @State private var isLoadingCabals = false
    @State private var cabalsLoadFailed = false

    init(
        auth: PrivyAuthService,
        symbol: String,
        kind: ProposalPickKind,
        stock: ProposeStock? = nil,
        onProposed: ((_ cabalName: String) -> Void)? = nil,
        service: ProposeService? = nil,
        holdingsDataSource: CabalHoldingsDataSource? = nil
    ) {
        self.auth = auth
        self.symbol = symbol
        self.stock = stock ?? ProposeStock(symbol: symbol)
        self.onProposed = onProposed
        _kind = State(initialValue: kind)
        _service = State(initialValue: service ?? LiveProposeService(auth: auth))
        _holdings = State(initialValue: CabalHoldingsModel(
            symbol: symbol,
            dataSource: holdingsDataSource ?? LiveCabalHoldingsDataSource(auth: auth)
        ))
    }

    private var cabals: [HomeGroupBoardRowDTO] {
        session.joinedCabals
    }

    private var ticker: String {
        AssetSymbolFormatter.display(symbol)
    }

    /// Re-runs the holdings fan-out when the cabal list arrives, and when Sell is chosen.
    private var holdingsTaskID: String {
        kind.rawValue + "|" + cabals.map(\.groupId).joined(separator: ",")
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                content
            }
            .padding(MonacoTheme.Space.m)
        }
        .monacoCanvas()
        .foregroundStyle(MonacoTheme.ink)
        .navigationTitle("Pick a cabal")
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("group-picker-root")
        .task { await loadCabals() }
        .task(id: holdingsTaskID) {
            guard kind == .sell, !cabals.isEmpty else { return }
            await holdings.load(cabals: cabals)
        }
        .onChange(of: holdings.sessionExpired) { _, expired in
            guard expired else { return }
            Task { await auth.signOutAfterRejectedSession() }
        }
    }

    @ViewBuilder
    private var content: some View {
        if isLoadingCabals {
            skeletonRows
        } else if cabalsLoadFailed {
            EmptyState(
                title: "Could not load your cabals",
                actionTitle: "Retry",
                action: { Task { await loadCabals(force: true) } }
            )
            .accessibilityIdentifier("group-picker-failed")
        } else if cabals.isEmpty {
            EmptyState(
                title: "Join a cabal first",
                message: "Buying and selling happen with a cabal, not on your own."
            )
            .accessibilityIdentifier("group-picker-no-cabals")
        } else {
            switch kind {
            case .buy:
                buyRows
            case .sell:
                sellRows
            }
        }
    }

    private var buyRows: some View {
        MonacoGroupedList {
            ForEach(Array(cabals.enumerated()), id: \.element.id) { index, cabal in
                NavigationLink {
                    ProposeAmountView(
                        service: service,
                        groupId: cabal.groupId,
                        stock: stock,
                        pot: nil,
                        onProposed: { _ in onProposed?(cabal.name) }
                    )
                } label: {
                    MonacoRow(
                        title: cabal.name,
                        subtitle: "Pot",
                        chevron: true,
                        isLast: index == cabals.count - 1
                    ) {
                        CabalMark(groupId: cabal.groupId, name: cabal.name)
                    } trailing: {
                        MoneyText(decimalString: cabal.potValueUsd, style: .row)
                    }
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier("pick-cabal-\(cabal.groupId)")
            }
        }
    }

    @ViewBuilder
    private var sellRows: some View {
        switch holdings.state {
        case .loading:
            skeletonRows
        case .failed:
            EmptyState(
                title: "Could not check who holds \(ticker)",
                actionTitle: "Retry",
                action: { Task { await holdings.load(cabals: cabals) } }
            )
            .accessibilityIdentifier("group-picker-holdings-failed")
        case .loaded(let rows) where rows.isEmpty:
            EmptyState(
                title: "None of your cabals hold \(ticker)",
                message: "You can propose a buy instead.",
                actionTitle: "Propose a buy",
                action: { kind = .buy }
            )
            .accessibilityIdentifier("group-picker-no-holders")
        case .loaded(let rows):
            MonacoGroupedList {
                ForEach(Array(rows.enumerated()), id: \.element.id) { index, holding in
                    NavigationLink {
                        ProposeSellAmountView(
                            service: service,
                            groupId: holding.groupId,
                            holding: holding.row,
                            pot: nil,
                            onProposed: { _ in onProposed?(holding.name) }
                        )
                    } label: {
                        MonacoRow(
                            title: holding.name,
                            subtitle: ProposalShareFormatter.sharesLabel(fromAtomics: holding.row.tokenAmount ?? "0"),
                            chevron: true,
                            isLast: index == rows.count - 1
                        ) {
                            CabalMark(groupId: holding.groupId, name: holding.name)
                        } trailing: {
                            MoneyText(decimalString: holding.row.valueUsd, style: .row)
                        }
                    }
                    .buttonStyle(.monacoRow)
                    .accessibilityIdentifier("pick-cabal-\(holding.groupId)")
                }
            }
        }
    }

    private var skeletonRows: some View {
        MonacoGroupedList {
            ForEach(0..<3, id: \.self) { _ in
                HStack(spacing: MonacoTheme.Space.sm) {
                    SkeletonBlock(width: 44, height: 44, radius: MonacoTheme.Radius.tile)
                    VStack(alignment: .leading, spacing: 6) {
                        SkeletonBlock(width: 120, height: 14)
                        SkeletonBlock(width: 56, height: 12)
                    }
                    Spacer()
                    SkeletonBlock(width: 64, height: 14)
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .frame(minHeight: 60)
            }
        }
        .accessibilityLabel("Loading your cabals")
    }

    /// `home == nil` after a refresh is the only signal the store gives that the load failed;
    /// without it "Join a cabal first" shows for members who already have cabals.
    private func loadCabals(force: Bool = false) async {
        guard force || session.home == nil else { return }
        isLoadingCabals = true
        cabalsLoadFailed = false
        defer { isLoadingCabals = false }
        await session.refreshHomeBoards(accessToken: auth.accessToken)
        cabalsLoadFailed = session.home == nil
    }
}
