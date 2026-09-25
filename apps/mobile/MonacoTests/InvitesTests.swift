import Foundation
import MonacoCore
import Testing
@testable import Monaco

// MARK: - Fakes

@MainActor
private final class FakeInviteSource: InviteSource {
    var previews: [String: InvitePreviewDTO] = [:]
    var previewError: Error?
    var previewDelays: [String: Duration] = [:]
    private(set) var previewCalls: [String] = []

    var current: Result<InviteDTO, Error> = .success(InviteDTO(code: "K7QM4XPD", url: "https://trymonaco.xyz/join/K7QM4XPD"))
    var renewed: Result<InviteDTO, Error> = .success(InviteDTO(code: "R8WN3HQT", url: "https://trymonaco.xyz/join/R8WN3HQT"))

    func currentInvite(groupId: String) async throws -> InviteDTO { try current.get() }
    func newInvite(groupId: String) async throws -> InviteDTO { try renewed.get() }

    func preview(code: String) async throws -> InvitePreviewDTO {
        previewCalls.append(code)
        if let delay = previewDelays[code] { try await Task.sleep(for: delay) }
        if let previewError { throw previewError }
        guard let preview = previews[code] else {
            throw MonacoCore.MonacoAPIError.rejected(status: 404, message: "invite not found")
        }
        return preview
    }

    func join(code: String) async throws -> InviteJoinResult {
        InviteJoinResult(status: .joined, groupId: previews[code]?.groupId)
    }
}

private func preview(_ code: String, name: String = "Sunday Investors", policy: GroupJoinMode = .open, pot: String = "1240.50") -> InvitePreviewDTO {
    InvitePreviewDTO(
        code: code, groupId: "5b1f0c9e-0005-4c55-9a51-000000000005", name: name, memberCount: 9,
        tint: "indigo", pictureUrl: nil, joinPolicy: policy, potValueUsd: pot
    )
}

/// Waits for the model's lookup to settle, failing after a second.
@MainActor
private func settled(_ model: InviteCodeEntryModel) async {
    for _ in 0..<200 {
        if !model.isLoadingPreview { return }
        try? await Task.sleep(for: .milliseconds(5))
    }
    Issue.record("the preview never settled")
}

// MARK: - The join screen's code field

@MainActor
struct InviteCodeEntryModelTests {
    @Test func anEmptyFieldAsksForACodeOrLink() {
        let model = InviteCodeEntryModel(source: FakeInviteSource(), debounce: .zero)

        model.update(text: "")

        #expect(model.footer == .init(text: InviteEntryCopy.footer, tone: .neutral))
        #expect(!model.canJoin)
        #expect(model.preview == .idle)
    }

    @Test func halfACodeIsQuietAndAWrongOneIsNot() {
        let model = InviteCodeEntryModel(source: FakeInviteSource(), debounce: .zero)

        model.update(text: "K7QM 4")
        #expect(model.footer == .init(text: InviteEntryCopy.typing, tone: .neutral))
        #expect(!model.canJoin)

        model.update(text: "AB12CD34")
        #expect(model.footer == .init(text: JoinCabalCopy.malformedCode, tone: .warning))
        #expect(!model.canJoin)
    }

    @Test func aWholeCodeShowsItsCabal() async {
        let source = FakeInviteSource()
        source.previews["K7QM4XPD"] = preview("K7QM4XPD")
        let model = InviteCodeEntryModel(source: source, debounce: .zero)

        model.update(text: "k7qm-4xpd")
        #expect(model.preview == .loading(code: "K7QM4XPD"))
        await settled(model)

        #expect(model.loadedPreview?.name == "Sunday Investors")
        #expect(model.canJoin)
        #expect(model.footer == nil)
    }

    @Test(arguments: [
        "https://trymonaco.xyz/join/K7QM4XPD",
        "monaco://join/K7QM4XPD",
        "Join Sunday Investors on Monaco: https://trymonaco.xyz/join/K7QM4XPD",
    ])
    func aLinkOrTheShareTextBecomesItsCode(text: String) async {
        let source = FakeInviteSource()
        source.previews["K7QM4XPD"] = preview("K7QM4XPD")
        let model = InviteCodeEntryModel(source: source, debounce: .zero)

        model.update(text: text, immediately: true)
        await settled(model)

        #expect(model.link == .code("K7QM4XPD"))
        #expect(source.previewCalls == ["K7QM4XPD"])
    }

    @Test func aDeadCodeCannotBeJoined() async {
        let model = InviteCodeEntryModel(source: FakeInviteSource(), debounce: .zero)

        model.update(text: "ZZZZ2222")
        await settled(model)

        #expect(model.preview == .notFound(code: "ZZZZ2222"))
        #expect(!model.canJoin)
        #expect(model.footer == .init(text: InviteEntryCopy.notFound, tone: .warning))
    }

