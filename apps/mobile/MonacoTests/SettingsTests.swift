import Foundation
import MonacoCore
import Testing
@testable import Monaco

/// Stands in for Face ID: answers with whatever the test queued, and counts the prompts.
@MainActor
private final class StubAuthenticator: DeviceOwnerAuthenticating {
    var method: String? = "Face ID"
    var answers: [Bool] = []
    private(set) var prompts: [String] = []

    func availableMethod() -> String? { method }

    func authenticate(reason: String) async -> Bool {
        prompts.append(reason)
        return answers.isEmpty ? false : answers.removeFirst()
    }
}

/// A clock the test moves by hand.
@MainActor
private final class StubClock {
    var now = Date(timeIntervalSince1970: 1_790_000_000)
    func advance(_ seconds: TimeInterval) { now = now.addingTimeInterval(seconds) }
}

@MainActor
private func freshStore(_ name: String = #function) -> SettingsStore {
    let suite = "monaco.tests.settings.\(name)"
    let defaults = UserDefaults(suiteName: suite)!
    defaults.removePersistentDomain(forName: suite)
    return SettingsStore(defaults: defaults)
}

@MainActor
struct AppLockTests {
    @Test func opensLockedWhenTheLockIsOn() {
        let store = freshStore()
        store.lockEnabled = true

        let lock = AppLock(settings: store, authenticator: StubAuthenticator())

        #expect(lock.isLocked)
        #expect(lock.takePendingPrompt(), "Face ID is offered at launch without a tap")
        #expect(!lock.takePendingPrompt(), "and only once")
    }

    @Test func sampleLaunchesNeverLock() {
        let store = freshStore()
        store.lockEnabled = true

        let lock = AppLock(settings: store, authenticator: StubAuthenticator(), isSuspended: true)
        lock.didEnterBackground()
        lock.didBecomeActive()

        #expect(!lock.isLocked)
        #expect(!lock.isEnabled)
    }

    @Test func comingBackBeforeTheDelayStaysOpen() {
        let store = freshStore()
        store.lockEnabled = true
        store.lockTimeout = .fiveMinutes
        let clock = StubClock()
        let lock = AppLock(settings: store, authenticator: StubAuthenticator(), now: { clock.now })
        lock.forget() // start unlocked, then turn it back on without a prompt
        store.lockEnabled = true

        lock.didEnterBackground()
        clock.advance(299)
        lock.didBecomeActive()

        #expect(!lock.isLocked)
    }

    @Test func comingBackAfterTheDelayLocksAndOffersFaceIDOnce() {
        let store = freshStore()
        store.lockEnabled = true
        store.lockTimeout = .oneMinute
        let clock = StubClock()
        let lock = AppLock(settings: store, authenticator: StubAuthenticator(), now: { clock.now })
        lock.forget()
        store.lockEnabled = true

        lock.didEnterBackground()
        clock.advance(61)
        lock.didBecomeActive()

        #expect(lock.isLocked)
        #expect(lock.takePendingPrompt())
        // The Face ID sheet itself makes the scene inactive and active again: no second prompt.
        lock.didBecomeActive()
        #expect(!lock.takePendingPrompt())
    }

    @Test func unlockOpensOnlyWhenTheMemberPasses() async {
        let store = freshStore()
        store.lockEnabled = true
        let authenticator = StubAuthenticator()
        authenticator.answers = [false, true]
        let lock = AppLock(settings: store, authenticator: authenticator)

        let first = await lock.unlock()
        #expect(!first)
        #expect(lock.isLocked)

        let second = await lock.unlock()
        #expect(second)
        #expect(!lock.isLocked)
        #expect(authenticator.prompts == [SettingsCopy.unlockReason, SettingsCopy.unlockReason])
    }

    @Test func turningTheLockOnNeedsFaceIDFirst() async {
        let store = freshStore()
        let authenticator = StubAuthenticator()
        authenticator.answers = [false, true]
        let lock = AppLock(settings: store, authenticator: authenticator)

        let refused = await lock.setEnabled(true)
        #expect(!refused)
        #expect(!store.lockEnabled)

        let accepted = await lock.setEnabled(true)
        #expect(accepted)
        #expect(store.lockEnabled)
        #expect(!lock.isLocked, "turning it on does not lock the open app")
    }

