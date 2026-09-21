import Foundation

/// SMS login accepts US 10-digit numbers as +1, or a full E.164 value with +.
enum LoginPhone {
    static func normalizedE164(_ raw: String) -> String {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.hasPrefix("+") {
            let digits = trimmed.filter(\.isNumber)
            return digits.isEmpty ? trimmed : "+\(digits)"
        }
        let digits = trimmed.filter(\.isNumber)
        if digits.count == 10 {
            return "+1\(digits)"
        }
        if digits.count == 11, digits.first == "1" {
            return "+\(digits)"
        }
        return trimmed
    }

    static func isComplete(_ raw: String) -> Bool {
        let digits = normalizedE164(raw).filter(\.isNumber)
        return digits.count >= 11
    }
}
