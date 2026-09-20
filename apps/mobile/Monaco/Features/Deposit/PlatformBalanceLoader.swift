import Combine
import MonacoCore
import SwiftUI

/// The account balance behind Add money and Cash out, and the rule that a reload never takes
/// the screen away from the member.
///
/// Both screens used to key their whole layout off a transient `isLoadingBalance` flag. Every
/// reload — after a successful fund, and again whenever the hourly token rotation restarted
/// `.task(id: auth.accessToken)` — flipped that flag, so the amount pad and the bottom button
/// were torn out of the hierarchy, the keyboard dropped, and a spinner took their place mid-entry.
/// The same flag turned a cancelled request into "No connection".
///
/// Here the state is what we have, not what we are doing:
/// - the spinner belongs to "nothing to show yet", so it only ever appears on the first load;
/// - a refresh that fails leaves the last good balance on screen;
/// - a cancelled request is not a failure, because nobody asked for it.
@MainActor
final class PlatformBalanceLoader: ObservableObject {
    enum Phase: Equatable {
        /// Nothing to show yet: the first load is still running.
        case loading
        /// A balance the member can act on. A refresh in flight never leaves this state.
        case loaded(PlatformBalanceDTO)
        /// The first load failed and there is nothing to show. Carries what to tell the member.
        case failed(String)
    }

    @Published private(set) var balance: PlatformBalanceDTO?
    @Published private(set) var failure: String?
    @Published private(set) var isLoading = false

    private let fetch: (String) async throws -> PlatformBalanceDTO
    private var requestsInFlight = 0

    init(fetch: @escaping (String) async throws -> PlatformBalanceDTO) {
        self.fetch = fetch
    }

    convenience init(apiClient: MonacoAPIClient = MonacoAPIClient()) {
        self.init { token in try await apiClient.getPlatformBalance(accessToken: token) }
    }

    var phase: Phase {
        if let balance { return .loaded(balance) }
        if let failure, !isLoading { return .failed(failure) }
        return .loading
    }

    /// The load the member asked for: on appear, and on Try again.
    func load(accessToken: String?) async {
        guard let accessToken else {
            balance = nil
            failure = "Sign in to see your account balance."
            return
        }

        requestsInFlight += 1
        isLoading = true
        defer {
            requestsInFlight -= 1
            isLoading = requestsInFlight > 0
        }

        do {
            balance = try await fetch(accessToken)
            failure = nil
        } catch {
            // `.task(id: auth.accessToken)` restarts this load whenever the token rotates, which
            // cancels the request in flight. That is bookkeeping, not something to report.
            guard !error.isRequestCancellation else { return }
            // A balance already on screen is better than an error message replacing it.
            guard balance == nil else { return }
            failure = Self.message(for: error)
        }
    }

    /// The background poll: never a spinner, never an error on screen. Rethrows so the caller's
    /// poll loop can back off, and returns the fresh balance for callers that announce new money.
    @discardableResult
    func refresh(accessToken: String?) async throws -> PlatformBalanceDTO? {
        guard let accessToken else { return nil }
        let fresh = try await fetch(accessToken)
        balance = fresh
        failure = nil
        return fresh
    }

    static func message(for error: Error) -> String {
        if case MonacoAPIError.httpStatus = error { return "Couldn't load your balance." }
        if case MonacoAPIError.apiError = error { return "Couldn't load your balance." }
        return "No connection. Check your internet."
    }
}
