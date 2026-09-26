import Foundation

public enum LeaveGroupBlockReason: String, Equatable {
    case shareUnitsRemaining = "share_units_remaining"
    case lastMemberWithTreasury = "last_member_with_treasury"
    case pendingRedeem = "pending_redeem"
    case soleRemainingVote = "sole_remaining_vote"
    case creatorMustTransfer = "creator_must_transfer"
    case unknown
}

/// Failures the API answered with carry the `X-Request-Id` the request was sent under,
/// so an error state can show a support reference that matches the server logs.
public enum MonacoAPIError: Error, Equatable {
    case invalidResponse
    case httpStatus(Int, requestID: String? = nil)
    case leaveBlocked(LeaveGroupBlockReason)
    /// 4xx with a server `{"error": "..."}` message meant for the user.
    case rejected(status: Int, message: String, requestID: String? = nil)
    /// 429. `retryAfterSeconds` comes from the `Retry-After` header when present.
    case rateLimited(retryAfterSeconds: Int?, requestID: String? = nil)

    /// The status the server answered with, for callers that only care about the code.
    /// Prefer this over matching `.httpStatus`: the same status can arrive as `.rejected`
    /// or `.rateLimited` when the response carried more than a code.
    public var statusCode: Int? {
        switch self {
        case .httpStatus(let status, _), .rejected(let status, _, _):
            return status
        case .rateLimited:
            return 429
        case .invalidResponse, .leaveBlocked:
            return nil
        }
    }

    public var requestID: String? {
        switch self {
        case .httpStatus(_, let requestID),
             .rejected(_, _, let requestID),
             .rateLimited(_, let requestID):
            return requestID
        case .invalidResponse, .leaveBlocked:
            return nil
        }
    }

    /// Two errors are equal when they describe the same failure. The request id names
    /// one attempt, not the failure, so it is left out.
    public static func == (lhs: MonacoAPIError, rhs: MonacoAPIError) -> Bool {
        switch (lhs, rhs) {
        case (.invalidResponse, .invalidResponse):
            return true
        case (.httpStatus(let a, _), .httpStatus(let b, _)):
            return a == b
        case (.leaveBlocked(let a), .leaveBlocked(let b)):
            return a == b
        case (.rejected(let aStatus, let aMessage, _), .rejected(let bStatus, let bMessage, _)):
            return aStatus == bStatus && aMessage == bMessage
        case (.rateLimited(let a, _), .rateLimited(let b, _)):
            return a == b
        default:
            return false
        }
    }
}

public typealias AccessTokenProvider = @Sendable () async throws -> String?

public final class MonacoAPIClient: @unchecked Sendable {
    // lane: watchlist — internal, so feature extensions in their own files can build requests.
    let baseURL: URL
    /// Every request goes through the transport so an expired access token is
    /// refreshed and the request retried once instead of surfacing a 401.
    private let session: MonacoHTTPTransport
    private let accessTokenProvider: AccessTokenProvider?

    /// - Parameter telemetry: receives one event per request; defaults to whatever is
    ///   registered in `APITelemetryRegistry.shared`.
    public convenience init(
        baseURL: URL = MonacoConfig.apiBaseURL,
        session: URLSession = .monaco,
        accessTokenProvider: AccessTokenProvider? = nil,
        telemetry: APITelemetry? = nil
    ) {
        self.init(
            baseURL: baseURL,
            transport: MonacoHTTPTransport(session: session, telemetry: telemetry),
            accessTokenProvider: accessTokenProvider
        )
    }

    public init(
        baseURL: URL = MonacoConfig.apiBaseURL,
        transport: MonacoHTTPTransport,
        accessTokenProvider: AccessTokenProvider? = nil
    ) {
        self.baseURL = baseURL
        self.session = transport
        self.accessTokenProvider = accessTokenProvider
    }

    public func platformBalance() async throws -> PlatformBalanceDTO {
        let url = baseURL.appending(path: "v1/me/balance")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/me/balance")
        return try JSONDecoder().decode(PlatformBalanceDTO.self, from: response.data)
    }

    public func fundGroup(groupId: String, amount: Int64, submission: IdempotentSubmission) async throws -> FundGroupResponseDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/fund")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try MonacoHTTPTransport.idempotentBodyEncoder().encode(FundGroupRequestDTO(amount: amount))

