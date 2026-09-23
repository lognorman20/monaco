import Foundation

/// The sign-in state machine, split in two: `step` is where the member is (which field is
/// on screen), `phase` is what just happened to their last request.
///
/// They used to be one enum, so any failure dropped the form back to the address field.
/// A member who tapped "Send a new code" a moment too early — and was throttled — lost the
/// code box while the first text was still arriving, with nothing to do but ask for another
/// code that would be throttled again. Here a failure only changes the phase: the step moves
/// back to the address field when the member asks for it, and at no other time.
///
/// Pure value type, so every transition is unit-testable without Dynamic.
struct LoginFlow: Equatable {
    enum Step: Equatable {
        case enterAddress
        /// A code has been sent to `destination` and the member is typing it in.
        case enterCode(destination: String)

        var isCodeEntry: Bool {
            if case .enterCode = self { return true }
            return false
        }

        var destination: String? {
            if case .enterCode(let destination) = self { return destination }
            return nil
        }
    }

    enum Phase: Equatable {
        /// Launch: a previous sign-in exists and Dynamic is restoring it. The gate shows
        /// a splash, not the login form, until this resolves.
        case restoring
        /// The previous sign-in could not be checked right now (offline). Retryable;
        /// the user stays signed in.
        case restoreFailed(message: String)
        case idle
        case sendingCode
        case awaitingCode
        case verifyingCode
        /// The last request failed. The member stays on whatever step they were on.
        case failed(message: String)
        case authenticated(userID: String)
    }

    private(set) var step: Step = .enterAddress
    private(set) var phase: Phase

    init(phase: Phase = .idle) {
        self.phase = phase
    }

    /// True while a request the member started is still in flight.
    var isBusy: Bool {
        phase == .sendingCode || phase == .verifyingCode
    }

    var isCodeEntry: Bool { step.isCodeEntry }
    var destination: String? { step.destination }

    // MARK: Sending a code

    /// Returns false when a request is already in flight, so a second tap can't send a
    /// second code (or start a second sign-in).
    mutating func beginSend() -> Bool {
        guard !isBusy else { return false }
        phase = .sendingCode
        return true
    }

    mutating func sendSucceeded(destination: String) {
        step = .enterCode(destination: destination)
        phase = .awaitingCode
    }

    mutating func sendFailed(message: String) {
        phase = .failed(message: message)
    }

    // MARK: Verifying a code

    mutating func beginVerify() -> Bool {
        guard !isBusy else { return false }
        phase = .verifyingCode
        return true
    }

    mutating func verifyFailed(message: String) {
        phase = .failed(message: message)
    }

    mutating func authenticated(userID: String) {
        step = .enterAddress
        phase = .authenticated(userID: userID)
    }

    // MARK: Leaving the code step

    /// "Change number", switching sign-in method: the only way back to the address field.
    /// A no-op once a session exists or is being restored.
    mutating func returnToAddressEntry() {
        switch phase {
        case .authenticated, .restoring, .restoreFailed:
            return
        default:
            step = .enterAddress
            phase = .idle
        }
    }

    mutating func signedOut() {
        step = .enterAddress
        phase = .idle
    }

    mutating func restoring() {
        step = .enterAddress
        phase = .restoring
    }

    mutating func restoreFailed(message: String) {
        phase = .restoreFailed(message: message)
    }
}
