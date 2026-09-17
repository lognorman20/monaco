import SwiftUI

struct YouSectionView: View {
    let slice: MemberSliceDTO

    var body: some View {
        Section("You") {
            metricRow(title: "Your slice", value: "$\(slice.equityUsd)")
            metricRow(title: "Slice %", value: formatPercent(slice.slicePercent))
            metricRow(title: "P&L", value: slice.dollarPnl)
            if let percentReturn = slice.percentReturn {
                metricRow(title: "Return", value: formatPercent(percentReturn))
            } else {
                metricRow(title: "Return", value: "—")
            }
        }
    }

    private func metricRow(title: String, value: String) -> some View {
        HStack {
            Text(title)
                .foregroundStyle(MonacoTheme.secondaryText)
            Spacer()
            Text(value)
                .font(.body.monospacedDigit())
                .foregroundStyle(MonacoTheme.primaryText)
        }
    }

    private func formatPercent(_ raw: String) -> String {
        guard let decimal = Double(raw) else { return raw }
        return String(format: "%.1f%%", decimal * 100)
    }
}
