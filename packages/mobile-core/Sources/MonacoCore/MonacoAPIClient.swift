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
