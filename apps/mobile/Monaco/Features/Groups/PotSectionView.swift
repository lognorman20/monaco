import SwiftUI
import UIKit
import MonacoCore

struct PotSectionView: View {
    let potTotalUsd: String
    let pot: [PotRowDTO]
    let treasuryAddress: String?

    @State private var didCopyTreasury = false
    @State private var isTreasuryExpanded = false

    private var sortedPot: [PotRowDTO] {
        pot.sorted { lhs, rhs in
            (Double(lhs.valueUsd) ?? 0) > (Double(rhs.valueUsd) ?? 0)
        }
    }

    var body: some View {
        Section {
            MonacoHeroHeader(
                title: UsdAmountFormatter.format(decimalString: potTotalUsd),
                caption: "Total pot"
            )
            .accessibilityIdentifier("pot-total-value")
            .listRowInsets(EdgeInsets(top: 12, leading: 16, bottom: 4, trailing: 16))
            .listRowBackground(Color.clear)
            .listRowSeparator(.hidden)

            if pot.isEmpty {
                Text("No holdings yet. Fund this cabal to get started.")
                    .font(MonacoTheme.TypeRole.body)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .listRowInsets(EdgeInsets(top: 4, leading: 16, bottom: 12, trailing: 16))
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
            } else {
                ForEach(sortedPot) { row in
                    holdingCard(row)
                        .listRowInsets(EdgeInsets(top: 6, leading: 16, bottom: 6, trailing: 16))
                        .listRowBackground(Color.clear)
                        .listRowSeparator(.hidden)
                }
            }

            if let treasuryAddress {
                treasuryAddressBlock(treasuryAddress)
                    .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 12, trailing: 16))
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
            }
        }
    }

    @ViewBuilder
    private func holdingCard(_ row: PotRowDTO) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
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

            MonacoRowCard(
                systemImage: "chart.line.uptrend.xyaxis",
                title: AssetSymbolFormatter.format(row.symbol),
                subtitle: "\(row.units) units @ $\(row.markUsd)",
                trailing: UsdAmountFormatter.format(decimalString: row.valueUsd),
                trailingCaption: row.dollarPnl,
                trailingCaptionColor: MonacoTheme.signed(row.dollarPnl),
                trailingCaptionAccessibilityIdentifier: "pot-row-pnl-\(row.symbol)"
            )
        }
        .accessibilityIdentifier("pot-row-\(row.symbol)")
    }

    @ViewBuilder
    private func treasuryAddressBlock(_ address: String) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Button {
                withAnimation(.easeInOut(duration: 0.2)) {
                    isTreasuryExpanded.toggle()
                }
            } label: {
                HStack {
                    Text("Cabal treasury")
                        .font(MonacoTheme.TypeRole.caption)
                        .foregroundStyle(MonacoTheme.secondaryText)
                    Spacer()
                    Image(systemName: isTreasuryExpanded ? "chevron.up" : "chevron.down")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
            }
            .buttonStyle(.plain)

            if isTreasuryExpanded {
                MonacoWalletAddressText(address: address, textStyle: .footnote)
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
        }
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
