import Foundation
import MonacoCore
import os

/// Writes every API request to the `api` log category: failures at error, slow requests
/// at notice, everything else at debug. Request ids are public so a sysdiagnose can be
/// matched to server logs; the rest keeps the default privacy.
struct APILogTelemetry: APITelemetry {
    static let slowRequestThresholdMs: Double = 2_000

    private let logger: Logger

    init(logger: Logger = AppLogger.api) {
        self.logger = logger
    }

    func record(_ event: APIRequestEvent) {
        let outcome = Self.describe(event.outcome)
        let durationMs = Int(event.durationMs.rounded())
        let serverRequestID = event.serverRequestID ?? "-"

        if event.isFailure {
            logger.error("\(event.method) \(event.route) failed: \(outcome) in \(durationMs)ms id=\(event.requestID, privacy: .public) server_id=\(serverRequestID, privacy: .public)")
        } else if event.durationMs > Self.slowRequestThresholdMs {
            logger.notice("\(event.method) \(event.route) slow: \(outcome) in \(durationMs)ms id=\(event.requestID, privacy: .public) server_id=\(serverRequestID, privacy: .public)")
        } else {
            logger.debug("\(event.method) \(event.route) \(outcome) in \(durationMs)ms id=\(event.requestID, privacy: .public) server_id=\(serverRequestID, privacy: .public)")
        }
    }

    private static func describe(_ outcome: APIRequestOutcome) -> String {
        switch outcome {
        case .status(let code): return "status \(code)"
        case .transportError(let category): return "transport \(category.rawValue)"
        }
    }
}