    @Test func aPhoneWithoutAPasscodeCannotTurnTheLockOn() async {
        let store = freshStore()
        let authenticator = StubAuthenticator()
        authenticator.method = nil
        authenticator.answers = [true]
        let lock = AppLock(settings: store, authenticator: authenticator)

        let done = await lock.setEnabled(true)

        #expect(!done)
        #expect(authenticator.prompts.isEmpty)
    }

    @Test func turningTheLockOffDoesNotAsk() async {
        let store = freshStore()
        store.lockEnabled = true
        let authenticator = StubAuthenticator()
        let lock = AppLock(settings: store, authenticator: authenticator)

        let done = await lock.setEnabled(false)

        #expect(done)
        #expect(!store.lockEnabled)
        #expect(!lock.isLocked)
        #expect(authenticator.prompts.isEmpty)
    }
}

@MainActor
struct SettingsStoreTests {
    @Test func valuesSurviveARelaunch() {
        let suite = "monaco.tests.settings.relaunch"
        let defaults = UserDefaults(suiteName: suite)!
        defaults.removePersistentDomain(forName: suite)
        let first = SettingsStore(defaults: defaults)
        first.lockEnabled = true
        first.lockTimeout = .fifteenMinutes
        first.appearance = .dark

        let second = SettingsStore(defaults: defaults)

        #expect(second.lockEnabled)
        #expect(second.lockTimeout == .fifteenMinutes)
        #expect(second.appearance == .dark)
    }

    @Test func defaultsAreLockOffImmediatelyAndSystem() {
        let store = freshStore()

        #expect(!store.lockEnabled)
        #expect(store.lockTimeout == .immediately)
        #expect(store.appearance == .system)
    }

    @Test func theNotificationMirrorBelongsToOneMember() {
        let store = freshStore()
        var chatOff = NotificationPreferencesDTO.allOn
        chatOff.chat = false

        store.mirrorNotifications(chatOff, for: "member-a")

        #expect(store.notifications(for: "member-a") == chatOff)
        #expect(store.notifications(for: "member-b") == .allOn)
        #expect(store.notifications(for: nil) == .allOn)
    }

    @Test func forgettingTheAccountKeepsOnlyTheAppearance() {
        let store = freshStore()
        store.lockEnabled = true
        store.appearance = .light
        var moneyOff = NotificationPreferencesDTO.allOn
        moneyOff.money = false
        store.mirrorNotifications(moneyOff, for: "member-a")

        store.forgetAccount()

        #expect(!store.lockEnabled)
        #expect(store.appearance == .light)
        #expect(store.notifications(for: "member-a") == .allOn)
    }
}

/// Answers the Settings screens with what the test queued.
@MainActor
private final class StubSettingsService: SettingsService {
    var identities: [SignInIdentity] = [.phone("+14155557177")]
    var server = PreferencesDTO.defaults
    var failPreferences = false
    var failWrites = false
    var check: Result<DeletionCheckDTO, Error> = .success(DeletionCheckDTO(canDelete: true, blockers: []))
    var deletion: Result<AccountDeletionOutcome, Error> = .success(.deleted(deletedAt: nil))
    private(set) var writes: [[NotificationCategory: Bool]] = []

    func signInIdentities() async -> [SignInIdentity] { identities }

    func preferences() async throws -> PreferencesDTO {
        if failPreferences { throw URLError(.notConnectedToInternet) }
        return server
    }

    func setNotifications(_ changes: [NotificationCategory: Bool]) async throws -> PreferencesDTO {
        writes.append(changes)
        if failWrites { throw URLError(.timedOut) }
        for (category, on) in changes { server.notifications[category] = on }
        return server
    }

    func deletionCheck() async throws -> DeletionCheckDTO { try check.get() }

    func deleteAccount() async throws -> AccountDeletionOutcome { try deletion.get() }
}

