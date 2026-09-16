import Foundation

enum DepositSweepPhase: Equatable {
    case idle
    case awaitingSweep
    case credited
    case failed(String)
}

/// Tracks deposit sweep status from backend DTO `status` strings.
struct DepositPollStateMachine: Equatable {
    private(set) var phase: DepositSweepPhase = .idle

    mutating func apply(status: String) {
        switch status.lowercased() {
        case "pending":
            phase = .awaitingSweep
        case "confirmed":
            phase = .credited
        default:
            phase = .failed(status)
        }
    }

    var statusCopy: String {
        switch phase {
        case .idle:
            "Add money to your club's pot."
        case .awaitingSweep:
            "Sweep in progress — we'll credit your share when USDC lands in the treasury."
        case .credited:
            "Added to the pot! Your share is updated."
        case .failed(let raw):
            "Unexpected deposit status: \(raw)"
        }
    }

    var isTerminal: Bool {
        switch phase {
        case .credited, .failed:
            true
        case .idle, .awaitingSweep:
            false
        }
    }
}
