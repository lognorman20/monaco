import Foundation
import os

/// Persistent, structured logging via os.Logger (visible in Console.app / `log stream`,
/// unlike a bare `print`). Add a category here per subsystem as needed.
enum AppLogger {
    static let session = Logger(
        subsystem: Bundle.main.bundleIdentifier ?? "com.monaco.app",
        category: "session"
    )
}
