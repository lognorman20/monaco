import Foundation
import MonacoCore

/// The reads behind the portfolio and history screens. A protocol so the sample harness and
/// the tests can stand in for the backend.
@MainActor
protocol PortfolioService {
    func portfolio() async throws -> PortfolioDTO
    func history(filter: HistoryFilter, cursor: String?) async throws -> HistoryPageDTO
    /// The history as a CSV file's bytes.
    func exportCSV(filter: HistoryFilter) async throws -> Data
}

/// The live reads, over MonacoCore's client with the signed-in member's token. A rejected
/// session signs the member out, the same way every other screen's reads do.
@MainActor
struct LivePortfolioService: PortfolioService {
    let auth: PrivyAuthService

    /// One screenful, and a size that keeps the infinite scroll a step ahead of the thumb.
    static let pageSize = 30

    func portfolio() async throws -> PortfolioDTO {
        try await auth.withAccessToken { try await client($0).getPortfolio() }
    }

    func history(filter: HistoryFilter, cursor: String?) async throws -> HistoryPageDTO {
        try await auth.withAccessToken { try await client($0).getHistory(filter: filter, cursor: cursor, limit: Self.pageSize) }
    }

    func exportCSV(filter: HistoryFilter) async throws -> Data {
        try await auth.withAccessToken { try await client($0).exportHistoryCSV(filter: filter) }
    }

    /// The token is read per call, not captured once: a screen can outlive the token it was
    /// opened with.
    private func client(_ token: String) -> MonacoCore.MonacoAPIClient {
        MonacoCore.MonacoAPIClient(baseURL: Config.apiBaseURL, accessTokenProvider: { token })
    }
}

/// Words for a read that failed, without a status code or a stack of jargon.
enum PortfolioFailureCopy {
    static func message(for error: Error) -> String {
        if error is URLError { return "Check your connection and try again." }
        let nsError = error as NSError
        if nsError.domain == NSURLErrorDomain { return "Check your connection and try again." }
        return "Monaco didn't answer just now. Try again in a moment."
    }
}
