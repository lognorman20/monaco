import MonacoCore
import SwiftUI

/// Which voice a figure speaks in. See `MonacoTheme.Typo`.
enum MoneyVoice {
    /// Avenir Next: money that is someone's — a slice, a pot, a return, a balance.
    case own
    /// SF Mono: a market figure — a quote, a day move, a mark.
    case market
}

/// Size role for every figure that renders `$`, `%` or a share count.
enum MoneyStyle {
    case hero, large, row, caption


    /// Design size at the default text size.
    var baseSize: CGFloat {
        switch self {
        case .hero: return 44
        case .large: return 28
        case .row: return 17
        case .caption: return 13
        }
    }

    /// The text style the figure scales with.
    var textStyle: Font.TextStyle {
        switch self {
        case .hero: return .largeTitle
        case .large: return .title
        case .row: return .body
        case .caption: return .footnote
        }
    }

    var weight: Font.Weight {
        switch self {
        case .hero, .large, .row: return .semibold
        case .caption: return .medium
        }
    }

    /// A row quote in the market's voice. See `MoneyFont.marketRow`.
    static let marketRowBaseSize: CGFloat = 15

    /// Hero figures shrink before they wrap; rows keep their size and truncate last.
    var minimumScaleFactor: CGFloat {

        switch self {
        case .hero: return 0.5
        case .large: return 0.6
        case .row, .caption: return 0.8
        }
    }
}

/// Scales a money figure inside the view tree, so a `.dynamicTypeSize` cap applies to it and a
/// text-size change invalidates the view. `MonacoTheme.Typo.money*` cannot do either: it asks
/// `UIFontMetrics` for a size once, outside the environment, and returns a fixed-size font.
struct MoneyFont: ViewModifier {
    let style: MoneyStyle
    var weightOverride: Font.Weight?
    var voice: MoneyVoice = .own


    // `@ScaledMetric` needs its text style and base size as literals in the property wrapper, so
    // there is one per role rather than one driven by `style`. The sizes come from `MoneyStyle`
    // so the two cannot drift; the text styles are asserted against it in `MoneyStyleScalingTests`.
    @ScaledMetric(relativeTo: .largeTitle) private var hero = MoneyStyle.hero.baseSize
    @ScaledMetric(relativeTo: .title) private var large = MoneyStyle.large.baseSize
    @ScaledMetric(relativeTo: .body) private var row = MoneyStyle.row.baseSize
    @ScaledMetric(relativeTo: .footnote) private var caption = MoneyStyle.caption.baseSize
    /// The market's row size. SF Mono is wide, so a quote in a row sets two points smaller
    /// than money in Avenir Next and still carries the same weight in the column.
    @ScaledMetric(relativeTo: .subheadline) private var marketRow = MoneyStyle.marketRowBaseSize

    private var size: CGFloat {
        switch style {
        case .hero: return hero
        case .large: return large
        case .row: return voice == .market ? marketRow : row
        case .caption: return caption
        }
    }

    private var weight: Font.Weight {
        if let weightOverride { return weightOverride }
        // A hero quote in mono is set a weight lighter: at 44pt semibold SF Mono reads as a
        // terminal, medium reads as a price board.
        if voice == .market, style == .hero { return .medium }
        return style.weight
    }

    func body(content: Content) -> some View {
        switch voice {
        case .own:
            // Avenir Next's lining figures are tabular already; see `MonacoTheme.Typo`.
            content.font(.custom(MonacoTypeface.avenirNext(weight), fixedSize: size))
        case .market:
            content.font(.system(size: size, weight: weight, design: .monospaced).monospacedDigit())
        }
    }

}

extension View {
    /// The one way to set a money figure's font.
    func moneyFont(_ style: MoneyStyle, weight: Font.Weight? = nil, voice: MoneyVoice = .own) -> some View {
        modifier(MoneyFont(style: style, weightOverride: weight, voice: voice))
    }
}


/// "$1,248.50" in tabular SF Pro. Rolls digits on change unless Reduce Motion is on.
struct MoneyText: View {
    private let text: String
    private let numericValue: Double?
    private let style: MoneyStyle
    private let color: Color
    private let voice: MoneyVoice

    /// `voice` is `.own` for money that is someone's, `.market` for a quote. See `MonacoTheme.Typo`.
    init(_ usd: Decimal, style: MoneyStyle, color: Color = MonacoTheme.ink, voice: MoneyVoice = .own) {
        text = UsdAmountFormatter.format(decimal: usd)
        numericValue = (usd as NSDecimalNumber).doubleValue
        self.style = style
        self.color = color
        self.voice = voice
    }