        let response = try await send(request, route: "/v1/groups/{id}/fund", submission: submission)
        return try JSONDecoder().decode(FundGroupResponseDTO.self, from: response.data)
    }

    public func createPlatformWithdrawal(
        amount: Int64,
        toAddress: String,
        submission: IdempotentSubmission
    ) async throws -> PlatformWithdrawalResponseDTO {
        let url = baseURL.appending(path: "v1/me/withdrawals")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try MonacoHTTPTransport.idempotentBodyEncoder().encode(
            CreatePlatformWithdrawalRequestDTO(amount: amount, toAddress: toAddress)
        )

        let response = try await send(request, route: "/v1/me/withdrawals", submission: submission)
        return try JSONDecoder().decode(PlatformWithdrawalResponseDTO.self, from: response.data)
    }

    public func me() async throws -> MeDTO {
        let url = baseURL.appending(path: "v1/me")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/me")
        return try JSONDecoder().decode(MeDTO.self, from: response.data)
    }

    /// `PATCH /v1/me` — set the signed-in user's display name.
    public func updateProfile(displayName: String) async throws -> MeDTO {
        let url = baseURL.appending(path: "v1/me")
        var request = URLRequest(url: url)
        request.httpMethod = "PATCH"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(UpdateProfileRequestDTO(displayName: displayName))

        let response = try await session.send(request, route: "/v1/me")
        try Self.requireOK(response)
        return try JSONDecoder().decode(MeDTO.self, from: response.data)
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

        let response = try await session.send(request, route: "/v1/me/profile-photo", timeout: MonacoRequestTimeout.upload)
        try Self.requireOK(response)
        return try JSONDecoder().decode(MeDTO.self, from: response.data)
    }

    /// `POST /v1/groups/{id}/picture` — multipart field `picture`; jpeg, png or
    /// webp up to 2MB. Only the cabal's creator may call it: a member who is not
    /// gets 403, anyone else 404.
    public func uploadCabalPicture(groupID: String, imageData: Data, mimeType: String) async throws -> CabalPictureDTO {
        let boundary = "Boundary-\(UUID().uuidString)"
        let route = "v1/groups/\(groupID)/picture"
        var request = URLRequest(url: baseURL.appending(path: route))
        request.httpMethod = "POST"
        request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = ImageUploadMultipart.body(
            fieldName: "picture",
            fileBaseName: "cabal",
            imageData: imageData,
            mimeType: mimeType,
            boundary: boundary
        )

        let response = try await session.send(
            request,
            route: "/v1/groups/{id}/picture",
            timeout: MonacoRequestTimeout.upload
        )
        try Self.requireOK(response)
        return try JSONDecoder().decode(CabalPictureDTO.self, from: response.data)
    }

    /// `DELETE /v1/groups/{id}/picture` — clears the picture, so the cabal falls
    /// back to its tinted initials. Same permission rule as the upload.
    public func removeCabalPicture(groupID: String) async throws -> CabalPictureDTO {
        let route = "v1/groups/\(groupID)/picture"
        var request = URLRequest(url: baseURL.appending(path: route))
        request.httpMethod = "DELETE"
        try await applyAuthorizationHeader(to: &request)

        let response = try await session.send(request, route: "/v1/groups/{id}/picture")
        try Self.requireOK(response)
        return try JSONDecoder().decode(CabalPictureDTO.self, from: response.data)
    }

    /// How much of a failed response a route keeps.
    enum ErrorMapping {
        /// Just the status. What most routes still do, because their callers pattern-match
        /// `.httpStatus(404)` and would silently stop matching on a richer case.
        case statusOnly
        /// Everything the response carried: the `Retry-After` on a 429, and a 4xx body
        /// written for members. Routes opt in once their callers read `statusCode`.
        case full
    }

    /// The one place a response becomes an error. Returns nil when the status is accepted.
    static func error(
        for response: MonacoHTTPResponse,
        accepting: Set<Int>,
        mapping: ErrorMapping
    ) -> MonacoAPIError? {
        guard let http = response.response as? HTTPURLResponse else {
            return .invalidResponse
        }
        guard !accepting.contains(http.statusCode) else { return nil }
        let requestID = response.requestID
        guard mapping == .full else {
            return .httpStatus(http.statusCode, requestID: requestID)
        }
        if http.statusCode == 429 {
            let retryAfter = http.value(forHTTPHeaderField: "Retry-After").flatMap { Int($0) }
            return .rateLimited(retryAfterSeconds: retryAfter, requestID: requestID)
        }
        if (400..<500).contains(http.statusCode), http.statusCode != 401,
           let body = try? JSONDecoder().decode(APIErrorBody.self, from: response.data),
           !body.error.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return .rejected(status: http.statusCode, message: body.error, requestID: requestID)
        }
        return .httpStatus(http.statusCode, requestID: requestID)
    }

    /// Maps non-200 responses to `MonacoAPIError`, keeping server copy for 4xx.
    static func requireOK(_ response: MonacoHTTPResponse) throws {
        if let error = error(for: response, accepting: [200], mapping: .full) { throw error }
    }

    /// Sends `request` under its route template and returns the response when the status
    /// is in `accepting`. Any other status throws, carrying as much of the answer as
    /// `mapping` allows plus the request id.
    /// Money POSTs pass their `submission` so the idempotency key rides along.
    // lane: watchlist — internal, see `baseURL`.
    func send(
        _ request: URLRequest,
        route: String,
        accepting: Set<Int> = [200],
        submission: IdempotentSubmission? = nil,
        mapping: ErrorMapping = .statusOnly,
        timeout: TimeInterval? = nil
    ) async throws -> MonacoHTTPResponse {
        let response = try await session.send(request, route: route, timeout: timeout, submission: submission)
        if let error = Self.error(for: response, accepting: accepting, mapping: mapping) {
            throw error
        }
        return response
    }

    private struct APIErrorBody: Decodable {
        let error: String
    }

    public func getHome() async throws -> HomeViewDTO {
        let url = baseURL.appending(path: "v1/home")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/home")
        return try JSONDecoder().decode(HomeViewDTO.self, from: response.data)
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

        let response = try await send(request, route: "/v1/home/dashboard")
        return try monacoISO8601JSONDecoder().decode(HomeDashboardDTO.self, from: response.data)
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

        let response = try await send(request, route: "/v1/home/pnl-series")
        return try monacoISO8601JSONDecoder().decode(HomePnLSeriesDTO.self, from: response.data)
    }

    public func getHomeMissedProposals() async throws -> HomeMissedProposalsDTO {
        let url = baseURL.appending(path: "v1/home/missed-proposals")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/home/missed-proposals")
        return try monacoISO8601JSONDecoder().decode(HomeMissedProposalsDTO.self, from: response.data)
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

        let response = try await send(request, route: "/v1/groups/{id}/assets")
        return try JSONDecoder().decode(SearchAssetsResponseDTO.self, from: response.data)
    }

    public func listMarketAssets(
        query: String = "",
        limit: Int = 25,
        offset: Int = 0,
        catalogKind: AssetKind? = nil
    ) async throws -> ListMarketAssetsResponseDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/assets"),
            resolvingAgainstBaseURL: false
        )!
        var items = [
            URLQueryItem(name: "limit", value: String(limit)),
            URLQueryItem(name: "offset", value: String(offset)),
        ]
        if !query.isEmpty {
            items.insert(URLQueryItem(name: "query", value: query), at: 0)
        }
        if let catalogKind {
            items.append(URLQueryItem(name: "kind", value: catalogKind.rawValue))
        }
        components.queryItems = items
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/assets")
        return try JSONDecoder().decode(ListMarketAssetsResponseDTO.self, from: response.data)
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

        let response = try await send(request, route: "/v1/assets/popular")
        return try JSONDecoder().decode(PopularAssetsResponseDTO.self, from: response.data)
    }

    /// What the caller's cabals hold and what they are voting on, for the Stocks
    /// tab's two social sections. One aggregate rather than a group-view call per
    /// cabal: the tab needs an answer before its first scroll, not N of them.
    public func getHeldAssets() async throws -> HeldAssetsResponseDTO {
        let url = baseURL.appending(path: "v1/assets/held")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/assets/held")
        return try JSONDecoder().decode(HeldAssetsResponseDTO.self, from: response.data)
    }

    public func getMarketAsset(symbol: String) async throws -> AssetDetailDTO {
        let url = baseURL.appending(path: "v1/assets/\(symbol)")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/assets/{symbol}")
        return try JSONDecoder().decode(AssetDetailDTO.self, from: response.data)
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

        let response = try await send(request, route: "/v1/assets/{symbol}/chart")
        return try JSONDecoder().decode(AssetChartDTO.self, from: response.data)
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

        let response = try await send(request, route: "/v1/groups/{id}/quotes")
        return try JSONDecoder().decode(BuyQuoteDTO.self, from: response.data)
    }

    public func createProposal(
        groupId: String,
        symbol: String,
        usdc: Int64? = nil,
        kind: String = "buy",
        tokenAmount: Int64? = nil,
        thesis: String? = nil,
        submission: IdempotentSubmission
    ) async throws -> CreateProposalResponseDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/proposals")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try MonacoHTTPTransport.idempotentBodyEncoder().encode(
            ProposalRequestDTO(symbol: symbol, kind: kind, usdc: usdc, tokenAmount: tokenAmount, thesis: thesis)
        )

        let response = try await send(request, route: "/v1/groups/{id}/proposals", submission: submission)
        return try JSONDecoder().decode(CreateProposalResponseDTO.self, from: response.data)
    }

    public func castVote(proposalId: String, choice: String) async throws {
        let url = baseURL.appending(path: "v1/proposals/\(proposalId)/votes")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(VoteRequestDTO(choice: choice))

        _ = try await send(request, route: "/v1/proposals/{id}/votes", accepting: [200, 204])
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

        let response = try await send(request, route: "/v1/groups/{id}/proposals")
        return try JSONDecoder().decode(ProposalListResponseDTO.self, from: response.data)
    }

    public func getProposalDetail(proposalId: String) async throws -> ProposalDTO {
        let url = baseURL.appending(path: "v1/proposals/\(proposalId)")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/proposals/{id}")
        return try JSONDecoder().decode(ProposalDTO.self, from: response.data)
    }

    public func listProposalComments(proposalId: String) async throws -> ProposalCommentsResponseDTO {
        let url = baseURL.appending(path: "v1/proposals/\(proposalId)/comments")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/proposals/{id}/comments")
        return try JSONDecoder().decode(ProposalCommentsResponseDTO.self, from: response.data)
    }

    /// Posts a top-level comment, or a reply when `parentId` is set. Server trims and validates the body.
    public func postProposalComment(proposalId: String, body: String, parentId: String? = nil) async throws -> ProposalCommentDTO {
        let url = baseURL.appending(path: "v1/proposals/\(proposalId)/comments")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(CommentRequestDTO(body: body, parentId: parentId))

        let response = try await send(request, route: "/v1/proposals/{id}/comments", accepting: [200, 201])
        return try JSONDecoder().decode(ProposalCommentDTO.self, from: response.data)
    }

    public func leaveGroup(groupId: String, withdrawStake: Bool = false, submission: IdempotentSubmission) async throws {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/leave")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try MonacoHTTPTransport.idempotentBodyEncoder().encode(LeaveGroupRequestDTO(withdrawStake: withdrawStake))
        let response = try await send(request, route: "/v1/groups/{id}/leave", accepting: [204, 409], submission: submission)
        if response.statusCode == 409 {
            throw MonacoAPIError.leaveBlocked(parseLeaveConflict(from: response.data))
        }
    }

    public func withdrawToBalance(groupId: String, shareAmountMicros: Int64? = nil, submission: IdempotentSubmission) async throws -> WithdrawToBalanceJobDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/withdraw-to-balance")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try MonacoHTTPTransport.idempotentBodyEncoder().encode(WithdrawToBalanceRequestDTO(shareAmountMicros: shareAmountMicros))
        let response = try await send(request, route: "/v1/groups/{id}/withdraw-to-balance", submission: submission)
        return try JSONDecoder().decode(WithdrawToBalanceJobDTO.self, from: response.data)
    }

    public func getGroupView(groupId: String) async throws -> GroupViewDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/view")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/groups/{id}/view")
        return try JSONDecoder().decode(GroupViewDTO.self, from: response.data)
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
        return try await getJSON(path: "v1/groups/search", route: "/v1/groups/search", queryItems: items, as: GroupSearchResponseDTO.self)
    }

    /// Platform-wide cabals ranked by percent return (server caps limit at 50).
    public func groupLeaderboard(limit: Int = 20) async throws -> GroupLeaderboardResponseDTO {
        try await getJSON(
            path: "v1/groups/leaderboard",
            route: "/v1/groups/leaderboard",
            queryItems: [URLQueryItem(name: "limit", value: String(limit))],
            as: GroupLeaderboardResponseDTO.self
        )
    }

    /// One P&L series per cabal the viewer belongs to, in a single request.
    public func myGroupsPnLHistory(range: GroupPnLRange = .oneMonth) async throws -> MyGroupsPnLHistoryDTO {
        try await getJSON(
            path: "v1/groups/pnl-history",
            route: "/v1/groups/pnl-history",
            queryItems: [URLQueryItem(name: "range", value: range.rawValue)],
            as: MyGroupsPnLHistoryDTO.self
        )
    }

    /// P&L series for one cabal; readable by any signed-in user.
    public func groupPnLHistory(groupId: String, range: GroupPnLRange = .oneMonth) async throws -> GroupPnLSeriesDTO {
        try await getJSON(
            path: "v1/groups/\(groupId)/pnl-history",
            route: "/v1/groups/{id}/pnl-history",
            queryItems: [URLQueryItem(name: "range", value: range.rawValue)],
            as: GroupPnLSeriesDTO.self
        )
    }

    private func getJSON<T: Decodable>(
        path: String,
        route: String,
        queryItems: [URLQueryItem],
        as type: T.Type
    ) async throws -> T {
        var components = URLComponents(url: baseURL.appending(path: path), resolvingAgainstBaseURL: false)!
        components.queryItems = queryItems
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: route)
        return try monacoISO8601JSONDecoder().decode(T.self, from: response.data)
    }

    public func postRedeem(
        groupId: String,
        shareUnits: String,
        payoutAddress: String,
        payoutProof: String,
        submission: IdempotentSubmission
    ) async throws -> RedeemJobDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/redeems")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try MonacoHTTPTransport.idempotentBodyEncoder().encode(
            RedeemRequestDTO(
                shareUnits: shareUnits,
                payoutAddress: payoutAddress,
                payoutProof: payoutProof
            )
        )

        let response = try await send(request, route: "/v1/groups/{id}/redeems", submission: submission)
        return try JSONDecoder().decode(RedeemJobDTO.self, from: response.data)
    }

    public func devBuy(groupId: String, symbol: String, usdc: Int64) async throws -> DevBuyResponseDTO {
        let url = baseURL.appending(path: "v1/dev/groups/\(groupId)/buy")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(DevBuyRequestDTO(symbol: symbol, usdc: usdc))

        // Buys the stock inside the request, so it gets the money budget, not the read one.
        let response = try await send(
            request,
            route: "/v1/dev/groups/{id}/buy",
            timeout: MonacoRequestTimeout.moneyWrite
        )
        return try JSONDecoder().decode(DevBuyResponseDTO.self, from: response.data)
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

    // lane: watchlist — internal, see `baseURL`.
    func applyAuthorizationHeader(to request: inout URLRequest) async throws {
        guard let accessTokenProvider else { return }
        guard let token = try await accessTokenProvider(), !token.isEmpty else { return }
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
    }

    /// `GET /v1/groups/{id}/messages` — newest first. Pass `before` from a prior page's `nextCursor`.
    public func listGroupMessages(
        groupId: String,
        before: String? = nil,
        limit: Int = 30
    ) async throws -> GroupMessagesPageDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/groups/\(groupId)/messages"),
            resolvingAgainstBaseURL: false
        )!
        var items = [URLQueryItem(name: "limit", value: String(limit))]
        if let before, !before.isEmpty {
            items.append(URLQueryItem(name: "before", value: before))
        }
        components.queryItems = items
        guard let url = components.url else {
            throw MonacoAPIError.invalidResponse
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/groups/{id}/messages", mapping: .full)
        return try JSONDecoder().decode(GroupMessagesPageDTO.self, from: response.data)
    }

    /// `POST /v1/groups/{id}/messages` — returns the stored message (201).
    public func postGroupMessage(groupId: String, body: String) async throws -> GroupMessageDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/messages")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(GroupMessageRequestDTO(body: body))

        let response = try await send(request, route: "/v1/groups/{id}/messages", accepting: [201], mapping: .full)
        return try JSONDecoder().decode(GroupMessageDTO.self, from: response.data)
    }

    private struct GroupMessageRequestDTO: Encodable {
        let body: String
    }

}
