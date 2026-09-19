import SwiftUI

struct PlatformBalanceCard: View {
    let balance: PlatformBalanceDTO?

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
            } else {
                Text("—")
                    .font(.title2.bold())
                    .foregroundStyle(MonacoTheme.secondaryText)
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
