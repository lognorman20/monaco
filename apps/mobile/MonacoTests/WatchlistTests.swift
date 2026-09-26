import Foundation
import MonacoCore
import Testing
@testable import Monaco

private typealias MarketAssetDTO = Monaco.MarketAssetDTO
private typealias AssetDetailDTO = Monaco.AssetDetailDTO

private struct StubFailure: Error {}

@MainActor
private final class StubWatchlistSource: WatchlistDataSource, PriceAlertDataSource {
    var watched: [MarketAssetDTO] = []
    var watchlistError: Error?
    var writeError: Error?
    var reorderError: Error?
    var alertsResponse = PriceAlertsResponseDTO(alerts: [])
    var alertsError: Error?
    var createError: Error?
    var deleteError: Error?
    private(set) var removed: [String] = []
    private(set) var added: [String] = []
    private(set) var reorders: [[String]] = []
    private(set) var created: [(PriceAlertDirection, Int64)] = []

    func watchlist() async throws -> WatchlistResponseDTO {
        if let watchlistError { throw watchlistError }
        return WatchlistResponseDTO(assets: watched)
    }

    func add(symbol: String) async throws {
        if let writeError { throw writeError }
        added.append(symbol)
    }

    func remove(symbol: String) async throws {
        if let writeError { throw writeError }
        removed.append(symbol)
    }

    func reorder(symbols: [String]) async throws -> [String] {
        reorders.append(symbols)
        if let reorderError { throw reorderError }
        return symbols
    }

    func alerts(symbol: String?) async throws -> PriceAlertsResponseDTO {
        if let alertsError { throw alertsError }
        return alertsResponse
    }

    func createAlert(symbol: String, direction: PriceAlertDirection, lineUsdcMicros: Int64) async throws -> PriceAlertDTO {
        if let createError { throw createError }
        created.append((direction, lineUsdcMicros))
        return PriceAlertDTO(id: "new", symbol: symbol, direction: direction, priceUsdcMicros: lineUsdcMicros, createdAt: Date())
    }

    func deleteAlert(id: String) async throws {
        if let deleteError { throw deleteError }
    }
}

private func asset(_ symbol: String) -> MarketAssetDTO {
    MarketSampleData.listAsset(symbol: symbol, name: symbol, priceUsdcMicros: 100_000_000, change24h: "0.01")
}

@MainActor
struct WatchlistModelTests {
    @Test func loadingRemembersTheCountAndThatTheMemberHasWatched() async {
        // Arrange
        let source = StubWatchlistSource()
        source.watched = [asset("AAPLx"), asset("TSLAx")]
        let memory = InMemoryWatchlistMemory()
        let model = WatchlistModel(dataSource: source, memory: memory)

        // Act
        await model.load()

        // Assert
        #expect(model.symbols == ["AAPLx", "TSLAx"])
        #expect(memory.lastCount == 2)
        #expect(memory.hasWatched)
        #expect(!model.showsFirstUseHint)
    }

    @Test func anEmptyWatchlistShowsTheHintOnlyUntilTheFirstStar() async {
        // Arrange
        let fresh = WatchlistModel(dataSource: StubWatchlistSource(), memory: InMemoryWatchlistMemory())
        let veteran = WatchlistModel(dataSource: StubWatchlistSource(), memory: InMemoryWatchlistMemory(lastCount: 0, hasWatched: true))

        // Act
        await fresh.load()
        await veteran.load()

        // Assert
        #expect(fresh.showsFirstUseHint)
        #expect(!veteran.showsFirstUseHint)
    }

    @Test func theSkeletonHoldsRoomForLastTimesRowsUpToFour() {
        let few = WatchlistModel(dataSource: StubWatchlistSource(), memory: InMemoryWatchlistMemory(lastCount: 2, hasWatched: true))
        let many = WatchlistModel(dataSource: StubWatchlistSource(), memory: InMemoryWatchlistMemory(lastCount: 30, hasWatched: true))
        let never = WatchlistModel(dataSource: StubWatchlistSource(), memory: InMemoryWatchlistMemory())
        #expect(few.skeletonRowCount == 2)
        #expect(many.skeletonRowCount == WatchlistModel.maximumSkeletonRows)
        #expect(never.skeletonRowCount == 0)
    }

    @Test func aFailedRefreshKeepsTheRowsAndMarksThemStale() async {
        // Arrange
        let source = StubWatchlistSource()
        source.watched = [asset("AAPLx")]
        let model = WatchlistModel(dataSource: source, memory: InMemoryWatchlistMemory())
        await model.load()
        source.watchlistError = StubFailure()

        // Act
        await model.load()

        // Assert
        #expect(model.symbols == ["AAPLx"])
        #expect(model.refreshFailed)
        #expect(model.state == .loaded)
    }

