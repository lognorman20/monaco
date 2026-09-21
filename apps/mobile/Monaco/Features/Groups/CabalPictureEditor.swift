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

    /// Which writes a refresh has to be judged against. Bumped when a write starts
    /// and again when it ends, so a refresh captured before or during a write never
    /// carries the value current once that write has finished.
    private var writeGeneration = 0

    /// Where a refresh stands relative to this editor's writes. A refresh takes one
    /// before it sends its request and hands it back with the reply.
    struct RefreshTicket: Equatable {
        fileprivate let generation: Int
    }

    /// Call before the refresh's request goes out, not when its reply lands.
    func beginRefresh() -> RefreshTicket {
        RefreshTicket(generation: writeGeneration)
    }

    /// Adopts a picture that arrived from a refresh, unless a write started since
    /// that refresh was sent.
    ///
    /// A refresh sent before a write (or while one was in flight) can be answered
    /// with the picture the cabal had before it, however late it lands. Adopting
    /// that reply would put the old picture back after the new one was saved, which
    /// reads to the member as the upload silently undoing itself. Such a reply is
    /// dropped; the next refresh, sent after the write, carries the truth.
    func adoptFromRefresh(_ refreshed: String?, ticket: RefreshTicket) {
        guard !isWorking, ticket.generation == writeGeneration else { return }
        pictureUrl = CabalPictureEditor.normalised(refreshed)
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
        writeGeneration += 1
        lastFailure = nil
        defer {
            isWorking = false
            writeGeneration += 1
        }

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
    /// of the same name; these writes go through the core client, whose error is
    /// `.httpStatus(Int)`, `.rejected(status:message:)` and
    /// `.rateLimited(retryAfterSeconds:)`.
    static func failureMessage(for error: Error, fallback: String) -> String {
        if case CabalPictureWriteError.notSignedIn = error {
            return "Sign in again to change the cabal picture."
        }
        // The writer has already reported the rejected token; the session is ending.
        if error is RejectedSession {
            return LoginFailureCopy.sessionExpired
        }
        guard let apiError = error as? MonacoCore.MonacoAPIError else {
            if let urlError = error as? URLError, urlError.code != .cancelled {
                return "Could not reach Monaco. Check your connection."
            }
            return fallback
        }
        switch apiError {
        case .rejected(_, let message)
            where !message.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty:
            return message
        case .rateLimited(let retryAfterSeconds):
            if let retryAfterSeconds, retryAfterSeconds > 0 {
                return "Too many changes. Try again in \(retryAfterSeconds)s."
            }
            return "Too many changes. Try again in a minute."
        default:
            break
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
/// it was opened with, and a stale one would 401 every write from then on. The
/// token a call actually sent is the one a 401 is reported against, through the
/// guarded `signOutAfterRejectedSession(rejectedToken:)`, so a reply that outlived
/// its sign-in cannot end the session that replaced it.
@MainActor
struct LiveCabalPictureWriter: CabalPictureWriting {
    private let currentToken: () -> String?
    private let reportRejected: (String) async -> Void
    private let makeClient: (String) -> MonacoCore.MonacoAPIClient

    init(auth: DynamicAuthService) {
        self.init(
            currentToken: { [weak auth] in auth?.accessToken },
            reportRejected: { [weak auth] token in
                await auth?.signOutAfterRejectedSession(rejectedToken: token)
            },
            makeClient: { token in
                MonacoCore.MonacoAPIClient(baseURL: Config.apiBaseURL, accessTokenProvider: { token })
            }
        )
    }

    /// The seams tests drive: where the token comes from, what a 401 reports, and
    /// the client a token is sent with.
    init(
        currentToken: @escaping () -> String?,
        reportRejected: @escaping (String) async -> Void,
        makeClient: @escaping (String) -> MonacoCore.MonacoAPIClient
    ) {
        self.currentToken = currentToken
        self.reportRejected = reportRejected
        self.makeClient = makeClient
    }

    func uploadPicture(groupId: String, imageData: Data, mimeType: String) async throws -> String? {
        try await sending { client in
            try await client.uploadCabalPicture(groupID: groupId, imageData: imageData, mimeType: mimeType).pictureUrl
        }
    }

    func removePicture(groupId: String) async throws -> String? {
        try await sending { client in
            try await client.removeCabalPicture(groupID: groupId).pictureUrl
        }
    }

    /// Runs one write with the token current now. A 401 (the transport has already
    /// tried a refresh) is reported against that token and comes back as
    /// `RejectedSession`.
    private func sending<T>(_ write: (MonacoCore.MonacoAPIClient) async throws -> T) async throws -> T {
        guard let token = currentToken(), !token.isEmpty else {
            throw CabalPictureWriteError.notSignedIn
        }
        do {
            return try await write(makeClient(token))
        } catch MonacoCore.MonacoAPIError.httpStatus(401) {
            await reportRejected(token)
            throw RejectedSession(token: token)
        }
    }
}
