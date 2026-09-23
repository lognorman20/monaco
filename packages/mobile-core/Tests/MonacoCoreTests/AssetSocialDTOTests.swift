import Foundation
import XCTest
@testable import MonacoCore

/// The shape of `GET /v1/assets/{symbol}/social` as the backend actually writes it.
final class AssetSocialDTOTests: XCTestCase {
    private func decode(_ json: String) throws -> AssetSocialDTO {
        try JSONDecoder().decode(AssetSocialDTO.self, from: Data(json.utf8))
    }

    func testDecodesTheFullPayload() throws {
        let social = try decode("""
        {
          "symbol": "AAPLc",
          "holdings": [{
            "groupId": "g1", "name": "Weekend investors", "units": "12",
            "tokenAmount": "1200000000", "markUsd": "232.05", "valueUsd": "2784.60",
            "costBasisUsd": "2600.00", "dollarPnl": "+184.60", "percentReturn": "0.071",
            "mySliceUsd": "556.92", "mySlicePercent": "0.2", "afterHours": true
          }],
          "openProposals": [{
            "id": "p1", "groupId": "g1", "groupName": "Weekend investors", "kind": "buy",
            "status": "open", "usdcMicros": 500000000, "tokenAmount": 0, "thesis": "Earnings",
            "yes": 2, "no": 1, "memberCount": 5, "myVote": "yes",
            "voters": [{"userId": "u1", "displayName": "Ada", "choice": "yes"}],
            "expiresAt": "2026-09-23T10:00:00Z", "createdAt": "2026-09-22T10:00:00Z"
          }],
          "activity": [{
            "id": "a1", "groupId": "g1", "groupName": "Weekend investors", "kind": "filled",
            "action": "buy", "usdcMicros": 905000000, "tokenAmount": 350000000,
            "actorName": "Ada", "txHash": "0xfeed", "createdAt": "2026-09-20T10:00:00Z"
          }],
          "holderCount": 1,
          "unvaluedGroups": 0
        }
        """)

        XCTAssertEqual(social.symbol, "AAPLc")
        XCTAssertEqual(social.holdings.count, 1)
        XCTAssertEqual(social.holdings[0].dollarPnl, "+184.60")
        XCTAssertTrue(social.holdings[0].afterHours)
        XCTAssertEqual(social.openProposals[0].myVote, .yes)
        XCTAssertEqual(social.openProposals[0].yesVoters.map(\.displayName), ["Ada"])
        XCTAssertEqual(social.activity[0].kind, .filled)
        XCTAssertEqual(social.activity[0].action, .buy)
        XCTAssertNotNil(social.activity[0].createdAt)
        XCTAssertFalse(social.isEmpty)
    }

    /// An older backend, or a member with nothing: every list must still decode to
    /// an array, because a nil here would crash a `ForEach`.
    func testMissingKeysDecodeToEmptyListsRatherThanFailing() throws {
        let social = try decode(#"{"symbol":"AAPLc"}"#)
        XCTAssertTrue(social.holdings.isEmpty)
        XCTAssertTrue(social.openProposals.isEmpty)
        XCTAssertTrue(social.activity.isEmpty)
        XCTAssertEqual(social.holderCount, 0)
        XCTAssertTrue(social.isEmpty)
    }

    /// The backend omits `myVote` entirely when the viewer has not voted. That
    /// absence is the difference between "2 open votes" and "waiting on you", so it
    /// must not decode to a default.
    func testAbsentMyVoteStaysNil() throws {
        let social = try decode("""
        {"symbol":"AAPLc","openProposals":[
          {"id":"p1","groupId":"g1","groupName":"G","kind":"buy","yes":0,"no":0}
        ]}
        """)
        XCTAssertNil(social.openProposals[0].myVote)
    }

    /// A vote choice or activity kind this build does not know must not fail the
    /// whole response.
    func testUnknownEnumValuesDegradeRatherThanThrow() throws {
        let social = try decode("""
        {"symbol":"AAPLc",
         "openProposals":[{"id":"p","groupId":"g","groupName":"G","kind":"swap","myVote":"abstain",
           "voters":[{"userId":"u","displayName":"Ada","choice":"abstain"}]}],
         "activity":[{"id":"a","groupId":"g","groupName":"G","kind":"teleported","action":"swap"}]}
        """)
        XCTAssertEqual(social.openProposals[0].kind, .unknown)
        XCTAssertEqual(social.openProposals[0].myVote, .unknown)
        XCTAssertEqual(social.openProposals[0].voters[0].choice, .unknown)
        XCTAssertEqual(social.activity[0].kind, .unknown)
        XCTAssertEqual(social.activity[0].action, .unknown)
    }

    /// An empty photo URL and an absent one are the same absence; the avatar must
    /// not try to load "".
    func testEmptyStringsBecomeNil() throws {
        let social = try decode("""
        {"symbol":"AAPLc",
         "openProposals":[{"id":"p","groupId":"g","groupName":"G","kind":"buy","thesis":"",
           "voters":[{"userId":"u","displayName":"Ada","profilePhotoUrl":"","choice":"yes"}]}],
         "activity":[{"id":"a","groupId":"g","groupName":"G","kind":"filled","actorName":"","txHash":""}]}
        """)
        XCTAssertNil(social.openProposals[0].voters[0].profilePhotoUrl)
        XCTAssertNil(social.openProposals[0].thesis)
        XCTAssertNil(social.activity[0].actorName)
        XCTAssertNil(social.activity[0].txHash)
    }

    /// A pass where a cabal could not be priced is not "empty": the card still has
    /// something to say.
    func testUnvaluedGroupsKeepTheCardOnScreen() throws {
        let social = try decode(#"{"symbol":"AAPLc","unvaluedGroups":2}"#)
        XCTAssertFalse(social.isEmpty)
        XCTAssertEqual(social.unvaluedGroups, 2)
    }

    func testRoundTripsThroughEncodeAndDecode() throws {
        let original = AssetSocialSampleData.social()
        let data = try JSONEncoder().encode(original)
        let decoded = try JSONDecoder().decode(AssetSocialDTO.self, from: data)
        XCTAssertEqual(decoded, original)
    }
}