    @Test func anUnreachablePreviewStillLetsTheMemberJoinAndRetry() async {
        let source = FakeInviteSource()
        source.previewError = URLError(.notConnectedToInternet)
        let model = InviteCodeEntryModel(source: source, debounce: .zero)

        model.update(text: "K7QM4XPD")
        await settled(model)
        #expect(model.preview == .unavailable(code: "K7QM4XPD"))
        #expect(model.canJoin)
        #expect(model.footer == .init(text: InviteEntryCopy.unavailable, tone: .neutral))

        source.previewError = nil
        source.previews["K7QM4XPD"] = preview("K7QM4XPD")
        model.retryPreview()
        await settled(model)
        #expect(model.loadedPreview != nil)
    }

    @Test func aLegacyCabalIdJoinsWithoutAPreview() {
        let source = FakeInviteSource()
        let model = InviteCodeEntryModel(source: source, debounce: .zero)

        model.update(text: "5b1f0c9e-0005-4c55-9a51-000000000005")

        #expect(model.link == .groupId("5b1f0c9e-0005-4c55-9a51-000000000005"))
        #expect(model.canJoin)
        #expect(model.preview == .idle)
        #expect(source.previewCalls.isEmpty)
    }

    @Test func regroupingTheSameCodeDoesNotLookItUpAgain() async {
        let source = FakeInviteSource()
        source.previews["K7QM4XPD"] = preview("K7QM4XPD")
        let model = InviteCodeEntryModel(source: source, debounce: .zero)

        model.update(text: "K7QM4XPD")
        await settled(model)
        model.update(text: "K7QM 4XPD")

        #expect(source.previewCalls == ["K7QM4XPD"])
        #expect(model.loadedPreview != nil)
    }

    @Test func aSlowAnswerForAnOldCodeNeverReplacesTheNewOne() async {
        let source = FakeInviteSource()
        source.previews["K7QM4XPD"] = preview("K7QM4XPD", name: "Old answer")
        source.previews["R8WN3HQT"] = preview("R8WN3HQT", name: "Semis or bust")
        source.previewDelays["K7QM4XPD"] = .milliseconds(150)
        let model = InviteCodeEntryModel(source: source, debounce: .zero)

        model.update(text: "K7QM4XPD")
        try? await Task.sleep(for: .milliseconds(20))
        model.update(text: "R8WN3HQT")
        await settled(model)
        try? await Task.sleep(for: .milliseconds(200))

        #expect(model.loadedPreview?.name == "Semis or bust")
    }
}

// MARK: - The details sheet's invite card

@MainActor
struct CabalInviteModelTests {
    @Test func loadsTheLiveCode() async {
        let model = CabalInviteModel(groupId: "g", source: FakeInviteSource())

        await model.load()

        #expect(model.invite?.code == "K7QM4XPD")
    }

    @Test func aFailedLoadSaysSoAndOffersARetry() async {
        let source = FakeInviteSource()
        source.current = .failure(MonacoCore.MonacoAPIError.httpStatus(503))
        let model = CabalInviteModel(groupId: "g", source: source)

        await model.load()

        #expect(model.state == .failed(CabalInviteCopy.loadFailedMessage))
    }

    @Test func aNewCodeReplacesTheOldOneWithASuccessToast() async {
        let model = CabalInviteModel(groupId: "g", source: FakeInviteSource())
        await model.load()

        let toast = await model.renew()

        #expect(model.invite?.code == "R8WN3HQT")
        #expect(toast?.isSuccess == true)
        #expect(toast?.message == CabalInviteCopy.newCodeDone)
    }

    @Test func aFailedNewCodeKeepsTheOldOne() async {
        let source = FakeInviteSource()
        source.renewed = .failure(MonacoCore.MonacoAPIError.rateLimited(retryAfterSeconds: 5))
        let model = CabalInviteModel(groupId: "g", source: source)
        await model.load()

        let toast = await model.renew()

        #expect(model.invite?.code == "K7QM4XPD")
        #expect(toast?.isSuccess == false)
        #expect(toast?.message.contains("Too many") == true)
    }
}

// MARK: - A link that opened the app

@MainActor
struct PendingInviteStoreTests {
    private func defaults() -> UserDefaults {
        let name = "invites-tests-\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: name)!
        defaults.removePersistentDomain(forName: name)
        return defaults
    }

    @Test func anInviteLinkWaitsUntilItIsTakenOnce() throws {
        let store = PendingInviteStore(defaults: defaults())

        #expect(store.receive(try #require(URL(string: "https://trymonaco.xyz/join/k7qm4xpd"))))

        #expect(store.take()?.text == "K7QM4XPD")
        #expect(store.take() == nil)
    }

    /// SwiftUI can hand one universal link to both the URL and the activity handler.
    @Test func oneTapDeliveredTwiceOpensOneSheet() throws {
        var now = Date(timeIntervalSince1970: 1_800_000_000)
        let store = PendingInviteStore(defaults: defaults(), now: { now })
        let link = try #require(URL(string: "https://trymonaco.xyz/join/K7QM4XPD"))

        store.receive(link)
        #expect(store.take()?.text == "K7QM4XPD")
        now = now.addingTimeInterval(1)
        store.receive(link)
        #expect(store.take() == nil)

        now = now.addingTimeInterval(PendingInviteStore.duplicateWindow)
        store.receive(link)
        #expect(store.take()?.text == "K7QM4XPD", "the same link tapped again later opens again")
    }

