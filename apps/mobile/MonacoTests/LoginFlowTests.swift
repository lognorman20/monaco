import Testing
@testable import Monaco

/// The rule under test: a failed request never takes the code field away. Only the member
/// asking to change their number (or signing out) goes back to the address step.
struct LoginFlowTests {
    private func onCodeStep(destination: String = "+15555550123") -> LoginFlow {
        var flow = LoginFlow()
        let started = flow.beginSend()
        #expect(started)
        flow.sendSucceeded(destination: destination)
        return flow
    }

    @Test func sendingACodeMovesToTheCodeStep() {
        let flow = onCodeStep()
        #expect(flow.isCodeEntry)
        #expect(flow.destination == "+15555550123")
        #expect(flow.phase == .awaitingCode)
    }

    @Test func aThrottledResendKeepsTheCodeStep() {
        var flow = onCodeStep()
        let resent = flow.beginSend()
        #expect(resent)
        flow.sendFailed(message: "Too many attempts. Try again in a minute.")

        // The first text may still be on its way: the member must be able to type it in.
        #expect(flow.isCodeEntry)
        #expect(flow.destination == "+15555550123")
        #expect(flow.phase == .failed(message: "Too many attempts. Try again in a minute."))
    }

    @Test func sendingStaysOnTheCodeStepSoTheFieldDoesNotFlicker() {
        var flow = onCodeStep()
        let started = flow.beginSend()
        #expect(started)
        #expect(flow.isCodeEntry)
        #expect(flow.phase == .sendingCode)
    }

    @Test func aRejectedOrRateLimitedVerifyKeepsTheCodeStep() {
        var flow = onCodeStep()
        let started = flow.beginVerify()
        #expect(started)
        flow.verifyFailed(message: "That code didn't work. Try again.")

        #expect(flow.isCodeEntry)
        #expect(flow.phase == .failed(message: "That code didn't work. Try again."))
    }

    @Test func aFailedFirstSendStaysOnTheAddressStep() {
        var flow = LoginFlow()
        let started = flow.beginSend()
        #expect(started)
        flow.sendFailed(message: "Couldn't send the code. Try again.")

        #expect(!flow.isCodeEntry)
        #expect(flow.destination == nil)
    }

    @Test func onlyTheMemberGoesBackToTheAddressStep() {
        var flow = onCodeStep()
        flow.returnToAddressEntry()

        #expect(!flow.isCodeEntry)
        #expect(flow.phase == .idle)
    }

    @Test func aSecondTapCannotStartASecondRequest() {
        var flow = LoginFlow()
        let started = flow.beginSend()
        let secondSend = flow.beginSend()
        let verifyWhileSending = flow.beginVerify()

        #expect(started)
        #expect(!secondSend)
        #expect(!verifyWhileSending)
    }

    @Test func aRestoredSessionIsNotSentBackToTheLoginForm() {
        var flow = LoginFlow(phase: .restoring)
        flow.returnToAddressEntry()
        #expect(flow.phase == .restoring)

        flow.authenticated(userID: "user-1")
        flow.returnToAddressEntry()
        #expect(flow.phase == .authenticated(userID: "user-1"))
    }

    @Test func signingOutClearsTheForm() {
        var flow = onCodeStep()
        flow.signedOut()

        #expect(!flow.isCodeEntry)
        #expect(flow.phase == .idle)
    }
}
