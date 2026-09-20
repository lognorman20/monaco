import XCTest
@testable import MonacoCore

final class ProposalDTOTests: XCTestCase {
    func testProposalDTO_decodesOpenPassedFailedExpiredStatuses() throws {
        // Arrange
        let statuses = ["open", "passed", "failed", "expired"]

        // Act
        let decoded = try statuses.map { status -> ProposalDTO in
            let json = """
            {
              "id": "prop-\(status)",
              "symbol": "AAPLx",
              "usdcMicros": "2500000",
              "status": "\(status)"
            }
            """
            return try JSONDecoder().decode(ProposalDTO.self, from: Data(json.utf8))
        }

        // Assert
        XCTAssertEqual(decoded.map(\.status), statuses)
        for (dto, status) in zip(decoded, statuses) {
            XCTAssertEqual(ProposalStatusDisplay.from(status: dto.status)?.label, ProposalStatusDisplay.from(status: status)?.label)
            XCTAssertEqual(dto.resolvedKind, "buy")
        }
    }

    func testProposalDTO_decodesSellKindAndTokenAmount() throws {
        let json = """
        {
          "id": "prop-sell",
          "symbol": "AAPLx",
          "kind": "sell",
          "tokenAmount": "50000000",
          "status": "open"
        }
        """

        let dto = try JSONDecoder().decode(ProposalDTO.self, from: Data(json.utf8))

        XCTAssertEqual(dto.resolvedKind, "sell")
        XCTAssertEqual(dto.tokenAmount, "50000000")
        XCTAssertNil(dto.usdcMicros)
    }

    func testProposalDTO_decodesThesisWhenPresentAndNilWhenAbsent() throws {
        let withThesis = """
        {
          "id": "prop-thesis",
          "symbol": "AAPLx",
          "usdcMicros": "2500000",
          "status": "open",
          "thesis": "Strong earnings beat, raising guidance."
        }
        """
        let withoutThesis = """
        {
          "id": "prop-no-thesis",
          "symbol": "AAPLx",
          "usdcMicros": "2500000",
          "status": "open"
        }
        """

        let dtoWithThesis = try JSONDecoder().decode(ProposalDTO.self, from: Data(withThesis.utf8))
        let dtoWithoutThesis = try JSONDecoder().decode(ProposalDTO.self, from: Data(withoutThesis.utf8))

        XCTAssertEqual(dtoWithThesis.thesis, "Strong earnings beat, raising guidance.")
        XCTAssertNil(dtoWithoutThesis.thesis)
    }

    func testProposalListResponse_decodesFeedCardFields() throws {
        // Arrange
        let json = """
        {
          "proposals": [
            {
              "id": "prop-1",
              "symbol": "AAPLx",
              "usdcMicros": "25000000",
              "status": "open",
              "proposerId": "user-ada",
              "proposerName": "Ada",
              "createdAt": "2026-09-18T01:00:00Z",
              "expiresAt": "2026-09-19T01:00:00Z",
              "canVote": true,
              "voteSummary": {"yesCount": 2, "noCount": 1, "eligibleCount": 5, "threshold": "majority"},
              "commentCount": 4,
              "thesis": "Earnings Thursday."
            }
          ]
        }
        """

        // Act
        let decoded = try JSONDecoder().decode(ProposalListResponseDTO.self, from: Data(json.utf8))

        // Assert
        let card = try XCTUnwrap(decoded.proposals.first)
        XCTAssertEqual(card.voteSummary, ProposalVoteSummaryDTO(yesCount: 2, noCount: 1, eligibleCount: 5, threshold: "majority"))
        XCTAssertEqual(card.commentCount, 4)
        XCTAssertEqual(card.proposerName, "Ada")
        // The feed card's reason line reads the thesis straight from the list item.
        XCTAssertEqual(card.thesis, "Earnings Thursday.")
        XCTAssertTrue(card.showsVoteActions)
        XCTAssertNil(card.votes)
    }

    func testProposalDTO_legacyListPayloadWithoutFeedFields_decodes() throws {
        // Arrange: servers before #149 omit canVote, voteSummary and commentCount.
        let json = """
        {"id":"prop-2","symbol":"NVDAx","usdcMicros":"1000000","status":"passed","proposerName":"Ben","createdAt":"2026-09-01T00:00:00Z"}
        """

        // Act
        let decoded = try JSONDecoder().decode(ProposalDTO.self, from: Data(json.utf8))

        // Assert
        XCTAssertNil(decoded.voteSummary)
        XCTAssertNil(decoded.commentCount)
        XCTAssertFalse(decoded.showsVoteActions)
    }

    func testProposalDTO_detailPayload_decodesVotesExecutionAndCommentCount() throws {
        // Arrange
        let json = """
        {
          "id": "prop-3",
          "groupId": "grp-1",
          "symbol": "AAPLx",
          "usdcMicros": "5000000",
          "status": "passed",
          "createdAt": "2026-09-18T01:00:00Z",
          "expiresAt": "2026-09-19T01:00:00Z",
          "proposerId": "user-ada",
          "proposerName": "Ada",
          "canVote": false,
          "votes": [{"voterId": "user-ben", "displayName": "Ben", "choice": "yes", "castAt": "2026-09-18T02:00:00Z"}],
          "voteSummary": {"yesCount": 1, "noCount": 0, "eligibleCount": 1, "threshold": "unanimous"},
          "execution": {"state": "pending"},
          "commentCount": 0
        }
        """

        // Act
        let decoded = try JSONDecoder().decode(ProposalDTO.self, from: Data(json.utf8))

        // Assert
        XCTAssertEqual(decoded.votes?.map(\.displayName), ["Ben"])
        XCTAssertEqual(decoded.execution?.state, "pending")
        XCTAssertEqual(decoded.commentCount, 0)
        XCTAssertFalse(decoded.showsVoteActions)
    }

    func testProposalDTO_addAgentDetail_decodesAgentFieldsAndMintedKey() throws {
        // Arrange
        let json = """
        {"id":"prop-a","symbol":"","kind":"add_agent","agentDisplayName":"Scout","allocationUsdcMicros":"500000000","mintedAgentKey":"mk_live_123","status":"passed","commentCount":1}
        """

        // Act
        let decoded = try JSONDecoder().decode(ProposalDTO.self, from: Data(json.utf8))

        // Assert
        XCTAssertEqual(decoded.resolvedKind, "add_agent")
        XCTAssertEqual(decoded.agentDisplayName, "Scout")
        XCTAssertEqual(decoded.allocationUsdcMicros, "500000000")
        XCTAssertEqual(decoded.mintedAgentKey, "mk_live_123")
        XCTAssertFalse(decoded.isTrade)
    }

    func testShowsVoteActions_closedProposalWithStaleCanVote_isFalse() {
        // Arrange
        let proposal = ProposalDTO(id: "p", symbol: "AAPLx", status: "failed", usdcMicros: "1", canVote: true)

        // Act
        let shows = proposal.showsVoteActions

        // Assert
        XCTAssertFalse(shows)
    }
}
