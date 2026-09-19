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

    @FocusState private var focused: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(
        amountText: Binding<String>,
        max: Decimal? = nil,
        presets: [AmountPreset] = [],
        helper: String? = nil,
        overLimitHelper: String = "More than you have"
    ) {
        _amountText = amountText
        self.max = max
        self.presets = presets
        self.helper = helper
        self.overLimitHelper = overLimitHelper
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
                    .foregroundStyle(isOverLimit ? MonacoTheme.loss : MonacoTheme.muted)
                    .multilineTextAlignment(.center)
                    .contentTransition(.opacity)
                    .animation(reduceMotion ? nil : .snappy, value: isOverLimit)
            }
        }
        .frame(maxWidth: .infinity)
        .onAppear {
            // Let the push/present transition settle before the keyboard rises.
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.35) { focused = true }
        }
        .onChange(of: amountText) { _, newValue in
            let cleaned = AmountEntryText.sanitize(newValue)
            if cleaned != newValue { amountText = cleaned }
        }
    }

    private var helperLine: String? {
        isOverLimit ? overLimitHelper : helper
    }

    private var figure: some View {
        ZStack {
            HStack(alignment: .center, spacing: 2) {
                Text(AmountEntryText.display(amountText))
                    .font(MonacoTheme.Typo.moneyHero)
                    .foregroundStyle(amountText.isEmpty ? MonacoTheme.tertiaryText : (isOverLimit ? MonacoTheme.loss : MonacoTheme.ink))
                    .lineLimit(1)
                    .minimumScaleFactor(0.4)
                    .contentTransition(reduceMotion ? .identity : .numericText())
                    .animation(reduceMotion ? nil : .snappy(duration: 0.2), value: amountText)
                AmountCaret(visible: focused)
            }
            .dynamicTypeSize(...DynamicTypeSize.accessibility2)
            .accessibilityHidden(true)

            // The real field sits over the figure: a big tap target, and what VoiceOver focuses.
            TextField("Amount", text: $amountText)
                .keyboardType(.decimalPad)
                .focused($focused)
                .foregroundStyle(.clear)
                .tint(.clear)
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .accessibilityLabel("Amount in dollars")
                .accessibilityValue(amountText.isEmpty ? "0" : AmountEntryText.display(amountText))
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
    @State private var on = true

    var body: some View {
        RoundedRectangle(cornerRadius: 1.5)
            .fill(MonacoTheme.ink)
            .frame(width: 3, height: 40)
            .opacity(visible && (reduceMotion || on) ? 1 : 0)
            .onAppear {
                guard !reduceMotion else { return }
                withAnimation(.easeInOut(duration: 0.5).repeatForever(autoreverses: true)) { on = false }
            }
    }
}

/// Pure text handling for `AmountEntry`, kept separate so it is easy to reason about.
enum AmountEntryText {
    private static let posix = Locale(identifier: "en_US_POSIX")

    /// Digits and one ".", at most two decimals, no leading zeros ("007" → "7", "." → "0.").
    static func sanitize(_ raw: String) -> String {
        let normalised = raw.replacingOccurrences(of: ",", with: ".")
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

    static func decimal(_ text: String) -> Decimal? {
        guard !text.isEmpty else { return nil }
        return Decimal(string: text, locale: posix)
    }

    /// "$1,250.5" while typing: grouping on the integer part, the typed decimals kept as typed.
    static func display(_ text: String) -> String {
        guard !text.isEmpty else { return "$0" }
        let parts = text.split(separator: ".", omittingEmptySubsequences: false)
        let integer = Int(parts[0]) ?? 0
        let formatter = NumberFormatter()
        formatter.locale = posix
        formatter.numberStyle = .decimal
        formatter.usesGroupingSeparator = true
        formatter.groupingSeparator = ","
        let integerText = formatter.string(from: NSNumber(value: integer)) ?? String(parts[0])
        if parts.count > 1 {
            return "$\(integerText).\(parts[1])"
        }
        return "$\(integerText)"
    }

    /// "25", "12.5", "12.05" — no grouping, no trailing zeros.
    static func plain(_ decimal: Decimal) -> String {
        let formatter = NumberFormatter()
        formatter.locale = posix
        formatter.numberStyle = .decimal
        formatter.usesGroupingSeparator = false
        formatter.minimumFractionDigits = 0
        formatter.maximumFractionDigits = 2
        return formatter.string(from: decimal as NSDecimalNumber) ?? "\(decimal)"
    }

    static func roundDownToCents(_ decimal: Decimal) -> Decimal {
        var source = decimal
        var result = Decimal()
        NSDecimalRound(&result, &source, 2, .down)
        return result
    }
}
