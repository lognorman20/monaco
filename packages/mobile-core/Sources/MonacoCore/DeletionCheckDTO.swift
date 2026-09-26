import Foundation

/// One thing standing between the member and deleting their account, from
/// `GET /v1/me/deletion-check` (and the `409` a refused `DELETE /v1/me` carries).
public struct DeletionBlockerDTO: Decodable, Equatable, Sendable, Identifiable {
    public enum Kind: Equatable, Sendable {
        /// Share units left in a cabal. The way out is cashing out of it.
        case cabalSlice
        /// A cash out from a cabal that has not landed yet.
        case cashOutPending
        /// A fund or a withdrawal still on its way.
        case transferPending
        /// USDC in the member's account balance.
        case accountBalance
        /// A kind this build does not know. It still blocks.
        case unknown(String)

        init(rawValue: String) {
            switch rawValue {
            case "cabal_slice": self = .cabalSlice
            case "cash_out_pending": self = .cashOutPending
            case "transfer_pending": self = .transferPending
            case "account_balance": self = .accountBalance
            default: self = .unknown(rawValue)
            }
        }

        public var rawValue: String {
            switch self {
            case .cabalSlice: return "cabal_slice"
            case .cashOutPending: return "cash_out_pending"
            case .transferPending: return "transfer_pending"
            case .accountBalance: return "account_balance"
            case .unknown(let raw): return raw
            }
        }
    }

    public let kind: Kind
    /// Set for the cabal kinds.
    public let groupId: String?
    public let groupName: String?
    /// Decimal dollars ("245.12"). Nil when the server could not price a slice right now.
    public let valueUsd: String?

    public var id: String { "\(kind.rawValue)|\(groupId ?? "")" }

    public init(kind: Kind, groupId: String? = nil, groupName: String? = nil, valueUsd: String?) {
        self.kind = kind
        self.groupId = groupId
        self.groupName = groupName
        self.valueUsd = valueUsd
    }

    enum CodingKeys: String, CodingKey {
        case kind, groupId, groupName, valueUsd
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        kind = Kind(rawValue: try container.decode(String.self, forKey: .kind))
        groupId = try container.decodeIfPresent(String.self, forKey: .groupId).flatMap { $0.isEmpty ? nil : $0 }
        groupName = try container.decodeIfPresent(String.self, forKey: .groupName).flatMap { $0.isEmpty ? nil : $0 }
        valueUsd = try container.decodeIfPresent(String.self, forKey: .valueUsd)
    }
}

/// `GET /v1/me/deletion-check`.
public struct DeletionCheckDTO: Decodable, Equatable, Sendable {
    public let canDelete: Bool
    public let blockers: [DeletionBlockerDTO]

    public init(canDelete: Bool, blockers: [DeletionBlockerDTO]) {
        self.canDelete = canDelete
        self.blockers = blockers
    }

    enum CodingKeys: String, CodingKey {
        case canDelete, blockers
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        blockers = try container.decodeIfPresent([DeletionBlockerDTO].self, forKey: .blockers) ?? []
        // An account with something in it is never deletable, whatever the flag says.
        canDelete = (try container.decode(Bool.self, forKey: .canDelete)) && blockers.isEmpty
    }
}

/// `DELETE /v1/me` answered.
public enum AccountDeletionOutcome: Equatable, Sendable {
    /// The account is gone. `deletedAt` is nil only if the server sent an unreadable time.
    case deleted(deletedAt: Date?)
    /// `409 account_not_empty`: money is still in the account, listed like the check lists it.
    case blocked(DeletionCheckDTO)
}

struct DeletedAccountDTO: Decodable {
    let deletedAt: String
}
