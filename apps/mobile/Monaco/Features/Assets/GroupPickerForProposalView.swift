import MonacoCore
import SwiftUI

enum ProposalPickKind: String, Hashable, Identifiable {
    case buy
    case sell

    var id: String { rawValue }
}

/// Choose a cabal, then go straight to the amount step with the stock and the pot already set.
///
/// This screen owns the pot load and its retry: the amount step is only ever reached with a pot
/// in hand, so it never has to fetch one and never renders a ceiling it does not know yet. The
/// same fan-out answers "who holds this?", so Sell lists only the cabals that hold the stock and
/// picking one can never dead-end on "this cabal does not hold this stock".
///
/// The question sits over the list, with the stock's ticker in the market's voice, and the cabals
/// are ruled rows: the mark, the name, and on the right the figure the next step turns on — the
/// pot for a buy, the cabal's holding for a sell.
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

    /// Re-runs the fan-out when the cabal list arrives or changes. Not keyed on `kind`: buy and
    /// sell read the same pots, so switching between them must not refetch.
    private var holdingsTaskID: String {
        cabals.map(\.groupId).joined(separator: ",")
    }

    var body: some View {
        ScrollView {
            // No side padding: the rows run edge to edge; the question insets itself.
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                question
                    .padding(.horizontal, MonacoTheme.Space.m)
                content
            }
            .padding(.top, MonacoTheme.Space.s)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .monacoCanvas()
        .foregroundStyle(MonacoTheme.ink)
        .navigationTitle("Pick a cabal")
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("group-picker-root")
        .task { await loadCabals() }
        .task(id: holdingsTaskID) {
            guard !cabals.isEmpty else { return }
            await holdings.load(cabals: cabals)
        }
    }

    /// "Which cabal should buy AAPL?" — what is being picked, and for which stock, with the ticker
    /// set the way every row in the app sets it.
    private var question: some View {
        let parts = ProposeScreenCopy.pickerQuestion(kind: kind)
        return (
            Text(parts.lead).font(MonacoTheme.Typo.section)
                + Text(ticker).font(MonacoTheme.Typo.ticker)
                + Text(parts.tail).font(MonacoTheme.Typo.section)
        )
        .foregroundStyle(MonacoTheme.ink)
        .fixedSize(horizontal: false, vertical: true)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityAddTraits(.isHeader)
        .accessibilityIdentifier("group-picker-question")
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
            pickerRows
        }
    }

    /// Both kinds wait on the same fan-out: the amount step is only reached with a pot.
    @ViewBuilder
    private var pickerRows: some View {
        switch holdings.state {
        case .loading:
            skeletonRows
        case .failed:
            EmptyState(
                title: "Could not load your cabals",
                actionTitle: "Retry",
                action: reloadHoldings
            )
            .accessibilityIdentifier("group-picker-holdings-failed")
        case .resolved:
            switch kind {
            case .buy:
                buyRows
            case .sell:
                sellRows
            }
        }
    }

    /// The pot is the buy ceiling, so it is the figure on the right: a cabal with $40 in it cannot
    /// buy $100 of anything, and the member can see that before picking it.
    @ViewBuilder
    private var buyRows: some View {
        let rows = holdings.cabals
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoGroupedList {
                ForEach(Array(rows.enumerated()), id: \.element.id) { index, cabal in
                    NavigationLink {
                        ProposeAmountView(
                            service: service,
                            groupId: cabal.groupId,
                            stock: stock,
                            pot: cabal.pot,
                            onProposed: { _ in onProposed?(cabal.name) }
                        )
                    } label: {
                        MonacoRow(
                            title: cabal.name,
                            chevron: true,
                            isLast: index == rows.count - 1
                        ) {
                            CabalMark(groupId: cabal.groupId, name: cabal.name, pictureUrl: pictureUrl(cabal.groupId))
                        } trailing: {
                            MoneyText(micros: cabal.pot.totalMicros, style: .row)
                            Text(ProposeScreenCopy.inThePot)
                                .font(MonacoTheme.Typo.caption)
                                .foregroundStyle(MonacoTheme.muted)
                        }
                    }
                    .buttonStyle(.monacoRow)
                    .accessibilityIdentifier("pick-cabal-\(cabal.groupId)")
                }
            }
            unreachableFooter
        }
    }

    /// Only the cabals that hold the stock, each with what it holds: the shares under its name and
    /// what they are worth, with what they have made, on the right.
    @ViewBuilder
    private var sellRows: some View {
        let rows = holdings.holders
        if rows.isEmpty, holdings.unreachableCount > 0 {
            // Some cabals did not answer, so "none of them hold it" is not ours to say.
            EmptyState(
                title: "Could not check who holds \(ticker)",
                message: "Some of your cabals did not answer.",
                actionTitle: "Retry",
                action: reloadHoldings
            )
            .accessibilityIdentifier("group-picker-holdings-failed")
        } else if rows.isEmpty {
            EmptyState(
                title: "None of your cabals hold \(ticker)",
                message: "You can propose a buy instead.",
                actionTitle: "Propose a buy",
                action: { kind = .buy }
            )
            .accessibilityIdentifier("group-picker-no-holders")
        } else {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                MonacoGroupedList {
                    ForEach(Array(rows.enumerated()), id: \.element.id) { index, cabal in
                        if let holding = cabal.holding {
                            NavigationLink {
                                ProposeSellAmountView(
                                    service: service,
                                    groupId: cabal.groupId,
                                    holding: holding,
                                    pot: cabal.pot,
                                    onProposed: { _ in onProposed?(cabal.name) }
                                )
                            } label: {
                                MonacoRow(
                                    title: cabal.name,
                                    subtitle: PotSectionView.quantityLabel(holding),
                                    chevron: true,
                                    isLast: index == rows.count - 1
                                ) {
                                    CabalMark(groupId: cabal.groupId, name: cabal.name, pictureUrl: pictureUrl(cabal.groupId))
                                } trailing: {
                                    MoneyText(decimalString: holding.valueUsd, style: .row)
                                    PnLText(dollarPnl: holding.dollarPnl, style: .caption)
                                }
                            }
                            .buttonStyle(.monacoRow)
                            .accessibilityIdentifier("pick-cabal-\(cabal.groupId)")
                        }
                    }
                }
                unreachableFooter
            }
        }
    }

    /// Says how many cabals are missing from the list above rather than hiding the whole list.
    @ViewBuilder
    private var unreachableFooter: some View {
        let missing = holdings.unreachableCount
        if missing > 0 {
            HStack(spacing: MonacoTheme.Space.xs) {
                Text(missing == 1 ? "Couldn't check 1 cabal." : "Couldn't check \(missing) cabals.")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                Button("Retry", action: reloadHoldings)
                    .font(MonacoTheme.Typo.captionStrong)
                    .buttonStyle(.plain)
                    .foregroundStyle(MonacoTheme.brand)
                    .frame(minHeight: 44)
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("group-picker-unreachable")
        }
    }

    /// The cabal's picture from the session's list of the member's cabals; nil draws its initials.
    private func pictureUrl(_ groupId: String) -> String? {
        cabals.first { $0.groupId == groupId }?.pictureUrl
    }

    private func reloadHoldings() {
        Task { await holdings.load(cabals: cabals) }
    }

    /// Loading rows in the shape of the cabal rows: the tile, the name, the figure.
    private var skeletonRows: some View {
        MonacoGroupedList {
            ForEach(0..<3, id: \.self) { index in
                HStack(spacing: MonacoTheme.Space.sm) {
                    SkeletonBlock(width: 44, height: 44, radius: MonacoTheme.Radius.tile)
                    SkeletonBlock(width: 140, height: 14)
                    Spacer(minLength: MonacoTheme.Space.s)
                    VStack(alignment: .trailing, spacing: 6) {
                        SkeletonBlock(width: 72, height: 14)
                        SkeletonBlock(width: 48, height: 10)
                    }
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .frame(minHeight: 60)
                .overlay(alignment: .bottom) {
                    if index < 2 {
                        MonacoRule().padding(.leading, MonacoTheme.Space.m + 44 + MonacoTheme.Space.sm)
                    }
                }
            }
        }
        .accessibilityElement(children: .ignore)
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
