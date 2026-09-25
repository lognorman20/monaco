import MonacoCore
import SwiftUI

/// What the account balance row shows.
///
/// The store keeps the balance as an optional plus a loading flag, and a read that fails
/// leaves it empty with nothing loading. That is not zero money: rendering it as $0.00 tells a
/// funded member their account is empty, right above "Cash out" (#277, #324).
enum HomeBalanceDisplay: Equatable {
    case loading
    case amount(Int64)
    case unavailable

    static func resolve(balance: PlatformBalanceDTO?, isLoading: Bool) -> HomeBalanceDisplay {
        if let balance { return .amount(balance.availableUsdcMicros) }
        return isLoading ? .loading : .unavailable
    }
}

/// The account balance as one ruled row — the cash coin, the figure — with the two money
/// actions as text buttons under it. Add money is the USDC deposit address for this account;
/// Cash out withdraws idle balance to an external address.
///
/// It used to be a white card with a balance and two capsule buttons, which is the anatomy of
/// a wallet app and was the second-largest thing on Home. The cash is a line in the ledger now,
/// and the actions are the size of the decision they represent.
struct HomeBalanceRowSection: View {
    @ObservedObject var auth: PrivyAuthService
    let balance: PlatformBalanceDTO?
    let isBalanceLoading: Bool
    let joinedCabals: [HomeGroupBoardRowDTO]
    /// Whether the retry this row offers is already running. Retrying the balance is the whole
    /// Home refresh, so without this the member can stack three of them by tapping.
    var isRetryingBalance = false
    var onRetryBalance: () -> Void = {}
    /// Profile draws the same row under its own names, so a test can tell the two apart.
    var identifierPrefix = "home"
    var balanceIdentifier = "platform-balance-value"
    /// Which outer rules to draw; Profile stacks this directly under its ruled stat band.
    var rules: MonacoListRules = .both



    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var display: HomeBalanceDisplay {
        HomeBalanceDisplay.resolve(balance: balance, isLoading: isBalanceLoading)
    }

    /// Side by side normally; one under the other at the accessibility sizes, where two
    /// labels in a row wrap into each other.
    private var isStacked: Bool { dynamicTypeSize.isAccessibilitySize }

    var body: some View {

        MonacoGroupedList(rules: rules) {
            HStack(spacing: MonacoTheme.Space.sm) {
                StockMark(symbol: "USDC", size: 40)

                    .frame(width: 44, height: 44)
                Text("Account balance")
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.ink)
                    .frame(maxWidth: .infinity, alignment: .leading)
                figure
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.vertical, MonacoTheme.Space.s)
            .frame(minHeight: 60)
            .accessibilityElement(children: .combine)

            actions
                .padding(.leading, MonacoTheme.Space.m + 44 + MonacoTheme.Space.sm)
                .padding(.trailing, MonacoTheme.Space.m)
                .padding(.bottom, MonacoTheme.Space.xs)
        }
    }

    @ViewBuilder
    private var actions: some View {
        if isStacked {
            VStack(alignment: .leading, spacing: 0) {
                addMoney
                cashOut
            }
        } else {
            HStack(spacing: MonacoTheme.Space.l) {
                addMoney
                cashOut
                Spacer(minLength: 0)
            }
        }
    }

    private var addMoney: some View {
        NavigationLink {
            DepositView(auth: auth, joinedCabals: joinedCabals)
        } label: {
            Text("Add money")
                .font(MonacoTheme.Typo.calloutStrong)
                .foregroundStyle(MonacoTheme.brand)
                .frame(minHeight: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("\(identifierPrefix)-add-money-link")
    }

    private var cashOut: some View {
        NavigationLink {
            WithdrawView(auth: auth)
        } label: {
            Text("Cash out")
                .font(MonacoTheme.Typo.calloutStrong)
                .foregroundStyle(MonacoTheme.brand)
                .frame(minHeight: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("\(identifierPrefix)-cash-out-link")
    }

    @ViewBuilder
    private var figure: some View {
        switch display {
        case .amount(let micros):
            MoneyText(micros: micros, style: .row)
                .accessibilityIdentifier(balanceIdentifier)

        case .loading:
            ProgressView()
                .tint(MonacoTheme.accent)
                .accessibilityIdentifier("platform-balance-loading")
        case .unavailable:
            unavailableBalance
        }
    }

    /// A dash, not a figure — and its own identifier, so nothing (a UI test included) can read
    /// a failed balance as a real one.
    private var unavailableBalance: some View {
        HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
            Button("Try again", action: onRetryBalance)
                .font(MonacoTheme.Typo.calloutStrong)
                .foregroundStyle(isRetryingBalance ? MonacoTheme.muted : MonacoTheme.brand)
                .buttonStyle(.plain)
                .frame(minHeight: 44)
                .disabled(isRetryingBalance)
                .accessibilityIdentifier("home-balance-retry")
            Text("—")
                .moneyFont(.row)
                .foregroundStyle(MonacoTheme.muted)
                .accessibilityLabel("Account balance unavailable")
                .accessibilityIdentifier("platform-balance-unavailable")
        }
    }
}
