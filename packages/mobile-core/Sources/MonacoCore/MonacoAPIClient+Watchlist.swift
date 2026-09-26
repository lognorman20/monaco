import Foundation

/// The member's watchlist and price alerts. Every route answers with the full error mapping,
/// so a screen can tell "the list is full" (409) from "no such stock" (404) from a failure.
extension MonacoAPIClient {
    /// `GET /v1/me/watchlist` — market rows in the member's order.
    public func getWatchlist() async throws -> WatchlistResponseDTO {
        var request = URLRequest(url: baseURL.appending(path: "v1/me/watchlist"))
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/me/watchlist", mapping: .full)
        return try JSONDecoder().decode(WatchlistResponseDTO.self, from: response.data)
    }

    /// `PUT /v1/me/watchlist/{symbol}` — puts the stock at the end, or leaves it where it is
    /// when it is already there. Safe to repeat.
    @discardableResult
    public func addToWatchlist(symbol: String) async throws -> WatchlistEntryDTO {
        var request = URLRequest(url: baseURL.appending(path: "v1/me/watchlist").appending(path: symbol))
        request.httpMethod = "PUT"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/me/watchlist/{symbol}", accepting: [200, 201], mapping: .full)
        return try JSONDecoder().decode(WatchlistEntryDTO.self, from: response.data)
    }

    /// `DELETE /v1/me/watchlist/{symbol}` — 204 whether or not it was there.
    public func removeFromWatchlist(symbol: String) async throws {
        var request = URLRequest(url: baseURL.appending(path: "v1/me/watchlist").appending(path: symbol))
        request.httpMethod = "DELETE"
        try await applyAuthorizationHeader(to: &request)

        _ = try await send(request, route: "/v1/me/watchlist/{symbol}", accepting: [200, 204], mapping: .full)
    }

    /// `PUT /v1/me/watchlist` — `symbols` names every stock on the watchlist once, in the new
    /// order. A 409 means the list changed elsewhere; reload before trying again.
    @discardableResult
    public func reorderWatchlist(symbols: [String]) async throws -> [String] {
        var request = URLRequest(url: baseURL.appending(path: "v1/me/watchlist"))
        request.httpMethod = "PUT"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(WatchlistOrderDTO(symbols: symbols))

        let response = try await send(request, route: "/v1/me/watchlist", mapping: .full)
        return try JSONDecoder().decode(WatchlistOrderDTO.self, from: response.data).symbols
    }

    /// `GET /v1/me/alerts` — every alert, or one stock's when `symbol` is set.
    public func listPriceAlerts(symbol: String? = nil) async throws -> PriceAlertsResponseDTO {
        var components = URLComponents(url: baseURL.appending(path: "v1/me/alerts"), resolvingAgainstBaseURL: false)!
        if let symbol, !symbol.trimmingCharacters(in: .whitespaces).isEmpty {
            components.queryItems = [URLQueryItem(name: "symbol", value: symbol)]
        }
        guard let url = components.url else { throw MonacoAPIError.invalidResponse }
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/me/alerts", mapping: .full)
        return try JSONDecoder().decode(PriceAlertsResponseDTO.self, from: response.data)
    }

    /// `POST /v1/me/alerts` — 409 past twenty waiting alerts, 422 when the price is already
    /// past the line.
    public func createPriceAlert(
        symbol: String,
        direction: PriceAlertDirection,
        priceUsdcMicros: Int64
    ) async throws -> PriceAlertDTO {
        var request = URLRequest(url: baseURL.appending(path: "v1/me/alerts"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(
            CreatePriceAlertRequestDTO(symbol: symbol, direction: direction, priceUsdcMicros: priceUsdcMicros)
        )

        let response = try await send(request, route: "/v1/me/alerts", accepting: [201], mapping: .full)
        return try JSONDecoder().decode(PriceAlertDTO.self, from: response.data)
    }

    /// `DELETE /v1/me/alerts/{id}` — waiting or fired.
    public func deletePriceAlert(id: String) async throws {
        var request = URLRequest(url: baseURL.appending(path: "v1/me/alerts").appending(path: id))
        request.httpMethod = "DELETE"
        try await applyAuthorizationHeader(to: &request)

        _ = try await send(request, route: "/v1/me/alerts/{id}", accepting: [204], mapping: .full)
    }
}
