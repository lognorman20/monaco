import Foundation

/// Ensures each failed deposit id toasts at most once per app launch.
enum DepositFailureToastTracker {
    private static var toastedDepositIDs: Set<String> = []

    static func consumeNewFailures(from items: [GroupActivityItemDTO]) -> [GroupActivityItemDTO] {
        let fresh = items.filter(isFailedDeposit).filter { !toastedDepositIDs.contains($0.id) }
        for item in fresh {
            toastedDepositIDs.insert(item.id)
        }
        return fresh
    }

    static func isFailedDeposit(_ item: GroupActivityItemDTO) -> Bool {
        guard item.kind.lowercased() == "deposit" else { return false }
        switch item.status.lowercased() {
        case "confirmed", "pending":
            return false
        default:
            return true
        }
    }

    static func message(for item: GroupActivityItemDTO) -> String {
        let dollars = Double(item.amountMicros) / 1_000_000.0
        let amount = String(format: "$%.2f", dollars)
        let status = item.status.trimmingCharacters(in: .whitespacesAndNewlines)
        if status.lowercased() == "failed" || status.isEmpty {
            return "Deposit failed — \(amount) wasn't swept into the club vault."
        }
        return "Deposit failed — \(amount). (\(status))"
    }
}
