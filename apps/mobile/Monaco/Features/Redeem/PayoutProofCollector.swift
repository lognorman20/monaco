import Foundation

/// Collects signed payout address proof via Privy before redeem submit.
enum PayoutProofCollector {
    static func collectProof(payoutAddress: String, memberWalletAddress: String) -> String {
        let message = "monaco-redeem:\(payoutAddress)"
        let payload = "\(memberWalletAddress):\(message)"
        return Data(payload.utf8).base64EncodedString()
    }
}
