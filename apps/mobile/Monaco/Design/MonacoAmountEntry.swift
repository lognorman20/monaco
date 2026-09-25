import MonacoCore
import SwiftUI

enum AmountPreset: Equatable {
    /// A fixed dollar amount, e.g. `.dollars(25)` → "$25".
    case dollars(Decimal)
    /// A share of `max`, e.g. `.fraction(1, label: "Max")`. Disabled when `max` is nil.
    case fraction(Double, label: String)
}

/// Big centred dollar figure over the system decimal pad, preset chips and one helper line.
/// `amountText` holds a plain decimal string ("50", "12.5"); the view keeps it to digits,
/// one ".", and two decimals.
///
/// The figure is the member's own money, so it sets in Avenir Next (`moneyFont(.hero)`); the
/// presets are a strip of choices, so they set in the market's voice at the size of Home's
/// range chips. A screen that says what the money will do puts an `AmountEntryNote` under it.
struct AmountEntry: View {
    @Binding private var amountText: String
    private let max: Decimal?
    private let presets: [AmountPreset]
    private let helper: String?
    private let overLimitHelper: String
    private let problem: String?
    private let showsKeyboardDoneButton: Bool

    @FocusState private var focused: Bool
    @State private var hasRaisedKeyboard = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// - Parameter problem: why the amount can't be used, in the member's words. It replaces
    ///   `helper` while it is set, so a screen with a rule of its own — a minimum, a remainder
    ///   too small to leave behind — can say so instead of leaving a dead button unexplained.
    /// - Parameter showsKeyboardDoneButton: puts a Done button in the navigation bar while the
    ///   decimal pad is up, because the pad has no return key of its own. It lives in the bar, not
    ///   on the keyboard: as a keyboard accessory it floated over the screen's `BottomCTA` on
    ///   iOS 26 and covered the end of its label. Opt-in, because it needs a navigation bar and a
    ///   screen that has nothing else in the bar's trailing slot.
    init(
        amountText: Binding<String>,
        max: Decimal? = nil,
        presets: [AmountPreset] = [],
        helper: String? = nil,
        overLimitHelper: String = "More than you have",
        problem: String? = nil,
        showsKeyboardDoneButton: Bool = false
    ) {
        _amountText = amountText
        self.max = max
        self.presets = presets
        self.helper = helper
        self.overLimitHelper = overLimitHelper
        self.problem = problem
        self.showsKeyboardDoneButton = showsKeyboardDoneButton
    }

    private var value: Decimal? {
        AmountEntryText.decimal(amountText)
    }

    private var isOverLimit: Bool {
        guard let max, let value else { return false }
        return value > max
    }

