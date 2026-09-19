import Foundation

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

private struct APIErrorBody: Decodable {
    let error: String
}

final class MonacoAPIClient {
    private let baseURL: URL
    private let session: URLSession

    init(baseURL: URL = Config.apiBaseURL, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.session = session
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

        let (data, response) = try await session.data(for: request)
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

    func fundGroup(accessToken: String, groupId: String, amount: Int64) async throws -> FundGroupResponse {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/fund")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try JSONEncoder().encode(FundGroupRequest(amount: amount))

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
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

    func leaveGroup(accessToken: String, groupId: String) async throws {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/leave")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw MonacoAPIError.invalidResponse }
        switch http.statusCode {
        case 204: return
        case 409: throw MonacoAPIError.leaveBlocked(parseLeaveConflict(from: data))
        default: throw MonacoAPIError.httpStatus(http.statusCode)
        }
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

    func postQuote(accessToken: String, groupId: String, symbol: String, usdc: Int64) async throws -> BuyQuoteDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/quotes")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try JSONEncoder().encode(QuoteRequest(symbol: symbol, usdc: usdc))

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(BuyQuoteDTO.self, from: data)
    }

    func createProposal(accessToken: String, groupId: String, symbol: String, usdc: Int64) async throws -> CreateProposalResponse {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/proposals")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try JSONEncoder().encode(ProposalRequest(symbol: symbol, usdc: usdc))

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw proposalCreateError(status: http.statusCode, data: data)
        }
        return try JSONDecoder().decode(CreateProposalResponse.self, from: data)
    }

    private func proposalCreateError(status: Int, data: Data) -> MonacoAPIError {
        if let body = try? JSONDecoder().decode(APIErrorBody.self, from: data),
           !body.error.isEmpty {
            return .apiError(status: status, message: body.error)
        }
        return .httpStatus(status)
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

    func retryTransaction(accessToken: String, transactionId: String) async throws -> RetryTransactionResponse {
        let url = baseURL.appending(path: "v1/transactions/\(transactionId)/retry")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
        return try JSONDecoder().decode(RetryTransactionResponse.self, from: data)
    }

    func listGroupProposals(accessToken: String, groupId: String, tab: String) async throws -> ProposalListResponse {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/groups/\(groupId)/proposals"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [URLQueryItem(name: "tab", value: tab)]
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
        return try JSONDecoder().decode(ProposalListResponse.self, from: data)
    }

    func getProposalDetail(accessToken: String, proposalId: String) async throws -> ProposalDTO {
        let url = baseURL.appending(path: "v1/proposals/\(proposalId)")
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
        return try JSONDecoder().decode(ProposalDTO.self, from: data)
    }

    func castVote(accessToken: String, proposalId: String, choice: String) async throws {
        let url = baseURL.appending(path: "v1/proposals/\(proposalId)/votes")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try JSONEncoder().encode(VoteRequest(choice: choice))

        let (_, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw MonacoAPIError.invalidResponse
        }
        guard http.statusCode == 200 || http.statusCode == 204 else {
            throw MonacoAPIError.httpStatus(http.statusCode)
        }
    }

    func postRedeem(
        accessToken: String,
        groupId: String,
        shareUnits: String,
        payoutAddress: String,
        payoutProof: String
    ) async throws -> RedeemJobDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/redeems")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try JSONEncoder().encode(
            RedeemSubmitRequest(
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

    func devBuy(accessToken: String, groupId: String, symbol: String, usdc: Int64) async throws -> DevBuyResponse {
        let url = baseURL.appending(path: "v1/dev/groups/\(groupId)/buy")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try applyAuthorizationHeader(accessToken: accessToken, to: &request)
        request.httpBody = try JSONEncoder().encode(DevBuyRequest(symbol: symbol, usdc: usdc))

        let (data, response) = try await session.data(for: request)
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
    let usdc: Int64
}

private struct ProposalRequest: Encodable {
    let symbol: String
    let usdc: Int64
}

private struct VoteRequest: Encodable {
    let choice: String
}

private struct RedeemSubmitRequest: Encodable {
    let shareUnits: String
    let payoutAddress: String
    let payoutProof: String
}
