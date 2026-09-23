import MonacoCore
import SwiftUI

/// What the account balance row shows.
///
/// The store keeps the balance as an optional plus a loading flag, and a read that fails
/// leaves it empty with nothing loading. That is not zero money: rendering it as $0.00 tells a
/// funded member their account is empty, right above "Cash out" (#277, #324).
enum HomeBalanceDisplay: Equatable {
    case loading
    case amount(Int64)
    case unavailable

    static func resolve(balance: PlatformBalanceDTO?, isLoading: Bool) -> HomeBalanceDisplay {
        if let balance { return .amount(balance.availableUsdcMicros) }
        return isLoading ? .loading : .unavailable
    }
}

/// The cash fold: the bottom third of Home's ink slab, under a 1pt hairline.
///
/// Cash that is not in a cabal yet, and the two ways to move it — Add money (the USDC deposit
/// address for this account) and Cash out (withdraw idle balance to an external address). It used
/// to be a second white card stacked under the hero, which made the top of Home three rectangles
/// instead of one object.
///
/// Ink carries **one** saturated fill (§1.9). Add money is it — the product's own next step for a
/// member with an empty account. Cash out is `#FFFFFF`@0.12, which is an ink wash, not an accent:
/// it is plainly tappable without competing with the thing most people came here to do.
struct HomeBalanceFold: View {
    @ObservedObject var auth: DynamicAuthService
    let balance: PlatformBalanceDTO?
    let isBalanceLoading: Bool
    let joinedCabals: [HomeGroupBoardRowDTO]
    /// Whether the retry this row offers is already running. Retrying the balance is the whole
    /// Home refresh, so without this the member can stack three of them by tapping.
    var isRetryingBalance = false
    var onRetryBalance: () -> Void = {}

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var display: HomeBalanceDisplay {
        HomeBalanceDisplay.resolve(balance: balance, isLoading: isBalanceLoading)
    }

    var body: some View {
        if dynamicTypeSize.isAccessibilitySize {
            // At AX sizes the figure and two capsules cannot share a line without one of them
            // shrinking into unreadability, so the fold stacks instead of scaling.
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                cash
                actions
            }
        } else {
            HStack(alignment: .center, spacing: MonacoTheme.Space.m) {
                cash
                Spacer(minLength: MonacoTheme.Space.s)
                actions
            }
        }
    }

    private var cash: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text("Cash")
                .displayFont(.eyebrow)
                .foregroundStyle(MonacoTheme.Ink.fgSubtle)
            switch display {
            case .amount(let micros):
                MoneyText(micros: micros, style: .row, color: MonacoTheme.Ink.fgPrimary)
                    .accessibilityIdentifier("platform-balance-value")
            case .loading:
                ProgressView()
                    .tint(MonacoTheme.Ink.fgPrimary)
                    .accessibilityIdentifier("platform-balance-loading")
            case .unavailable:
                unavailableBalance
            }
        }
        .accessibilityElement(children: .contain)
    }

    private var actions: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            NavigationLink {
                DepositView(auth: auth, joinedCabals: joinedCabals)
            } label: {
                InkCapsuleLabel(title: "Add money", isProminent: true)
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("home-add-money-link")

            NavigationLink {
                WithdrawView(auth: auth)
            } label: {
                InkCapsuleLabel(title: "Cash out", isProminent: false)
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("home-cash-out-link")
        }
    }

    /// A dash, not a figure — and its own identifier, so nothing (a UI test included) can read
    /// a failed balance as a real one.
    private var unavailableBalance: some View {
        HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
            Text("—")
                .moneyFont(.row)
                .foregroundStyle(MonacoTheme.Ink.fgMuted)
                .accessibilityLabel("Account balance unavailable")
                .accessibilityIdentifier("platform-balance-unavailable")
            Button("Try again", action: onRetryBalance)
                .font(MonacoTheme.Typo.callout.weight(.semibold))
                .foregroundStyle(isRetryingBalance ? MonacoTheme.Ink.fgSubtle : MonacoTheme.Ink.accent)
                .buttonStyle(.plain)
                .frame(minHeight: 44)
                .disabled(isRetryingBalance)
                .accessibilityIdentifier("home-balance-retry")
        }
    }
}

/// A capsule action on ink. `isProminent` is the one brand fill the slab is allowed; everything
/// else is an ink wash with a white label.
private struct InkCapsuleLabel: View {
    let title: String
    let isProminent: Bool

    var body: some View {
        Text(title)
            .font(MonacoTheme.Typo.callout.weight(.semibold))
            .lineLimit(1)
            .minimumScaleFactor(0.8)
            .foregroundStyle(isProminent ? MonacoTheme.onBrand : MonacoTheme.Ink.fgPrimary)
            .padding(.horizontal, MonacoTheme.Space.m)
            .frame(minHeight: 44)
            .background(
                Capsule().fill(isProminent ? MonacoTheme.brandFill : Color.white.opacity(0.12))
            )
            .contentShape(Capsule())
    }
}
