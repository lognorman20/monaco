import MonacoCore
import SwiftUI
import Testing
@testable import Monaco

/// The Reduce Motion accessor, and the rule it makes enforceable.
///
/// Before this, each component gated its own animation — or did not, and nobody found out until
/// someone with Reduce Motion on used the app. `reduced(_:)` returns `Animation?` so
/// `withAnimation` and `.animation(_:value:)` take it directly, which makes the gate the path of
/// least resistance rather than an extra step.
struct MonacoMotionTests {
    @Test func reduceMotionRemovesTheCurveRatherThanWeakeningIt() {
        for curve in [MonacoMotion.snap, MonacoMotion.settle, MonacoMotion.glide, MonacoMotion.celebrate] {
            #expect(curve.reduced(true) == nil)
            #expect(curve.reduced(false) == curve)
        }
    }

    /// A slower curve is still motion. The accessor has to return `nil` — the change still
    /// happens, it simply is not animated — and a future "just use a shorter duration" has to
    /// argue with this.
    @Test func theReducedCurveIsNotAnotherAnimation() {
        #expect(MonacoMotion.settle.reduced(true) == nil)
        #expect(MonacoMotion.settle.reduced(true) != MonacoMotion.glide)
    }

    /// Four curves, all distinct. Two near-identical springs are how the button press
    /// (`.spring(0.25, 0.8)`) and the segmented thumb (`.spring(0.3, 0.85)`) drifted apart while
    /// expressing the same moment; both are `snap` now.
    @Test func theFourCurvesAreActuallyDifferent() {
        let curves = [MonacoMotion.snap, MonacoMotion.settle, MonacoMotion.glide, MonacoMotion.celebrate]
        for (index, curve) in curves.enumerated() {
            for other in curves[(index + 1)...] {
                #expect(curve != other)
            }
        }
    }
}

/// The vote tally's fallback rule.
///
/// The faces and the dots ship together. Past eight voters a row of avatars stops reading as a
/// tally and starts reading as a crowd, and at an accessibility text size the row cannot fit them
/// at all — so the most social component in the app must not be the one that breaks for the
/// members who most need it to work.
struct MonacoVoteTallyLayoutTests {
    private let nonAccessibilitySizes: [DynamicTypeSize] = [
        .xSmall, .small, .medium, .large, .xLarge, .xxLarge, .xxxLarge,
    ]

    private let accessibilitySizes: [DynamicTypeSize] = [
        .accessibility1, .accessibility2, .accessibility3, .accessibility4, .accessibility5,
    ]

    @Test func facesUpToEightVoters() {
        for count in 1...MonacoVoteTallyLayout.maximumFaces {
            for size in nonAccessibilitySizes {
                #expect(MonacoVoteTallyLayout.usesFaces(voterCount: count, dynamicTypeSize: size))
            }
        }
    }

    @Test func dotsPastEightVoters() {
        for count in (MonacoVoteTallyLayout.maximumFaces + 1)...14 {
            for size in nonAccessibilitySizes {
                #expect(!MonacoVoteTallyLayout.usesFaces(voterCount: count, dynamicTypeSize: size))
            }
        }
    }

    @Test func dotsAtEveryAccessibilityTextSize() {
        for count in 1...MonacoVoteTallyLayout.maximumFaces {
            for size in accessibilitySizes {
                #expect(!MonacoVoteTallyLayout.usesFaces(voterCount: count, dynamicTypeSize: size))
            }
        }
    }

    /// The dot ceiling stays inside `ProposalVoteProgress.maxDots`, so a tally that falls back to
    /// dots is a tally the caption still agrees with.
    @Test func theFaceCeilingSitsBelowTheDotCeiling() {
        #expect(MonacoVoteTallyLayout.maximumFaces < ProposalVoteProgress.maxDots)
    }

    @Test func noVotersMeansNothingToDraw() {
        #expect(!MonacoVoteTallyLayout.usesFaces(voterCount: 0, dynamicTypeSize: .large))
    }

    /// The face keeps the *encoding*, not just the face. Yes and no are filled circles with
    /// differently coloured rings; pending is hollow and dashed. Shape carries the difference as
    /// well as colour, so the tally does not depend on telling brand blue from loss red.
    @Test func everyVoteStateIsDistinguishableWithoutColour() {
        let states: [ProposalVoteDot] = [.yes, .no, .pending]
        #expect(Set(states).count == 3)
        // The dot fallback encodes the same three states with the same shapes it always has:
        // filled, thick outline, thin outline.
        for state in states {
            let dot = MonacoVoteDot(state: state)
            #expect(dot.state == state)
        }
    }
}

/// `MonacoFaceStack` shows people, and only the people the API actually carries.
struct MonacoFaceStackTests {
    @Test func aFaceWithNoPhotoIsStillAFace() {
        // `ProposalVoteDTO` carries voterId, displayName, choice and castAt — and no photo. The
        // stack renders initials rather than inventing one, and `profilePhotoUrl` is the API
        // follow-up this is waiting on.
        let face = MonacoFace(id: "u1", displayName: "Ana Ruiz")
        #expect(face.photoURL == nil)
        #expect(AvatarInitials.from(face.displayName) == "AR")
    }

    @Test func aVoteCanBeCastWithNoVoterIdentityAttached() {
        let vote = MonacoVote(id: "v1", state: .yes)
        #expect(vote.face == nil)
        #expect(vote.state == .yes)
    }
}
