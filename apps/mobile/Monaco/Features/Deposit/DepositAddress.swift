import Foundation

/// One rule for "is this an address the member can actually send USDC to".
///
/// A wallet that is still being made comes back as an empty string, and dev environments hand out
/// placeholder addresses beginning "FAKE". Both would otherwise be copied to the clipboard and
/// sent real money.
enum DepositAddress {
    static func usable(_ raw: String?) -> String? {
        guard let trimmed = raw?.trimmingCharacters(in: .whitespacesAndNewlines),
              !trimmed.isEmpty,
              !trimmed.hasPrefix("FAKE") else {
            return nil
        }
        return trimmed
    }
}