    @Test func aFailedFirstReadIsOnlyShownToAMemberWhoHadAWatchlist() async {
        let source = StubWatchlistSource()
        source.watchlistError = StubFailure()
        let had = WatchlistModel(dataSource: source, memory: InMemoryWatchlistMemory(lastCount: 3, hasWatched: true))
        let never = WatchlistModel(dataSource: source, memory: InMemoryWatchlistMemory())
        await had.load()
        await never.load()
        #expect(had.showsFailure)
        #expect(!never.showsFailure)
    }

    @Test func removeTakesTheRowOffAtOnceAndPutsItBackOnARefusal() async {
        // Arrange
        let source = StubWatchlistSource()
        source.watched = [asset("AAPLx"), asset("TSLAx"), asset("NVDAx")]
        let model = WatchlistModel(dataSource: source, memory: InMemoryWatchlistMemory())
        await model.load()
        source.writeError = StubFailure()

        // Act
        await model.remove(symbol: "TSLAx")

        // Assert
        #expect(model.symbols == ["AAPLx", "TSLAx", "NVDAx"])
        #expect(model.toast?.message == WatchlistCopy.writeFailed)
    }

    @Test func removeConfirmsWithAToast() async {
        let source = StubWatchlistSource()
        source.watched = [asset("AAPLx"), asset("TSLAx")]
        let model = WatchlistModel(dataSource: source, memory: InMemoryWatchlistMemory())
        await model.load()

        await model.remove(symbol: "AAPLx")

        #expect(model.symbols == ["TSLAx"])
        #expect(source.removed == ["AAPLx"])
        #expect(model.toast?.message == WatchlistCopy.removed)
    }

    @Test func reorderSendsTheWholeOrderOnce() async {
        let source = StubWatchlistSource()
        source.watched = [asset("AAPLx"), asset("TSLAx")]
        let model = WatchlistModel(dataSource: source, memory: InMemoryWatchlistMemory())
        await model.load()

        await model.reorder(to: ["TSLAx", "AAPLx"])
        await model.reorder(to: ["TSLAx", "AAPLx"])

        #expect(model.symbols == ["TSLAx", "AAPLx"])
        #expect(source.reorders == [["TSLAx", "AAPLx"]])
    }

    @Test func aReorderFromAStaleCopyReloadsAndSaysSo() async {
        // Arrange
        let source = StubWatchlistSource()
        source.watched = [asset("AAPLx"), asset("TSLAx")]
        let model = WatchlistModel(dataSource: source, memory: InMemoryWatchlistMemory())
        await model.load()
        source.watched = [asset("AAPLx"), asset("TSLAx"), asset("NVDAx")]
        source.reorderError = MonacoCore.MonacoAPIError.rejected(status: 409, message: "changed")

        // Act
        await model.reorder(to: ["TSLAx", "AAPLx"])

        // Assert
        #expect(model.symbols == ["AAPLx", "TSLAx", "NVDAx"])
        #expect(model.toast?.message == WatchlistCopy.changed)
    }
}

@MainActor
struct AssetWatchModelTests {
    private func detail(watching: Bool?, alerts: Int?) -> AssetDetailDTO {
        WatchlistSampleData.alphabetDetail(watching: watching ?? false, alertCount: alerts ?? 0)
    }

    @Test func theStarWaitsForTheFirstDetail() {
        let model = AssetWatchModel(symbol: "GOOGLx", dataSource: StubWatchlistSource())
        #expect(model.watching == nil)
        model.absorb(detail(watching: true, alerts: 2))
        #expect(model.watching == true)
        #expect(model.alertCount == 2)
    }

    @Test func togglingAddsAndConfirms() async {
        let source = StubWatchlistSource()
        let model = AssetWatchModel(symbol: "GOOGLx", dataSource: source)
        model.absorb(detail(watching: false, alerts: 0))

        let toast = await model.toggle()

        #expect(model.watching == true)
        #expect(source.added == ["GOOGLx"])
        #expect(toast?.message == WatchlistCopy.added)
    }

    @Test func aRefusedToggleSnapsBack() async {
        let source = StubWatchlistSource()
        source.writeError = MonacoCore.MonacoAPIError.rejected(status: 409, message: "full")
        let model = AssetWatchModel(symbol: "GOOGLx", dataSource: source)
        model.absorb(detail(watching: false, alerts: 0))

        let toast = await model.toggle()

        #expect(model.watching == false)
        #expect(toast?.message == WatchlistCopy.full)
    }

    @Test func aPollThatLeftBeforeTheTapCannotFlipTheStarBack() async {
        // Arrange
        var now = Date(timeIntervalSince1970: 1_790_000_000)
        let model = AssetWatchModel(symbol: "GOOGLx", dataSource: StubWatchlistSource(), clock: { now })
        model.absorb(detail(watching: false, alerts: 0))
        _ = await model.toggle()

        // Act: a stale poll lands inside the grace window, a fresh one after it.
        now = now.addingTimeInterval(5)
        model.absorb(detail(watching: false, alerts: 0))
        let duringGrace = model.watching
        now = now.addingTimeInterval(AssetWatchModel.localChangeGrace)
        model.absorb(detail(watching: true, alerts: 0))

        // Assert
        #expect(duringGrace == true)
        #expect(model.watching == true)
    }
}

