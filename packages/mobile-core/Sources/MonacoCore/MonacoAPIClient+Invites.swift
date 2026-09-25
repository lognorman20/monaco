import Foundation

// Invite codes: see InviteDTO.swift for the routes. Every call keeps the server's whole
// answer (`.full`), so a caller reads `statusCode` and a 429 arrives as `.rateLimited`.
extension MonacoAPIClient {
    /// The cabal's live code, which the server makes on the spot if it has none. Members only.
    public func currentInvite(groupId: String) async throws -> InviteDTO {
        var request = URLRequest(url: baseURL.appending(path: "v1/groups/\(groupId)/invites"))
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)
        let response = try await send(request, route: "/v1/groups/{id}/invites", mapping: .full)
        return try JSONDecoder().decode(InviteDTO.self, from: response.data)
    }

    /// A new code for the cabal. The old code and its link stop working at once.
    public func createInvite(groupId: String) async throws -> InviteDTO {
        var request = URLRequest(url: baseURL.appending(path: "v1/groups/\(groupId)/invites"))
        request.httpMethod = "POST"
        try await applyAuthorizationHeader(to: &request)
        let response = try await send(request, route: "/v1/groups/{id}/invites", accepting: [200, 201], mapping: .full)
        return try JSONDecoder().decode(InviteDTO.self, from: response.data)
    }

    /// Leaves the cabal with no live code until a member asks for one again.
    public func revokeInvite(groupId: String) async throws {
        var request = URLRequest(url: baseURL.appending(path: "v1/groups/\(groupId)/invites/revoke"))
        request.httpMethod = "POST"
        try await applyAuthorizationHeader(to: &request)
        _ = try await send(request, route: "/v1/groups/{id}/invites/revoke", accepting: [204], mapping: .full)
    }

    /// The public preview of the cabal behind a code. Sent without the session: the route
    /// needs none, and the join screen asks before the member has decided anything. A
    /// malformed, unknown or revoked code is a 404.
    public func invitePreview(code: String) async throws -> InvitePreviewDTO {
        guard let canonical = InviteLink.normalizeCode(code) else {
            throw MonacoAPIError.httpStatus(404)
        }
        var request = URLRequest(url: baseURL.appending(path: "v1/invites/\(canonical)"))
        request.httpMethod = "GET"
        let response = try await send(request, route: "/v1/invites/{code}", mapping: .full)
        return try JSONDecoder().decode(InvitePreviewDTO.self, from: response.data)
    }

    /// Joins through a code, following the cabal's policy: in at once (204), or a request
    /// its admin approves (202).
    public func joinGroup(inviteCode: String) async throws -> InviteJoinResult {
        var request = URLRequest(url: baseURL.appending(path: "v1/groups/join-by-code"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(JoinByCodeRequestDTO(code: inviteCode))
        let response = try await send(request, route: "/v1/groups/join-by-code", accepting: [202, 204], mapping: .full)
        let location = (response.response as? HTTPURLResponse)?.value(forHTTPHeaderField: "Location")
        let fromLocation = location.flatMap(Self.groupId(fromLocation:))
        if response.statusCode == 202 {
            let body = try? JSONDecoder().decode(JoinByCodePendingDTO.self, from: response.data)
            return InviteJoinResult(status: .pending, groupId: body?.groupId ?? fromLocation)
        }
        return InviteJoinResult(status: .joined, groupId: fromLocation)
    }

    /// "/v1/groups/<id>" → "<id>".
    static func groupId(fromLocation location: String) -> String? {
        let parts = location.split(separator: "/").map(String.init)
        guard parts.count == 3, parts[0] == "v1", parts[1] == "groups", !parts[2].isEmpty else { return nil }
        return parts[2]
    }
}

private struct JoinByCodeRequestDTO: Encodable {
    let code: String
}

private struct JoinByCodePendingDTO: Decodable {
    let status: String
    let groupId: String?
}
