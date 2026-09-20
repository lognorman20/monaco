import XCTest
@testable import MonacoCore

final class DiagnosticPayloadStoreTests: XCTestCase {
    private var root: URL!

    override func setUpWithError() throws {
        root = FileManager.default.temporaryDirectory
            .appending(path: "DiagnosticPayloadStoreTests-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    }

    override func tearDown() {
        try? FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: root.path)
        try? FileManager.default.removeItem(at: root)
        super.tearDown()
    }

    func testSaveCreatesTheDirectoryAndWritesThePayload() throws {
        let directory = root.appending(path: "nested/diagnostics")
        let store = DiagnosticPayloadStore(directory: directory, now: { Date(timeIntervalSince1970: 1_788_273_000) })

        let file = try store.save(Data(#"{"crash":1}"#.utf8), kind: "diagnostic").get()

        XCTAssertEqual(try Data(contentsOf: file), Data(#"{"crash":1}"#.utf8))
        XCTAssertEqual(file.deletingLastPathComponent().standardizedFileURL, directory.standardizedFileURL)
        XCTAssertTrue(file.lastPathComponent.hasPrefix("20260901T"), file.lastPathComponent)
        XCTAssertTrue(file.lastPathComponent.hasSuffix(".json"))
        XCTAssertTrue(file.lastPathComponent.contains("-diagnostic-"))
    }

    func testRotationKeepsOnlyTheNewestFiles() throws {
        let clock = TestClock(start: Date(timeIntervalSince1970: 1_788_273_000))
        let store = DiagnosticPayloadStore(directory: root, maxFiles: 3, now: { clock.tick() })

        for index in 0..<7 {
            try store.save(Data("\(index)".utf8), kind: "diagnostic").get()
        }

        let kept = try store.storedFiles().map { String(decoding: try Data(contentsOf: $0), as: UTF8.self) }
        XCTAssertEqual(kept, ["6", "5", "4"])
    }

    func testRotationFollowsTheClockNotTheWriteOrder() throws {
        let old = DiagnosticPayloadStore(directory: root, maxFiles: 2, now: { Date(timeIntervalSince1970: 1_000) })
        let recent = DiagnosticPayloadStore(directory: root, maxFiles: 2, now: { Date(timeIntervalSince1970: 9_000) })

        try recent.save(Data("recent-a".utf8), kind: "diagnostic").get()
        try recent.save(Data("recent-b".utf8), kind: "diagnostic").get()
        try old.save(Data("old".utf8), kind: "diagnostic").get()

        let kept = try Set(old.storedFiles().map { String(decoding: try Data(contentsOf: $0), as: UTF8.self) })
        XCTAssertEqual(kept, ["recent-a", "recent-b"])
    }

    func testDefaultLimitIsTwentyAndOtherFilesAreLeftAlone() throws {
        let clock = TestClock(start: Date(timeIntervalSince1970: 1_788_273_000))
        let store = DiagnosticPayloadStore(directory: root, now: { clock.tick() })
        let unrelated = root.appending(path: "notes.txt")
        try Data("keep me".utf8).write(to: unrelated)

        for index in 0..<25 {
            store.save(Data("\(index)".utf8), kind: "diagnostic")
        }

        XCTAssertEqual(store.storedFiles().count, 20)
        XCTAssertTrue(FileManager.default.fileExists(atPath: unrelated.path))
    }

    func testKindIsSanitisedForTheFileName() throws {
        let store = DiagnosticPayloadStore(directory: root)

        let file = try store.save(Data("{}".utf8), kind: "../../etc/passwd").get()
        let unnamed = try store.save(Data("{}".utf8), kind: "///").get()

        XCTAssertEqual(file.deletingLastPathComponent().standardizedFileURL, root.standardizedFileURL)
        XCTAssertTrue(file.lastPathComponent.contains("-etcpasswd-"))
        XCTAssertTrue(unnamed.lastPathComponent.contains("-payload-"))
    }

    func testUnwritableDirectoryFailsWithoutThrowing() throws {
        try FileManager.default.setAttributes([.posixPermissions: 0o555], ofItemAtPath: root.path)
        let store = DiagnosticPayloadStore(directory: root.appending(path: "blocked"))

        let result = store.save(Data("{}".utf8), kind: "diagnostic")

        guard case .failure = result else {
            return XCTFail("Expected the write to fail")
        }
        XCTAssertEqual(store.storedFiles(), [])
    }

    func testDirectoryPathOccupiedByAFileFailsWithoutThrowing() throws {
        let occupied = root.appending(path: "diagnostics")
        try Data("not a directory".utf8).write(to: occupied)
        let store = DiagnosticPayloadStore(directory: occupied)

        guard case .failure = store.save(Data("{}".utf8), kind: "diagnostic") else {
            return XCTFail("Expected the write to fail")
        }
        XCTAssertEqual(store.storedFiles(), [])
    }
}

/// Clock that moves forward one second every time it is read.
private final class TestClock: @unchecked Sendable {
    private let lock = NSLock()
    private var current: Date

    init(start: Date) {
        current = start
    }

    func tick() -> Date {
        lock.lock()
        defer { lock.unlock() }
        current.addTimeInterval(1)
        return current
    }
}
