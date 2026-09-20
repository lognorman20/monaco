import Foundation

/// Why a pasted destination address cannot be used.
public enum EVMAddressProblem: Error, Equatable, Sendable {
    case empty
    case badCharacter(Character)
    case notAnAccountAddress
    case ownDepositAddress
}

/// Client-side Base address check: `0x` + 40 hex chars, compared lowercase.
public enum EVMAddress {
    public static let hexCount = 40

    /// Trimmed lowercase address, or the reason it cannot be used as a withdrawal destination.
    public static func validate(
        _ raw: String,
        ownDepositAddress: String? = nil
    ) -> Result<String, EVMAddressProblem> {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return .failure(.empty) }
        guard trimmed.lowercased().hasPrefix("0x") else {
            if let offender = trimmed.first(where: { !$0.isHexDigit }) {
                return .failure(.badCharacter(offender))
            }
            return .failure(.notAnAccountAddress)
        }
        let body = trimmed.dropFirst(2)
        if let offender = body.first(where: { !$0.isHexDigit }) {
            return .failure(.badCharacter(offender))
        }
        guard body.count == hexCount else {
            return .failure(.notAnAccountAddress)
        }
        let normalized = "0x" + body.lowercased()
        if let own = ownDepositAddress?.trimmingCharacters(in: .whitespacesAndNewlines),
           !own.isEmpty,
           own.lowercased() == normalized {
            return .failure(.ownDepositAddress)
        }
        return .success(normalized)
    }

    public static func isValid(_ raw: String, ownDepositAddress: String? = nil) -> Bool {
        if case .success = validate(raw, ownDepositAddress: ownDepositAddress) { return true }
        return false
    }

    public static func message(for problem: EVMAddressProblem) -> String {
        switch problem {
        case .empty:
            return "Paste the Base address you want the USDC sent to."
        case .badCharacter(let character):
            return "That doesn't look like a Base address — it has a \"\(character)\" in it."
        case .notAnAccountAddress:
            return "That isn't a Base wallet address. Check you copied the whole thing."
        case .ownDepositAddress:
            return "That's your own Monaco deposit address. Paste the wallet you want to send to."
        }
    }

    /// "0x83…2913" for confirmation screens. Never hyphenated.
    public static func shortened(_ address: String, lead: Int = 4, tail: Int = 4) -> String {
        let trimmed = address.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.count > lead + tail + 1 else { return trimmed }
        return "\(trimmed.prefix(lead))…\(trimmed.suffix(tail))"
    }
}
