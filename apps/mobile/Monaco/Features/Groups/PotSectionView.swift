import SwiftUI

struct PotSectionView: View {
    let pot: [PotRowDTO]

    var body: some View {
        Section("Pot") {
            if pot.isEmpty {
                Text("No holdings yet. Add money to get started.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            } else {
                ForEach(pot) { row in
                    VStack(alignment: .leading, spacing: 4) {
                        HStack {
                            Text(row.symbol)
                                .font(.body.bold())
                            if row.afterHours == true {
                                Text("After hours")
                                    .font(.caption2.bold())
                                    .padding(.horizontal, 6)
                                    .padding(.vertical, 2)
                                    .background(Color.orange.opacity(0.15))
                                    .foregroundStyle(.orange)
                                    .clipShape(Capsule())
                                    .accessibilityIdentifier("pot-after-hours-\(row.symbol)")
                            }
                            Spacer()
                            Text("$\(row.valueUsd)")
                                .font(.body.monospacedDigit())
                        }
                        HStack {
                            Text("\(row.units) units @ $\(row.markUsd)")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                            Spacer()
                        }
                    }
                    .accessibilityIdentifier("pot-row-\(row.symbol)")
                }
            }
        }
    }
}