    /// Unparseable input renders "—" in muted.
    init(decimalString: String, style: MoneyStyle, color: Color = MonacoTheme.ink, voice: MoneyVoice = .own) {
        self.voice = voice
        let trimmed = decimalString.trimmingCharacters(in: .whitespacesAndNewlines)
        if let decimal = Decimal(string: trimmed, locale: Locale(identifier: "en_US_POSIX")),
           trimmed.allSatisfy({ $0.isNumber || $0 == "." || $0 == "-" || $0 == "+" }) {
            text = UsdAmountFormatter.format(decimal: decimal)
            numericValue = (decimal as NSDecimalNumber).doubleValue
            self.color = color
        } else {
            text = "—"
            numericValue = nil
            self.color = MonacoTheme.muted
        }
        self.style = style
    }

    init(micros: Int64, style: MoneyStyle, color: Color = MonacoTheme.ink, voice: MoneyVoice = .own) {
        self.init(Decimal(micros) / Decimal(1_000_000), style: style, color: color, voice: voice)
    }

    var body: some View {
        MoneyFigure(text: text, value: numericValue, style: style, color: color, voice: voice)
    }
}

/// Signed dollar P&L: "+$48.20" profit, "−$7.60" loss, "$0.00" muted when it rounds to zero.
struct PnLText: View {
    private let dollarPnl: String
    private let style: MoneyStyle
    private let onInk: Bool

    /// `onInk` switches to the saturated pair for figures on a deep ink hero card.
    init(dollarPnl: String, style: MoneyStyle, onInk: Bool = false) {
        self.dollarPnl = dollarPnl
        self.style = style
        self.onInk = onInk
    }

    var body: some View {
        let tone = PnLTone(dollarPnl: dollarPnl)
        return MoneyFigure(
            text: SignedUsdFormatter.format(dollarPnl),
            value: SignedUsdFormatter.parse(dollarPnl).map { ($0 as NSDecimalNumber).doubleValue },
            style: style,
            color: onInk ? tone.inkCardColor : tone.color
        )
        .accessibilityLabel(PnLSpeech.dollars(dollarPnl))
    }
}

/// Signed return: "+9.6%" / "−3.6%" coloured; nil or "—" muted.
struct PercentText: View {
    private let formatted: String
    private let style: MoneyStyle

    init(percentReturn: String?, style: MoneyStyle) {
        formatted = PercentReturnFormatter.format(percentReturn)
        self.style = style
    }

    var body: some View {
        MoneyFigure(
            text: formatted,
            value: PnLSpeech.percentValue(formatted),
            style: style,
            color: MonacoTheme.signed(formatted)
        )
        .accessibilityLabel(PnLSpeech.percent(formatted))
    }
}

/// Capsule on the profit/loss wash: "▲ $48.20 · 9.6%". Dollars only when the percent is missing.
struct PnLBadge: View {
    private let dollarPnl: String
    private let percentReturn: String?
    private let style: MoneyStyle
    private let onInk: Bool

    /// `onInk` switches to the vivid pair and a wash that reads on a deep ink hero card.
    init(dollarPnl: String, percentReturn: String?, style: MoneyStyle = .caption, onInk: Bool = false) {
        self.dollarPnl = dollarPnl
        self.percentReturn = percentReturn
        self.style = style
        self.onInk = onInk
    }

    private var tone: PnLTone { PnLTone(dollarPnl: dollarPnl) }

    private var label: String {
        let dollars = SignedUsdFormatter.format(dollarPnl)
        let magnitude = dollars.drop(while: { $0 == "+" || $0 == "\u{2212}" })
        var parts = [String(magnitude)]
        let percent = PercentReturnFormatter.format(percentReturn)
        if percent != "—" {
            parts.append(String(percent.drop(while: { $0 == "+" || $0 == "\u{2212}" })))
        }
        let body = parts.joined(separator: " · ")
        switch tone {
        case .profit: return "▲ " + body
        case .loss: return "▼ " + body
        case .flat: return body
        }
    }

    var body: some View {
        Text(label)
            .moneyFont(style, weight: .semibold)
            .foregroundStyle(onInk ? tone.inkCardColor : tone.washColor)
            .lineLimit(1)
            .minimumScaleFactor(0.8)
            .padding(.horizontal, style == .caption ? 9 : 12)
            .padding(.vertical, style == .caption ? 5 : 6)
            .background(Capsule().fill(onInk ? tone.inkCardWash : tone.wash))
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Profit and loss")
            .accessibilityValue(PnLSpeech.badge(dollarPnl: dollarPnl, percentReturn: percentReturn))
    }
}

// MARK: - Internals

