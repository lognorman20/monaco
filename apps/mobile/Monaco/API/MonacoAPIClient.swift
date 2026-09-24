import Foundation
import MonacoCore

enum LeaveGroupBlockReason: String, Equatable {
    case shareUnitsRemaining = "share_units_remaining"
    case lastMemberWithTreasury = "last_member_with_treasury"
    case pendingRedeem = "pending_redeem"
    case soleRemainingVote = "sole_remaining_vote"
    case creatorMustTransfer = "creator_must_transfer"
    case unknown
}

enum MonacoAPIError: Error {
    case invalidResponse
    case httpStatus(Int)
    case apiError(status: Int, message: String)
    case missingAccessToken
    case leaveBlocked(LeaveGroupBlockReason)
}

extension Error {
    /// SwiftUI `.task` cancellation usually surfaces as `URLError.cancelled`, not `CancellationError`.
    var isRequestCancellation: Bool {
        if self is CancellationError {
            return true
        }
        if let urlError = self as? URLError {
            return urlError.code == .cancelled
        }
        let nsError = self as NSError
        return nsError.domain == NSURLErrorDomain && nsError.code == NSURLErrorCancelled
    }
}

private struct APIErrorBody: Decodable {
    let error: String
}

final class MonacoAPIClient {
    private let baseURL: URL
    /// Every request goes through the transport so an expired access token is
    /// refreshed and the request retried once instead of signing the user out.
    private let session: MonacoHTTPTransport

    init(baseURL: URL = Config.apiBaseURL, session: URLSession = .monaco) {
        self.baseURL = baseURL
        self.session = MonacoHTTPTransport(session: session)
    }

    func health() async throws -> HealthResponse {
        let url = baseURL.appending(path: "health")
        let (data, response) = try await session.data(from: url)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(HealthResponse.self, from: data)
    }

    func openSession(accessToken: String) async throws -> MeResponse {
        let url = baseURL.appending(path: "v1/auth/session")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(SessionRequest(accessToken: accessToken))

        // Cold-start gate: a Privy verify plus wallet provisioning on first sign-in, so it
        // names its own budget rather than inheriting the short read default.
        let (data, response) = try await session.data(for: request, timeout: MonacoRequestTimeout.signIn)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(MeResponse.self, from: data)
    }

    func me(accessToken: String) async throws -> MeResponse {
        let url = baseURL.appending(path: "v1/me")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(MeResponse.self, from: data)
    }

    func getPlatformBalance(accessToken: String) async throws -> PlatformBalanceDTO {
        let url = baseURL.appending(path: "v1/me/balance")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(PlatformBalanceDTO.self, from: data)
    }

