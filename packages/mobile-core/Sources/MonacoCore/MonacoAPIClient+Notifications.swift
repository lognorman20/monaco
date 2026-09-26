import Foundation

/// The inbox, push devices and proposal reminders.
extension MonacoAPIClient {
    /// `GET /v1/me/notifications` — newest first. Pass `cursor` from a page's `nextCursor`.
    public func listNotifications(cursor: String? = nil, limit: Int = 30) async throws -> NotificationsPageDTO {
        var components = URLComponents(url: baseURL.appending(path: "v1/me/notifications"), resolvingAgainstBaseURL: false)!
        var items = [URLQueryItem(name: "limit", value: String(limit))]
        if let cursor, !cursor.isEmpty {
            items.append(URLQueryItem(name: "cursor", value: cursor))
        }
        components.queryItems = items
        guard let url = components.url else { throw MonacoAPIError.invalidResponse }
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/me/notifications", mapping: .full)
        return try monacoISO8601JSONDecoder().decode(NotificationsPageDTO.self, from: response.data)
    }

    /// `POST /v1/me/notifications/read` with the ids the member opened.
    public func markNotificationsRead(ids: [String]) async throws -> UnreadCountDTO {
        try await postRead(MarkReadBody(ids: ids, all: nil))
    }

    /// `POST /v1/me/notifications/read` with `all: true`.
    public func markAllNotificationsRead() async throws -> UnreadCountDTO {
        try await postRead(MarkReadBody(ids: nil, all: true))
    }

    /// `PUT /v1/me/devices` — register this install's APNs token (hex) for the signed-in member.
    public func registerDevice(token: String, appEnv: DeviceAppEnv) async throws {
        var request = URLRequest(url: baseURL.appending(path: "v1/me/devices"))
        request.httpMethod = "PUT"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(RegisterDeviceBody(token: token, platform: "ios", appEnv: appEnv))
        _ = try await send(request, route: "/v1/me/devices", accepting: [204], mapping: .full)
    }

    /// `DELETE /v1/me/devices/{token}` — stop pushing to this install.
    public func unregisterDevice(token: String) async throws {
        var request = URLRequest(url: baseURL.appending(path: "v1/me/devices/\(token)"))
        request.httpMethod = "DELETE"
        try await applyAuthorizationHeader(to: &request)
        _ = try await send(request, route: "/v1/me/devices/{token}", accepting: [204], mapping: .full)
    }

    /// `POST /v1/proposals/{id}/nudge` — remind the voters who have not voted. 429 carries
    /// `Retry-After` when someone already reminded them this hour.
    public func nudgeProposal(id: String) async throws -> NudgeResultDTO {
        var request = URLRequest(url: baseURL.appending(path: "v1/proposals/\(id)/nudge"))
        request.httpMethod = "POST"
        try await applyAuthorizationHeader(to: &request)
        let response = try await send(request, route: "/v1/proposals/{id}/nudge", mapping: .full)
        return try JSONDecoder().decode(NudgeResultDTO.self, from: response.data)
    }

    private func postRead(_ body: MarkReadBody) async throws -> UnreadCountDTO {
        var request = URLRequest(url: baseURL.appending(path: "v1/me/notifications/read"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try JSONEncoder().encode(body)
        let response = try await send(request, route: "/v1/me/notifications/read", mapping: .full)
        return try JSONDecoder().decode(UnreadCountDTO.self, from: response.data)
    }
}

private struct MarkReadBody: Encodable {
    let ids: [String]?
    let all: Bool?
}

private struct RegisterDeviceBody: Encodable {
    let token: String
    let platform: String
    let appEnv: DeviceAppEnv
}