    var body: some View {
        VStack(spacing: MonacoTheme.Space.l) {
            figure
            if !presets.isEmpty {
                presetRow
            }
            if let helperLine {
                Text(helperLine)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(hasProblem ? MonacoTheme.loss : MonacoTheme.muted)
                    .multilineTextAlignment(.center)
                    .contentTransition(.opacity)
                    .animation(reduceMotion ? nil : .snappy, value: helperLine)
                    .accessibilityIdentifier("amount-entry-helper")
            }
        }
        .frame(maxWidth: .infinity)
        .task {
            // Let the push transition settle before the keyboard rises. This is the view's own
            // task, so it is cancelled with the screen: a keyboard never arrives after the member
            // has left, and coming back from a pushed screen does not raise it a second time.
            guard !hasRaisedKeyboard else { return }
            try? await Task.sleep(for: AmountEntry.keyboardRevealDelay)
            guard !Task.isCancelled else { return }
            hasRaisedKeyboard = true
            focused = true
        }
        .toolbar {
            // The decimal pad has no return key, so the member needs a way out of it. Only while
            // the pad is up, and only for the screens that asked.
            if showsKeyboardDoneButton, focused {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Done") { focused = false }
                        .font(MonacoTheme.Typo.bodyStrong)
                        .accessibilityIdentifier("amount-entry-done-button")
                }
            }
        }
        .onChange(of: amountText) { _, newValue in
            let cleaned = AmountEntryText.sanitize(newValue)
            if cleaned != newValue { amountText = cleaned }
        }
    }

    /// Long enough for a push transition to settle before the keyboard rises over it.
    private static let keyboardRevealDelay: Duration = .milliseconds(350)

    private var hasProblem: Bool {
        problem != nil || isOverLimit
    }

    /// The empty "$0" is a placeholder, not an amount, so it takes the placeholder token: at
    /// `tertiaryText` it read as money the member had already typed (see `disabledLabel`).
    private var figureColor: Color {
        if amountText.isEmpty { return MonacoTheme.disabledLabel }
        return hasProblem ? MonacoTheme.loss : MonacoTheme.ink
    }

    private var helperLine: String? {
        if let problem { return problem }
        return isOverLimit ? overLimitHelper : helper
    }

    private var figure: some View {
        ZStack {
            HStack(alignment: .center, spacing: 2) {
                // `moneyFont` scales inside the view tree, so the cap below reaches the figure;
                // the pre-scaled `Typo.moneyHero` it used to set ignored it.
                Text(AmountEntryText.display(amountText))
                    .moneyFont(.hero)
                    .foregroundStyle(figureColor)
                    .lineLimit(1)
                    .minimumScaleFactor(0.4)
                    .contentTransition(reduceMotion ? .identity : .numericText())
                    .animation(reduceMotion ? nil : .snappy(duration: 0.2), value: amountText)
                AmountCaret(visible: focused)
            }
            .dynamicTypeSize(...DynamicTypeSize.accessibility2)
            .accessibilityHidden(true)

            // The real field sits over the figure: a big tap target, and what VoiceOver focuses.
            // No title: a TextField draws its title as a placeholder in its own colour, which
            // `.foregroundStyle(.clear)` does not reach, so it would sit on top of the "$0" figure.
            // The figure is the empty state; VoiceOver gets its name from the label below.
            TextField(text: $amountText, prompt: nil) { EmptyView() }
                .keyboardType(.decimalPad)
                .focused($focused)
                .foregroundStyle(.clear)
                .tint(.clear)
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .accessibilityLabel("Amount")
                .accessibilityValue(AmountEntryText.display(amountText))
                .accessibilityIdentifier("amount-entry-field")
        }
        .frame(maxWidth: .infinity, minHeight: 72)
        .contentShape(Rectangle())
        .onTapGesture { focused = true }
    }

    /// One strip of chips. At the accessibility text sizes four mono chips are wider than the
    /// screen, so they fall into two rows rather than squeezing their labels. Decided by the
    /// text size, not by measuring: each chip is in the tree once, so a UI test asking for the
    /// "$50" button finds exactly one.
    private var presetRow: some View {
        VStack(spacing: 0) {
            ForEach(Array(AmountEntryText.presetRows(presets.count, stacked: dynamicTypeSize.isAccessibilitySize).enumerated()), id: \.offset) { _, row in
                HStack(spacing: MonacoTheme.Space.s) {
                    ForEach(row, id: \.self) { index in
                        presetChip(presets[index])
                    }
                }
            }
        }
    }

    /// Drawn 34pt tall like Home's range chips and padded out to a 44pt target. The label is
    /// what UI tests and VoiceOver find the chip by ("$50", "Max"), so it stays the bare amount.
    private func presetChip(_ preset: AmountPreset) -> some View {
        let target = amount(for: preset)
        let selected = target != nil && value == target
        return Button {
            guard let target else { return }
            Haptics.selection()
            amountText = AmountEntryText.plain(target)
        } label: {
            Text(label(for: preset))
                .font(MonacoTheme.Typo.dataCaption)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .foregroundStyle(chipLabelColor(selected: selected, enabled: target != nil))
                .padding(.horizontal, 14)
                .frame(minWidth: 56, minHeight: 34)
                .background(Capsule().fill(selected ? MonacoTheme.primaryButtonFill : MonacoTheme.surfaceSunken))
                .padding(.vertical, 5)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(target == nil)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    /// A chip with nothing to fill in (Max before there is a balance) keeps its shape and
    /// greys its label, the way a disabled button does.
    private func chipLabelColor(selected: Bool, enabled: Bool) -> Color {
        if !enabled { return MonacoTheme.disabledLabel }
        return selected ? MonacoTheme.primaryButtonLabel : MonacoTheme.ink
    }

    private func amount(for preset: AmountPreset) -> Decimal? {
        switch preset {
        case .dollars(let dollars):
            return dollars
        case .fraction(let fraction, _):
            guard let max, max > 0 else { return nil }
            return AmountEntryText.roundDownToCents(max * Decimal(fraction))
        }
    }

    private func label(for preset: AmountPreset) -> String {
        switch preset {
        case .dollars(let dollars):
            return AmountEntryText.display(AmountEntryText.plain(dollars))
        case .fraction(_, let label):
            return label
        }
    }
}

/// Blinking ink caret after the figure while the field has focus. It grows with the figure,
/// and stops where the figure's own text-size cap stops it.
private struct AmountCaret: View {
    let visible: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @ScaledMetric(relativeTo: .largeTitle) private var height: CGFloat = 40

    var body: some View {
        RoundedRectangle(cornerRadius: 1.5)
            .fill(MonacoTheme.ink)
            .frame(width: 3, height: height)
            .opacityLoop(to: 0, halfPeriod: 0.5, active: visible && !reduceMotion)
            .opacity(visible ? 1 : 0)
    }
}

/// The sentence under an amount pad that says what the money will do. Fund this cabal and
/// Cash out both end on one, set the same way, so the two screens read as a pair.
struct AmountEntryNote: View {
    private let text: String

    init(_ text: String) {
        self.text = text
    }

    var body: some View {
        Text(text)
            .font(MonacoTheme.Typo.caption)
            .foregroundStyle(MonacoTheme.muted)
            .multilineTextAlignment(.center)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity)
            .padding(.horizontal, MonacoTheme.Space.sm)
    }
}

