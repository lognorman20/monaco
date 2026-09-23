import MonacoCore
import SwiftUI

/// Top of the group screen: identity, the pot, how it's doing, and your slice of it.
///
/// Ink in both schemes, because this is where the cabal's money is held — with one flat wash of
/// the cabal's own colour over it and a 2pt rule under the name. Back out, open a different
/// cabal, and the room is a different colour. The tint never reaches a control on this screen:
/// blue still means tap.
struct GroupHeroSection: View {
    let view: GroupViewDTO

    /// Set at the root of the cabal screen from the viewer's resolved tints; nil for a cabal
    /// being previewed outside that set, where the mark hashes the id itself.
    @Environment(\.cabalTint) private var cabalTint

    private var tint: MonacoTheme.CabalTint {
        cabalTint ?? .forGroupId(view.id)
    }

    /// At an accessibility text size an eyebrow is 30pt of tracked uppercase, and two of them
    /// side by side break mid-word. Every side-by-side pair in the hero stacks instead.
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var stacksPairs: Bool { dynamicTypeSize.isAccessibilitySize }

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            VStack(alignment: .leading, spacing: 12) {
                HStack(alignment: .center, spacing: MonacoTheme.Space.sm) {
                    CabalMark(tint: tint, name: view.name, size: 36, onInk: true)
                    Text(view.name)
                        .displayFont(.title)
                        .foregroundStyle(MonacoTheme.Ink.fgPrimary)
                        .lineLimit(2)
                        .minimumScaleFactor(0.75)
                        .accessibilityAddTraits(.isHeader)
                }
                // The accent rule. Short and deliberate: it is a signature, not a divider.
                Capsule()
                    .fill(tint.onInk)
                    .frame(width: 44, height: 2)
                    .accessibilityHidden(true)
                memberLine
                    .accessibilityElement(children: .combine)
            }

            VStack(alignment: .leading, spacing: 6) {
                Text("In the pot")
                    .displayFont(.eyebrow)
                    .foregroundStyle(MonacoTheme.Ink.fgSubtle)
                    .fixedSize(horizontal: false, vertical: true)
                MoneyText(decimalString: view.resolvedPotTotalUsd, style: .hero, color: MonacoTheme.Ink.fgPrimary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
                    .dynamicTypeSize(...DynamicTypeSize.accessibility2)
                    .accessibilityIdentifier("pot-total-value")
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 8) {
                        PnLBadge(dollarPnl: GroupHeroMath.potDollarPnl(view.pot), percentReturn: nil, onInk: true)
                        Text("all time")
                            .font(MonacoTheme.Typo.caption)
                            .foregroundStyle(MonacoTheme.Ink.fgMuted)
                    }
                    VStack(alignment: .leading, spacing: 6) {
                        PnLBadge(dollarPnl: GroupHeroMath.potDollarPnl(view.pot), percentReturn: nil, onInk: true)
                        Text("all time")
                            .font(MonacoTheme.Typo.caption)
                            .foregroundStyle(MonacoTheme.Ink.fgMuted)
                    }
                }
            }
            .accessibilityElement(children: .combine)

            Rectangle()
                .fill(MonacoTheme.Ink.line)
                .frame(height: 1)

            sliceRow
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("group-hero-slice")
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityIdentifier("group-hero")
    }

    @ViewBuilder
    private var memberLine: some View {
        let caption = Text(view.members.count == 1 ? "1 member" : "\(view.members.count) members")
            .font(MonacoTheme.Typo.caption)
            .foregroundStyle(MonacoTheme.Ink.fgMuted)
        if stacksPairs {
            VStack(alignment: .leading, spacing: 6) {
                GroupMemberAvatarStack(members: view.members, ringColor: MonacoTheme.Ink.base)
                caption.fixedSize(horizontal: false, vertical: true)
            }
        } else {
            HStack(spacing: 8) {
                GroupMemberAvatarStack(members: view.members, ringColor: MonacoTheme.Ink.base)
                caption
            }
        }
    }

    @ViewBuilder
    private var sliceRow: some View {
        let mine = VStack(alignment: .leading, spacing: 2) {
            Text("Your slice")
                .displayFont(.eyebrow)
                .foregroundStyle(MonacoTheme.Ink.fgSubtle)
                .fixedSize(horizontal: false, vertical: true)
            MoneyText(decimalString: view.you.equityUsd, style: .row, color: MonacoTheme.Ink.fgPrimary)
        }
        let share = VStack(alignment: stacksPairs ? .leading : .trailing, spacing: 2) {
            Text(GroupHeroMath.sliceCaption(view.you))
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.Ink.fgMuted)
                .lineLimit(stacksPairs ? nil : 1)
                .minimumScaleFactor(0.8)
                .fixedSize(horizontal: false, vertical: true)
            if GroupHeroMath.hasSlice(view.you) {
                PnLText(dollarPnl: view.you.dollarPnl, style: .caption, onInk: true)
            }
        }
        if stacksPairs {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) { mine; share }
                .frame(maxWidth: .infinity, alignment: .leading)
        } else {
            HStack(alignment: .lastTextBaseline, spacing: 8) {
                mine
                Spacer(minLength: 8)
                share
            }
        }
    }
}

