import Foundation

public enum MonacoAPIError: Error, Equatable {
    case invalidResponse
    case httpStatus(Int)
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

    public func searchAssets(groupId: String, query: String) async throws -> SearchAssetsResponseDTO {
        var components = URLComponents(
            url: baseURL.appending(path: "v1/groups/\(groupId)/assets"),
            resolvingAgainstBaseURL: false
        )!
        components.queryItems = [URLQueryItem(name: "query", value: query)]
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

    private func applyAuthorizationHeader(to request: inout URLRequest) async throws {
        guard let accessTokenProvider else { return }
        guard let token = try await accessTokenProvider(), !token.isEmpty else { return }
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
    }
}
