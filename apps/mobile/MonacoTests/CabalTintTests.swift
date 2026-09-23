import SwiftUI
import Testing
import UIKit
@testable import Monaco

/// The seven-tint ramp: what each pair has to clear, and where the one strict bar is.
struct CabalTintRampTests {
    /// `fill` carries bold tile initials at 15pt and up — large text under WCAG, so 3:1.
    @Test func whiteOnFillClearsTheLargeTextBar() {
        for tint in MonacoTheme.CabalTint.allCases {
            for scheme in [UIUserInterfaceStyle.light, .dark] {
                let ratio = WCAGContrast.ratio(tint.onFill, on: [tint.fill], scheme)
                #expect(ratio >= 3, "\(tint.name) fill in \(scheme == .light ? "light" : "dark") is \(ratio)")
            }
        }
    }

    /// `cta` is the reason the ramp is nine values and not seven, and this is the assertion that
    /// earns it. `fill` clears only 3:1, which is wrong for the two places a tint carries *small*
    /// white text: the rank badge on the Cabals leaderboard (a 13pt bold numeral, not "large text"
    /// under WCAG) and your own chat bubbles. Both take `cta`, and the closest pair — peach in
    /// dark — lands at exactly 4.50, so this bar is load-bearing rather than decorative.
    @Test func whiteOnCtaClearsTheBodyBar() {
        for tint in MonacoTheme.CabalTint.allCases {
            for scheme in [UIUserInterfaceStyle.light, .dark] {
                let ratio = WCAGContrast.ratio(tint.onFill, on: [tint.cta], scheme)
                #expect(
                    ratio >= 4.5,
                    "\(tint.name) cta in \(scheme == .light ? "light" : "dark") is \(ratio), below 4.5"
                )
            }
        }
    }

    /// `cta` has to be *deeper* than `fill`, or it is not doing anything and a later retune will
    /// quietly collapse the two back into one token.
    @Test func ctaIsDeeperThanFill() {
        for tint in MonacoTheme.CabalTint.allCases {
            for scheme in [UIUserInterfaceStyle.light, .dark] {
                let fill = WCAGContrast.luminance(WCAGContrast.resolve(tint.fill, scheme))
                let cta = WCAGContrast.luminance(WCAGContrast.resolve(tint.cta, scheme))
                #expect(cta < fill, "\(tint.name) cta is not deeper than its fill in \(scheme.rawValue)")
            }
        }
    }

    /// `soft` still carries `fgPrimary` body text on both canvas and card.
    @Test func inkTextOnSoftClearsAA() {
        for tint in MonacoTheme.CabalTint.allCases {
            for surface in [MonacoTheme.bgBase, MonacoTheme.bgRaised] {
                for scheme in [UIUserInterfaceStyle.light, .dark] {
                    let ratio = WCAGContrast.ratio(MonacoTheme.fgPrimary, on: [surface, tint.soft], scheme)
                    #expect(ratio >= 4.5, "\(tint.name) soft is \(ratio)")
                }
            }
        }
    }

    /// `onInk` is what makes a tint readable on an ink band — the paper `fill` does not.
    @Test func onInkClearsTheGraphicalBarOnEveryInkSurface() {
        let surfaces = [MonacoTheme.Ink.base, MonacoTheme.Ink.raised, MonacoTheme.Ink.sunken]
        for tint in MonacoTheme.CabalTint.allCases {
            for surface in surfaces {
                let ratio = WCAGContrast.ratio(tint.onInk, on: [surface], .dark)
                #expect(ratio >= 3, "\(tint.name) onInk is \(ratio)")
            }
        }
    }

    /// No purple (250–335°) and no brand blue (210–250°). Without the bands, "seven hue-spaced
    /// tints" is a claim rather than a constraint — and a tint that drifts into the brand band
    /// breaks blue-means-tap on a strip card, which is the rule with the most riding on it.
    ///
    /// Green is checked separately, as a distance from `profit` rather than a band, in
    /// `DesignTokenOrderTests.nothingOutsideTheMoneyRampIsGreen`.
    @Test func everyTintStaysOutOfTheReservedHueBands() {
        for tint in MonacoTheme.CabalTint.allCases {
            for scheme in [UIUserInterfaceStyle.light, .dark] {
                for (role, colour) in [("fill", tint.fill), ("cta", tint.cta), ("onInk", tint.onInk)] {
                    guard let hue = DesignTokenOrderTests.hue(WCAGContrast.resolve(colour, scheme)) else {
                        Issue.record("\(tint.name) \(role) has no measurable hue")
                        continue
                    }
                    #expect(!(hue >= 250 && hue <= 335), "\(tint.name) \(role) is purple at \(hue)°")
                    #expect(
                        !(hue >= 210 && hue < 250),
                        "\(tint.name) \(role) is in the brand band at \(hue)°"
                    )
                }
            }
        }
    }

    /// Consecutive entries are the resolver's clockwise neighbours, so they are the pair a member
    /// is most likely to be handed together. They are ordered to be maximally hue-distant, and the
    /// minimum adjacent gap is 49°.
    @Test func adjacentTintsInTheWalkOrderAreFarApartInHue() {
        let all = MonacoTheme.CabalTint.allCases
        for index in all.indices {
            let current = all[index]
            let next = all[(index + 1) % all.count]
            guard
                let a = DesignTokenOrderTests.hue(WCAGContrast.resolve(current.fill, .light)),
                let b = DesignTokenOrderTests.hue(WCAGContrast.resolve(next.fill, .light))
            else {
                Issue.record("a tint in the walk order has no measurable hue")
                continue
            }
            let raw = abs(a - b)
            let gap = min(raw, 360 - raw)
            #expect(gap >= 49, "\(current.name) → \(next.name) is only \(gap)° apart in the walk order")
        }
    }
}

