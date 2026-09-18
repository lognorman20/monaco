import Foundation

public enum LeaveGroupBlockReason: String, Equatable {
    case shareUnitsRemaining = "share_units_remaining"
    case lastMemberWithTreasury = "last_member_with_treasury"
    case pendingRedeem = "pending_redeem"
    case soleRemainingVote = "sole_remaining_vote"
    case creatorMustTransfer = "creator_must_transfer"
    case unknown
}

public enum MonacoAPIError: Error, Equatable {
    case invalidResponse
    case httpStatus(Int)
    case leaveBlocked(LeaveGroupBlockReason)
}

public typealias AccessTokenProvider = @Sendable () async throws -> String?

public final class MonacoAPIClient: @unchecked Sendable {
    private let baseURL: URL
    private let session: URLSession
    private let accessTokenProvider: AccessTokenProvider?

    public init(
        baseURL: URL = MonacoConfig.defaultAPIBaseURL,
        session: URLSession = .shared,
        accessTokenProvider: AccessTokenProvider? = nil
    ) {
        self.baseURL = baseURL
        self.session = session
        self.accessTokenProvider = accessTokenProvider
    }

    public func me() async throws -> MeDTO {
        let url = baseURL.appending(path: "v1/me")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(MeDTO.self, from: data)
    }

    public func getHome() async throws -> HomeViewDTO {
        let url = baseURL.appending(path: "v1/home")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(HomeViewDTO.self, from: data)
    }

    public func getHomeDashboard(leaderboardRange: HomeLeaderboardRange = .all) async throws -> HomeDashboardDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/home/dashboard"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [
            URLQueryItem(name: "leaderboardRange", value: leaderboardRange.rawValue),
        ]
        var request = URLRequest(url: components.url!)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return try decoder.decode(HomeDashboardDTO.self, from: data)
    }

    public func listMarketAssets(
        query: String,
        limit: Int = 25,
        offset: Int = 0
    ) async throws -> ListMarketAssetsResponseDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/assets"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [
            URLQueryItem(name: "query", value: query),
            URLQueryItem(name: "limit", value: String(limit)),
            URLQueryItem(name: "offset", value: String(offset)),
        ]
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(ListMarketAssetsResponseDTO.self, from: data)
    }

    public func getPopularMarketAssets(limit: Int = 10) async throws -> PopularMarketAssetsResponseDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/assets/popular"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [URLQueryItem(name: "limit", value: String(limit))]
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(PopularMarketAssetsResponseDTO.self, from: data)
    }

    public func getMarketAsset(symbol: String) async throws -> AssetDetailDTO {
        let url = baseURL.appending(path: "v1/assets/\(symbol)")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(AssetDetailDTO.self, from: data)
    }

    public func getMarketAssetChart(symbol: String, range: AssetChartRange) async throws -> AssetChartResponseDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/assets/\(symbol)/chart"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [URLQueryItem(name: "range", value: range.rawValue)]
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(AssetChartResponseDTO.self, from: data)
    }

    public func searchGroups(query: String, limit: Int = 25, offset: Int = 0) async throws -> GroupSearchResponseDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/groups/search"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [
            URLQueryItem(name: "q", value: query),
            URLQueryItem(name: "limit", value: String(limit)),
            URLQueryItem(name: "offset", value: String(offset)),
        ]
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(GroupSearchResponseDTO.self, from: data)
    }

    public func getGroupLeaderboard(limit: Int = 25, offset: Int = 0) async throws -> GroupLeaderboardResponseDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/groups/leaderboard"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [
            URLQueryItem(name: "limit", value: String(limit)),
            URLQueryItem(name: "offset", value: String(offset)),
        ]
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(GroupLeaderboardResponseDTO.self, from: data)
    }

    public func getGroupPnLHistory(groupId: String, days: Int = 90) async throws -> GroupPnLHistoryDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/groups/\(groupId)/pnl-history"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [URLQueryItem(name: "days", value: String(days))]
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(GroupPnLHistoryDTO.self, from: data)
    }

ning = "share_units_remaining"
    case lastMemberWithTreasury = "last_member_with_treasury"
    case pendingRedeem = "pending_redeem"
    case soleRemainingVote = "sole_remaining_vote"
    case creatorMustTransfer = "creator_must_transfer"
    case unknown
}

public enum MonacoAPIError: Error, Equatable {
    case invalidResponse
    case httpStatus(Int)
    case leaveBlocked(LeaveGroupBlockReason)
}

public typealias AccessTokenProvider = @Sendable () async throws -> String?

public final class MonacoAPIClient: @unchecked Sendable {
    private let baseURL: URL
    private let session: URLSession
    private let accessTokenProvider: AccessTokenProvider?

    public init(
        baseURL: URL = MonacoConfig.defaultAPIBaseURL,
        session: URLSession = .shared,
        accessTokenProvider: AccessTokenProvider? = nil
    ) {
        self.baseURL = baseURL
        self.session = session
        self.accessTokenProvider = accessTokenProvider
    }

    public func me() async throws -> MeDTO {
        let url = baseURL.appending(path: "v1/me")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(MeDTO.self, from: data)
    }

    public func getHome() async throws -> HomeViewDTO {
        let url = baseURL.appending(path: "v1/home")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(HomeViewDTO.self, from: data)
    }

<<<<<<< HEAD
    public func listMarketAssets(
        query: String,
        limit: Int = 25,
        offset: Int = 0
    ) async throws -> ListMarketAssetsResponseDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/assets"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [
            URLQueryItem(name: "query", value: query),
            URLQueryItem(name: "limit", value: String(limit)),
            URLQueryItem(name: "offset", value: String(offset)),
        ]
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }

        var request = URLRequest(url: url)
