import Foundation
import MonacoCore

/// Result of a profile write, phrased for a toast.
enum ProfileSaveOutcome: Equatable {
    case saved
    case unchanged
    case failed(String)
}

/// Self-profile writes. After a save, `me` holds the server copy and home boards are
/// refetched so the new name and photo show up on the people and member boards.
extension AppSessionStore {
    /// Optimistically renames the signed-in user, rolling back if the server rejects it.
    ///
    /// First run passes `optimistic: false`. `FirstRunGate` routes on `me.displayName`,
    /// so writing the name before the server confirms it would drop the user into the
    /// tabs mid-request and bounce them back out on a rejection.
    func updateDisplayName(
        _ draft: String,
        auth: DynamicAuthService,
        optimistic: Bool = true
    ) async -> ProfileSaveOutcome {
        guard let current = me else {
            return .failed("Your profile is still loading.")
        }
        let normalized: String
        switch DisplayNameRules.normalize(draft) {
        case .success(let value):
            normalized = value
        case .failure(let error):
            return .failed(error.message)
        }
        guard normalized != current.displayName else {
            return .unchanged
        }
        guard let client = profileClient(auth: auth) else {
            return .failed("Sign in again to edit your profile.")
        }

        let pending = current.withDisplayName(normalized)
        if optimistic {
            me = pending
        }
        do {
            me = try await client.updateProfile(displayName: normalized)
        } catch {
            // Only ever rolls back our own optimistic write, never a fresher one.
            if me == pending {
                me = current
            }
            return await failure(for: error, auth: auth, fallback: "Could not save your name. Try again.")
        }
        await refresh(auth: auth)
        return .saved
    }

    /// Uploads an already-prepared photo (see `ProfilePhotoUploadPreparer`).
    func uploadProfilePhoto(_ imageData: Data, mimeType: String, auth: DynamicAuthService) async -> ProfileSaveOutcome {
        guard let client = profileClient(auth: auth) else {
            return .failed("Sign in again to change your photo.")
        }
        do {
            me = try await client.uploadProfilePhoto(imageData: imageData, mimeType: mimeType)
        } catch {
            return await failure(for: error, auth: auth, fallback: "Could not upload your photo. Try again.")
        }
        await refresh(auth: auth)
        return .saved
    }

    private func profileClient(auth: DynamicAuthService) -> MonacoCore.MonacoAPIClient? {
        guard let token = auth.accessToken, !token.isEmpty else { return nil }
        return MonacoCore.MonacoAPIClient(baseURL: Config.apiBaseURL, accessTokenProvider: { token })
    }

    private func failure(for error: Error, auth: DynamicAuthService, fallback: String) async -> ProfileSaveOutcome {
        if case MonacoCore.MonacoAPIError.httpStatus(401) = error {
            await auth.signOutAfterRejectedSession()
            return .failed(LoginFailureCopy.sessionExpired)
        }
        return .failed(Self.profileErrorMessage(for: error, fallback: fallback))
    }

    static func profileErrorMessage(for error: Error, fallback: String) -> String {
        switch error {
        case MonacoCore.MonacoAPIError.rejected(_, let message):
            return message
        case MonacoCore.MonacoAPIError.rateLimited(let retryAfter):
            if let retryAfter, retryAfter > 0 {
                return "Too many changes. Try again in \(retryAfter)s."
            }
            return "Too many changes. Try again in a minute."
        case MonacoCore.MonacoAPIError.httpStatus(503):
            return "Photo uploads are not set up on this server."
        case let urlError as URLError where urlError.code != .cancelled:
            return "Could not reach Monaco. Check your connection."
        default:
            return fallback
        }
    }
}
