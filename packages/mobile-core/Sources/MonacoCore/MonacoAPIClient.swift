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

    private func applyAuthorizationHeader(to request: inout URLRequest) async throws {
        guard let accessTokenProvider else { return }
        guard let token = try await accessTokenProvider(), !token.isEmpty else { return }
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
    }
}
