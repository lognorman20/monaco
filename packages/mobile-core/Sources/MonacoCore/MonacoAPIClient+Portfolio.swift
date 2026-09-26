import Foundation

/// The member's portfolio across cabals and their money history.
extension MonacoAPIClient {
    /// `GET /v1/me/portfolio`: the member's slice of every cabal, by stock.
    public func getPortfolio() async throws -> PortfolioDTO {
        try await getJSON(path: "v1/me/portfolio", route: "/v1/me/portfolio", queryItems: [], as: PortfolioDTO.self)
    }

    /// `GET /v1/me/transactions`: one page of history, newest first. Pass the previous page's
    /// `nextCursor` to continue.
    public func getHistory(filter: HistoryFilter = .all, cursor: String? = nil, limit: Int = 30) async throws -> HistoryPageDTO {
        try await getJSON(
            path: "v1/me/transactions",
            route: "/v1/me/transactions",
            queryItems: Self.historyQuery(filter: filter, cursor: cursor, limit: limit),
            as: HistoryPageDTO.self
        )
    }

    /// `GET /v1/me/transactions/export.csv`: every row the filter keeps, as the CSV file's bytes.
    public func exportHistoryCSV(filter: HistoryFilter = .all) async throws -> Data {
        var components = URLComponents(url: baseURL.appending(path: "v1/me/transactions/export.csv"), resolvingAgainstBaseURL: false)!
        components.queryItems = [URLQueryItem(name: "type", value: filter.rawValue)]
        guard let url = components.url else { throw MonacoAPIError.invalidResponse }
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.setValue("text/csv", forHTTPHeaderField: "Accept")
        try await applyAuthorizationHeader(to: &request)
        // A long history is one response the server builds by walking every page, so it gets
        // a file's budget rather than a poll's.
        let response = try await send(request, route: "/v1/me/transactions/export.csv", timeout: MonacoRequestTimeout.upload)
        return response.data
    }

    static func historyQuery(filter: HistoryFilter, cursor: String?, limit: Int) -> [URLQueryItem] {
        var items = [
            URLQueryItem(name: "type", value: filter.rawValue),
            URLQueryItem(name: "limit", value: String(max(1, min(limit, 100)))),
        ]
        if let cursor = cursor?.trimmingCharacters(in: .whitespacesAndNewlines), !cursor.isEmpty {
            items.append(URLQueryItem(name: "cursor", value: cursor))
        }
        return items
    }
}
