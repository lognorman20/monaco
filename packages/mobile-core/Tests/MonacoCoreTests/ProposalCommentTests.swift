import XCTest
@testable import MonacoCore

final class ProposalCommentTests: XCTestCase {
    private func comment(_ id: String, parent: String? = nil, author: String = "Ada") -> ProposalCommentDTO {
        ProposalCommentDTO(
            id: id,
            proposalId: "prop-1",
            parentId: parent,
            authorId: "user-\(author.lowercased())",
            authorName: author,
            body: "Comment \(id)",
            createdAt: "2026-09-18T01:00:00Z"
        )
    }

    func testProposalCommentsResponse_decodesTopLevelAndReply() throws {
        // Arrange
        let json = """
        {
          "comments": [
            {"id":"c1","proposalId":"prop-1","authorId":"u1","authorName":"Ben","body":"Why Apple?","createdAt":"2026-09-18T01:00:00Z"},
            {"id":"c2","proposalId":"prop-1","parentId":"c1","authorId":"u2","authorName":"Ada","body":"Lower drawdown.","createdAt":"2026-09-18T01:05:00Z"}
          ]
        }
        """

        // Act
        let decoded = try JSONDecoder().decode(ProposalCommentsResponseDTO.self, from: Data(json.utf8))

        // Assert
        XCTAssertEqual(decoded.comments.count, 2)
        XCTAssertNil(decoded.comments[0].parentId)
        XCTAssertEqual(decoded.comments[1].parentId, "c1")
        XCTAssertEqual(decoded.comments[1].authorName, "Ada")
    }

    func testThreadRows_placeRepliesDirectlyUnderParentInOrder() {
        // Arrange: flat oldest-first list where a later top-level comment arrives before a reply.
        let comments = [
            comment("c1", author: "Ben"),
            comment("c2", author: "Cy"),
            comment("c3", parent: "c1", author: "Ada"),
            comment("c4", parent: "c3", author: "Ben"),
        ]

        // Act
        let rows = ProposalCommentThread.rows(from: comments)

        // Assert
        XCTAssertEqual(rows.map(\.id), ["c1", "c3", "c4", "c2"])
        XCTAssertEqual(rows.map(\.depth), [0, 1, 2, 0])
        XCTAssertEqual(rows.map(\.replyToName), [nil, "Ben", "Ada", nil])
    }

    func testThreadRows_replyWithMissingParent_rendersAsTopLevel() {
        // Arrange
        let comments = [comment("c1"), comment("c2", parent: "gone")]

        // Act
        let rows = ProposalCommentThread.rows(from: comments)

        // Assert
        XCTAssertEqual(rows.map(\.id), ["c1", "c2"])
        XCTAssertEqual(rows.map(\.depth), [0, 0])
    }

    func testThreadRows_deepChain_capsIndentButKeepsEveryComment() {
        // Arrange
        var comments = [comment("c0")]
        for index in 1...6 {
            comments.append(comment("c\(index)", parent: "c\(index - 1)"))
        }

        // Act
        let rows = ProposalCommentThread.rows(from: comments)

        // Assert
        XCTAssertEqual(rows.count, 7)
        XCTAssertEqual(rows.last?.depth, 6)
        XCTAssertEqual(rows.last?.indentLevel, ProposalCommentThread.maxIndentLevel)
    }

    func testThreadRows_cycleInData_doesNotLoopOrDropComments() {
        // Arrange: corrupt data where two comments name each other as parent.
        let comments = [comment("root"), comment("a", parent: "b"), comment("b", parent: "a")]

        // Act
        let rows = ProposalCommentThread.rows(from: comments)

        // Assert: the cycle is unreachable from a root, so only the root renders; no hang.
        XCTAssertEqual(rows.map(\.id), ["root"])
    }

    func testCommentDraft_whitespaceOnly_isEmpty() {
        // Arrange
        let text = "  \n\t "

        // Act
        let draft = ProposalCommentDraft(text: text)

        // Assert
        XCTAssertEqual(draft, .empty)
        XCTAssertNil(draft.body)
    }

    func testCommentDraft_trimsSurroundingWhitespace() {
        // Arrange
        let text = "  In on Apple.\n"

        // Act
        let draft = ProposalCommentDraft(text: text)

        // Assert
        XCTAssertEqual(draft.body, "In on Apple.")
    }

    func testCommentDraft_countsCodePointsLikeBackend() {
        // Arrange: "é" as one scalar; limit matches domain.MaxProposalCommentRunes.
        let atLimit = String(repeating: "é", count: ProposalCommentDraft.maxCodePoints)
        let overLimit = atLimit + "é"

        // Act
        let ok = ProposalCommentDraft(text: atLimit)
        let tooLong = ProposalCommentDraft(text: overLimit)

        // Assert
        XCTAssertNotNil(ok.body)
        XCTAssertEqual(tooLong, .tooLong(overBy: 1))
    }
}
