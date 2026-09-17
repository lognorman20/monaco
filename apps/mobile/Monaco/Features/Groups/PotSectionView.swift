import SwiftUI

struct PotSectionView: View {
    let pot: [PotRowDTO]

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
        }
    }
}
