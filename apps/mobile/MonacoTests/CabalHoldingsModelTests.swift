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
    var errorsByGroup: [String: Error] = [:]
    var fetched: [String] = []

    func groupView(groupId: String) async throws -> GroupViewDTO {
        fetched.append(groupId)
        if let error = errorsByGroup[groupId] { throw error }
        return GroupViewDTO(
            id: groupId,
            name: groupId,
            treasuryAddress: nil,
            potTotalUsd: "100.00",
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

        #expect(model.state == .loaded([
            CabalHoldingsModel.Holding(groupId: "a", name: "Cabal a", row: holding("AAPLx")),
            CabalHoldingsModel.Holding(groupId: "c", name: "Cabal c", row: holding("AAPLx")),
        ]))
    }

    @Test func aZeroBalanceRowDoesNotCountAsHolding() async throws {
        let source = StubCabalHoldingsDataSource()
        source.potsByGroup = ["a": [holding("AAPLx", atomics: "0")]]
        let model = CabalHoldingsModel(symbol: "AAPLx", dataSource: source)

        await model.load(cabals: [cabal("a")])

        #expect(model.state == .loaded([]))
    }

    /// Telling someone none of their cabals hold a stock when a fetch failed would be a lie.
    @Test func oneFailedFetchIsReportedRatherThanShownAsNoHolders() async throws {
        let source = StubCabalHoldingsDataSource()
        source.potsByGroup = ["a": [holding("AAPLx")]]
        source.errorsByGroup = ["b": Monaco.MonacoAPIError.httpStatus(500)]
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

        #expect(model.state == .loaded([]))
        #expect(source.fetched.isEmpty)
    }
}