=======
    public func getHomeDashboard(leaderboardRange: HomeLeaderboardRange = .all) async throws -> HomeDashboardDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/home/dashboard"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [
            URLQueryItem(name: "leaderboardRange", value: leaderboardRange.rawValue),
        ]
        var request = URLRequest(url: components.url!)
>>>>>>> c7069b8 (feat(157): ship Home dashboard with net worth, chart, leaderboard, missed votes)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
<<<<<<< HEAD
        return try JSONDecoder().decode(ListMarketAssetsResponseDTO.self, from: data)
    }

    public func getPopularMarketAssets(limit: Int = 10) async throws -> PopularMarketAssetsResponseDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/assets/popular"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [URLQueryItem(name: "limit", value: String(limit))]
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(PopularMarketAssetsResponseDTO.self, from: data)
    }

    public func getMarketAsset(symbol: String) async throws -> AssetDetailDTO {
        let url = baseURL.appending(path: "v1/assets/\(symbol)")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(AssetDetailDTO.self, from: data)
    }

    public func getMarketAssetChart(symbol: String, range: AssetChartRange) async throws -> AssetChartResponseDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/assets/\(symbol)/chart"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [URLQueryItem(name: "range", value: range.rawValue)]
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(AssetChartResponseDTO.self, from: data)
    }

    public func searchGroups(query: String, limit: Int = 25, offset: Int = 0) async throws -> GroupSearchResponseDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/groups/search"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [
            URLQueryItem(name: "q", value: query),
            URLQueryItem(name: "limit", value: String(limit)),
            URLQueryItem(name: "offset", value: String(offset)),
        ]
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(GroupSearchResponseDTO.self, from: data)
    }

    public func getGroupLeaderboard(limit: Int = 25, offset: Int = 0) async throws -> GroupLeaderboardResponseDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/groups/leaderboard"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [
            URLQueryItem(name: "limit", value: String(limit)),
            URLQueryItem(name: "offset", value: String(offset)),
        ]
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(GroupLeaderboardResponseDTO.self, from: data)
    }

    public func getGroupPnLHistory(groupId: String, days: Int = 90) async throws -> GroupPnLHistoryDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/groups/\(groupId)/pnl-history"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [URLQueryItem(name: "days", value: String(days))]
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(GroupPnLHistoryDTO.self, from: data)
=======
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return try decoder.decode(HomeDashboardDTO.self, from: data)
>>>>>>> c7069b8 (feat(157): ship Home dashboard with net worth, chart, leaderboard, missed votes)
    }

    public func searchAssets(
        groupId: String,
        query: String,
        limit: Int = 25,
        offset: Int = 0
    ) async throws -> SearchAssetsResponseDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/groups/\(groupId)/assets"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [
            URLQueryItem(name: "query", value: query),
            URLQueryItem(name: "limit", value: String(limit)),
            URLQueryItem(name: "offset", value: String(offset)),
        ]
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(SearchAssetsResponseDTO.self, from: data)
    }

    public func postQuote(groupId: String, symbol: String, usdc: Int64) async throws -> BuyQuoteDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/quotes")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(QuoteRequestDTO(symbol: symbol, usdc: usdc))

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(BuyQuoteDTO.self, from: data)
    }

    public func createProposal(groupId: String, symbol: String, usdc: Int64) async throws -> CreateProposalResponseDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/proposals")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(ProposalRequestDTO(symbol: symbol, usdc: usdc))

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(CreateProposalResponseDTO.self, from: data)
    }

    public func castVote(proposalId: String, choice: String) async throws {
        let url = baseURL.appending(path: "v1/proposals/\(proposalId)/votes")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(VoteRequestDTO(choice: choice))

        let (_, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 || http.statusCode == 204 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
    }

    public func leaveGroup(groupId: String) async throws {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/leave")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        try await applyAuthorizationHeader(to: &request)
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw MonacoAPIError.invalidResponse }
        switch http.statusCode {
        case 204: return
        case 409: throw MonacoAPIError.leaveBlocked(parseLeaveConflict(from: data))
        default: throw MonacoAPIError.httpStatus(http.statusCode)
        }
    }

    public func getGroupView(groupId: String) async throws -> GroupViewDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/view")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(GroupViewDTO.self, from: data)
    }

    public func postRedeem(
        groupId: String,
        shareUnits: String,
        payoutAddress: String,
        payoutProof: String
    ) async throws -> RedeemJobDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/redeems")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(
            RedeemRequestDTO(
                shareUnits: shareUnits,
                payoutAddress: payoutAddress,
                payoutProof: payoutProof
            )
        )

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(RedeemJobDTO.self, from: data)
    }

    public func devBuy(groupId: String, symbol: String, usdc: Int64) async throws -> DevBuyResponseDTO {
        let url = baseURL.appending(path: "v1/dev/groups/\(groupId)/buy")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(DevBuyRequestDTO(symbol: symbol, usdc: usdc))

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(DevBuyResponseDTO.self, from: data)
    }

    private struct QuoteRequestDTO: Encodable {
        let symbol: String
        let usdc: Int64
    }

    private struct ProposalRequestDTO: Encodable {
        let symbol: String
        let usdc: Int64
    }

    private struct VoteRequestDTO: Encodable {
        let choice: String
    }

    private func parseLeaveConflict(from data: Data) -> LeaveGroupBlockReason {
        struct Body: Decodable { let reason: String? }
        guard let body = try? JSONDecoder().decode(Body.self, from: data), let reason = body.reason, let parsed = LeaveGroupBlockReason(rawValue: reason) else { return .unknown }
        return parsed
    }

    private func applyAuthorizationHeader(to request: inout URLRequest) async throws {
        guard let accessTokenProvider else { return }
        guard let token = try await accessTokenProvider(), !token.isEmpty else { return }
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
    }
}
