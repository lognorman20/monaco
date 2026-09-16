import Foundation

enum MonacoAPIError: Error {
    case invalidResponse
    case httpStatus(Int)
    case missingAccessToken
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

    func createGroup(accessToken: String, name: String) async throws -> CreateGroupResponse {
        try await createGroup(
            accessToken: accessToken,
            name: name,
            joinPolicyMode: "open",
            joinPassword: nil,
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
        joinPassword: String?,
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
                joinPolicy: CreateGroupJoinPolicyRequest(mode: joinPolicyMode, password: joinPassword),
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
    let password: String?

    enum CodingKeys: String, CodingKey {
        case mode
        case password
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(mode, forKey: .mode)
        if let password {
            try container.encode(password, forKey: .password)
        }
    }
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
