import MonacoCore
import SwiftUI

/// What is in the account, above a flow that is about to spend it.
///
/// It is an E1 card — a shadow in light, an edge in dark — rather than a hairline box, and the
/// label is an eyebrow over the figure, which is the shape every money label in v3 takes.
struct PlatformBalanceCard: View {
    let balance: PlatformBalanceDTO?
    var isLoading: Bool = false

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
            Text("Account balance")
                .displayFont(.eyebrow)
                .foregroundStyle(MonacoTheme.fgSubtle)
            if let balance {
                MoneyText(micros: balance.availableUsdcMicros, style: .large)
                    .accessibilityIdentifier("platform-balance-value")
                if balance.pendingAllocationMicros > 0 {
                    Text("\(UsdAmountFormatter.format(micros: balance.pendingAllocationMicros)) funding a cabal")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.fgMuted)
                }
            } else if isLoading {
                ProgressView()
                    .tint(MonacoTheme.controlTint)
                    .accessibilityIdentifier("platform-balance-loading")
            } else {
                MoneyText(micros: 0, style: .large)
                    .accessibilityIdentifier("platform-balance-value")
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(MonacoTheme.Space.m)
        .monacoElevation(.card)
    }
}
