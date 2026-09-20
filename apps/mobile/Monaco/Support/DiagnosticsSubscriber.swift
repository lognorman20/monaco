import Foundation
import MonacoCore

#if canImport(MetricKit)
import MetricKit

/// Receives MetricKit diagnostic payloads (crashes, hangs, CPU exceptions, disk-write
/// exceptions), keeps the newest ones as JSON in Application Support and logs a summary.
/// iOS delivers payloads for earlier runs too, so a crash shows up on the next launch.
final class DiagnosticsSubscriber: NSObject, MXMetricManagerSubscriber {
    static let shared = DiagnosticsSubscriber()

    private let store: DiagnosticPayloadStore

    init(store: DiagnosticPayloadStore = DiagnosticPayloadStore(directory: DiagnosticsSubscriber.defaultDirectory)) {
        self.store = store
    }

    /// Call once at launch. The manager holds subscribers weakly; `shared` keeps this one alive.
    func start() {
        MXMetricManager.shared.add(self)
    }

    func didReceive(_ payloads: [MXDiagnosticPayload]) {
        for payload in payloads {
            let crashes = payload.crashDiagnostics?.count ?? 0
            let hangs = payload.hangDiagnostics?.count ?? 0
            let cpuExceptions = payload.cpuExceptionDiagnostics?.count ?? 0
            let diskWrites = payload.diskWriteExceptionDiagnostics?.count ?? 0

            switch store.save(payload.jsonRepresentation(), kind: "diagnostic") {
            case .success(let file):
                AppLogger.diagnostics.error("Diagnostics received: crashes=\(crashes, privacy: .public) hangs=\(hangs, privacy: .public) cpu=\(cpuExceptions, privacy: .public) disk_writes=\(diskWrites, privacy: .public) saved=\(file.lastPathComponent, privacy: .public)")
            case .failure(let error):
                AppLogger.diagnostics.error("Diagnostics received: crashes=\(crashes, privacy: .public) hangs=\(hangs, privacy: .public) cpu=\(cpuExceptions, privacy: .public) disk_writes=\(diskWrites, privacy: .public) not saved: \(String(describing: error), privacy: .public)")
            }
        }
    }

    private static var defaultDirectory: URL {
        URL.applicationSupportDirectory.appending(path: "Diagnostics", directoryHint: .isDirectory)
    }
}
#endif