/// The resolver, which is the part that actually fixes cabal identity.
///
/// Hashing alone left a member of four cabals with roughly a 70% chance that two shared a colour,
/// and on the strip and the multi-line chart the tint was the only identity signal there was.
/// Growing five buckets to seven narrows that; it does not close it.
struct CabalTintResolverTests {
    /// Group ids in the shape the API actually sends: lowercase UUIDs.
    ///
    /// This matters more than it looks. The obvious test input — "group-0", "group-1", … — is a
    /// *pathological* shape for FNV-1a: four ids differing only in their last byte land on four
    /// different buckets essentially every time, so a suite built on them passes whether or not
    /// the resolver does anything at all. Measured over 300 draws, sequential ids collide 0 times
    /// at four cabals and UUIDs collide 193 times. The generator is a seeded LCG so the ids are
    /// random-looking and the test is still reproducible.
    private func ids(_ count: Int, seed: UInt64) -> [String] {
        var state = seed &* 6_364_136_223_846_793_005 &+ 1_442_695_040_888_963_407
        func nextHex(_ digits: Int) -> String {
            var out = ""
            for _ in 0..<digits {
                state = state &* 6_364_136_223_846_793_005 &+ 1_442_695_040_888_963_407
                out += String((state >> 33) & 0xF, radix: 16)
            }
            return out
        }
        return (0..<count).map { _ in
            "\(nextHex(8))-\(nextHex(4))-\(nextHex(4))-\(nextHex(4))-\(nextHex(12))"
        }
    }

