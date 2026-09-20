import Foundation

/// One comment from `GET /v1/proposals/{id}/comments` or the body of `POST .../comments`.
/// `parentId` is nil for top-level comments.
public struct ProposalCommentDTO: Codable, Equatable, Sendable, Identifiable {
    public let id: String
    public let proposalId: String
    public let parentId: String?
    public let authorId: String
    public let authorName: String
    public let body: String
    public let createdAt: String

    public init(
        id: String,
        proposalId: String,
        parentId: String? = nil,
        authorId: String,
        authorName: String,
        body: String,
        createdAt: String
    ) {
        self.id = id
        self.proposalId = proposalId
        self.parentId = parentId
        self.authorId = authorId
        self.authorName = authorName
        self.body = body
        self.createdAt = createdAt
    }
}

public struct ProposalCommentsResponseDTO: Codable, Equatable, Sendable {
    public let comments: [ProposalCommentDTO]

    public init(comments: [ProposalCommentDTO]) {
        self.comments = comments
    }
}

/// A comment placed in its thread: `depth` 0 is top level, `indentLevel` caps visual nesting.
public struct ProposalCommentThreadRow: Equatable, Sendable, Identifiable {
    public let comment: ProposalCommentDTO
    public let depth: Int
    public let replyToName: String?

    public var id: String { comment.id }

    public var indentLevel: Int {
        min(depth, ProposalCommentThread.maxIndentLevel)
    }
}

/// Builds display order for a flat, oldest-first comment list: each comment is followed by its replies.
public enum ProposalCommentThread {
    /// Deeper replies keep their place in the thread but stop indenting so narrow screens stay readable.
    public static let maxIndentLevel = 3

    public static func rows(from comments: [ProposalCommentDTO]) -> [ProposalCommentThreadRow] {
        let byID = Dictionary(comments.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        var children: [String: [ProposalCommentDTO]] = [:]
        var roots: [ProposalCommentDTO] = []

        for comment in comments {
            if let parentId = comment.parentId, !parentId.isEmpty, parentId != comment.id, byID[parentId] != nil {
                children[parentId, default: []].append(comment)
            } else {
                // Missing parent (outside the fetched page) still renders, as a top-level comment.
                roots.append(comment)
            }
        }

        var out: [ProposalCommentThreadRow] = []
        out.reserveCapacity(comments.count)
        var visited = Set<String>()

        func visit(_ comment: ProposalCommentDTO, depth: Int, replyToName: String?) {
            guard visited.insert(comment.id).inserted else { return }
            out.append(ProposalCommentThreadRow(comment: comment, depth: depth, replyToName: replyToName))
            for child in children[comment.id] ?? [] {
                visit(child, depth: depth + 1, replyToName: comment.authorName)
            }
        }

        for root in roots {
            visit(root, depth: 0, replyToName: nil)
        }
        return out
    }
}

/// Client-side mirror of the backend comment rule: trimmed, 1...1,000 Unicode code points.
public enum ProposalCommentDraft: Equatable {
    public static let maxCodePoints = 1000

    case empty
    case tooLong(overBy: Int)
    case ready(body: String)

    public init(text: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        let count = trimmed.unicodeScalars.count
        if trimmed.isEmpty {
            self = .empty
        } else if count > Self.maxCodePoints {
            self = .tooLong(overBy: count - Self.maxCodePoints)
        } else {
            self = .ready(body: trimmed)
        }
    }

    public var body: String? {
        if case .ready(let body) = self { return body }
        return nil
    }
}
