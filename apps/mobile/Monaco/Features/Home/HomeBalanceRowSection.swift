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
            // shrinking into unreadability, so the fold stacks instead of scaling — and the two
            // capsules stack with it. Stacking the figure above a still-horizontal pair only
            // moved the problem: "Add money" and "Cash out" went on sharing one line and the
            // slab's one brand CTA truncated to "Add mone…".
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

    /// Side by side at normal sizes; stacked full-width at accessibility sizes, where two
    /// capsules on one line cannot hold their labels.
    @ViewBuilder
    private var actions: some View {
        let isStacked = dynamicTypeSize.isAccessibilitySize
        let layout = isStacked
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: MonacoTheme.Space.s))
            : AnyLayout(HStackLayout(spacing: MonacoTheme.Space.s))

        layout {
            NavigationLink {
                DepositView(auth: auth, joinedCabals: joinedCabals)
            } label: {
                InkCapsuleLabel(title: "Add money", isProminent: true, isStacked: isStacked)
            }
            .buttonStyle(InkPressStyle())
            .accessibilityIdentifier("home-add-money-link")

            NavigationLink {
                WithdrawView(auth: auth)
            } label: {
                InkCapsuleLabel(title: "Cash out", isProminent: false, isStacked: isStacked)
            }
            .buttonStyle(InkPressStyle())
            .accessibilityIdentifier("home-cash-out-link")
        }
        .frame(maxWidth: isStacked ? .infinity : nil, alignment: .leading)
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
///
/// `isStacked` is the accessibility-size layout: the capsule takes the full width of the fold and
/// the label is allowed to wrap, because at AX3 and up "Add money" does not fit on one line at a
/// size anybody set the text that large to read.
private struct InkCapsuleLabel: View {
    let title: String
    let isProminent: Bool
    var isStacked: Bool = false

    var body: some View {
        Text(title)
            .font(MonacoTheme.Typo.callout.weight(.semibold))
            .lineLimit(isStacked ? nil : 1)
            .minimumScaleFactor(isStacked ? 1 : 0.8)
            .multilineTextAlignment(.leading)
            .fixedSize(horizontal: false, vertical: isStacked)
            .foregroundStyle(isProminent ? MonacoTheme.onBrand : MonacoTheme.Ink.fgPrimary)
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.vertical, isStacked ? MonacoTheme.Space.s : 0)
            .frame(maxWidth: isStacked ? .infinity : nil, minHeight: 44, alignment: .leading)
            .background(
                Capsule().fill(isProminent ? MonacoTheme.brandFill : Color.white.opacity(0.12))
            )
            .contentShape(Capsule())
    }
}

/// §4 #1 on ink: press scales to 0.97 on `snap`, and drops to 0.80 opacity with no animation
/// under Reduce Motion. `MonacoPressEffect` in `Design/MonacoButtons.swift` is the same feel, but
/// it is `private` to that file; this folds into it the moment Chunk B exposes it.
struct InkPressStyle: ButtonStyle {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed && !reduceMotion ? 0.97 : 1)
            .opacity(configuration.isPressed && reduceMotion ? 0.8 : 1)
            .animation(MonacoMotion.snap.reduced(reduceMotion), value: configuration.isPressed)
    }
}