    func createPlatformWithdrawal(accessToken: String, amount: Int64, toAddress: String, submission: IdempotentSubmission) async throws -> PlatformWithdrawalDTO {
        let url = baseURL.appending(path: "v1/me/withdrawals")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try MonacoHTTPTransport.idempotentBodyEncoder().encode(CreatePlatformWithdrawalRequest(amount: amount, toAddress: toAddress))

        let (data, response) = try await session.data(for: request, submission: submission)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw apiFailure(status: http.statusCode, data: data)
        }
        return try JSONDecoder().decode(PlatformWithdrawalDTO.self, from: data)
    }

    func getPlatformWithdrawal(accessToken: String, withdrawalId: String) async throws -> PlatformWithdrawalDTO {
        let url = baseURL.appending(path: "v1/me/withdrawals/\(withdrawalId)")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(PlatformWithdrawalDTO.self, from: data)
    }

    func fundGroup(accessToken: String, groupId: String, amount: Int64, submission: IdempotentSubmission) async throws -> FundGroupResponse {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/fund")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try MonacoHTTPTransport.idempotentBodyEncoder().encode(FundGroupRequest(amount: amount))

        let (data, response) = try await session.data(for: request, submission: submission)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw apiFailure(status: http.statusCode, data: data)
        }
        return try JSONDecoder().decode(FundGroupResponse.self, from: data)
    }

    func getHome(accessToken: String) async throws -> HomeViewDTO {
        let url = baseURL.appending(path: "v1/home")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(HomeViewDTO.self, from: data)
    }

    func getHomeDashboard(accessToken: String, leaderboardRange: HomeLeaderboardRange = .all) async throws -> HomeDashboardDTO {
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
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try monacoISO8601JSONDecoder().decode(HomeDashboardDTO.self, from: data)
    }

    func getHomePnLSeries(accessToken: String, range: HomeLeaderboardRange = .oneHour) async throws -> HomePnLSeriesDTO {
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
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try monacoISO8601JSONDecoder().decode(HomePnLSeriesDTO.self, from: data)
    }

    func getHomeMissedProposals(accessToken: String) async throws -> HomeMissedProposalsDTO {
        let url = baseURL.appending(path: "v1/home/missed-proposals")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try monacoISO8601JSONDecoder().decode(HomeMissedProposalsDTO.self, from: data)
    }

    func getUserSharedGroups(accessToken: String, userId: String) async throws -> [HomeGroupBoardRowDTO] {
        let url = baseURL.appending(path: "v1/users/\(userId)/groups")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        let payload = try JSONDecoder().decode(UserSharedGroupsResponse.self, from: data)
        return payload.groups
    }

    func createGroup(accessToken: String, name: String) async throws -> CreateGroupResponse {
        try await createGroup(
            accessToken: accessToken,
            name: name,
            joinPolicyMode: "open",
            voterSetMode: "all_members",
            voterMemberIds: [],
            threshold: "majority",
            voteExpirySeconds: 86_400
        )
    }

    func createGroup(
        accessToken: String,
        name: String,
        joinPolicyMode: String,
        voterSetMode: String,
        voterMemberIds: [String],
        threshold: String,
        voteExpirySeconds: Int64
    ) async throws -> CreateGroupResponse {
        let url = baseURL.appending(path: "v1/groups")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try JSONEncoder().encode(
            CreateGroupRulesRequest(
                name: name,
                joinPolicy: CreateGroupJoinPolicyRequest(mode: joinPolicyMode),
                voterSet: CreateGroupVoterSetRequest(mode: voterSetMode, memberIds: voterMemberIds),
                threshold: threshold,
                voteExpirySeconds: voteExpirySeconds
            )
        )

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(CreateGroupResponse.self, from: data)
    }

    func leaveGroup(accessToken: String, groupId: String, withdrawStake: Bool = false, submission: IdempotentSubmission) async throws {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/leave")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try MonacoHTTPTransport.idempotentBodyEncoder().encode(LeaveGroupRequestDTO(withdrawStake: withdrawStake))
        let (data, response) = try await session.data(for: request, submission: submission)
        guard let http = response as? HTTPURLResponse else { throw MonacoAPIError.invalidResponse }
        switch http.statusCode {
        case 204: return
        case 409: throw MonacoAPIError.leaveBlocked(parseLeaveConflict(from: data))
        default: throw MonacoAPIError.httpStatus(http.statusCode)
        }
    }

    func withdrawToBalance(accessToken: String, groupId: String, shareAmountMicros: Int64? = nil, submission: IdempotentSubmission) async throws -> WithdrawToBalanceJobDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/withdraw-to-balance")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try MonacoHTTPTransport.idempotentBodyEncoder().encode(WithdrawToBalanceRequestDTO(shareAmountMicros: shareAmountMicros))
        let (data, response) = try await session.data(for: request, submission: submission)
        guard let http = response as? HTTPURLResponse else { throw MonacoAPIError.invalidResponse }
        // 4xx cash out refusals carry a message the member can act on (amount too small to
        // route, pot short on USDC); surface it instead of a generic failure.
        guard http.statusCode == 200 else { throw apiFailure(status: http.statusCode, data: data) }
        return try JSONDecoder().decode(WithdrawToBalanceJobDTO.self, from: data)
    }

    /// Money endpoints explain a refusal in the body; keep it so the screen can say why.
    private func apiFailure(status: Int, data: Data) -> MonacoAPIError {
        if let body = try? JSONDecoder().decode(APIErrorBody.self, from: data),
           !body.error.isEmpty {
            return .apiError(status: status, message: body.error)
        }
        return .httpStatus(status)
    }

    func joinGroup(accessToken: String, groupId: String) async throws -> JoinGroupOutcome {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/join")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = Data("{}".utf8)
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw MonacoAPIError.invalidResponse }
        switch http.statusCode {
        case 204: return .joined
        case 202: return try JSONDecoder().decode(JoinGroupStatusResponse.self, from: data).status
        case 403: throw MonacoAPIError.httpStatus(403)
        case 404: throw MonacoAPIError.httpStatus(404)
        default: throw MonacoAPIError.httpStatus(http.statusCode)
        }
    }

    func listJoinRequests(accessToken: String, groupId: String) async throws -> [JoinRequestDTO] {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/join-requests")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw MonacoAPIError.invalidResponse }
        guard http.statusCode == 200 else { throw MonacoAPIError.httpStatus(http.statusCode) }
        return try JSONDecoder().decode(JoinRequestsListResponse.self, from: data).items
    }

    func approveJoinRequest(accessToken: String, groupId: String, requestId: String) async throws {
        try await decideJoinRequest(accessToken: accessToken, groupId: groupId, requestId: requestId, approve: true)
    }

    func denyJoinRequest(accessToken: String, groupId: String, requestId: String) async throws {
        try await decideJoinRequest(accessToken: accessToken, groupId: groupId, requestId: requestId, approve: false)
    }

    private func decideJoinRequest(accessToken: String, groupId: String, requestId: String, approve: Bool) async throws {
        let action = approve ? "approve" : "deny"
        let url = baseURL.appending(path: "v1/groups/\(groupId)/join-requests/\(requestId)/\(action)")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        let (_, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw MonacoAPIError.invalidResponse }
        guard http.statusCode == 204 else { throw MonacoAPIError.httpStatus(http.statusCode) }
    }


    func getGroup(accessToken: String, groupId: String) async throws -> GetGroupResponse {
        let url = baseURL.appending(path: "v1/groups/\(groupId)")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(GetGroupResponse.self, from: data)
    }

    func getGroupView(accessToken: String, groupId: String) async throws -> GroupViewDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/view")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(GroupViewDTO.self, from: data)
    }

    // MARK: Groups tab (#148). Requests and DTOs live in MonacoCore; these wrappers
    // add the session token and map MonacoCore errors onto this client's errors.

    func searchGroups(accessToken: String, query: String, limit: Int = 20, cursor: String? = nil) async throws -> GroupSearchResponseDTO {
        try await withCoreClient(accessToken) { try await $0.searchGroups(query: query, limit: limit, cursor: cursor) }
    }

    func groupLeaderboard(accessToken: String, limit: Int = 20) async throws -> GroupLeaderboardResponseDTO {
        try await withCoreClient(accessToken) { try await $0.groupLeaderboard(limit: limit) }
    }

    func myGroupsPnLHistory(accessToken: String, range: GroupPnLRange = .oneMonth) async throws -> MyGroupsPnLHistoryDTO {
        try await withCoreClient(accessToken) { try await $0.myGroupsPnLHistory(range: range) }
    }

    private func withCoreClient<T>(
        _ accessToken: String,
        _ call: (MonacoCore.MonacoAPIClient) async throws -> T
    ) async throws -> T {
        let token = accessToken.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !token.isEmpty else { throw MonacoAPIError.missingAccessToken }
        let core = MonacoCore.MonacoAPIClient(baseURL: baseURL, transport: session, accessTokenProvider: { token })
        do {
            return try await call(core)
        } catch let error as MonacoCore.MonacoAPIError {
            switch error {
            case .httpStatus(let status, _): throw MonacoAPIError.httpStatus(status)
            case .invalidResponse: throw MonacoAPIError.invalidResponse
            case .leaveBlocked: throw MonacoAPIError.invalidResponse
            case .rejected(let status, let message, _): throw MonacoAPIError.apiError(status: status, message: message)
            case .rateLimited: throw MonacoAPIError.httpStatus(429)
            }
        }
    }

    func createDeposit(accessToken: String, groupId: String, amount: Int64) async throws -> CreateDepositResponse {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/deposits")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try JSONEncoder().encode(CreateDepositRequest(amount: amount))

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(CreateDepositResponse.self, from: data)
    }

    func getDeposit(accessToken: String, depositId: String) async throws -> GetDepositResponse {
        let url = baseURL.appending(path: "v1/deposits/\(depositId)")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(GetDepositResponse.self, from: data)
    }

    func searchAssets(
        accessToken: String,
        groupId: String,
        query: String,
        limit: Int = 25,
        offset: Int = 0
    ) async throws -> SearchAssetsResponse {
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
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(SearchAssetsResponse.self, from: data)
    }

    func listMarketAssets(
        accessToken: String,
        query: String = "",
        limit: Int = 25,
        offset: Int = 0
    ) async throws -> ListMarketAssetsResponse {
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
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(ListMarketAssetsResponse.self, from: data)
    }

    func getPopularAssets(
        accessToken: String,
        limit: Int = 10
    ) async throws -> PopularAssetsResponse {
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
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(PopularAssetsResponse.self, from: data)
    }

    func getMarketAsset(
        accessToken: String,
        symbol: String
    ) async throws -> AssetDetailDTO {
        let url = baseURL.appending(path: "v1/assets/\(symbol)")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(AssetDetailDTO.self, from: data)
    }

    func getMarketAssetChart(
        accessToken: String,
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
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(AssetChartDTO.self, from: data)
    }

    func postQuote(
        accessToken: String,
        groupId: String,
        symbol: String,
        kind: String = "buy",
        usdc: Int64? = nil,
        tokenAmount: Int64? = nil
    ) async throws -> BuyQuoteDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/quotes")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try JSONEncoder().encode(
            QuoteRequest(symbol: symbol, kind: kind, usdc: usdc, tokenAmount: tokenAmount)
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

    func createProposal(
        accessToken: String,
        groupId: String,
        kind: String = "buy",
        symbol: String? = nil,
        usdcMicros: Int64? = nil,
        tokenAmount: Int64? = nil,
        agentDisplayName: String? = nil,
        allocationUsdcMicros: Int64? = nil,
        thesis: String? = nil,
        submission: IdempotentSubmission
    ) async throws -> CreateProposalResponse {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/proposals")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try MonacoHTTPTransport.idempotentBodyEncoder().encode(
            ProposalRequest(
                kind: kind,
                symbol: symbol,
                usdc: usdcMicros,
                tokenAmount: tokenAmount,
                agentDisplayName: agentDisplayName,
                allocationUsdcMicros: allocationUsdcMicros,
                thesis: thesis
            )
        )

        let (data, response) = try await session.data(for: request, submission: submission)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw apiFailure(status: http.statusCode, data: data)
        }
        return try JSONDecoder().decode(CreateProposalResponse.self, from: data)
    }

    func getGroupActivity(accessToken: String, groupId: String) async throws -> GroupActivityResponse {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/activity")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(GroupActivityResponse.self, from: data)
    }

    func getTransactionDetail(accessToken: String, transactionId: String) async throws -> TransactionDetailDTO {
        let url = baseURL.appending(path: "v1/transactions/\(transactionId)")
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(TransactionDetailDTO.self, from: data)
    }

    func retryTransaction(accessToken: String, transactionId: String, submission: IdempotentSubmission) async throws -> RetryTransactionResponse {
        let url = baseURL.appending(path: "v1/transactions/\(transactionId)/retry")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request, submission: submission)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(RetryTransactionResponse.self, from: data)
    }

    func postRedeem(
        accessToken: String,
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
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try MonacoHTTPTransport.idempotentBodyEncoder().encode(
            RedeemSubmitRequest(
                shareUnits: shareUnits,
                payoutAddress: payoutAddress,
                payoutProof: payoutProof
            )
        )

        let (data, response) = try await session.data(for: request, submission: submission)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(RedeemJobDTO.self, from: data)
    }

    func devBuy(accessToken: String, groupId: String, symbol: String, usdc: Int64) async throws -> DevBuyResponse {
        let url = baseURL.appending(path: "v1/dev/groups/\(groupId)/buy")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try JSONEncoder().encode(DevBuyRequest(symbol: symbol, usdc: usdc))

        // Buys the stock inside the request, so it gets the money budget, not the read one.
        let (data, response) = try await session.data(for: request, timeout: MonacoRequestTimeout.moneyWrite)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(DevBuyResponse.self, from: data)
    }

    private func parseLeaveConflict(from data: Data) -> LeaveGroupBlockReason {
        struct Body: Decodable { let reason: String? }
        guard let body = try? JSONDecoder().decode(Body.self, from: data), let reason = body.reason, let parsed = LeaveGroupBlockReason(rawValue: reason) else { return .unknown }
        return parsed
    }

}

