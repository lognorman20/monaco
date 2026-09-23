import XCTest
@testable import Monaco

/// The Profile tab glyph is drawn from the member's own name, so the one thing that can go wrong
/// silently is picking the wrong character out of it — a box glyph, a space, or a punctuation
/// mark rendered at 26pt in the tab bar with no way for anyone to notice until a screenshot.
final class TabShellGlyphTests: XCTestCase {
    func testTakesTheFirstLetterUppercased() {
        XCTAssertEqual(MonacoTabGlyph.initial(from: "ana"), "A")
        XCTAssertEqual(MonacoTabGlyph.initial(from: "Ana Ruiz"), "A")
        XCTAssertEqual(MonacoTabGlyph.initial(from: "  dev  "), "D")
    }

    func testSkipsLeadingPunctuationAndEmoji() {
        XCTAssertEqual(MonacoTabGlyph.initial(from: "🚀 rocket"), "R")
        XCTAssertEqual(MonacoTabGlyph.initial(from: "@maya"), "M")
        XCTAssertEqual(MonacoTabGlyph.initial(from: "···· Leo"), "L")
    }

    func testTakesADigitWhenThatIsWhatTheNameStartsWith() {
        XCTAssertEqual(MonacoTabGlyph.initial(from: "2cool"), "2")
    }

    /// No initial means the fallback symbol, not a blank tab and not an invented letter.
    func testNoInitialForNamesWithNoLetterOrDigit() {
        XCTAssertNil(MonacoTabGlyph.initial(from: nil))
        XCTAssertNil(MonacoTabGlyph.initial(from: ""))
        XCTAssertNil(MonacoTabGlyph.initial(from: "   "))
        XCTAssertNil(MonacoTabGlyph.initial(from: "🚀🚀🚀"))
        XCTAssertNil(MonacoTabGlyph.initial(from: "···"))
    }

    /// Both glyphs are template images: a `UITabBarItem` tints its icon, so anything carrying its
    /// own colour would flatten to a silhouette. This is the assertion that has to be deleted to
    /// change that.
    func testBothGlyphsAreTemplateImages() {
        XCTAssertEqual(MonacoTabGlyph.mark.renderingMode, .alwaysTemplate)
        XCTAssertEqual(MonacoTabGlyph.monogram(for: "Ana").renderingMode, .alwaysTemplate)
        XCTAssertEqual(MonacoTabGlyph.monogram(for: nil).renderingMode, .alwaysTemplate)
    }

    func testGlyphsAreDrawnAtTheTabBarSize() {
        XCTAssertEqual(MonacoTabGlyph.mark.size.width, MonacoTabGlyph.side, accuracy: 0.5)
        XCTAssertEqual(MonacoTabGlyph.mark.size.height, MonacoTabGlyph.side, accuracy: 0.5)
        XCTAssertEqual(MonacoTabGlyph.monogram(for: "Ana").size.width, MonacoTabGlyph.side, accuracy: 0.5)
    }

    /// The cache is keyed on the initial, so two members whose names start differently must not
    /// share a glyph — and two whose names start the same must.
    ///
    /// Asserted on the *key*, not on object identity. `NSCache` may evict at any moment, and on a
    /// shared build machine under memory pressure it will: `===` on two cache reads is a test that
    /// fails for a reason that has nothing to do with the code it names.
    func testMonogramCacheIsKeyedOnTheInitial() {
        XCTAssertEqual(MonacoTabGlyph.initial(from: "Ana"), MonacoTabGlyph.initial(from: "Alex"))
        XCTAssertNotEqual(MonacoTabGlyph.initial(from: "Ana"), MonacoTabGlyph.initial(from: "dev"))
    }

    /// Two names with the same initial draw the same glyph, cached or freshly rendered — which is
    /// what the key is for. Compared on pixels, so an eviction between the two reads changes
    /// nothing.
    func testTheSameInitialDrawsTheSameGlyph() {
        let ana = MonacoTabGlyph.monogram(for: "Ana")
        let alex = MonacoTabGlyph.monogram(for: "Alex")
        let dev = MonacoTabGlyph.monogram(for: "dev")
        XCTAssertEqual(ana.pngData(), alex.pngData(), "Two names with the same initial draw one glyph")
        XCTAssertNotEqual(ana.pngData(), dev.pngData(), "Different initials must not draw the same glyph")
    }
}
