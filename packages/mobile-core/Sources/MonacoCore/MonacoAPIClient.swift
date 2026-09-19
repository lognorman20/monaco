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
    /// 4xx with a server `{"error": "..."}` message meant for the user.
    case rejected(status: Int, message: String)
    /// 429. `retryAfterSeconds` comes from the `Retry-After` header when present.
    case rateLimited(retryAfterSeconds: Int?)
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

    public func platformBalance() async throws -> PlatformBalanceDTO {
        let url = baseURL.appending(path: "v1/me/balance")
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
        return try JSONDecoder().decode(PlatformBalanceDTO.self, from: data)
    }

    public func fundGroup(groupId: String, amount: Int64) async throws -> FundGroupResponseDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/fund")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(FundGroupRequestDTO(amount: amount))

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(FundGroupResponseDTO.self, from: data)
    }

    public func createPlatformWithdrawal(
        amount: Int64,
        toAddress: String
    ) async throws -> PlatformWithdrawalResponseDTO {
        let url = baseURL.appending(path: "v1/me/withdrawals")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(
            CreatePlatformWithdrawalRequestDTO(amount: amount, toAddress: toAddress)
        )

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(PlatformWithdrawalResponseDTO.self, from: data)
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

    /// `PATCH /v1/me` — set the signed-in user's display name.
    public func updateProfile(displayName: String) async throws -> MeDTO {
        let url = baseURL.appending(path: "v1/me")
        var request = URLRequest(url: url)
        request.httpMethod = "PATCH"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(UpdateProfileRequestDTO(displayName: displayName))

        let (data, response) = try await session.data(for: request)
        try Self.requireOK(response, data: data)
        return try JSONDecoder().decode(MeDTO.self, from: data)
    }

    /// `POST /v1/me/profile-photo` — multipart field `photo`; jpeg, png, or webp up to 2MB.
    public func uploadProfilePhoto(imageData: Data, mimeType: String) async throws -> MeDTO {
        let boundary = "Boundary-\(UUID().uuidString)"
        let url = baseURL.appending(path: "v1/me/profile-photo")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = ProfilePhotoMultipart.body(imageData: imageData, mimeType: mimeType, boundary: boundary)

        let (data, response) = try await session.data(for: request)
        try Self.requireOK(response, data: data)
        return try JSONDecoder().decode(MeDTO.self, from: data)
    }

    /// Maps non-200 responses to `MonacoAPIError`, keeping server copy for 4xx.
    static func requireOK(_ response: URLResponse, data: Data) throws {
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode != 200 else { return }
        if http.statusCode == 429 {
            let retryAfter = http.value(forHTTPHeaderField: "Retry-After").flatMap { Int($0) }
            throw MonacoAPIError.rateLimited(retryAfterSeconds: retryAfter)
        }
        if (400..<500).contains(http.statusCode), http.statusCode != 401,
           let body = try? JSONDecoder().decode(APIErrorBody.self, from: data),
           !body.error.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            throw MonacoAPIError.rejected(status: http.statusCode, message: body.error)
        }
        throw MonacoAPIError.httpStatus(http.statusCode)
    }

    private struct APIErrorBody: Decodable {
        let error: String
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
        return try monacoISO8601JSONDecoder().decode(HomeDashboardDTO.self, from: data)
    }

    public func getHomePnLSeries(range: HomeLeaderboardRange = .oneHour) async throws -> HomePnLSeriesDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/home/pnl-series"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [
            URLQueryItem(name: "range", value: range.rawValue),
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
        return try monacoISO8601JSONDecoder().decode(HomePnLSeriesDTO.self, from: data)
    }

    public func getHomeMissedProposals() async throws -> HomeMissedProposalsDTO {
        let url = baseURL.appending(path: "v1/home/missed-proposals")
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
        return try monacoISO8601JSONDecoder().decode(HomeMissedProposalsDTO.self, from: data)
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

    public func listMarketAssets(
        query: String = "",
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

    public func getPopularAssets(limit: Int = 10) async throws -> PopularAssetsResponseDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/assets/popular"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [
            URLQueryItem(name: "limit", value: String(limit)),
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
        return try JSONDecoder().decode(PopularAssetsResponseDTO.self, from: data)
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

    public func getMarketAssetChart(
        symbol: String,
        range: AssetChartRange = .oneDay
    ) async throws -> AssetChartDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/assets/\(symbol)/chart"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [
            URLQueryItem(name: "range", value: range.rawValue),
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
        return try JSONDecoder().decode(AssetChartDTO.self, from: data)
    }

    public func postQuote(
        groupId: String,
        symbol: String,
        usdc: Int64? = nil,
        kind: String = "buy",
        tokenAmount: Int64? = nil
    ) async throws -> BuyQuoteDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/quotes")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(
            QuoteRequestDTO(symbol: symbol, kind: kind, usdc: usdc, tokenAmount: tokenAmount)
        )

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(BuyQuoteDTO.self, from: data)
    }

    public func createProposal(
        groupId: String,
        symbol: String,
        usdc: Int64? = nil,
        kind: String = "buy",
        tokenAmount: Int64? = nil,
        thesis: String? = nil
    ) async throws -> CreateProposalResponseDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/proposals")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(
            ProposalRequestDTO(symbol: symbol, kind: kind, usdc: usdc, tokenAmount: tokenAmount, thesis: thesis)
        )

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

    public func listGroupProposals(groupId: String, tab: ProposalFeedTab) async throws -> ProposalListResponseDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/groups/\(groupId)/proposals"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [URLQueryItem(name: "tab", value: tab.rawValue)]
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
        return try JSONDecoder().decode(ProposalListResponseDTO.self, from: data)
    }

    public func getProposalDetail(proposalId: String) async throws -> ProposalDTO {
        let url = baseURL.appending(path: "v1/proposals/\(proposalId)")
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
        return try JSONDecoder().decode(ProposalDTO.self, from: data)
    }

    public func listProposalComments(proposalId: String) async throws -> ProposalCommentsResponseDTO {
        let url = baseURL.appending(path: "v1/proposals/\(proposalId)/comments")
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
        return try JSONDecoder().decode(ProposalCommentsResponseDTO.self, from: data)
    }

    /// Posts a top-level comment, or a reply when `parentId` is set. Server trims and validates the body.
    public func postProposalComment(proposalId: String, body: String, parentId: String? = nil) async throws -> ProposalCommentDTO {
        let url = baseURL.appending(path: "v1/proposals/\(proposalId)/comments")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(CommentRequestDTO(body: body, parentId: parentId))

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 201 || http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(ProposalCommentDTO.self, from: data)
    }

    public func leaveGroup(groupId: String, withdrawStake: Bool = false) async throws {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/leave")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(LeaveGroupRequestDTO(withdrawStake: withdrawStake))
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw MonacoAPIError.invalidResponse }
        switch http.statusCode {
        case 204: return
        case 409: throw MonacoAPIError.leaveBlocked(parseLeaveConflict(from: data))
        default: throw MonacoAPIError.httpStatus(http.statusCode)
        }
    }

    public func withdrawToBalance(groupId: String, shareAmountMicros: Int64? = nil) async throws -> WithdrawToBalanceJobDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/withdraw-to-balance")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(WithdrawToBalanceRequestDTO(shareAmountMicros: shareAmountMicros))
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw MonacoAPIError.invalidResponse }
        guard http.statusCode == 200 else { throw MonacoAPIError.httpStatus(http.statusCode) }
        return try JSONDecoder().decode(WithdrawToBalanceJobDTO.self, from: data)
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

    // MARK: Groups tab (#148)

    /// Case-insensitive name search. `query` must be 2...64 characters after
    /// trimming (see `GroupSearchQuery`); pass the previous page's
    /// `nextCursor` to continue.
    public func searchGroups(query: String, limit: Int = 20, cursor: String? = nil) async throws -> GroupSearchResponseDTO {
        var items = [
            URLQueryItem(name: "q", value: query),
            URLQueryItem(name: "limit", value: String(limit)),
        ]
        if let cursor {
            items.append(URLQueryItem(name: "cursor", value: cursor))
        }
        return try await getJSON(path: "v1/groups/search", queryItems: items, as: GroupSearchResponseDTO.self)
    }

    /// Platform-wide cabals ranked by percent return (server caps limit at 50).
    public func groupLeaderboard(limit: Int = 20) async throws -> GroupLeaderboardResponseDTO {
        try await getJSON(
            path: "v1/groups/leaderboard",
            queryItems: [URLQueryItem(name: "limit", value: String(limit))],
            as: GroupLeaderboardResponseDTO.self
        )
    }

    /// One P&L series per cabal the viewer belongs to, in a single request.
    public func myGroupsPnLHistory(range: GroupPnLRange = .oneMonth) async throws -> MyGroupsPnLHistoryDTO {
        try await getJSON(
            path: "v1/groups/pnl-history",
            queryItems: [URLQueryItem(name: "range", value: range.rawValue)],
            as: MyGroupsPnLHistoryDTO.self
        )
    }

    /// P&L series for one cabal; readable by any signed-in user.
    public func groupPnLHistory(groupId: String, range: GroupPnLRange = .oneMonth) async throws -> GroupPnLSeriesDTO {
        try await getJSON(
            path: "v1/groups/\(groupId)/pnl-history",
            queryItems: [URLQueryItem(name: "range", value: range.rawValue)],
            as: GroupPnLSeriesDTO.self
        )
    }

    private func getJSON<T: Decodable>(path: String, queryItems: [URLQueryItem], as type: T.Type) async throws -> T {
        var components = URLComponents(url: baseURL.appending(path: path), resolvingAgainstBaseURL: false)!
        components.queryItems = queryItems
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
        return try monacoISO8601JSONDecoder().decode(T.self, from: data)
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
        let kind: String?
        let usdc: Int64?
        let tokenAmount: Int64?

        enum CodingKeys: String, CodingKey {
            case symbol, kind, usdc, tokenAmount
        }

        func encode(to encoder: Encoder) throws {
            var container = encoder.container(keyedBy: CodingKeys.self)
            try container.encode(symbol, forKey: .symbol)
            if let kind { try container.encode(kind, forKey: .kind) }
            if let usdc { try container.encode(usdc, forKey: .usdc) }
            if let tokenAmount { try container.encode(tokenAmount, forKey: .tokenAmount) }
        }
    }

    private struct ProposalRequestDTO: Encodable {
        let symbol: String
        let kind: String?
        let usdc: Int64?
        let tokenAmount: Int64?
        let thesis: String?

        enum CodingKeys: String, CodingKey {
            case symbol, kind, usdc, tokenAmount, thesis
        }

        func encode(to encoder: Encoder) throws {
            var container = encoder.container(keyedBy: CodingKeys.self)
            try container.encode(symbol, forKey: .symbol)
            if let kind { try container.encode(kind, forKey: .kind) }
            if let usdc { try container.encode(usdc, forKey: .usdc) }
            if let tokenAmount { try container.encode(tokenAmount, forKey: .tokenAmount) }
            if let thesis { try container.encode(thesis, forKey: .thesis) }
        }
    }

    private struct VoteRequestDTO: Encodable {
        let choice: String
    }

    private struct CommentRequestDTO: Encodable {
        let body: String
        let parentId: String?
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
