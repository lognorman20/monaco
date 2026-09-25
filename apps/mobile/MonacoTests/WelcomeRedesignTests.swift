import Foundation
import MonacoCore
import Testing
@testable import Monaco

/// The one line under a sign-in field: whichever of the error, the hint and the explainer is
/// most pressing, and never two of them at once.
@MainActor
struct OTPFieldCaptionTests {
    private let explainer = OTPDestination.sms.caption
    private let hint = OTPDestination.sms.invalidHint

    private func caption(
        _ phase: LoginFlow.Phase,
        codeStep: Bool = false,
        invalid: Bool = false,
        resent: Bool = false
    ) -> OTPFieldCaption? {
        OTPFieldCaption.resolve(
            phase: phase,
            isCodeStep: codeStep,
            explainer: explainer,
            invalidHint: invalid ? hint : nil,
            resent: resent
        )
    }

    @Test func anUntouchedAddressStepSaysWhatTheButtonWillDo() {
        #expect(caption(.idle) == .note(explainer))
        #expect(caption(.sendingCode) == .note(explainer))
    }

    @Test func aNumberThatCannotBeTextedGetsTheHintInsteadOfTheExplainer() {
        #expect(caption(.idle, invalid: true) == .hint(hint))
    }

    @Test func aFailureWinsOnEitherStep() {
        let message = LoginFailureCopy.message(for: .rateLimited, step: .sendCode)
        #expect(caption(.failed(message: message), invalid: true) == .error(message))
        #expect(caption(.failed(message: message), codeStep: true, resent: true) == .error(message))
        #expect(caption(.failed(message: message))?.isError == true)
    }

    /// The read-back above the code field already says where the code went.
    @Test func theCodeStepIsQuietUntilThereIsSomethingToSay() {
        #expect(caption(.awaitingCode, codeStep: true) == nil)
        #expect(caption(.verifyingCode, codeStep: true) == nil)
    }

    @Test func aResendIsSaidWhileItRunsAndOnceItLands() {
        #expect(caption(.sendingCode, codeStep: true) == .note(OTPFieldCaption.sendingNewCode))
        #expect(caption(.awaitingCode, codeStep: true, resent: true) == .note(OTPFieldCaption.newCodeSent))
    }
}

/// The button says what it will do, or what it is doing.
@MainActor
struct OTPPrimaryActionTests {
    @Test func theAddressStepSendsACode() {
        #expect(OTPPrimaryAction.title(phase: .idle, isCodeStep: false) == "Send code")
        #expect(OTPPrimaryAction.title(phase: .sendingCode, isCodeStep: false) == "Sending code\u{2026}")
    }

    @Test func theCodeStepSignsIn() {
        #expect(OTPPrimaryAction.title(phase: .awaitingCode, isCodeStep: true) == "Continue")
        #expect(OTPPrimaryAction.title(phase: .verifyingCode, isCodeStep: true) == "Signing you in\u{2026}")
    }

    /// A new code on its way is not the member signing in.
    @Test func aResendDoesNotRenameTheButton() {
        #expect(OTPPrimaryAction.title(phase: .sendingCode, isCodeStep: true) == "Continue")
    }
}

/// How the number a code went to reads back.
@MainActor
struct PhoneReadBackTests {
    @Test func aNorthAmericanNumberReadsBackInTheShapeTheHintAsksFor() {
        #expect(PhoneReadBack.format("+15551234567") == "+1 555 123 4567")
    }

    @Test func whatAutofillHandsTheFieldReadsBackTheSameWay() throws {
        let parsed = try #require(E164PhoneNumber("+1 (555) 123-4567"))
        #expect(PhoneReadBack.format(parsed.value) == "+1 555 123 4567")
    }

    /// Where the spaces go depends on the country, which the app does not look up.
    @Test func otherCountriesReadBackAsSent() {
        #expect(PhoneReadBack.format("+447700900123") == "+447700900123")
    }

    @Test func anythingElseComesBackUntouched() {
        #expect(PhoneReadBack.format("+1555") == "+1555")
        #expect(PhoneReadBack.format("") == "")
    }
}

/// Which ways in the form offers.
@MainActor
struct LoginMethodTests {
    @Test func textingComesFirstWhenBothAreOn() {
        #expect(LoginMethod.available(sms: true, email: true) == [.sms, .email])
    }

    @Test func onlyTheMethodsTurnedOnAreOffered() {
        #expect(LoginMethod.available(sms: false, email: true) == [.email])
        #expect(LoginMethod.available(sms: true, email: false) == [.sms])
        #expect(LoginMethod.available(sms: false, email: false).isEmpty)
    }
}

/// The words on the way in.
@MainActor
struct WelcomeCopyTests {
    @Test func firstRunSaysWhatHappensAfterTheName() {
        #expect(FirstRunCopy.nextSteps == [
            "Start a cabal or join one",
            "Add money to the pot",
            "Vote on every buy",
        ])
    }

    /// Nothing on the way in says wallet, group, treasury or the rest of the banned list.
    @Test func theWayInUsesTheProductsWords() {
        let strings = FirstRunCopy.nextSteps + [
            FirstRunCopy.nextStepsTitle,
            SessionGateCopy.restoreFailedTitle,
            SessionGateCopy.openFailedTitle,
            OTPFieldCaption.sendingNewCode,
            OTPFieldCaption.newCodeSent,
            OTPDestination.sms.caption,
            OTPDestination.sms.invalidHint,
            OTPDestination.sms.changeLabel,
            OTPDestination.email.caption,
            OTPDestination.email.invalidHint,
            OTPDestination.email.changeLabel,
            OTPPrimaryAction.title(phase: .idle, isCodeStep: false),
            OTPPrimaryAction.title(phase: .sendingCode, isCodeStep: false),
            OTPPrimaryAction.title(phase: .awaitingCode, isCodeStep: true),
            OTPPrimaryAction.title(phase: .verifyingCode, isCodeStep: true),
        ]
        #expect(MainFlowCopyAudit.stringsAreClean(strings))
    }

    /// The gate's title and the generic message under it used to be the same sentence.
    @Test func theGateTitleDoesNotRepeatTheMessageUnderIt() throws {
        struct SomethingElse: Error {}
        let base = try #require(URL(string: "https://example.com"))
        let generic = SessionErrorMapping.describe(SomethingElse(), apiBaseURL: base)
        #expect(generic.message == "Couldn't open Monaco. Try again.")
        #expect(!generic.message.hasPrefix(SessionGateCopy.openFailedTitle))
    }
}