extension MonacoAPIClient {
    private func applyAuthorizationHeader(accessToken: String, to request: inout URLRequest) throws {
        let token = accessToken.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !token.isEmpty else {
            throw MonacoAPIError.missingAccessToken
        }
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
    }
}

private struct SessionRequest: Encodable {
    let accessToken: String
}

private struct CreateGroupJoinPolicyRequest: Encodable {
    let mode: String
}

private struct CreateGroupVoterSetRequest: Encodable {
    let mode: String
    let memberIds: [String]

    enum CodingKeys: String, CodingKey {
        case mode
        case memberIds
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(mode, forKey: .mode)
        if mode == "named_subset" {
            try container.encode(memberIds, forKey: .memberIds)
        }
    }
}

private struct CreateGroupRulesRequest: Encodable {
    let name: String
    let joinPolicy: CreateGroupJoinPolicyRequest
    let voterSet: CreateGroupVoterSetRequest
    let threshold: String
    let voteExpirySeconds: Int64
}


private struct CreateDepositRequest: Encodable {
    let amount: Int64
}

private struct QuoteRequest: Encodable {
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

private struct ProposalRequest: Encodable {
    let kind: String?
    let symbol: String?
    let usdc: Int64?
    let tokenAmount: Int64?
    let agentDisplayName: String?
    let allocationUsdcMicros: Int64?
    let thesis: String?

    enum CodingKeys: String, CodingKey {
        case kind, symbol, usdc, tokenAmount, agentDisplayName, allocationUsdcMicros, thesis
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        if let kind { try container.encode(kind, forKey: .kind) }
        if let symbol { try container.encode(symbol, forKey: .symbol) }
        if let usdc { try container.encode(usdc, forKey: .usdc) }
        if let tokenAmount { try container.encode(tokenAmount, forKey: .tokenAmount) }
        if let agentDisplayName { try container.encode(agentDisplayName, forKey: .agentDisplayName) }
        if let allocationUsdcMicros { try container.encode(allocationUsdcMicros, forKey: .allocationUsdcMicros) }
        if let thesis { try container.encode(thesis, forKey: .thesis) }
    }
}

private struct RedeemSubmitRequest: Encodable {
    let shareUnits: String
    let payoutAddress: String
    let payoutProof: String
}
