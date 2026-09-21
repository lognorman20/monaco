import MonacoCore
import SwiftUI

struct PlatformBalanceCard: View {
    let balance: PlatformBalanceDTO?
    var isLoading: Bool = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Account balance")
                .font(.caption.weight(.semibold))
                .foregroundStyle(MonacoTheme.secondaryText)
            if let balance {
                Text(UsdAmountFormatter.format(micros: balance.availableUsdcMicros))
                    .font(.title2.bold().monospacedDigit())
                    .foregroundStyle(MonacoTheme.primaryText)
                    .accessibilityIdentifier("platform-balance-value")
                if balance.pendingAllocationMicros > 0 {
                    Text("\(UsdAmountFormatter.format(micros: balance.pendingAllocationMicros)) funding a cabal")
                        .font(.caption)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
            } else if isLoading {
                ProgressView()
                    .tint(MonacoTheme.accent)
                    .accessibilityIdentifier("platform-balance-loading")
            } else {
                Text("$0.00")
                    .font(.title2.bold().monospacedDigit())
                    .foregroundStyle(MonacoTheme.primaryText)
                    .accessibilityIdentifier("platform-balance-value")
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .monacoSurfaceCard()
    }
}
