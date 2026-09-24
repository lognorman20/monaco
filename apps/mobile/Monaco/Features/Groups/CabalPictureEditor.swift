import Combine
import Foundation
import MonacoCore

/// Why a picture write could not even be attempted. The API's own errors
/// cover everything that can happen once a request is on the wire.
enum CabalPictureWriteError: Error {
    case notSignedIn
}

/// How a cabal picture write ended.
enum CabalPictureOutcome: Equatable {
    /// The picture the cabal now has; nil after a removal.
    case saved(String?)
    /// Copy to show the member. Already phrased for them.
    case failed(String)
}

/// What the cabal picture needs the network to do. A protocol so the editor can
/// be driven in tests without a server, and so the view does not have to know
/// how a client is built.
@MainActor
protocol CabalPictureWriting {
    func uploadPicture(groupId: String, imageData: Data, mimeType: String) async throws -> String?
    func removePicture(groupId: String) async throws -> String?
}

/// Drives set, replace and remove for one cabal's picture.
///
/// It owns exactly two things: which picture is current, and whether a write is
/// in flight. Both matter to the view — the second is what stops a member
/// firing a second upload into a rate limit while the first is still going.
@MainActor
final class CabalPictureEditor: ObservableObject {
    /// The picture the cabal has right now, as far as this screen knows.
    @Published private(set) var pictureUrl: String?
    /// True while a write is in flight.
    @Published private(set) var isWorking = false
    /// The last failure, for the screen to toast. Cleared when a write starts.
    @Published private(set) var lastFailure: String?

    private let writer: CabalPictureWriting
    private let groupId: String

    init(groupId: String, pictureUrl: String?, writer: CabalPictureWriting) {
        self.groupId = groupId
        self.pictureUrl = pictureUrl
        self.writer = writer
    }

    /// Adopts a picture that arrived from a refresh, unless a write is in flight.
    ///
    /// Without the guard a refresh that started before an upload can land after
    /// it and put the old picture back, which reads to the member as the upload
    /// silently undoing itself.
    func adoptFromRefresh(_ refreshed: String?) {
        guard !isWorking else { return }
        pictureUrl = refreshed
    }

    func setPicture(imageData: Data, mimeType: String) async -> CabalPictureOutcome {
        await write(fallback: "Could not update the cabal picture. Try again.") { [groupId, writer] in
            try await writer.uploadPicture(groupId: groupId, imageData: imageData, mimeType: mimeType)
        }
    }

    func removePicture() async -> CabalPictureOutcome {
        await write(fallback: "Could not remove the cabal picture. Try again.") { [groupId, writer] in
            try await writer.removePicture(groupId: groupId)
        }
    }

    /// One in-flight write at a time. A second call while one is running is
    /// refused rather than queued: two uploads racing would leave the screen
    /// showing whichever finished last, not whichever the member picked last.
    private func write(
        fallback: String,
        _ work: () async throws -> String?
    ) async -> CabalPictureOutcome {
        guard !isWorking else {
            return .failed("Still working on the last change.")
        }
        isWorking = true
        lastFailure = nil
        defer { isWorking = false }

        do {
            let saved = try await work()
            pictureUrl = CabalPictureEditor.normalised(saved)
            return .saved(pictureUrl)
        } catch {
            // The picture on screen is deliberately left alone: the write failed,
            // so what the cabal has has not changed.
            let message = CabalPictureEditor.failureMessage(for: error, fallback: fallback)
            lastFailure = message
            return .failed(message)
        }
    }

    /// A blank URL is no picture. The server sends null, but a blank string
    /// would otherwise be loaded and fail forever.
    static func normalised(_ raw: String?) -> String? {
        guard let trimmed = raw?.trimmingCharacters(in: .whitespacesAndNewlines), !trimmed.isEmpty else {
            return nil
        }
        return trimmed
    }

    /// Server copy wins where the server wrote some: it knows which rule was
    /// broken ("only the cabal's creator..."), and this screen does not.
    ///
    /// `MonacoCore` is spelled out because the app target declares an error type
    /// of the same name; these writes go through the core client.
    static func failureMessage(for error: Error, fallback: String) -> String {
        if case CabalPictureWriteError.notSignedIn = error {
            return "Sign in again to change the cabal picture."
        }
        guard let apiError = error as? MonacoCore.MonacoAPIError else {
            if let urlError = error as? URLError, urlError.code != .cancelled {
                return "Could not reach Monaco. Check your connection."
            }
            return fallback
        }
        if case .rejected(_, let message, _) = apiError, !message.isEmpty {
            return message
        }
        if case .rateLimited(let retryAfterSeconds, _) = apiError {
            if let retryAfterSeconds, retryAfterSeconds > 0 {
                return "Too many changes. Try again in \(retryAfterSeconds)s."
            }
            return "Too many changes. Try again in a minute."
        }
        switch apiError.statusCode {
        case 403: return "Only the cabal's creator can change its picture."
        case 404: return "This cabal is no longer available."
        case 413: return "That picture is too big. Try another."
        case 503: return "Cabal pictures are not set up on this server."
        default: return fallback
        }
    }
}

/// The live writer: talks to the backend with the signed-in member's token.
///
/// The token is read per call, not captured once: a screen can outlive the token
/// it was opened with, and a stale one would 401 every write from then on.
@MainActor
struct LiveCabalPictureWriter: CabalPictureWriting {
    let accessToken: () -> String?

    private func client() throws -> MonacoCore.MonacoAPIClient {
        guard let token = accessToken(), !token.isEmpty else {
            throw CabalPictureWriteError.notSignedIn
        }
        return MonacoCore.MonacoAPIClient(baseURL: Config.apiBaseURL, accessTokenProvider: { token })
    }

    func uploadPicture(groupId: String, imageData: Data, mimeType: String) async throws -> String? {
        try await client().uploadCabalPicture(groupID: groupId, imageData: imageData, mimeType: mimeType).pictureUrl
    }

    func removePicture(groupId: String) async throws -> String? {
        try await client().removeCabalPicture(groupID: groupId).pictureUrl
    }
}
