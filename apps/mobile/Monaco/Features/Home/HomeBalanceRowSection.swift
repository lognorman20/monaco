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

/// Account balance pill with the two money-movement actions: Add money (the USDC deposit
/// address for this account) and Cash out (withdraw idle balance to an external address).
struct HomeBalanceRowSection: View {
    @ObservedObject var auth: PrivyAuthService
    let balance: PlatformBalanceDTO?
    let isBalanceLoading: Bool
    let joinedCabals: [HomeGroupBoardRowDTO]
    /// Whether the retry this row offers is already running. Retrying the balance is the whole
    /// Home refresh, so without this the member can stack three of them by tapping.
    var isRetryingBalance = false
    var onRetryBalance: () -> Void = {}

    private var display: HomeBalanceDisplay {
        HomeBalanceDisplay.resolve(balance: balance, isLoading: isBalanceLoading)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            VStack(alignment: .leading, spacing: 2) {
                Text("Account balance")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                switch display {
                case .amount(let micros):
                    MoneyText(micros: micros, style: .row)
                        .accessibilityIdentifier("platform-balance-value")
                case .loading:
                    ProgressView()
                        .tint(MonacoTheme.accent)
                        .accessibilityIdentifier("platform-balance-loading")
                case .unavailable:
                    unavailableBalance
                }
            }

            HStack(spacing: MonacoTheme.Space.s) {
                NavigationLink {
                    DepositView(auth: auth, joinedCabals: joinedCabals)
                } label: {
                    Text("Add money")
                        .frame(maxWidth: .infinity)
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                }
                .buttonStyle(.monacoPrimary)
                .accessibilityIdentifier("home-add-money-link")

                NavigationLink {
                    WithdrawView(auth: auth)
                } label: {
                    Text("Cash out")
                        .frame(maxWidth: .infinity)
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                }
                .buttonStyle(.monacoSecondary)
                .accessibilityIdentifier("home-cash-out-link")
            }
        }
        .padding(MonacoTheme.Space.m)
        .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous))
    }

    /// A dash, not a figure — and its own identifier, so nothing (a UI test included) can read
    /// a failed balance as a real one.
    private var unavailableBalance: some View {
        HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
            Text("—")
                .font(MoneyStyle.row.font)
                .foregroundStyle(MonacoTheme.muted)
                .accessibilityLabel("Account balance unavailable")
                .accessibilityIdentifier("platform-balance-unavailable")
            Button("Try again", action: onRetryBalance)
                .font(MonacoTheme.Typo.callout.weight(.semibold))
                .foregroundStyle(isRetryingBalance ? MonacoTheme.muted : MonacoTheme.brand)
                .buttonStyle(.plain)
                .frame(minHeight: 44)
                .disabled(isRetryingBalance)
                .accessibilityIdentifier("home-balance-retry")
        }
    }
}
