import MonacoCore
import SwiftUI

struct YouSectionView: View {
    let slice: MemberSliceDTO

    var body: some View {
        Section("You") {
            metricRow(title: "Your slice", value: "$\(slice.equityUsd)")
            metricRow(title: "Slice %", value: SlicePercentFormatter.format(slice.slicePercent))
            metricRow(title: "P&L", value: slice.dollarPnl)
            metricRow(title: "Return", value: PercentReturnFormatter.format(slice.percentReturn))
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
}
