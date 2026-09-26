import Foundation

/// Settings: the member's preferences, the deletion check, and deleting the account.
extension MonacoAPIClient {
    /// `GET /v1/me/preferences`.
    public func getPreferences() async throws -> PreferencesDTO {
        var request = URLRequest(url: baseURL.appending(path: "v1/me/preferences"))
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/me/preferences", mapping: .full)
        return try JSONDecoder().decode(PreferencesDTO.self, from: response.data)
    }

    /// `PATCH /v1/me/preferences` with only the switches that changed. Returns the whole
    /// document as the server now has it.
    public func updateNotificationPreferences(_ changes: [NotificationCategory: Bool]) async throws -> PreferencesDTO {
        var request = URLRequest(url: baseURL.appending(path: "v1/me/preferences"))
        request.httpMethod = "PATCH"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        try await applyAuthorizationHeader(to: &request)
        request.httpBody = try Self.sortedKeysEncoder.encode(PreferencesPatchDTO(notifications: changes))

        let response = try await send(request, route: "/v1/me/preferences", mapping: .full)
        return try JSONDecoder().decode(PreferencesDTO.self, from: response.data)
    }

    /// `GET /v1/me/deletion-check`.
    public func getDeletionCheck() async throws -> DeletionCheckDTO {
        var request = URLRequest(url: baseURL.appending(path: "v1/me/deletion-check"))
        request.httpMethod = "GET"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/me/deletion-check", mapping: .full)
        return try JSONDecoder().decode(DeletionCheckDTO.self, from: response.data)
    }

    /// `DELETE /v1/me`. A `409` is not an error here: it is the answer "not yet", with the
    /// blockers, so the screen can show them without asking again.
    public func deleteAccount() async throws -> AccountDeletionOutcome {
        var request = URLRequest(url: baseURL.appending(path: "v1/me"))
        request.httpMethod = "DELETE"
        try await applyAuthorizationHeader(to: &request)

        let response = try await send(request, route: "/v1/me", accepting: [200, 409], mapping: .full)
        if (response.response as? HTTPURLResponse)?.statusCode == 409 {
            return .blocked(try JSONDecoder().decode(DeletionCheckDTO.self, from: response.data))
        }
        let body = try JSONDecoder().decode(DeletedAccountDTO.self, from: response.data)
        return .deleted(deletedAt: MonacoISO8601.date(from: body.deletedAt))
    }

    /// Stable key order, so a patch body reads the same in logs and tests every time.
    private static var sortedKeysEncoder: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return encoder
    }
}
