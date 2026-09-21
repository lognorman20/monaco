import Testing
@testable import Monaco

struct DynamicSessionTokenTests {
    @Test func prefersMinAuthTokenOverIdToken() {
        let got = DynamicSessionToken.preferred(minAuthToken: " access ", idToken: "id")
        #expect(got == "access")
    }

    @Test func fallsBackToIdToken() {
        let got = DynamicSessionToken.preferred(minAuthToken: "  ", idToken: "id-token")
        #expect(got == "id-token")
    }

    @Test func emptyBothIsNil() {
        #expect(DynamicSessionToken.preferred(minAuthToken: nil, idToken: nil) == nil)
        #expect(DynamicSessionToken.preferred(minAuthToken: "", idToken: " ") == nil)
    }
}
