import DynamicSDKSwift
import Foundation
import MonacoCore
import Testing
@testable import Monaco

/// What `DynamicAuthService` decides on its own, without a Dynamic environment. The guard
/// types it is built from (`SessionTokenLedger`, `PendingRevoke`, `LoginFlow`,
/// `E164PhoneNumber`) have their own suites; these check the service wires them the way
/// sign-in and sign-out rely on.
@MainActor
final class DynamicAuthServiceTests {
    private static let unconfigured = DynamicAuthSettings(
        environmentID: "",
        smsLoginEnabled: true,
        emailLoginEnabled: true
    )

    /// Each test gets its own defaults suite. Signing out clears the session markers, and
    /// with the standard defaults that would wipe the test host's stored login and race the
    /// other tests, which Swift Testing runs in parallel.
    private let defaultsSuite = "DynamicAuthServiceTests.\(UUID().uuidString)"

    private var defaults: UserDefaults { UserDefaults(suiteName: defaultsSuite)! }

    private func makeService() -> DynamicAuthService {
        DynamicAuthService(settings: Self.unconfigured, sessionStore: MonacoSessionStore(defaults: defaults))
    }

    deinit {
        UserDefaults.standard.removePersistentDomain(forName: defaultsSuite)
    }

    /// Signing out clears the store the service was given, and only that one.
    @Test func signOutClearsItsOwnSessionStoreAndNoOther() async {
        let standardHadLogin = UserDefaults.standard.object(forKey: "monaco.session.hasExplicitLogin")
        defaults.set(true, forKey: "monaco.session.hasExplicitLogin")
        let auth = makeService()
        #expect(auth.phase == .restoring)

        await auth.logout()

        #expect(!MonacoSessionStore(defaults: defaults).hasExplicitLogin)
        #expect(auth.phase == .idle)
        #expect(
            UserDefaults.standard.object(forKey: "monaco.session.hasExplicitLogin") as? Bool
                == standardHadLogin as? Bool
        )
    }

    /// The ballots remembered this session belong to the member who cast them. Signing out
    /// forgets them, so the next member on this device never sees "You voted" on a card.
    @Test func signOutForgetsTheSessionsBallots() async {
        let proposal = ProposalDTO(
            id: "p-sign-out", symbol: "AAPLc", status: "open", kind: "buy",
            usdcMicros: "25000000", canVote: false, votes: nil
        )
        ProposalVoteLedger.shared.record(.yes, for: "p-sign-out", viewerId: "member-1")
        #expect(ProposalVoteLedger.shared.choice(for: proposal, viewerId: "member-1") == "yes")
        let auth = makeService()

        await auth.logout()

        #expect(ProposalVoteLedger.shared.choice(for: proposal, viewerId: "member-1") == nil)
    }

    /// Dynamic's SMS API takes the number split up. The split is read off the one parser
    /// the form validated with, so there is no second rule about what is sendable.
    @Test func thePhoneNumberIsSplitTheWayDynamicTakesIt() throws {
        let us = try DynamicAuthService.phoneData(from: "+1 (555) 123-4567")
        #expect(us.dialCode == "+1")
        #expect(us.iso2 == "US")
        #expect(us.phone == "5551234567")

        let singapore = try DynamicAuthService.phoneData(from: "+65 9123 4567")
        #expect(singapore.dialCode == "+65")
        #expect(singapore.iso2 == "SG")
        #expect(singapore.phone == "91234567")
    }

    /// Main's old parser defaulted an unknown country code to the US, which texts a
    /// stranger. It is refused before the request instead.
    @Test func aNumberWithNoCountryWeCanNameIsRefused() {
        #expect(throws: DynamicAuthHTTPError.self) {
            try DynamicAuthService.phoneData(from: "+299 32 1234")
        }
        #expect(throws: DynamicAuthHTTPError.self) {
            try DynamicAuthService.phoneData(from: "3475757")
        }
    }

    /// A failed send notes the failure but never moves the member: the flow is idle again
    /// (not busy) and still on the address step it was on.
    @Test func aFailedSendLeavesTheFormUsable() async {
        let auth = makeService()

        await auth.sendSMSCode(to: "+15551234567")

        guard case .failed = auth.phase else {
            Issue.record("expected a failed phase, got \(auth.phase)")
            return
        }
        #expect(!auth.flow.isBusy)
        #expect(auth.loginStep == .enterAddress)
    }

    /// A 401 naming a token this session never used belongs to a session that has already
    /// ended. It must not end anything nor leave a reason on the login screen.
    @Test func aRejectedTokenFromNoLiveSessionIsIgnored() async {
        let auth = makeService()

        await auth.signOutAfterRejectedSession(rejectedToken: "someone-elses-token")
        await auth.signOut(reason: "Your session expired. Sign in again.", rejectedToken: "someone-elses-token")

        #expect(auth.lastSignOutReason == nil)
        #expect(auth.sessionIdentity == nil)
    }

    /// Sign-out is local-first: it returns with the session already gone, without waiting
    /// on the network, and a refresh for the old token cannot mint a new one. With no SDK
    /// configured no revoke is scheduled here; whether a revoke may run is
    /// `SessionEventGate.shouldRevoke`, covered in `SessionEventGateTests`.
    @Test func signOutReturnsAtOnceAndLeavesNothingToRefresh() async throws {
        let auth = makeService()

        await auth.logout()

        #expect(auth.accessToken == nil)
        #expect(auth.sessionIdentity == nil)
        #expect(auth.phase == .idle)
        let refreshed = try await auth.refreshedAccessToken(replacing: "old-token")
        #expect(refreshed == nil)
    }
}
