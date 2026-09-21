import Foundation

enum DynamicLoginChannel: Equatable, Sendable {
    case sms
    case email

    var deviceRegistrationCopy: String {
        switch self {
        case .sms:
            return "We’ll text another code to this number so this phone can stay signed in."
        case .email:
            return "We’ll email another code so this phone can stay signed in."
        }
    }
}
