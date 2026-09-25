import Foundation

extension MonacoAPIClient {
    /// `GET /v1/assets/{symbol}/news`: headlines about one stock, newest first.
    public func getAssetNews(symbol: String) async throws -> NewsFeedDTO {
        try await getJSON(
            path: "v1/assets/\(symbol)/news",
            route: "/v1/assets/{symbol}/news",
            queryItems: [],
            as: NewsFeedDTO.self
        )
    }

    /// `GET /v1/news/market`: the day's market headlines for the Stocks tab.
    public func getMarketNews() async throws -> NewsFeedDTO {
        try await getJSON(
            path: "v1/news/market",
            route: "/v1/news/market",
            queryItems: [],
            as: NewsFeedDTO.self
        )
    }
}