@MainActor
struct SettingsModelTests {
    @Test func drawsTheMirrorFirstThenTheServer() async {
        let store = freshStore()
        var mirror = NotificationPreferencesDTO.allOn
        mirror.chat = false
        store.mirrorNotifications(mirror, for: "member-a")
        let service = StubSettingsService()
        service.server.notifications.money = false
        let model = SettingsModel(service: service, store: store, userId: "member-a")

        #expect(model.notifications == mirror)
        await model.load()

        #expect(model.notifications == service.server.notifications)
        #expect(store.notifications(for: "member-a") == service.server.notifications)
        #expect(model.identities == [.phone("+14155557177")])
    }

    @Test func aFailedLoadKeepsTheMirrorQuietly() async {
        let store = freshStore()
        let service = StubSettingsService()
        service.failPreferences = true
        let model = SettingsModel(service: service, store: store, userId: "member-a")

        await model.load()

        #expect(model.notifications == .allOn)
        #expect(model.toast == nil)
        #expect(model.hasLoadedIdentities)
    }

    @Test func aSwitchSavesOnlyWhatChanged() async {
        let store = freshStore()
        let service = StubSettingsService()
        let model = SettingsModel(service: service, store: store, userId: "member-a")

        await model.setNotification(.chat, on: false)

        #expect(service.writes == [[.chat: false]])
        #expect(!model.notifications.chat)
        #expect(!store.notifications(for: "member-a").chat)
        #expect(!model.isSaving(.chat))
    }

    @Test func aFailedSaveMovesTheSwitchBackAndSaysSo() async {
        let store = freshStore()
        let service = StubSettingsService()
        service.failWrites = true
        let model = SettingsModel(service: service, store: store, userId: "member-a")

        await model.setNotification(.money, on: false)

        #expect(model.notifications.money)
        #expect(store.notifications(for: "member-a").money)
        #expect(model.toast?.message == SettingsCopy.notificationsSaveFailed)
    }
}

@MainActor
struct DeleteAccountModelTests {
    private static let slice = DeletionBlockerDTO(kind: .cabalSlice, groupId: "g-1", groupName: "Sunday Investors", valueUsd: "245.12")

    @Test func anEmptyAccountIsReadyToDelete() async {
        let model = DeleteAccountModel(service: StubSettingsService())

        await model.load()

        #expect(model.phase == .ready)
    }

    @Test func moneyInTheAccountShowsTheBlockers() async {
        let service = StubSettingsService()
        service.check = .success(DeletionCheckDTO(canDelete: false, blockers: [Self.slice]))
        let model = DeleteAccountModel(service: service)

        await model.load()

        #expect(model.phase == .blocked([Self.slice]))
    }

    @Test func aCheckThatCannotReachTheServerFails() async {
        let service = StubSettingsService()
        service.check = .failure(URLError(.notConnectedToInternet))
        let model = DeleteAccountModel(service: service)

        await model.load()

        #expect(model.phase == .failed)
    }

    @Test func aDeleteRefusedWithBlockersShowsThem() async {
        let service = StubSettingsService()
        service.deletion = .success(.blocked(DeletionCheckDTO(canDelete: false, blockers: [Self.slice])))
        let model = DeleteAccountModel(service: service)
        await model.load()

        let deleted = await model.delete()

        #expect(!deleted)
        #expect(model.phase == .blocked([Self.slice]))
    }

    @Test func aFailedDeleteSaysSoAndStaysReady() async {
        let service = StubSettingsService()
        service.deletion = .failure(URLError(.timedOut))
        let model = DeleteAccountModel(service: service)
        await model.load()

        let deleted = await model.delete()

        #expect(!deleted)
        #expect(model.phase == .ready)
        #expect(model.deleteError == SettingsCopy.deleteFailed)
    }

    @Test func aRateLimitedDeleteSaysWhenToTryAgain() {
        let message = DeleteAccountModel.message(for: MonacoCore.MonacoAPIError.rateLimited(retryAfterSeconds: 20))
        #expect(message == "Too many tries. Try again in 20s.")
    }

    @Test func aSuccessfulDeleteReportsIt() async {
        let model = DeleteAccountModel(service: StubSettingsService())
        await model.load()

        let deleted = await model.delete()

        #expect(deleted)
        #expect(model.deleteError == nil)
    }
}
