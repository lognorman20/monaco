import Foundation
import MonacoCore
import Observation

/// The invite card's state on a cabal's details sheet: the live code, loaded when the sheet
/// opens, and "New code", which retires it.
@MainActor
@Observable
final class CabalInviteModel {
    enum State: Equatable {
        case loading
        case loaded(InviteDTO)
        case failed(String)
    }

    let groupId: String
    private(set) var state: State = .loading
    private(set) var isRenewing = false

    @ObservationIgnored private let source: InviteSource

    init(groupId: String, source: InviteSource) {
        self.groupId = groupId
        self.source = source
    }

    var invite: InviteDTO? {
        if case .loaded(let invite) = state { return invite }
        return nil
    }

    /// The member opened the sheet to share: a failure here is theirs to see, with a retry.
    func load() async {
        if invite == nil { state = .loading }
        do {
            state = .loaded(try await source.currentInvite(groupId: groupId))
        } catch {
            guard !error.isRequestCancellation else { return }
            if invite == nil { state = .failed(CabalInviteCopy.loadFailed(error)) }
        }
    }

    /// Replaces the code. Returns the toast to show, success or failure; the card keeps the
    /// old code on screen if the new one could not be made.
    func renew() async -> MonacoToast? {
        guard !isRenewing else { return nil }
        isRenewing = true
        defer { isRenewing = false }
        do {
            state = .loaded(try await source.newInvite(groupId: groupId))
            return MonacoToast(message: CabalInviteCopy.newCodeDone, isSuccess: true)
        } catch {
            guard !error.isRequestCancellation else { return nil }
            return MonacoToast(message: CabalInviteCopy.newCodeFailed(error))
        }
    }
}
