import MonacoCore
import SwiftUI

/// The account balance as a line in the ledger: the cash coin, the label, the figure, and —
/// while a fund is on its way into a cabal — how much of it is, under the label.
///
/// It used to be a white card with the figure at 28pt, which made the balance the loudest
/// thing on a money screen whose job is somewhere else (an address to copy, an amount to
/// type). It is the row Home draws (`HomeBalanceRowSection`) now, so the balance reads the
/// same wherever it appears. The type keeps the file's name; it is no longer a card.
///
/// Put it inside a `MonacoGroupedList`, which draws the rules.
struct PlatformBalanceCard: View {
    let display: HomeBalanceDisplay
    var pendingAllocationMicros: Int64 = 0
    /// The figure's identifier. Each screen names its own, so a test can tell them apart.
    var valueIdentifier = "platform-balance-value"

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    init(display: HomeBalanceDisplay, pendingAllocationMicros: Int64 = 0, valueIdentifier: String = "platform-balance-value") {
        self.display = display
        self.pendingAllocationMicros = pendingAllocationMicros
        self.valueIdentifier = valueIdentifier
    }

    /// A balance the screen holds itself. `nil` is loading while `isLoading`, unavailable after,
    /// never $0.00 — see `HomeBalanceDisplay`.
    init(balance: PlatformBalanceDTO?, isLoading: Bool = false, valueIdentifier: String = "platform-balance-value") {
        self.init(
            display: .resolve(balance: balance, isLoading: isLoading),
            pendingAllocationMicros: balance?.pendingAllocationMicros ?? 0,
            valueIdentifier: valueIdentifier
        )
    }

    /// "$50.00 funding a cabal", or nil when nothing is on its way.
    static func pendingLine(micros: Int64) -> String? {
        guard micros > 0 else { return nil }
        return "\(UsdAmountFormatter.format(micros: micros)) funding a cabal"
    }

    /// At the accessibility sizes the figure drops under the label, the way `MonacoRow` stacks,
    /// so neither the label nor the money is cut.
    private var isStacked: Bool { dynamicTypeSize.isAccessibilitySize }

    var body: some View {
        Group {
            if isStacked {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    HStack(spacing: MonacoTheme.Space.sm) {
                        coin
                        labels
                    }
                    figure
                }
            } else {
                HStack(spacing: MonacoTheme.Space.sm) {
                    coin
                    labels
                    figure
                        .layoutPriority(1)
                }
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, MonacoTheme.Space.s)
        .frame(minHeight: 60)
        .accessibilityElement(children: .combine)
    }

    private var coin: some View {
        StockMark(symbol: "USDC", size: 40)
            .frame(width: 44, height: 44)
    }

    private var labels: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text("Account balance")
                .font(MonacoTheme.Typo.rowTitle)
                .foregroundStyle(MonacoTheme.ink)
            if let pending = Self.pendingLine(micros: pendingAllocationMicros) {
                Text(pending)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private var figure: some View {
        switch display {
        case .amount(let micros):
            MoneyText(micros: micros, style: .row)
                .accessibilityIdentifier(valueIdentifier)
        case .loading:
            ProgressView()
                .tint(MonacoTheme.accent)
                .accessibilityIdentifier("platform-balance-loading")
        case .unavailable:
            // A dash, not a figure: a balance that could not be read is not an empty account.
            Text("—")
                .moneyFont(.row)
                .foregroundStyle(MonacoTheme.muted)
                .accessibilityLabel("Account balance unavailable")
                .accessibilityIdentifier("platform-balance-unavailable")
        }
    }
}
