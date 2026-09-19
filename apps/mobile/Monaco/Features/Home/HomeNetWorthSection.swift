import MonacoCore
import SwiftUI

struct HomeNetWorthSection<DepositLink: View>: View {
    let dashboard: HomeDashboardDTO
    let balance: PlatformBalanceDTO?
    let isBalanceLoading: Bool
    @ViewBuilder var depositLink: DepositLink

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                MonacoHeroHeader(
                    title: UsdAmountFormatter.format(decimalString: dashboard.netWorthUsd),
                    caption: "Your cabals"
                )
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("home-net-worth")

                HStack(spacing: MonacoTheme.Space.m) {
                    Text(dashboard.netWorthDollarPnl)
                        .font(.subheadline.monospacedDigit())
                        .foregroundStyle(HomePnLTint.color(dashboard.netWorthDollarPnl))
                    Text(PercentReturnFormatter.format(dashboard.netWorthPercentReturn))
                        .font(.subheadline.monospacedDigit())
                        .foregroundStyle(MonacoTheme.muted)
                }
            }

            HStack(alignment: .center, spacing: MonacoTheme.Space.m) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Account")
                        .font(MonacoTheme.TypeRole.caption)
                        .foregroundStyle(MonacoTheme.muted)
                    if let balance {
                        Text(UsdAmountFormatter.format(micros: balance.availableUsdcMicros))
                            .font(.body.monospacedDigit().weight(.semibold))
                            .foregroundStyle(MonacoTheme.ink)
                            .accessibilityIdentifier("platform-balance-value")
                    } else if isBalanceLoading {
                        ProgressView()
                            .tint(MonacoTheme.accent)
                            .accessibilityIdentifier("platform-balance-loading")
                    } else {
                        Text("$0.00")
                            .font(.body.monospacedDigit().weight(.semibold))
                            .foregroundStyle(MonacoTheme.ink)
                            .accessibilityIdentifier("platform-balance-value")
                    }
                }
                Spacer(minLength: 8)
                depositLink
            }
        }
    }
}

enum HomePnLTint {
    static func color(_ raw: String) -> Color {
        if raw.hasPrefix("-") {
            return MonacoTheme.destructive
        }
        if raw.hasPrefix("+"), raw != "+0.00" {
            return MonacoTheme.success
        }
        return MonacoTheme.muted
    }
}
