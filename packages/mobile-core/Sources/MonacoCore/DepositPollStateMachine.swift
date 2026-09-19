import Foundation

/// Shared poll cadence for inbound USDC balance and fund-to-cabal sweep status.
public enum DepositPolling {
    public static let balanceInterval: Duration = .seconds(3)
    public static let sweepStatusInterval: Duration = .seconds(3)
    /// Stop polling fund sweeps after this long; backend poller keeps working.
    public static let sweepMaxWait: Duration = .seconds(120)
}

/// Normalizes backend deposit status strings (`failed`, `failed: submit_sweep`, …).
public enum DepositStatusNormalizer {
    public static func isPending(_ status: String) -> Bool {
        status.lowercased() == "pending"
    }

    public static func isConfirmed(_ status: String) -> Bool {
        status.lowercased() == "confirmed"
    }

    public static func isFailed(_ status: String) -> Bool {
        let normalized = status.lowercased()
        return normalized == "failed" || normalized.hasPrefix("failed:")
    }
}

public enum DepositSweepPhase: Equatable {
    case idle
    case awaitingSweep
    case credited
    case failed(String)
}

/// Tracks deposit sweep status from backend DTO `status` strings.
public struct DepositPollStateMachine: Equatable {
    public private(set) var phase: DepositSweepPhase = .idle

    public init() {}

    public mutating func apply(status: String) {
        if DepositStatusNormalizer.isPending(status) {
            phase = .awaitingSweep
        } else if DepositStatusNormalizer.isConfirmed(status) {
            phase = .credited
        } else if DepositStatusNormalizer.isFailed(status) {
            phase = .failed(status)
        } else {
            phase = .failed(status)
        }
    }

    /// Polls `fetchStatus` until the deposit reaches a terminal phase or `maxWait` elapses.
    public mutating func pollUntilTerminal(
        maxWait: Duration = DepositPolling.sweepMaxWait,
        interval: Duration = DepositPolling.sweepStatusInterval,
        fetchStatus: () async throws -> String
    ) async -> DepositSweepPhase {
        let deadline = ContinuousClock.now + maxWait
        while ContinuousClock.now < deadline {
            if Task.isCancelled {
                return phase
            }
            do {
                apply(status: try await fetchStatus())
                if isTerminal {
                    return phase
                }
            } catch {
                // Keep last known phase; retry on transient auth/network/decode errors.
            }
            try? await Task.sleep(for: interval)
        }
        return phase
    }

    public var isTerminal: Bool {
        switch phase {
        case .credited, .failed:
            true
        case .idle, .awaitingSweep:
            false
        }
    }
}

public struct DepositStatusDTO: Decodable, Equatable {
    public let depositId: String
    public let status: String
    public let shareUnits: Int64

    public init(depositId: String, status: String, shareUnits: Int64) {
        self.depositId = depositId
        self.status = status
        self.shareUnits = shareUnits
    }
}