private struct MoneyFigure: View {
    let text: String
    let value: Double?
    let style: MoneyStyle
    let color: Color
    var voice: MoneyVoice = .own

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Text(text)
            .moneyFont(style, voice: voice)
            .foregroundStyle(color)
            .lineLimit(1)
            .minimumScaleFactor(style.minimumScaleFactor)
            .contentTransition(reduceMotion || value == nil ? .identity : .numericText(value: value ?? 0))
            .animation(reduceMotion ? nil : .snappy, value: text)
            .modifier(HeroTypeCap(isHero: style == .hero))
    }
}

private struct HeroTypeCap: ViewModifier {
    let isHero: Bool

    func body(content: Content) -> some View {
        if isHero {
            content.dynamicTypeSize(...DynamicTypeSize.accessibility2)
        } else {
            content
        }
    }
}

enum PnLTone {
    case profit, loss, flat

    init(dollarPnl: String) {
        if SignedUsdFormatter.isZero(dollarPnl) || SignedUsdFormatter.parse(dollarPnl) == nil {
            self = .flat
        } else if SignedUsdFormatter.isLoss(dollarPnl) {
            self = .loss
        } else {
            self = .profit
        }
    }

    var color: Color {
        switch self {
        case .profit: return MonacoTheme.profit
        case .loss: return MonacoTheme.loss
        case .flat: return MonacoTheme.muted
        }
    }

    var wash: Color {
        switch self {
        case .profit: return MonacoTheme.profitWash
        case .loss: return MonacoTheme.lossWash
        case .flat: return MonacoTheme.surfaceSunken
        }
    }

    /// Text drawn on `wash`. Deeper than `color`, which is derived for bare paper.
    var washColor: Color {
        switch self {
        case .profit: return MonacoTheme.profitOnWash
        case .loss: return MonacoTheme.lossOnWash
        case .flat: return MonacoTheme.muted
        }
    }

    /// Saturated pair for figures drawn on a deep ink hero card.
    var inkCardColor: Color {
        switch self {
        case .profit: return MonacoTheme.profitOnHero
        case .loss: return MonacoTheme.lossOnHero
        case .flat: return MonacoTheme.onHeroMuted
        }
    }

    var inkCardWash: Color {
        switch self {
        case .profit: return MonacoTheme.profitWashOnHero
        case .loss: return MonacoTheme.lossWashOnHero
        case .flat: return Color.white.opacity(0.12)
        }
    }
}

/// Words VoiceOver reads for signed figures ("up 48 dollars 20 cents, 9.6 percent").
enum PnLSpeech {
    static func dollars(_ raw: String) -> String {
        guard let value = SignedUsdFormatter.parse(raw) else { return "unavailable" }
        if SignedUsdFormatter.isZero(raw) { return "no change" }
        let magnitude = value < 0 ? -value : value
        let direction = value < 0 ? "down" : "up"
        return "\(direction) \(spokenAmount(magnitude))"
    }

    static func spokenAmount(_ magnitude: Decimal) -> String {
        var cents = Decimal()
        var scaled = magnitude * 100
        NSDecimalRound(&cents, &scaled, 0, .plain)
        let totalCents = (cents as NSDecimalNumber).int64Value
        let dollars = totalCents / 100
        let remainder = totalCents % 100
        var parts: [String] = []
        if dollars > 0 || remainder == 0 {
            parts.append("\(dollars) \(dollars == 1 ? "dollar" : "dollars")")
        }
        if remainder > 0 {
            parts.append("\(remainder) \(remainder == 1 ? "cent" : "cents")")
        }
        return parts.joined(separator: " ")
    }

    static func percentValue(_ formatted: String) -> Double? {
        let normalised = formatted
            .replacingOccurrences(of: "\u{2212}", with: "-")
            .replacingOccurrences(of: "%", with: "")
            .replacingOccurrences(of: "+", with: "")
        return Double(normalised)
    }

    static func percent(_ formatted: String) -> String {
        guard let value = percentValue(formatted) else { return "unavailable" }
        if value == 0 { return "0 percent" }
        let magnitude = formatted
            .replacingOccurrences(of: "\u{2212}", with: "")
            .replacingOccurrences(of: "+", with: "")
            .replacingOccurrences(of: "%", with: "")
        return "\(value < 0 ? "down" : "up") \(magnitude) percent"
    }

    static func badge(dollarPnl: String, percentReturn: String?) -> String {
        let dollars = Self.dollars(dollarPnl)
        let formatted = PercentReturnFormatter.format(percentReturn)
        guard formatted != "—", percentValue(formatted) != nil else { return dollars }
        let magnitude = formatted
            .replacingOccurrences(of: "\u{2212}", with: "")
            .replacingOccurrences(of: "+", with: "")
            .replacingOccurrences(of: "%", with: "")
        return "\(dollars), \(magnitude) percent"
    }
}
