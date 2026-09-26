import MonacoCore
import Observation
import SwiftUI

/// What a matchup surface is showing. One value, so a screen cannot fall through to a
/// fabricated empty matchup while it waits.
enum MatchupLoadState<Value: Equatable>: Equatable {
    case loading
    case loaded(Value)
    case failed
    /// No session (a harness or a signed-out moment): the surface stays out of the way.
    case hidden

    var value: Value? {
        if case .loaded(let value) = self { return value }
        return nil
    }
}

/// Decides what a read's failure does to what is on screen. A quiet read (a poll) never takes
/// data away; a first read or one the member asked for says it failed.
enum MatchupLoadPolicy {
    static func afterFailure<Value>(_ error: Error, current: MatchupLoadState<Value>, quiet: Bool) -> MatchupLoadState<Value> {
        if isSignedOut(error) { return .hidden }
        if current.value != nil { return current }
        return quiet ? current : .failed
    }

    static func isSignedOut(_ error: Error) -> Bool {
        if case MonacoAPIError.missingAccessToken = error { return true }
        return false
    }
}

/// One cabal's matchup: the live pair, its record and results, and next week's challenges.
@Observable
@MainActor
final class GroupMatchupModel {
    let groupId: String
    private(set) var state: MatchupLoadState<GroupMatchupDTO> = .loading
    /// Challenges being accepted right now, so their buttons cannot be tapped twice.
    private(set) var accepting: Set<String> = []

    init(groupId: String, initial: GroupMatchupDTO? = nil) {
        self.groupId = groupId
        if let initial { state = .loaded(initial) }
    }

    /// `quiet` is a poll: it leaves the screen alone when it fails and rethrows so the loop backs off.
    func load(from source: any MatchupDataSource, quiet: Bool = false) async throws {
        if !quiet, state.value == nil { state = .loading }
        do {
            let dto = try await source.groupMatchup(groupId: groupId)
            if state.value != dto { state = .loaded(dto) }
        } catch {
            if error.isRequestCancellation { return }
            state = MatchupLoadPolicy.afterFailure(error, current: state, quiet: quiet)
            if quiet { throw error }
        }
    }

    /// Accepts a challenge and returns the toast that says how it went.
    func accept(_ challenge: MatchupChallengeDTO, from source: any MatchupDataSource) async -> MonacoToast? {
        guard !accepting.contains(challenge.id) else { return nil }
        accepting.insert(challenge.id)
        defer { accepting.remove(challenge.id) }
        do {
            _ = try await source.accept(groupId: groupId, challengeId: challenge.id)
            try? await load(from: source, quiet: true)
            return MonacoToast(message: MatchupCopy.challengeAccepted(opponent: challenge.opponent.name), isSuccess: true)
        } catch let refused as MatchupChallengeRefused {
            try? await load(from: source, quiet: true)
            return MonacoToast(message: MatchupCopy.refusal(refused.reason))
        } catch {
            if error.isRequestCancellation { return nil }
            return MonacoToast(message: MatchupCopy.acceptFailed)
        }
    }
}

/// Home's "This week": every matchup the member's cabals are in.
@Observable
@MainActor
final class HomeMatchupsModel {
    private(set) var state: MatchupLoadState<HomeMatchupsDTO> = .loading

    func load(from source: any MatchupDataSource, quiet: Bool = false) async throws {
        do {
            let dto = try await source.homeMatchups()
            if state.value != dto { state = .loaded(dto) }
        } catch {
            if error.isRequestCancellation { return }
            state = MatchupLoadPolicy.afterFailure(error, current: state, quiet: quiet)
            if quiet { throw error }
        }
    }

    func retry(from source: any MatchupDataSource) async {
        state = .loading
        try? await load(from: source)
    }
}

/// The season table.
@Observable
@MainActor
final class MatchupTableModel {
    private(set) var state: MatchupLoadState<MatchupTableDTO> = .loading

    func load(from source: any MatchupDataSource, quiet: Bool = false) async throws {
        if !quiet, state.value == nil { state = .loading }
        do {
            let dto = try await source.table()
            if state.value != dto { state = .loaded(dto) }
        } catch {
            if error.isRequestCancellation { return }
            state = MatchupLoadPolicy.afterFailure(error, current: state, quiet: quiet)
            if quiet { throw error }
        }
    }
}

/// Sending challenges from one cabal: which cabals have been challenged in this visit, and which
/// send is in flight.
@Observable
@MainActor
final class ChallengeSendModel {
    let groupId: String
    private(set) var sent: Set<String> = []
    private(set) var sending: Set<String> = []

    init(groupId: String, alreadyChallenged: Set<String> = []) {
        self.groupId = groupId
        sent = alreadyChallenged
    }

    func send(to opponent: GroupDiscoveryRowDTO, from source: any MatchupDataSource) async -> MonacoToast? {
        guard !sending.contains(opponent.groupID), !sent.contains(opponent.groupID) else { return nil }
        sending.insert(opponent.groupID)
        defer { sending.remove(opponent.groupID) }
        do {
            _ = try await source.challenge(groupId: groupId, opponentId: opponent.groupID)
            sent.insert(opponent.groupID)
            Haptics.success()
            return MonacoToast(message: MatchupCopy.challengeSent(to: opponent.name), isSuccess: true)
        } catch let refused as MatchupChallengeRefused {
            return MonacoToast(message: MatchupCopy.refusal(refused.reason))
        } catch {
            if error.isRequestCancellation { return nil }
            return MonacoToast(message: MatchupCopy.refusal(nil))
        }
    }
}
