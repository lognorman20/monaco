import MonacoCore
import SwiftUI

/// Account balance pill with the two money-movement actions: Add money (deposit into a
/// cabal) and Cash out (withdraw idle balance to an external address).
struct HomeBalanceRowSection: View {
    @ObservedObject var auth: DynamicAuthService
    let balance: PlatformBalanceDTO?
    let isBalanceLoading: Bool
    let joinedCabals: [HomeGroupBoardRowDTO]

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            VStack(alignment: .leading, spacing: 2) {
                Text("Account balance")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                if let balance {
                    MoneyText(micros: balance.availableUsdcMicros, style: .row)
                        .accessibilityIdentifier("platform-balance-value")
                } else if isBalanceLoading {
                    ProgressView()
                        .tint(MonacoTheme.accent)
                        .accessibilityIdentifier("platform-balance-loading")
                } else {
                    MoneyText(0, style: .row)
                        .accessibilityIdentifier("platform-balance-value")
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
}
