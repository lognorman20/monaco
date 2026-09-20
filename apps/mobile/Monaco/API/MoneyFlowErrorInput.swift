import Foundation
import MonacoCore

extension FlowErrorInput {
    /// Reduces whatever a money request threw to what `MoneyFlowCopy` words its failures from.
    /// Anything without an HTTP status that isn't provably "never sent" stays status-less, so
    /// the copy treats it as unconfirmed rather than inviting a second transfer.
    init(_ error: Error) {
        switch error {
        case MonacoAPIError.httpStatus(let status):
            self.init(status: status)
        case MonacoAPIError.apiError(let status, let message):
            self.init(status: status, serverMessage: message)
        case let urlError as URLError where Self.neverSentURLErrorCodes.contains(urlError.code):
            self.init(isOffline: true)
        default:
            self.init()
        }
    }
}
