import Foundation

extension MonacoAPIClient {
    /// `GET /v1/groups/{id}/matchup` — the cabal's live matchup (the cabal as side `a`), its
    /// record, its last four results and, for members, next week's challenges.
    public func groupMatchup(groupId: String) async throws -> GroupMatchupDTO {
        try await getJSON(
            path: "v1/groups/\(groupId)/matchup",
            route: "/v1/groups/{id}/matchup",
            queryItems: [],
            as: GroupMatchupDTO.self
        )
    }

    /// `GET /v1/home/matchups` — this week's matchup for each of the viewer's cabals.
    public func homeMatchups() async throws -> HomeMatchupsDTO {
        try await getJSON(
            path: "v1/home/matchups",
            route: "/v1/home/matchups",
            queryItems: [],
            as: HomeMatchupsDTO.self
        )
    }

    /// `GET /v1/matchups/table` — the season table. The server caps `limit` at 50.
    public func matchupTable(limit: Int = 20) async throws -> MatchupTableDTO {
        try await getJSON(
            path: "v1/matchups/table",
            route: "/v1/matchups/table",
            queryItems: [URLQueryItem(name: "limit", value: String(limit))],
            as: MatchupTableDTO.self
        )
    }

    /// `POST /v1/groups/{id}/matchups/challenge` — challenge `opponentGroupId` for next week.
    /// Sending the same challenge again returns the one already open. A refusal the rules make
    /// throws `MatchupChallengeRefused` with the server's reason.
    public func challengeCabal(groupId: String, opponentGroupId: String) async throws -> MatchupChallengeDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/matchups/challenge")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(MatchupChallengeRequest(groupId: opponentGroupId))

        let response = try await send(
            request,
            route: "/v1/groups/{id}/matchups/challenge",
            accepting: [200, 201, 409],
            mapping: .full
        )
        return try Self.challengeOrRefusal(response)
    }

    /// `POST /v1/groups/{id}/matchups/challenges/{challengeId}/accept` — accept a challenge sent
    /// to this cabal, locking the pair for next week.
    public func acceptMatchupChallenge(groupId: String, challengeId: String) async throws -> MatchupChallengeDTO {
        let url = baseURL.appending(path: "v1/groups/\(groupId)/matchups/challenges/\(challengeId)/accept")
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(
            request,
            route: "/v1/groups/{id}/matchups/challenges/{challengeId}/accept",
            accepting: [200, 409],
            mapping: .full
        )
        return try Self.challengeOrRefusal(response)
    }

    private static func challengeOrRefusal(_ response: MonacoHTTPResponse) throws -> MatchupChallengeDTO {
        if response.statusCode == 409 {
            throw MatchupChallengeRefused(reason: MatchupChallengeRefusal(errorBody: response.data))
        }
        return try monacoISO8601JSONDecoder().decode(MatchupChallengeDTO.self, from: response.data)
    }
}

/// A challenge the matchup rules refused (409). `reason` is nil for a reason this build does not know.
public struct MatchupChallengeRefused: Error, Equatable, Sendable {
    public let reason: MatchupChallengeRefusal?

    public init(reason: MatchupChallengeRefusal?) {
        self.reason = reason
    }
}

private struct MatchupChallengeRequest: Encodable {
    let groupId: String
}

/// Why the server refused a challenge, read from the `reason` of a 409.
public enum MatchupChallengeRefusal: String, Equatable, Sendable {
    /// One of the two cabals already has next week's opponent.
    case alreadyMatched = "already_matched"
    /// The other cabal challenged this one first.
    case incomingChallenge = "incoming_challenge"
    /// Five challenges are already waiting on an answer this week.
    case tooManyChallenges = "too_many_challenges"
    /// The week was drawn; the challenge is closed.
    case challengeClosed = "challenge_closed"

    /// Reads the reason out of a 409 body; nil for any other body.
    public init?(errorBody data: Data) {
        struct Body: Decodable { let reason: String? }
        guard let reason = (try? JSONDecoder().decode(Body.self, from: data))?.reason,
              let parsed = MatchupChallengeRefusal(rawValue: reason)
        else { return nil }
        self = parsed
    }
}