/// The ink surface the hero, the action row and the chat bar all sit on, washed in the cabal's
/// own colour.
///
/// One object, not three stacked rectangles: identity and the pot, then the thing a cabal is for,
/// then the room's conversation. Tap a different cabal and the whole thing is a different colour.
/// The hero band's measurements, in a non-generic namespace so they have somewhere to live and
/// somewhere to be quoted from — `GroupHeroBand` is generic over its content and cannot hold a
/// stored static.
enum GroupHeroBandMetrics {
    /// The cabal's wash over ink.
    ///
    /// `CabalTint.soft` is a *paper* wash — the deep `fill` at 12% — and over `#0B1220` it is not
    /// there at all, which is why the band reaches for the `onInk` pair instead, exactly as
    /// `brandWashOnInk` is the light blue rather than `brand`. The opacity is named here rather
    /// than written inline so there is one number with one reason: at 10% the worst case in the
    /// ramp (olive `#B9C93A`) lifts the band far enough that `Ink.fgSubtle` — white@0.48, 4.98:1
    /// on bare ink per §1.5 — still measures ~4.7:1 on the washed band, so the 11pt eyebrows hold
    /// AA. Anything heavier is a fill, and the hero's one saturated fill is the Propose capsule.
    ///
    /// This belongs in the token table as `CabalTint.softOnInk` with a contrast row of its own.
    /// That is a Chunk A edit to `MonacoTheme.swift`, which this chunk may not make; it is filed
    /// as the follow-up and named here so the value is not a bare literal in the meantime.
    static let washOpacity: Double = 0.10
}

struct GroupHeroBand<Content: View>: View {
    let tint: MonacoTheme.CabalTint
    @ViewBuilder let content: Content

    private var shape: AnyShape {
        AnyShape(RoundedRectangle(cornerRadius: MonacoTheme.Radius.object, style: .continuous))
    }

    var body: some View {
        content
            .monacoWorld(.ink)
            .padding(MonacoTheme.Space.l)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background {
                InkSurface(shape: shape)
                    .overlay { shape.fill(tint.onInk.opacity(GroupHeroBandMetrics.washOpacity)) }
            }
    }
}

/// Up to four member avatars, then "+N". Each is a full circle ringed in the hero tint; the
/// overlap stays under a fifth of the diameter so every avatar's initials stay fully visible.
struct GroupMemberAvatarStack: View {
    let members: [LeaderboardRowDTO]
    var size: CGFloat = 32
    var visibleLimit = 4
    var ringColor: Color = MonacoTheme.bgRaised

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
                        .foregroundStyle(discLabel)
                }
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(members.count == 1 ? "1 member" : "\(members.count) members")
    }

    /// Five solid-white discs on a deep ink band is more white than anything else on the screen,
    /// and the members are not the loudest thing on a cabal's hero — the pot is. On ink the disc
    /// is a raised ink card; on paper it stays the surface it always was.
    @Environment(\.monacoWorld) private var world

    /// `Ink.raised`, not `Ink.lineStrong`: §1.5 gives `lineStrong` one job, the *edge* of a raised
    /// card on ink, and a filled object inside ink is `Ink.raised`. The initials keep ~16.8:1
    /// either way, so this is a token doing the job it is defined for rather than a legibility
    /// fix — and against the hero's tint wash the raised fill reads as a disc that belongs to the
    /// band instead of a white chip sitting on top of it.
    private var discFill: Color {
        world == .ink ? MonacoTheme.Ink.raised : MonacoTheme.bgRaised
    }

    private var discLabel: Color {
        world == .ink ? MonacoTheme.Ink.fgPrimary : MonacoTheme.fgPrimary
    }

    @ViewBuilder
    private func avatar(_ member: LeaderboardRowDTO) -> some View {
        let photo = member.profilePhotoUrl?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if photo.isEmpty {
            bubble {
                Text(CabalMark.initials(for: member.displayName))
                    .font(.system(size: size * 0.36, weight: .semibold))
                    .foregroundStyle(discLabel)
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
            .fill(discFill)
            // The ring punches the surface behind the stack out between faces, so overlapping
            // discs read as separate people rather than one welded shape.
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
