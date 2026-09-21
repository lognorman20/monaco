import Foundation
import MonacoCore

/// One rule for "is this an address the member can actually send USDC to".
///
/// The deposit address is the member's Base wallet, and it is what this screen puts on the
/// clipboard, so real money gets sent to it. A wallet that is still being made comes back as an
/// empty string; anything that is not `0x` plus 40 hex characters is not a Base address at all.
/// Neither may be shown as somewhere to send USDC.
enum DepositAddress {
    /// The address in the lowercase form the backend stores and compares, or `nil` when it cannot
    /// receive USDC on Base.
    static func usable(_ raw: String?) -> String? {
        guard let raw, case .success(let address) = EVMAddress.validate(raw) else { return nil }
        return address
    }
}