/// An amount screen before its balance has loaded: the figure, the chips and the helper line
/// in their own places, so nothing moves when the real ones arrive.
struct AmountEntrySkeleton: View {
    var presetCount = 4

    var body: some View {
        VStack(spacing: MonacoTheme.Space.l) {
            SkeletonBlock(width: 132, height: 48)
                .frame(minHeight: 72)
            HStack(spacing: MonacoTheme.Space.s) {
                ForEach(0..<presetCount, id: \.self) { _ in
                    SkeletonBlock(width: 56, height: 34, radius: 17)
                        .padding(.vertical, 5)
                }
            }
            SkeletonBlock(width: 168, height: 14)
        }
        .frame(maxWidth: .infinity)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
    }
}

/// Pure text handling for `AmountEntry`, kept separate so it is easy to reason about.
enum AmountEntryText {
    private static let posix = Locale(identifier: "en_US_POSIX")

    /// Digits and one ".", at most two decimals, no leading zeros ("007" → "7", "." → "0.").
    static func sanitize(_ raw: String) -> String {
        let normalised = normalisingSeparators(raw)
        var result = ""
        var seenDot = false
        var decimals = 0
        for character in normalised {
            if character.isASCII, character.isNumber {
                if seenDot {
                    guard decimals < 2 else { continue }
                    decimals += 1
                }
                result.append(character)
            } else if character == ".", !seenDot {
                seenDot = true
                result.append(result.isEmpty ? "0." : ".")
            }
        }
        while result.count > 1, result.hasPrefix("0"), !result.hasPrefix("0.") {
            result.removeFirst()
        }
        if result.count > 12 { result = String(result.prefix(12)) }
        return result
    }

    /// Which separator in the text is the decimal point, and which is grouping.
    ///
    /// The figure over the pad renders "$1,250.50", and that is what gets copied and pasted back
    /// in. Treating every comma as a decimal point turned a pasted "$1,250.50" into $1.25 — a
    /// thousandfold error on a money screen.
    ///
    /// - Both separators present: only a paste can produce that, and the one that comes last is
    ///   the decimal point. "1,250.50" and "1.250,50" are both 1250.50.
    /// - Only commas: decided by shape, not by count. Commas laid out like grouping separators
    ///   ("1,250", "1,250,000") are grouping and come out. Anything else is the member typing a
    ///   decimal point on a comma-decimal pad — "12,", "12,5", and "1,2,5" from a double tap —
    ///   so the first comma becomes the point and the rest are dropped, which is exactly what
    ///   `sanitize` already does with extra dots.
    /// - Only dots: left alone. `sanitize` keeps the first and ignores the rest, which is what
    ///   typing needs — "1.2" plus another "." must stay 1.2, not become 12.
    private static func normalisingSeparators(_ raw: String) -> String {
        let hasComma = raw.contains(",")
        guard hasComma else { return raw }

        if let lastComma = raw.lastIndex(of: ","), let lastDot = raw.lastIndex(of: ".") {
            if lastComma > lastDot {
                return raw.replacingOccurrences(of: ".", with: "").replacingOccurrences(of: ",", with: ".")
            }
            return raw.replacingOccurrences(of: ",", with: "")
        }

        if commasAreGrouping(raw) {
            return raw.replacingOccurrences(of: ",", with: "")
        }
        // Every comma becomes a dot; `sanitize` keeps the first and ignores the rest. Treating a
        // stray second comma as grouping instead turned "1,250,5" into 12505 — a hundredfold
        // error on a money field.
        return raw.replacingOccurrences(of: ",", with: ".")
    }

