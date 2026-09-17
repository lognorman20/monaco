import SwiftUI
import UIKit

struct PotSectionView: View {
    let pot: [PotRowDTO]
    let treasuryAddress: String?

    @State private var didCopyTreasury = false

    var body: some View {
        Section("Pot") {
            if pot.isEmpty {
                Text("No holdings yet. Add money to get started.")
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.secondaryText)
            } else {
                ForEach(pot) { row in
                    VStack(alignment: .leading, spacing: 4) {
                        HStack {
                            Text(row.symbol)
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
                            Text("$\(row.valueUsd)")
                                .font(.body.monospacedDigit())
                                .foregroundStyle(MonacoTheme.primaryText)
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
        }
    }

    @ViewBuilder
    private func treasuryAddressBlock(_ address: String) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Club treasury")
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
}
