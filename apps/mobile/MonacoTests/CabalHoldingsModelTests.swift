import MonacoCore
import Testing
@testable import Monaco

// The app target shadows these MonacoCore DTOs; pin the tests to the ones the views use.
private typealias GroupViewDTO = Monaco.GroupViewDTO
private typealias PotRowDTO = Monaco.PotRowDTO
private typealias MemberSliceDTO = Monaco.MemberSliceDTO
private typealias HomeGroupBoardRowDTO = Monaco.HomeGroupBoardRowDTO

@MainActor
private final class StubCabalHoldingsDataSource: CabalHoldingsDataSource {
    var potsByGroup: [String: [PotRowDTO]] = [:]
    var potTotalsByGroup: [String: String] = [:]
    var errorsByGroup: [String: Error] = [:]
    var fetched: [String] = []

    func groupView(groupId: String) async throws -> GroupViewDTO {
        fetched.append(groupId)
        if let error = errorsByGroup[groupId] { throw error }
        return GroupViewDTO(
            id: groupId,
            name: groupId,
            treasuryAddress: nil,
            potTotalUsd: potTotalsByGroup[groupId] ?? "100.00",
            pot: potsByGroup[groupId] ?? [],
            you: MemberSliceDTO(
                shareUnits: "1", equityUsd: "1.00", slicePercent: "1", dollarPnl: "+0.00", percentReturn: nil
            ),
            members: [],
            proposals: nil,
            agent: nil
        )
    }
}

@MainActor
struct CabalHoldingsModelTests {
    private func cabal(_ id: String) -> HomeGroupBoardRowDTO {
        HomeGroupBoardRowDTO(
            groupId: id, name: "Cabal \(id)", potValueUsd: "100.00",
            percentReturn: nil, dollarPnl: "+0.00", isJoined: true
        )
    }

    private func holding(_ symbol: String, atomics: String = "100000000") -> PotRowDTO {
        PotRowDTO(
            symbol: symbol, units: "1", markUsd: "185.00", valueUsd: "185.00",
            dollarPnl: "+0.00", afterHours: nil, tokenAmount: atomics
        )
    }

    /// The bug: every cabal was offered, and only after picking one did the app admit that
    /// cabal does not hold the stock.
    @Test func onlyTheCabalsThatHoldTheStockAreOffered() async throws {
        let source = StubCabalHoldingsDataSource()
        source.potsByGroup = [
            "a": [holding("AAPLx")],
            "b": [holding("TSLAx")],
            "c": [holding("AAPLx")],
        ]
        let model = CabalHoldingsModel(symbol: "AAPLx", dataSource: source)

        await model.load(cabals: [cabal("a"), cabal("b"), cabal("c")])

        #expect(model.holders.map(\.groupId) == ["a", "c"])
        #expect(model.holders.map { $0.holding } == [holding("AAPLx"), holding("AAPLx")])
        // Buy is offered every cabal, in the order they were listed.
        #expect(model.cabals.map(\.groupId) == ["a", "b", "c"])
        #expect(model.unreachableCount == 0)
    }

    /// The blocking review finding: Buy passed `pot: nil` and let the amount step fetch its own.
    /// The picker owns that load, so every row it offers already carries its cabal's pot.
    @Test func everyOfferedCabalCarriesThePotTheAmountStepNeeds() async throws {
        let source = StubCabalHoldingsDataSource()
        source.potsByGroup = ["a": [holding("AAPLx")], "b": []]
        source.potTotalsByGroup = ["a": "250.00", "b": "1000.50"]
        let model = CabalHoldingsModel(symbol: "AAPLx", dataSource: source)

        await model.load(cabals: [cabal("a"), cabal("b")])

        #expect(model.cabals.map(\.pot.totalMicros) == [250_000_000, 1_000_500_000])
        #expect(model.cabals.map(\.pot.groupId) == ["a", "b"])
        // The sell row's pot is the same one, so it does not have to be fetched again either.
        #expect(model.holders.first?.pot.totalMicros == 250_000_000)
    }

    @Test func aZeroBalanceRowDoesNotCountAsHolding() async throws {
        let source = StubCabalHoldingsDataSource()
        source.potsByGroup = ["a": [holding("AAPLx", atomics: "0")]]
        let model = CabalHoldingsModel(symbol: "AAPLx", dataSource: source)

        await model.load(cabals: [cabal("a")])

        #expect(model.holders.isEmpty)
        #expect(model.unreachableCount == 0)
    }

    /// The other half of the review finding: collapsing the whole picker on one failed fetch
    /// stopped a member selling from the cabal that demonstrably holds the stock.
    @Test func aCabalThatAnsweredIsStillOfferedWhenAnotherDidNot() async throws {
        let source = StubCabalHoldingsDataSource()
        source.potsByGroup = ["a": [holding("AAPLx")], "c": [holding("AAPLx")]]
        source.errorsByGroup = ["b": Monaco.MonacoAPIError.httpStatus(500)]
        let model = CabalHoldingsModel(symbol: "AAPLx", dataSource: source)

        await model.load(cabals: [cabal("a"), cabal("b"), cabal("c")])

        #expect(model.holders.map(\.groupId) == ["a", "c"])
        #expect(model.unreachableCount == 1)
        #expect(model.state != .failed)
    }

    /// Telling someone none of their cabals hold a stock when a fetch failed would be a lie:
    /// the count is what lets the picker say "couldn't check" instead of "nobody holds it".
    @Test func aCabalThatDidNotAnswerIsCountedRatherThanCountedOut() async throws {
        let source = StubCabalHoldingsDataSource()
        source.potsByGroup = ["a": [holding("TSLAx")]]
        source.errorsByGroup = ["b": Monaco.MonacoAPIError.httpStatus(500)]
        let model = CabalHoldingsModel(symbol: "AAPLx", dataSource: source)

        await model.load(cabals: [cabal("a"), cabal("b")])

        #expect(model.holders.isEmpty)
        #expect(model.unreachableCount == 1, "the picker must not claim nobody holds it")
    }

    /// Nothing answered: there is no partial answer to show, only the failure.
    @Test func everyCabalFailingIsAFailure() async throws {
        let source = StubCabalHoldingsDataSource()
        source.errorsByGroup = [
            "a": Monaco.MonacoAPIError.httpStatus(500),
            "b": Monaco.MonacoAPIError.httpStatus(500),
        ]
        let model = CabalHoldingsModel(symbol: "AAPLx", dataSource: source)

        await model.load(cabals: [cabal("a"), cabal("b")])

        #expect(model.state == .failed)
    }

    @Test func rejectedSessionAsksTheViewToSignOut() async throws {
        let source = StubCabalHoldingsDataSource()
        source.errorsByGroup = ["a": Monaco.MonacoAPIError.httpStatus(401)]
        let model = CabalHoldingsModel(symbol: "AAPLx", dataSource: source)

        await model.load(cabals: [cabal("a")])

        #expect(model.sessionExpired)
        #expect(model.state == .loading)
    }

    @Test func noCabalsNeedsNoFetch() async throws {
        let source = StubCabalHoldingsDataSource()
        let model = CabalHoldingsModel(symbol: "AAPLx", dataSource: source)

        await model.load(cabals: [])

        #expect(model.state == .resolved(cabals: [], unreachable: 0))
        #expect(source.fetched.isEmpty)
    }
}
