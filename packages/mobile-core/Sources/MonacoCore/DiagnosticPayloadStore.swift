import Foundation

/// Keeps the newest diagnostic payloads (crash, hang, CPU and disk-write reports) as JSON
/// files in one directory and deletes the rest, so the folder can never grow without bound.
///
/// File names start with a UTC timestamp, which makes name order the same as age order.
/// Writing is best effort: a full disk or an unwritable directory is reported in the
/// result, never thrown, because diagnostics must not take the app down with them.
public struct DiagnosticPayloadStore: Sendable {
    public static let defaultMaxFiles = 20

    public let directory: URL
    public let maxFiles: Int
    private let now: @Sendable () -> Date

    public init(
        directory: URL,
        maxFiles: Int = DiagnosticPayloadStore.defaultMaxFiles,
        now: @escaping @Sendable () -> Date = { Date() }
    ) {
        self.directory = directory
        self.maxFiles = max(1, maxFiles)
        self.now = now
    }

    /// Writes `payload` as a new file, then prunes down to `maxFiles`.
    /// - Parameter kind: short label for the file name, e.g. `diagnostic`.
    @discardableResult
    public func save(_ payload: Data, kind: String) -> Result<URL, Error> {
        let fileManager = FileManager.default
        do {
            try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
            let file = directory.appending(path: fileName(kind: kind))
            try payload.write(to: file, options: .atomic)
            prune()
            return .success(file)
        } catch {
            return .failure(error)
        }
    }

    /// Stored payload files, newest first.
    public func storedFiles() -> [URL] {
        let contents = (try? FileManager.default.contentsOfDirectory(
            at: directory,
            includingPropertiesForKeys: nil
        )) ?? []
        return contents
            .filter { $0.pathExtension == Self.fileExtension }
            .sorted { $0.lastPathComponent > $1.lastPathComponent }
    }

    private func prune() {
        for stale in storedFiles().dropFirst(maxFiles) {
            try? FileManager.default.removeItem(at: stale)
        }
    }

    private static let fileExtension = "json"

    private func fileName(kind: String) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(identifier: "UTC")
        formatter.dateFormat = "yyyyMMdd'T'HHmmss.SSS'Z'"
        let label = kind.filter { $0.isASCII && ($0.isLetter || $0.isNumber) }
        // The suffix keeps two payloads delivered in the same millisecond apart.
        let suffix = UUID().uuidString.prefix(8).lowercased()
        return "\(formatter.string(from: now()))-\(label.isEmpty ? "payload" : label)-\(suffix).\(Self.fileExtension)"
    }
}