    @Test func noTwoOfYourCabalsCollideUpToSeven() {
        for size in 1...MonacoTheme.CabalTint.allCases.count {
            for seed in 0..<40 {
                let groupIds = ids(size, seed: UInt64(seed))
                let resolved = CabalTintAssignment.resolve(orderedGroupIds: groupIds)
                let tints = groupIds.map { CabalTintAssignment.tint(forGroupId: $0, in: resolved) }
                #expect(
                    Set(tints).count == size,
                    "seed \(seed) with \(size) cabals collided: \(tints.map(\.name))"
                )
            }
        }
    }

    /// The hash alone does not have that property, which is the whole reason the resolver exists.
    /// Stated as a test so a future "simplify this back to `forGroupId`" has to argue with a
    /// measurement rather than with a comment.
    @Test func thePlainHashDoesCollideAtFourCabals() {
        var collidingSeeds = 0
        for seed in 0..<200 {
            let groupIds = ids(4, seed: UInt64(seed))
            let tints = groupIds.map { MonacoTheme.CabalTint.forGroupId($0) }
            if Set(tints).count < groupIds.count { collidingSeeds += 1 }
        }
        #expect(
            collidingSeeds > 50,
            "the hash collided on only \(collidingSeeds) of 200 draws: the resolver's premise changed"
        )
    }

    /// The eighth cabal is arithmetic, not a bug: there are seven tints. It still has to get a
    /// stable colour rather than nothing.
    @Test func anEighthCabalStillGetsAStableTint() {
        let groupIds = ids(9, seed: 1)
        let first = CabalTintAssignment.resolve(orderedGroupIds: groupIds)
        for _ in 0..<10 {
            let again = CabalTintAssignment.resolve(orderedGroupIds: groupIds)
            #expect(again == first)
        }
        for groupId in groupIds {
            #expect(first[MonacoTheme.CabalTint.normalisedId(groupId)] != nil)
        }
    }

    /// The API's ordering is not stable — a list can come back sorted by activity today and by
    /// name tomorrow — so the walk sorts the ids itself. A cabal that changed colour when the feed
    /// re-sorted would be worse than the collision this replaces.
    @Test func theAssignmentDoesNotDependOnTheOrderTheIdsArriveIn() {
        let groupIds = ids(6, seed: 2)
        let expected = CabalTintAssignment.resolve(orderedGroupIds: groupIds)
        #expect(CabalTintAssignment.resolve(orderedGroupIds: groupIds.reversed()) == expected)
        #expect(CabalTintAssignment.resolve(orderedGroupIds: groupIds.shuffled()) == expected)
    }

    /// Swift's `UUID.uuidString` is uppercase and the API sends lowercase, so the same cabal
    /// arrives spelled two ways. It is one cabal and it is one colour.
    @Test func caseAndWhitespaceDoNotChangeTheAssignment() {
        let id = "3F5B2C9E-8D1A-4E7F-9B6C-2A1D0E4F7C88"
        let resolved = CabalTintAssignment.resolve(orderedGroupIds: [id, "other-cabal"])
        let viaOriginal = CabalTintAssignment.tint(forGroupId: id, in: resolved)
        let viaLowercase = CabalTintAssignment.tint(forGroupId: id.lowercased(), in: resolved)
        let viaPadded = CabalTintAssignment.tint(forGroupId: " \(id)\n", in: resolved)
        #expect(viaOriginal == viaLowercase)
        #expect(viaOriginal == viaPadded)
    }

    /// A duplicate in the list is one cabal, and must not consume two tints.
    @Test func aRepeatedIdConsumesOneTint() {
        let resolved = CabalTintAssignment.resolve(
            orderedGroupIds: ["alpha", "alpha", "ALPHA", "beta"]
        )
        let alpha = CabalTintAssignment.tint(forGroupId: "alpha", in: resolved)
        let beta = CabalTintAssignment.tint(forGroupId: "beta", in: resolved)
        #expect(alpha != beta)
        #expect(CabalTintAssignment.tint(forGroupId: "ALPHA", in: resolved) == alpha)
    }

    /// A cabal outside the viewer's set — a public cabal being previewed, a leaderboard row —
    /// still needs a colour, and it needs the same one everywhere.
    @Test func aCabalOutsideTheSetFallsBackToItsHashedTint() {
        let resolved = CabalTintAssignment.resolve(orderedGroupIds: ids(3, seed: 3))
        let stranger = "someone-elses-cabal"
        #expect(
            CabalTintAssignment.tint(forGroupId: stranger, in: resolved)
                == MonacoTheme.CabalTint.forGroupId(stranger)
        )
    }

    /// Most cabals keep the colour they hashed to; the walk only moves the ones that had to move.
    /// A resolver that reassigned everything would make the tint a function of your membership
    /// list rather than of the cabal.
    @Test func theWalkMovesAsFewCabalsAsItCan() {
        var kept = 0
        var total = 0
        for seed in 0..<100 {
            let groupIds = ids(5, seed: UInt64(seed))
            let resolved = CabalTintAssignment.resolve(orderedGroupIds: groupIds)
            for groupId in groupIds {
                total += 1
                if CabalTintAssignment.tint(forGroupId: groupId, in: resolved)
                    == MonacoTheme.CabalTint.forGroupId(groupId) {
                    kept += 1
                }
            }
        }
        #expect(Double(kept) / Double(total) > 0.6, "the walk is reassigning more than it resolves")
    }

    @Test func anEmptyListResolvesToAnEmptyMap() {
        #expect(CabalTintAssignment.resolve(orderedGroupIds: []).isEmpty)
    }
}