@MainActor
struct PriceAlertSheetModelTests {
    private func sheet(price: Int64? = 352_100_000, source: StubWatchlistSource? = nil) -> PriceAlertSheetModel {
        PriceAlertSheetModel(symbol: "GOOGLx", name: "Alphabet", currentPriceUsdcMicros: price, dataSource: source ?? StubWatchlistSource())
    }

    @Test func aPresetFillsTheLineAndTheSentence() {
        let model = sheet()
        model.choose(percent: 5)
        #expect(model.lineUsdcMicros == 369_710_000)
        #expect(model.sentence == "Tell me when Alphabet is above $369.71")
        #expect(model.canSave)
    }

    @Test func flippingTheSideKeepsTheSameMove() {
        let model = sheet()
        model.choose(percent: 10)
        model.choose(direction: .below)
        #expect(model.presetPercent == 10)
        #expect(model.lineUsdcMicros == 316_890_000)
        #expect(model.problem == nil)
    }

    @Test func typingAPriceAlreadyPassedExplainsAndBlocksSave() {
        let model = sheet()
        model.choose(direction: .below)
        model.type("360")
        #expect(model.problem == "Alphabet is at $352.10 now. Pick a price below that.")
        #expect(!model.canSave)
    }

    @Test func typingLeavesThePreset() {
        let model = sheet()
        model.choose(percent: 2)
        model.type("400")
        #expect(model.presetPercent == nil)
        #expect(model.lineUsdcMicros == 400_000_000)
    }

    @Test func withoutAPriceThePresetsAreOff() {
        let model = sheet(price: nil)
        model.choose(percent: 5)
        #expect(model.lineUsdcMicros == nil)
        #expect(model.presetLine(5) == nil)
    }

    @Test func savingReturnsTheConfirmationAndCountsIt() async {
        let source = StubWatchlistSource()
        var counts: [Int] = []
        let model = PriceAlertSheetModel(symbol: "GOOGLx", name: "Alphabet", currentPriceUsdcMicros: 352_100_000, dataSource: source) { counts.append($0) }
        model.type("360")

        let toast = await model.save()

        #expect(toast?.message == "We'll tell you when Alphabet is above $360.00")
        #expect(counts.last == 1)
        #expect(model.amountText.isEmpty)
    }

    @Test func refusalsStayInTheSheetInWords() async {
        for (status, message) in [(409, AlertCopy.limitReached), (422, AlertCopy.alreadyReached(name: "Alphabet", direction: .above))] {
            let source = StubWatchlistSource()
            source.createError = MonacoCore.MonacoAPIError.rejected(status: status, message: "no")
            let model = sheet(source: source)
            model.type("360")

            let toast = await model.save()

            #expect(toast == nil)
            #expect(model.toast?.message == message)
        }
    }

    @Test func aRefusedDeletePutsTheAlertBack() async {
        let source = StubWatchlistSource()
        source.alertsResponse = PriceAlertsResponseDTO(alerts: WatchlistSampleData.alphabetAlerts(now: Date()))
        source.deleteError = StubFailure()
        let model = sheet(source: source)
        await model.load()
        let first = model.waiting[0]

        await model.delete(first)

        #expect(model.waiting.count == 2)
        #expect(model.waiting.contains(first))
        #expect(model.toast?.message == AlertCopy.deleteFailed)
    }
}

@MainActor
struct AlertsModelTests {
    @Test func deletingTheLastAlertOnAStockDropsItsGroup() async {
        let source = StubWatchlistSource()
        source.alertsResponse = WatchlistSampleData.alerts(now: Date())
        let model = AlertsModel(dataSource: source)
        await model.load()
        guard let tesla = model.groups.first(where: { $0.symbol == "TSLAx" }) else {
            Issue.record("no TSLAx group")
            return
        }

        await model.delete(tesla.alerts[0])

        #expect(!model.groups.contains { $0.symbol == "TSLAx" })
        #expect(model.toast?.message == AlertCopy.removed)
    }

    @Test func groupsFollowTheServerOrder() async {
        let source = StubWatchlistSource()
        source.alertsResponse = WatchlistSampleData.alerts(now: Date())
        let model = AlertsModel(dataSource: source)
        await model.load()
        #expect(model.state == .loaded)
        #expect(model.groups.map(\.symbol) == ["GOOGLx", "TSLAx", "NVDAx"])
    }

    @Test func aFailedFirstReadIsAFailureAndALaterOneIsStale() async {
        // Arrange
        let source = StubWatchlistSource()
        source.alertsError = StubFailure()
        let model = AlertsModel(dataSource: source)

        // Act
        await model.load()
        let firstState = model.state
        source.alertsError = nil
        source.alertsResponse = WatchlistSampleData.alerts(now: Date())
        await model.load()
        source.alertsError = StubFailure()
        await model.load()

        // Assert
        #expect(firstState == .failed)
        #expect(model.state == .loaded)
        #expect(model.refreshFailed)
        #expect(!model.groups.isEmpty)
    }
}
