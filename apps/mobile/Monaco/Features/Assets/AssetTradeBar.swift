import MonacoCore
import SwiftUI

/// The bar that stays under the thumb: propose a buy, and — only when one of the
/// member's own cabals holds the stock — propose a sell.
///
/// It lives in a `safeAreaInset`, not in the scroll view, so the action is reachable
/// without scrolling a screen that is now five cards long. It replaces the inline
/// action row, which sat below everything and offered a Sell that walked the member
/// into "This cabal does not hold this stock." A button that cannot work is worse
/// than no button, so Sell is not drawn until the holdings answer says it can.
///
/// What the bar offers is decided in `AssetTradeBarState` (MonacoCore, tested).
struct AssetTradeBar: View {
    let state: AssetTradeBarState
    let onBuy: () -> Void
    let onSell: () -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// Side by side normally; stacked once the labels stop fitting on one line.
    private var isStacked: Bool { dynamicTypeSize >= .accessibility1 }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            if let reason = state.buyDisabledReason {
                // The reason lives here, next to the disabled button, rather than in
                // a toast after a tap that was never going to work.
                Label(reason, systemImage: "exclamationmark.triangle")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.warning)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("asset-trade-bar-blocked")
            }
            buttons
            if let caption = state.caption {
                Text(caption)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .frame(maxWidth: .infinity, alignment: .center)
                    .accessibilityIdentifier("asset-trade-bar-caption")
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.top, MonacoTheme.Space.sm)
        .padding(.bottom, MonacoTheme.Space.s)
        // A material, so the cards scrolling under the bar stay legible as they pass
        // behind it instead of colliding with a flat fill.
        .background(.regularMaterial)
        .overlay(alignment: .top) {
            Rectangle()
                .fill(MonacoTheme.hairline)
                .frame(height: 1)
        }
        // No identifier on this stack. A modifier on a `VStack` is applied to each of
        // its children, so naming the bar would rename both buttons and the blocked
        // notice, and a test asking for "asset-detail-sell" would find nothing.
    }

    @ViewBuilder
    private var buttons: some View {
        if isStacked {
            VStack(spacing: MonacoTheme.Space.s) {
                buyButton
                sellButton
            }
        } else {
            HStack(spacing: MonacoTheme.Space.s) {
                buyButton
                sellButton
            }
        }
    }

    private var buyButton: some View {
        Button(state.buyTitle, action: onBuy)
            .buttonStyle(.monacoPrimary)
            .disabled(!state.canBuy)
            .accessibilityIdentifier("asset-detail-buy")
    }

    @ViewBuilder
    private var sellButton: some View {
        if case .available(let caption) = state.sell {
            Button(action: onSell) {
                VStack(spacing: 1) {
                    Text("Propose sell")
                    if let caption, !isStacked {
                        // Which cabal can sell it, so the member is not sent to a
                        // picker to find out.
                        Text(caption)
                            .font(MonacoTheme.Typo.micro)
                            .foregroundStyle(MonacoTheme.muted)
                            .lineLimit(1)
                    }
                }
            }
            .buttonStyle(.monacoSecondary)
            .accessibilityLabel(sellAccessibilityLabel)
            .accessibilityIdentifier("asset-detail-sell")
            // The button arrives once the holdings answer does. Fading it in reads as
            // an answer landing; appearing instantly reads as a layout jump.
            .transition(.opacity)
        }
    }

    private var sellAccessibilityLabel: String {
        guard case .available(let caption) = state.sell, let caption else { return "Propose sell" }
        return "Propose sell, held by \(caption)"
    }
}
