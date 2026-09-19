import SwiftUI
import UIKit
import MonacoCore

struct PotSectionView: View {
    let potTotalUsd: String
    let pot: [PotRowDTO]
    let treasuryAddress: String?

    @State private var didCopyTreasury = false

    var body: some View {
        Section {
            if pot.isEmpty {
                Text("No holdings yet. Fund this cabal to get started.")
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.secondaryText)
            } else {
                ForEach(pot) { row in
                    VStack(alignment: .leading, spacing: 4) {
                        HStack {
                            Text(AssetSymbolFormatter.format(row.symbol))
                                .font(.body.bold())
                                .foregroundStyle(MonacoTheme.primaryText)
                            if row.afterHours == true {
                                Text("After hours")
                                    .font(.caption2.bold())
                                    .padding(.horizontal, 6)
                                    .padding(.vertical, 2)
                                    .background(MonacoTheme.warning.opacity(0.15))
                                    .foregroundStyle(MonacoTheme.warning)
                                    .clipShape(Capsule())
                                    .accessibilityIdentifier("pot-after-hours-\(row.symbol)")
                            }
                            Spacer()
                            VStack(alignment: .trailing, spacing: 2) {
                                Text("$\(row.valueUsd)")
                                    .font(.body.monospacedDigit())
                                    .foregroundStyle(MonacoTheme.primaryText)
                                Text(row.dollarPnl)
                                    .font(.caption.monospacedDigit())
                                    .foregroundStyle(pnlColor(for: row.dollarPnl))
                                    .accessibilityIdentifier("pot-row-pnl-\(row.symbol)")
                            }
                        }
                        HStack {
                            Text("\(row.units) units @ $\(row.markUsd)")
                                .font(.caption)
                                .foregroundStyle(MonacoTheme.secondaryText)
                            Spacer()
                        }
                    }
                    .accessibilityIdentifier("pot-row-\(row.symbol)")
                }
            }

            if let treasuryAddress {
                treasuryAddressBlock(treasuryAddress)
            }
        } header: {
            HStack {
                Text("Pot")
                Spacer()
                if !pot.isEmpty {
                    Text("$\(potTotalUsd)")
                        .font(.subheadline.bold().monospacedDigit())
                        .foregroundStyle(MonacoTheme.primaryText)
                        .accessibilityIdentifier("pot-total-value")
                }
            }
        }
    }

    @ViewBuilder
    private func treasuryAddressBlock(_ address: String) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Cabal treasury")
                .font(.caption)
                .foregroundStyle(MonacoTheme.secondaryText)

            MonacoWalletAddressText(address: address, font: .footnote.monospaced())
                .accessibilityIdentifier("group-treasury-address-value")
                .onTapGesture {
                    copyTreasuryAddress(address)
                }

            HStack {
                Button {
                    copyTreasuryAddress(address)
                } label: {
                    Label(
                        didCopyTreasury ? "Copied" : "Copy address",
                        systemImage: didCopyTreasury ? "checkmark" : "doc.on.doc"
                    )
                }
                .buttonStyle(.monacoSecondary)
                .accessibilityIdentifier("group-treasury-copy-button")

                if didCopyTreasury {
                    Text("Copied")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(MonacoTheme.success)
                        .accessibilityIdentifier("group-treasury-copied-feedback")
                }
            }
        }
        .padding(.top, 4)
        .accessibilityIdentifier("group-treasury-address-block")
    }

    private func copyTreasuryAddress(_ address: String) {
        UIPasteboard.general.string = address
        didCopyTreasury = true
        Task {
            try? await Task.sleep(for: .seconds(2))
            didCopyTreasury = false
        }
    }

    private func pnlColor(for dollarPnl: String) -> Color {
        if dollarPnl.hasPrefix("-") {
            return MonacoTheme.warning
        }
        if dollarPnl.hasPrefix("+") && dollarPnl != "+0.00" {
            return MonacoTheme.success
        }
        return MonacoTheme.secondaryText
    }
}
