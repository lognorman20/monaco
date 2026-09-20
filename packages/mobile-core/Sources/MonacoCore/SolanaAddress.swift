import Foundation

/// Why a pasted destination address cannot be used.
public enum SolanaAddressProblem: Error, Equatable, Sendable {
    case empty
    /// A character outside the base58 alphabet (`0`, `O`, `I`, `l` and punctuation are excluded).
    case badCharacter(Character)
    /// Decoded, but not the 32 bytes a Solana account address is made of.
    case notAnAccountAddress
    /// The user's own Monaco deposit address: sending there would be a no-op round trip.
    case ownDepositAddress
}

/// Client-side mirror of the backend's `privy.ValidateSolanaAddress`: base58 that decodes
/// to exactly 32 bytes. Kept pure so the cash-out screen can tell the member what is wrong
/// while they type, instead of letting the server reject the transfer after the fact.
public enum SolanaAddress {
    /// A Solana account address is an ed25519 public key.
    public static let byteCount = 32

    /// Bitcoin/Solana base58 alphabet: no `0`, `O`, `I` or `l`.
    public static let alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

    /// 32 bytes encode to 32 (all-zero) … 44 base58 characters.
    public static let lengthRange = 32...44

    private static let alphabetIndex: [Character: Int] = {
        var index: [Character: Int] = [:]
        for (position, character) in alphabet.enumerated() {
            index[character] = position
        }
        return index
    }()

    /// Trimmed address, or the reason it cannot be used as a withdrawal destination.
    ///
    /// `ownDepositAddress` is the member's own wallet: the backend refuses that transfer
    /// (`cannot withdraw to your deposit address`), so we catch it before the request.
    public static func validate(
        _ raw: String,
        ownDepositAddress: String? = nil
    ) -> Result<String, SolanaAddressProblem> {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return .failure(.empty) }
        if let offender = trimmed.first(where: { alphabetIndex[$0] == nil }) {
            return .failure(.badCharacter(offender))
        }
        guard let decoded = decodeBase58(trimmed), decoded.count == byteCount else {
            return .failure(.notAnAccountAddress)
        }
        if let own = ownDepositAddress?.trimmingCharacters(in: .whitespacesAndNewlines),
           !own.isEmpty,
           own == trimmed {
            return .failure(.ownDepositAddress)
        }
        return .success(trimmed)
    }

    public static func isValid(_ raw: String, ownDepositAddress: String? = nil) -> Bool {
        if case .success = validate(raw, ownDepositAddress: ownDepositAddress) { return true }
        return false
    }

    /// Big-endian base58 decode. Returns nil on any character outside the alphabet.
    public static func decodeBase58(_ input: String) -> [UInt8]? {
        guard !input.isEmpty else { return nil }
        let characters = Array(input)
        var leadingZeros = 0
        while leadingZeros < characters.count, characters[leadingZeros] == "1" {
            leadingZeros += 1
        }

        // log(58)/log(256) ≈ 0.733: enough room for the decoded big-endian number.
        var buffer = [UInt8](repeating: 0, count: (characters.count * 733 / 1000) + 1)
        for character in characters {
            guard var carry = alphabetIndex[character] else { return nil }
            var position = buffer.count - 1
            while position >= 0 {
                carry += 58 * Int(buffer[position])
                buffer[position] = UInt8(carry % 256)
                carry /= 256
                position -= 1
            }
            guard carry == 0 else { return nil }
        }

        var firstSignificant = 0
        while firstSignificant < buffer.count, buffer[firstSignificant] == 0 {
            firstSignificant += 1
        }
        return [UInt8](repeating: 0, count: leadingZeros) + buffer[firstSignificant...]
    }

    /// One short sentence the member can act on.
    public static func message(for problem: SolanaAddressProblem) -> String {
        switch problem {
        case .empty:
            return "Paste the Solana address you want the USDC sent to."
        case .badCharacter(let character):
            return "That doesn't look like a Solana address — it has a \"\(character)\" in it."
        case .notAnAccountAddress:
            return "That isn't a Solana wallet address. Check you copied the whole thing."
        case .ownDepositAddress:
            return "That's your own Monaco deposit address. Paste the wallet you want to send to."
        }
    }

    /// "7xKX…gAsU" for confirmation screens. Never hyphenated, never truncated mid-copy.
    public static func shortened(_ address: String, lead: Int = 4, tail: Int = 4) -> String {
        let trimmed = address.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.count > lead + tail + 1 else { return trimmed }
        return "\(trimmed.prefix(lead))…\(trimmed.suffix(tail))"
    }
}