    /// Commas in the shape grouping separators actually take: one to three digits, then groups of
    /// exactly three. Currency symbols and spaces around them are ignored.
    private static func commasAreGrouping(_ raw: String) -> Bool {
        let compact = raw.filter { ($0.isASCII && $0.isNumber) || $0 == "," }
        let groups = compact.split(separator: ",", omittingEmptySubsequences: false)
        guard groups.count >= 2, (1...3).contains(groups[0].count) else { return false }
        return groups.dropFirst().allSatisfy { $0.count == 3 }
    }

    static func decimal(_ text: String) -> Decimal? {
        guard !text.isEmpty else { return nil }
        return Decimal(string: text, locale: posix)
    }

    /// The typed amount in USDC micros, rounded to the nearest micro. Add money, Cash out and
    /// Withdraw each used to carry their own copy of this conversion.
    static func micros(_ text: String) -> Int64? {
        guard let value = decimal(text), value >= 0 else { return nil }
        var scaled = value * 1_000_000
        var rounded = Decimal()
        NSDecimalRound(&rounded, &scaled, 0, .plain)
        return (rounded as NSDecimalNumber).int64Value
    }

    /// "$1,250.5" while typing: grouping on the integer part, the typed decimals kept as typed.
    static func display(_ text: String) -> String {
        guard !text.isEmpty else { return "$0" }
        let parts = text.split(separator: ".", omittingEmptySubsequences: false)
        let integer = Int(parts[0]) ?? 0
        let integerText = groupedIntegerFormatter.string(from: NSNumber(value: integer)) ?? String(parts[0])
        if parts.count > 1 {
            return "$\(integerText).\(parts[1])"
        }
        return "$\(integerText)"
    }

    /// "25", "12.5", "12.05" — no grouping, no trailing zeros.
    static func plain(_ decimal: Decimal) -> String {
        plainFormatter.string(from: decimal as NSDecimalNumber) ?? "\(decimal)"
    }

    // A NumberFormatter loads ICU data when it is built and is cheap to read afterwards. These
    // run on every body pass of a screen the member is typing into — several times per keystroke
    // between the figure, the CTA title and the preset chips — so they are built once.
    private static let groupedIntegerFormatter: NumberFormatter = {
        let formatter = NumberFormatter()
        formatter.locale = posix
        formatter.numberStyle = .decimal
        formatter.usesGroupingSeparator = true
        formatter.groupingSeparator = ","
        return formatter
    }()

    private static let plainFormatter: NumberFormatter = {
        let formatter = NumberFormatter()
        formatter.locale = posix
        formatter.numberStyle = .decimal
        formatter.usesGroupingSeparator = false
        formatter.minimumFractionDigits = 0
        formatter.maximumFractionDigits = 2
        return formatter
    }()

    /// Which preset goes on which row: one row normally, two at the accessibility text sizes
    /// (the longer half first) once there are more than two chips.
    static func presetRows(_ count: Int, stacked: Bool) -> [[Int]] {
        guard count > 0 else { return [] }
        guard stacked, count > 2 else { return [Array(0..<count)] }
        let split = (count + 1) / 2
        return [Array(0..<split), Array(split..<count)]
    }

    static func roundDownToCents(_ decimal: Decimal) -> Decimal {
        var source = decimal
        var result = Decimal()
        NSDecimalRound(&result, &source, 2, .down)
        return result
    }
}
