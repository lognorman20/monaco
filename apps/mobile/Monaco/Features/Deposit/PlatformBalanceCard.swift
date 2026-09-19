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
                Text(formatUsdc(balance.availableUsdcMicros))
                    .font(.title2.bold().monospacedDigit())
                    .foregroundStyle(MonacoTheme.primaryText)
                    .accessibilityIdentifier("platform-balance-value")
                if balance.pendingAllocationMicros > 0 {
                    Text("$\(formatUsdAmount(balance.pendingAllocationMicros)) funding a cabal")
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

    private func formatUsdc(_ micros: Int64) -> String {
        String(format: "$%.2f", Double(micros) / 1_000_000.0)
    }

    private func formatUsdAmount(_ micros: Int64) -> String {
        String(format: "%.2f", Double(micros) / 1_000_000.0)
    }
}