    @Test func otherURLsAreLeftForOtherHandlers() throws {
        let store = PendingInviteStore(defaults: defaults())

        #expect(!store.receive(try #require(URL(string: "https://trymonaco.xyz/"))))
        #expect(!store.receive(try #require(URL(string: "monaco://settings"))))
        #expect(store.pending == nil)
    }

    @Test func theCodeSurvivesARelaunchDuringSignIn() throws {
        let shared = defaults()
        PendingInviteStore(defaults: shared).receive(try #require(URL(string: "monaco://join/K7QM4XPD")))

        let relaunched = PendingInviteStore(defaults: shared)

        #expect(relaunched.take()?.text == "K7QM4XPD")
    }

    @Test func aWeekOldLinkIsDropped() throws {
        var now = Date(timeIntervalSince1970: 1_800_000_000)
        let store = PendingInviteStore(defaults: defaults(), now: { now })
        store.receive(try #require(URL(string: "monaco://join/K7QM4XPD")))

        now = now.addingTimeInterval(PendingInviteStore.lifetime + 1)

        #expect(store.take() == nil)
        #expect(store.pending == nil)
    }
}

// MARK: - QR code

@MainActor
struct InviteQRCodeTests {
    @Test func theLinkBecomesAWellFormedSymbol() throws {
        let matrix = try #require(InviteQRCode.matrix(for: "https://trymonaco.xyz/join/K7QM4XPD"))

        // Versions run 21, 25, 29… modules a side.
        #expect(matrix.size >= 21 && (matrix.size - 17) % 4 == 0)
        #expect(matrix.modules.count == matrix.size * matrix.size)
    }

    /// Finder patterns sit top-left, top-right and bottom-left: the symbol is not mirrored.
    @Test func theSymbolIsTheRightWayUp() throws {
        let matrix = try #require(InviteQRCode.matrix(for: "https://trymonaco.xyz/join/K7QM4XPD"))
        let last = matrix.size - 1

        func finder(row: Int, column: Int) -> Bool {
            // A finder is a dark 7×7 ring around a light ring around a dark 3×3 core.
            (0..<7).allSatisfy { matrix.isDark(row: row, column: column + $0) && matrix.isDark(row: row + 6, column: column + $0) }
                && !matrix.isDark(row: row + 1, column: column + 1)
                && matrix.isDark(row: row + 3, column: column + 3)
        }

        #expect(finder(row: 0, column: 0))
        #expect(finder(row: 0, column: last - 6))
        #expect(finder(row: last - 6, column: 0))
        #expect(!finder(row: last - 6, column: last - 6))
    }
}

// MARK: - Copy

@MainActor
struct InviteCopyTests {
    @Test func theButtonNamesThePreviewedCabal() {
        #expect(JoinCabalScreenCopy.actionTitle(cabalName: "Sunday Investors", joinMode: .open, isJoining: false, requestPending: false) == "Join Sunday Investors")
        #expect(JoinCabalScreenCopy.actionTitle(cabalName: "Semis or bust", joinMode: .request, isJoining: false, requestPending: false) == "Ask to join Semis or bust")
        #expect(JoinCabalScreenCopy.actionTitle(cabalName: "Semis or bust", joinMode: .request, isJoining: false, requestPending: true) == "Request sent")
        #expect(JoinCabalScreenCopy.actionTitle(cabalName: "Sunday Investors", joinMode: .open, isJoining: true, requestPending: false) == "Joining…")
    }

    @Test func thePreviewLineCarriesMembersAndPot() {
        #expect(InviteEntryCopy.facts(for: preview("K7QM4XPD")) == "9 members · $1,240.50 in the pot")
        #expect(InviteEntryCopy.facts(for: preview("K7QM4XPD", pot: "0.00")) == "9 members · Nothing in the pot yet")
        #expect(InviteEntryCopy.potLine("lots") == nil)
    }

    @Test func joinFailuresFromTheCodeRouteReadAsWords() {
        #expect(JoinCabalCopy.failureMessage(for: MonacoCore.MonacoAPIError.rejected(status: 404, message: "invite not found"), enteredCode: true).contains("invite code"))
        #expect(JoinCabalCopy.failureMessage(for: MonacoCore.MonacoAPIError.rateLimited(retryAfterSeconds: 3), enteredCode: true).contains("Too many"))
        #expect(JoinCabalCopy.failureMessage(for: URLError(.notConnectedToInternet), enteredCode: true).contains("offline"))
    }

    @Test func everyInviteLineIsInTheProductsWords() {
        #expect(MainFlowCopyAudit.stringsAreClean(CabalInviteCopy.auditedStrings))
        #expect(MainFlowCopyAudit.stringsAreClean(InviteEntryCopy.auditedStrings))
        #expect(MainFlowCopyAudit.stringsAreClean(CabalDetailsCopy.auditedStrings))
    }
}
