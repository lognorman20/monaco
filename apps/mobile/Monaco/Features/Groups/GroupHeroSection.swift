import Charts
import MonacoCore
import SwiftUI

/// The top of the cabal screen: who this is, the pot, how it has been doing, and your slice
/// of it — one deep ink band across the whole width, in both schemes.
///
/// This is the one high-impact surface in the app, and it is the cabal's. The pot's curve
/// runs edge to edge inside it with the range under it; the cabal's tint is the rule along
/// the band's bottom edge and the mark at its top, never a full-bleed wash — five cabals in
/// five colours of ink would stop being one app.
struct GroupHeroSection: View {
    let view: GroupViewDTO
    /// The pot's curve for `range`, owned by the screen.
    var chart: GroupHeroChart = .loading
    var range: GroupPnLRange = .oneMonth
    var onRange: (GroupPnLRange) -> Void = { _ in }
    /// Set, replace and remove the cabal picture. Nil on surfaces that only show
    /// the hero (the sample harness's read-only states), which then get a plain mark.
    var pictureEditor: CabalPictureEditor?
    var onPictureResult: (MonacoToast) -> Void = { _ in }

    private static let chartHeight: CGFloat = 76

    private var tint: MonacoTheme.CabalTint { .forGroupId(view.id) }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
            identity
            pot
            curve
            MonacoRule(color: MonacoTheme.onHeroHairline)
            slice
        }
        .padding(.horizontal, MonacoTheme.Space.gutter)
        .padding(.top, MonacoTheme.Space.sm)
        .padding(.bottom, MonacoTheme.Space.l)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(MonacoTheme.heroInk)
        // The cabal's own colour, as the band's edge: enough to be this cabal's, not enough to
        // be a second canvas.
        .overlay(alignment: .bottom) {
            Rectangle()
                .fill(tint.onInk)
                .frame(height: 3)
                .accessibilityHidden(true)
        }
        .accessibilityIdentifier("group-hero")
    }

    // MARK: - Who

    private var identity: some View {
        HStack(alignment: .center, spacing: MonacoTheme.Space.sm) {
            if let pictureEditor {
                CabalPicturePicker(
                    groupId: view.id,
                    name: view.name,
                    canEdit: view.viewerIsCreator,
                    size: 48,
                    onInk: true,
                    onResult: onPictureResult,
                    editor: pictureEditor
                )
            } else {
                CabalMark(
                    groupId: view.id,
                    name: view.name,
                    size: 48,
                    onInk: true,
                    pictureUrl: view.pictureUrl,
                    accessibilityLabel: view.pictureUrl == nil ? nil : "\(view.name) picture"
                )
            }
            VStack(alignment: .leading, spacing: 4) {
                Text(view.name)
                    .font(MonacoTheme.Typo.title)
                    .foregroundStyle(MonacoTheme.onHero)
                    .lineLimit(2)
                    .minimumScaleFactor(0.75)
                    .accessibilityAddTraits(.isHeader)
                HStack(spacing: MonacoTheme.Space.s) {
                    GroupMemberAvatarStack(members: view.members, size: 22, ringColor: MonacoTheme.heroInk)
                    Text(view.members.count == 1 ? "1 member" : "\(view.members.count) members")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.onHeroMuted)
                }
                .accessibilityElement(children: .combine)
            }
        }
    }

    // MARK: - The pot

    private var pot: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("In the pot")
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.onHeroMuted)
            MoneyText(decimalString: view.resolvedPotTotalUsd, style: .hero, color: MonacoTheme.onHero)
                .lineLimit(1)
                .minimumScaleFactor(0.6)
                .dynamicTypeSize(...DynamicTypeSize.accessibility2)
                .accessibilityIdentifier("pot-total-value")
            HStack(spacing: MonacoTheme.Space.s) {
                PnLBadge(dollarPnl: GroupHeroMath.potDollarPnl(view.pot), percentReturn: nil, onInk: true)
                Text("all time")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.onHeroMuted)
            }
        }
        .accessibilityElement(children: .combine)
    }

    // MARK: - The curve

    /// The pot's P&L over the chosen window, edge to edge, with the window chips under it.
    /// Held at one height in every state so a range switch never moves the slice below.
    private var curve: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            chartSlot
                .frame(height: Self.chartHeight)
                .padding(.horizontal, -MonacoTheme.Space.gutter)
            rangeChips
        }
    }

    @ViewBuilder
    private var chartSlot: some View {
        switch chart {
        case .curve(let points):
            GroupPnLCurve(points: points)
                .accessibilityIdentifier("group-hero-chart")
        case .loading:
            Color.clear
                .accessibilityHidden(true)
                .accessibilityIdentifier("group-hero-chart-loading")
        case .sparse:
            chartNote("Not enough history for \(range.spokenWindow) yet")
                .accessibilityIdentifier("group-hero-chart-sparse")
        case .failed:
            chartNote("Couldn't load the curve")
                .accessibilityIdentifier("group-hero-chart-failed")
        }
    }

    private func chartNote(_ text: String) -> some View {
        ZStack {
            MonacoRule(color: MonacoTheme.onHeroHairline)
            Text(text)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.onHeroMuted)
                .padding(.horizontal, MonacoTheme.Space.s)
                .background(MonacoTheme.heroInk)
        }
    }

    private var rangeChips: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            ForEach(GroupPnLRange.allCases, id: \.self) { option in
                let isSelected = option == range
                Button {
                    guard !isSelected else { return }
                    Haptics.selection()
                    onRange(option)
                } label: {
                    Text(option.label)
                        .font(MonacoTheme.Typo.dataCaption)
                        .foregroundStyle(isSelected ? MonacoTheme.heroInk : MonacoTheme.onHeroMuted)
                        .padding(.horizontal, 12)
                        .frame(minWidth: 44, minHeight: 30)
                        .background(Capsule().fill(isSelected ? MonacoTheme.onHero : Color.white.opacity(0.10)))
                        .padding(.vertical, 7)
                        .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(option.spokenWindow)
                .accessibilityAddTraits(isSelected ? [.isSelected] : [])
                .accessibilityIdentifier("group-hero-range-\(option.rawValue)")
            }
        }
    }

    // MARK: - Your slice

    private var slice: some View {
        HStack(alignment: .lastTextBaseline, spacing: MonacoTheme.Space.s) {
            VStack(alignment: .leading, spacing: 2) {
                Text("Your slice")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.onHeroMuted)
                MoneyText(decimalString: view.you.equityUsd, style: .large, color: MonacoTheme.onHero)
            }
            Spacer(minLength: MonacoTheme.Space.s)
            VStack(alignment: .trailing, spacing: 2) {
                Text(GroupHeroMath.sliceCaption(view.you))
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.onHeroMuted)
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
                if GroupHeroMath.hasSlice(view.you) {
                    PnLText(dollarPnl: view.you.dollarPnl, style: .row, onInk: true)
                }
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("group-hero-slice")
    }
}

