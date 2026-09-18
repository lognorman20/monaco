import XCTest
@testable import MonacoCore

@MainActor
final class RefreshableSnapshotTests: XCTestCase {
    func testRefreshReplacesEntireSnapshot() async throws {
        let state = RefreshableSnapshot<String>()
        try await state.refresh { "before" }
        try await state.refresh { "after" }
        XCTAssertEqual(state.value, "after")
        XCTAssertEqual(state.revision, 2)
    }

    func testFailurePreservesVisibleSnapshot() async throws {
        let state = RefreshableSnapshot<String>()
        try await state.refresh { "visible" }
        do {
            try await state.refresh { throw URLError(.notConnectedToInternet) }
            XCTFail("Expected failure")
        } catch { }
        XCTAssertEqual(state.value, "visible")
        XCTAssertEqual(state.revision, 1)
    }

    func testOlderResponseCannotOverwriteNewerRefresh() async throws {
        let state = RefreshableSnapshot<String>()
        var release: CheckedContinuation<String, Never>?
        let old = Task { try await state.refresh { await withCheckedContinuation { release = $0 } } }
        while release == nil { await Task.yield() }
        try await state.refresh { "new membership" }
        release?.resume(returning: "stale membership")
        try await old.value
        XCTAssertEqual(state.value, "new membership")
        XCTAssertEqual(state.revision, 1)
    }

    func testLogoutDiscardsInflightResponse() async throws {
        let state = RefreshableSnapshot<String>()
        var release: CheckedContinuation<String, Never>?
        let pending = Task { try await state.refresh { await withCheckedContinuation { release = $0 } } }
        while release == nil { await Task.yield() }
        state.clear()
        release?.resume(returning: "previous user's data")
        try await pending.value
        XCTAssertNil(state.value)
    }

    func testObsoleteFailureDoesNotInvalidateNewerSnapshot() async throws {
        let state = RefreshableSnapshot<String>()
        var release: CheckedContinuation<String, Error>?
        let old = Task { try await state.refresh { try await withCheckedThrowingContinuation { release = $0 } } }
        while release == nil { await Task.yield() }
        try await state.refresh { "new session" }
        release?.resume(throwing: URLError(.userAuthenticationRequired))
        try await old.value
        XCTAssertEqual(state.value, "new session")
    }

    func testCancelledRefreshKeepsPreviousSnapshot() async throws {
        let state = RefreshableSnapshot<String>()
        try await state.refresh { "visible" }
        var release: CheckedContinuation<String, Never>?
        let pending = Task { try await state.refresh { await withCheckedContinuation { release = $0 } } }
        while release == nil { await Task.yield() }
        pending.cancel()
        release?.resume(returning: "cancelled response")
        try await pending.value
        XCTAssertEqual(state.value, "visible")
        XCTAssertEqual(state.revision, 1)
    }

}
