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
    /// Issued in order, one per request, so responses can only ever be applied in that order.
    private var lastIssuedToken: UInt64 = 0
    private var lastAppliedToken: UInt64 = 0

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
            // Signed out. Nothing already in flight may repopulate the screen behind that.
            lastAppliedToken = lastIssuedToken
            balance = nil
            failure = "Sign in to see your account balance."
            return
        }

        let requestToken = issueToken()
        requestsInFlight += 1
        isLoading = true
        defer {
            requestsInFlight -= 1
            isLoading = requestsInFlight > 0
        }

        do {
            let fresh = try await fetch(accessToken)
            apply(fresh, token: requestToken)
        } catch {
            // `.task(id: auth.accessToken)` restarts this load whenever the token rotates, which
            // cancels the request in flight. That is bookkeeping only while another load is
            // already queued to take its place — `requestsInFlight` still counts this one, so
            // anything above 1 is that replacement, and the spinner belongs to it.
            //
            // A cancellation with nothing behind it has to be reported. URLSession returns -999
            // for more than a cancelled task, and swallowing it silently left `balance` and
            // `failure` both nil, which `phase` reads as `.loading`: a spinner with no retry, no
            // form and no way off the screen.
            if error.isRequestCancellation, requestsInFlight > 1 { return }
            // A balance already on screen is better than an error message replacing it.
            guard balance == nil else { return }
            failure = Self.message(for: error)
        }
    }

    /// The background poll: never a spinner, never an error on screen. Rethrows so the caller's
    /// poll loop can back off.
    ///
    /// Returns the fresh balance, or `nil` when the response was already out of date on arrival
    /// and nothing was written — there is no new money to announce either way.
    @discardableResult
    func refresh(accessToken: String?) async throws -> PlatformBalanceDTO? {
        guard let accessToken else { return nil }
        let requestToken = issueToken()
        let fresh = try await fetch(accessToken)
        guard apply(fresh, token: requestToken) else { return nil }
        return fresh
    }

    /// Writes a response only when it is newer than the last one written.
    ///
    /// The 3s poll and the reload after a fund overlap: a tick fetched before the money moved can
    /// land after the reload that followed it and restore the pre-fund figure. Add money and Cash
    /// out both derive their submit limit from this number, so a stale write lets a member spend a
    /// balance that is already gone. `requestsInFlight` only drives the spinner; it does not order
    /// writes.
    @discardableResult
    private func apply(_ fresh: PlatformBalanceDTO, token: UInt64) -> Bool {
        guard token > lastAppliedToken else { return false }
        lastAppliedToken = token
        balance = fresh
        failure = nil
        return true
    }

    private func issueToken() -> UInt64 {
        lastIssuedToken += 1
        return lastIssuedToken
    }

    static func message(for error: Error) -> String {
        if case MonacoAPIError.httpStatus = error { return "Couldn't load your balance." }
        if case MonacoAPIError.apiError = error { return "Couldn't load your balance." }
        return "No connection. Check your internet."
    }
}