/// The pot's P&L on ink: the same strip Home draws, tinted by the window's own direction.
private struct GroupPnLCurve: View {
    let points: [GroupPnLPointDTO]

    private var isUp: Bool {
        guard let first = points.first?.chartValue, let last = points.last?.chartValue else { return true }
        return last >= first
    }

    private var tint: Color { isUp ? MonacoTheme.profitOnHero : MonacoTheme.lossOnHero }

    var body: some View {
        Chart(points) { point in
            AreaMark(x: .value("Time", point.at), y: .value("P&L", point.chartValue))
                .foregroundStyle(LinearGradient(colors: [tint.opacity(0.30), tint.opacity(0)], startPoint: .top, endPoint: .bottom))
                .interpolationMethod(.monotone)
            LineMark(x: .value("Time", point.at), y: .value("P&L", point.chartValue))
                .foregroundStyle(tint)
                .lineStyle(StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
                .interpolationMethod(.monotone)
        }
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .chartPlotStyle { $0.background(Color.clear) }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Pot curve")
        .accessibilityValue(summary)
    }

    private var summary: String {
        guard let first = points.first?.chartValue, let last = points.last?.chartValue else { return "" }
        return PnLSpeech.dollars(String(format: "%+.2f", last - first)) + " over the window"
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
            // The member's animal, the same one they are everywhere else.
            MonacoAvatar(photoURL: nil, displayName: member.displayName, size: size, seed: member.userId)
                .overlay(Circle().strokeBorder(ringColor, lineWidth: 2))
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
