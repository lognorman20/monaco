import Testing
@testable import Monaco

@MainActor
struct HomeBalanceDisplayTests {
    private func balance(micros: Int64) -> PlatformBalanceDTO {
        PlatformBalanceDTO(
            availableUsdcMicros: micros,
            memberWalletAddress: "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
            pendingAllocationMicros: 0
        )
    }

    @Test func aLoadedBalanceShowsItsAmount() {
        #expect(HomeBalanceDisplay.resolve(balance: balance(micros: 248_500_000), isLoading: false) == .amount(248_500_000))
    }

    @Test func aFirstLoadInFlightShowsLoading() {
        #expect(HomeBalanceDisplay.resolve(balance: nil, isLoading: true) == .loading)
    }

    /// The failure that used to read "Account balance $0.00" above Cash out.
    @Test func aFailedBalanceIsUnavailableNotZero() {
        #expect(HomeBalanceDisplay.resolve(balance: nil, isLoading: false) == .unavailable)
    }

    @Test func aRealZeroBalanceStillShowsZero() {
        #expect(HomeBalanceDisplay.resolve(balance: balance(micros: 0), isLoading: false) == .amount(0))
    }
}
