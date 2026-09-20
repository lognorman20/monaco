import MonacoCore
import SwiftUI

/// Top of the group screen: identity, the pot, how it's doing, and your slice of it.
/// A deep ink money card in both schemes; the cabal's tint is the mark and the accent
/// rule, never a full-bleed wash.
struct GroupHeroSection: View {
    let view: GroupViewDTO

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            VStack(alignment: .leading, spacing: 12) {
                HStack(alignment: .center, spacing: MonacoTheme.Space.sm) {
                    CabalMark(groupId: view.id, name: view.name, size: 36, onInk: true)
                    Text(view.name)
                        .font(MonacoTheme.Typo.title)
                        .foregroundStyle(MonacoTheme.onHero)
                        .lineLimit(2)
                        .minimumScaleFactor(0.75)
                        .accessibilityAddTraits(.isHeader)
                }
                HStack(spacing: 8) {
                    GroupMemberAvatarStack(members: view.members, ringColor: MonacoTheme.heroInk)
                    Text(view.members.count == 1 ? "1 member" : "\(view.members.count) members")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.onHeroMuted)
                }
                .accessibilityElement(children: .combine)
            }

            VStack(alignment: .leading, spacing: 6) {
                Text("In the pot")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.onHeroMuted)
                MoneyText(decimalString: view.resolvedPotTotalUsd, style: .hero, color: MonacoTheme.onHero)
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
                    .dynamicTypeSize(...DynamicTypeSize.accessibility2)
                    .accessibilityIdentifier("pot-total-value")
                HStack(spacing: 8) {
                    PnLBadge(dollarPnl: GroupHeroMath.potDollarPnl(view.pot), percentReturn: nil, onInk: true)
                    Text("all time")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.onHeroMuted)
                }
            }
            .accessibilityElement(children: .combine)

            Rectangle()
                .fill(MonacoTheme.onHeroHairline)
                .frame(height: 1)

            HStack(alignment: .lastTextBaseline, spacing: 8) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Your slice")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.onHeroMuted)
                    MoneyText(decimalString: view.you.equityUsd, style: .row, color: MonacoTheme.onHero)
                }
                Spacer(minLength: 8)
                VStack(alignment: .trailing, spacing: 2) {
                    Text(GroupHeroMath.sliceCaption(view.you))
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.onHeroMuted)
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                    if GroupHeroMath.hasSlice(view.you) {
                        PnLText(dollarPnl: view.you.dollarPnl, style: .caption, onInk: true)
                    }
                }
            }
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("group-hero-slice")
        }
        .monacoHeroCard()
        .accessibilityIdentifier("group-hero")
    }
}

/// Up to four member avatars, then "+N". Each is a full circle ringed in the hero tint; the
/// overlap stays under a fifth of the diameter so every avatar's initials stay fully visible.
struct GroupMemberAvatarStack: View {
    let members: [LeaderboardRowDTO]
    var size: CGFloat = 32
    var visibleLimit = 4
    var ringColor: Color = MonacoTheme.surface

    private var overlap: CGFloat { (size * 0.19).rounded() }

    var body: some View {
        let visible = Array(members.prefix(visibleLimit))
        let overflow = members.count - visible.count
        HStack(spacing: -overlap) {
            ForEach(visible) { member in
                avatar(member)
            }
            if overflow > 0 {
                bubble {
                    Text("+\(overflow)")
                        .font(.system(size: size * 0.36, weight: .semibold).monospacedDigit())
                        .foregroundStyle(MonacoTheme.heroInk)
                }
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(members.count == 1 ? "1 member" : "\(members.count) members")
    }

    @ViewBuilder
    private func avatar(_ member: LeaderboardRowDTO) -> some View {
        let photo = member.profilePhotoUrl?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if photo.isEmpty {
            bubble {
                Text(CabalMark.initials(for: member.displayName))
                    .font(.system(size: size * 0.36, weight: .semibold))
                    .foregroundStyle(MonacoTheme.heroInk)
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
            }
        } else {
            MonacoAvatar(photoURL: photo, displayName: member.displayName, size: size)
                .overlay(Circle().strokeBorder(ringColor, lineWidth: 2))
        }
    }

    private func bubble<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        Circle()
            .fill(Color.white)
            .overlay(Circle().strokeBorder(ringColor, lineWidth: 2))
            .overlay(content().padding(.horizontal, 5))
            .frame(width: size, height: size)
    }
}

/// Figures the hero derives from the view DTO. Parses raw server strings, never formatted ones.
enum GroupHeroMath {
    /// Pot P&L = sum of every holding's dollar P&L, as a signed server-style string ("+50.58").
    static func potDollarPnl(_ pot: [PotRowDTO]) -> String {
        let total = pot.reduce(Decimal.zero) { $0 + (decimal(from: $1.dollarPnl) ?? .zero) }
        var value = total
        var rounded = Decimal()
        NSDecimalRound(&rounded, &value, 2, .plain)
        let magnitude = pnlFormatter.string(from: NSDecimalNumber(decimal: rounded < 0 ? -rounded : rounded)) ?? "0.00"
        return (rounded < 0 ? "-" : "+") + magnitude
    }

    /// Built once: the hero recomputes this on every body pass.
    private static let pnlFormatter: NumberFormatter = {
        let formatter = NumberFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.minimumFractionDigits = 2
        formatter.maximumFractionDigits = 2
        formatter.minimumIntegerDigits = 1
        return formatter
    }()

    static func hasSlice(_ slice: MemberSliceDTO) -> Bool {
        (Double(slice.slicePercent) ?? 0) > 0
    }

    /// "57% of the pot", or a nudge when the member hasn't put money in yet.
    static func sliceCaption(_ slice: MemberSliceDTO) -> String {
        guard let fraction = Double(slice.slicePercent), fraction > 0 else {
            return "Add money to get a slice"
        }
        let percent = fraction * 100
        let label = percent >= 10 || percent.rounded() == percent
            ? String(format: "%.0f%%", percent)
            : String(format: "%.1f%%", percent)
        return "\(label) of the pot"
    }

    static func decimal(from raw: String) -> Decimal? {
        var trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
            .replacingOccurrences(of: "\u{2212}", with: "-")
            .replacingOccurrences(of: "$", with: "")
            .replacingOccurrences(of: ",", with: "")
        if trimmed.hasPrefix("+") { trimmed.removeFirst() }
        return Decimal(string: trimmed, locale: Locale(identifier: "en_US_POSIX"))
    }
}
