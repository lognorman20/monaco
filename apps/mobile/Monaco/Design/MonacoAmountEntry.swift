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

    /// - Parameter problem: why the amount can't be used, in the member's words. It replaces
    ///   `helper` while it is set, so a screen with a rule of its own — a minimum, a remainder
    ///   too small to leave behind — can say so instead of leaving a dead button unexplained.
    /// - Parameter showsKeyboardDoneButton: adds a Done bar above the decimal pad, which has no
    ///   return key of its own. It is opt-in and off by default because it is a keyboard accessory
    ///   view: it makes the keyboard ~44pt taller on every screen that asks for it, which moves
    ///   anything the screen pins to the bottom. A screen turns it on once it has checked that its
    ///   own layout — and its UI tests — survive the taller keyboard.
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
            // The decimal pad has no return key, so the member needs a way out of it. Only for
            // the screens that asked: this bar is part of the keyboard, so it changes the height
            // of everything below it.
            if showsKeyboardDoneButton, focused {
                ToolbarItemGroup(placement: .keyboard) {
                    Spacer()
                    Button("Done") { focused = false }
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

    private var helperLine: String? {
        if let problem { return problem }
        return isOverLimit ? overLimitHelper : helper
    }

    private var figure: some View {
        ZStack {
            HStack(alignment: .center, spacing: 2) {
                Text(AmountEntryText.display(amountText))
                    .font(MonacoTheme.Typo.moneyHero)
                    .foregroundStyle(amountText.isEmpty ? MonacoTheme.tertiaryText : (hasProblem ? MonacoTheme.loss : MonacoTheme.ink))
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

    private var presetRow: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            ForEach(Array(presets.enumerated()), id: \.offset) { _, preset in
                let target = amount(for: preset)
                let selected = target != nil && value == target
                Button {
                    guard let target else { return }
                    Haptics.selection()
                    amountText = AmountEntryText.plain(target)
                } label: {
                    Text(label(for: preset))
                        .font(MonacoTheme.Typo.callout.weight(.semibold).monospacedDigit())
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                        .foregroundStyle(selected ? MonacoTheme.primaryButtonLabel : MonacoTheme.ink)
                        .padding(.horizontal, 16)
                        .frame(minWidth: 64, minHeight: 44)
                        .background(Capsule().fill(selected ? MonacoTheme.primaryButtonFill : MonacoTheme.surfaceSunken))
                        .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .disabled(target == nil)
                .opacity(target == nil ? 0.4 : 1)
                .accessibilityAddTraits(selected ? .isSelected : [])
            }
        }
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

/// Blinking ink caret after the figure while the field has focus.
private struct AmountCaret: View {
    let visible: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        RoundedRectangle(cornerRadius: 1.5)
            .fill(MonacoTheme.ink)
            .frame(width: 3, height: 40)
            .opacityLoop(to: 0, halfPeriod: 0.5, active: visible && !reduceMotion)
            .opacity(visible ? 1 : 0)
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

    static func roundDownToCents(_ decimal: Decimal) -> Decimal {
        var source = decimal
        var result = Decimal()
        NSDecimalRound(&result, &source, 2, .down)
        return result
    }
}
