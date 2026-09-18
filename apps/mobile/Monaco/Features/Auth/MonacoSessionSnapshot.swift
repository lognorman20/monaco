import MonacoCore
import SwiftUI

struct MonacoSessionSnapshot: Sendable {
    let home: HomeViewDTO
    let profile: MeResponse
}

typealias MonacoSessionState = RefreshableSnapshot<MonacoSessionSnapshot>

private struct SessionSnapshotKey: EnvironmentKey {
    static let defaultValue: MonacoSessionSnapshot? = nil
}

private struct SessionRevisionKey: EnvironmentKey {
    static let defaultValue = 0
}

private struct SessionRefreshKey: EnvironmentKey {
    static let defaultValue: @MainActor () async -> Void = {}
}

extension EnvironmentValues {
    var monacoSessionSnapshot: MonacoSessionSnapshot? {
        get { self[SessionSnapshotKey.self] }
        set { self[SessionSnapshotKey.self] = newValue }
    }
    var monacoSessionRevision: Int {
        get { self[SessionRevisionKey.self] }
        set { self[SessionRevisionKey.self] = newValue }
    }
    var refreshMonacoSession: @MainActor () async -> Void {
        get { self[SessionRefreshKey.self] }
        set { self[SessionRefreshKey.self] = newValue }
    }
}

extension Notification.Name {
    static let monacoSessionChanged = Notification.Name("monaco.sessionChanged")
}
