import SwiftUI
import Testing
@testable import Monaco

struct MonacoRowLayoutTests {
    @Test func staysInlineAtEveryNonAccessibilitySize() {
        let sizes: [DynamicTypeSize] = [.xSmall, .small, .medium, .large, .xLarge, .xxLarge, .xxxLarge]
        for size in sizes {
            let layout = MonacoRowLayout(dynamicTypeSize: size)
            #expect(layout.isStacked == false)
            #expect(layout.titleLineLimit == 1)
            #expect(layout.separatorLeadingInset == 72)
        }
    }

    @Test func stacksAtAccessibilitySizes() {
        let sizes: [DynamicTypeSize] = [
            .accessibility1, .accessibility2, .accessibility3, .accessibility4, .accessibility5,
        ]
        for size in sizes {
            let layout = MonacoRowLayout(dynamicTypeSize: size)
            #expect(layout.isStacked)
            // The title gets two lines instead of being squeezed to an ellipsis by the figures.
            #expect(layout.titleLineLimit == 2)
            #expect(layout.minimumTitleWidth == nil)
        }
    }

    @Test func inlineRowsKeepAFloorForTheTitle() {
        // The trailing column used to be fixedSize, so a wide figure took the whole row.
        let layout = MonacoRowLayout(dynamicTypeSize: .large)
        #expect(layout.minimumTitleWidth == MonacoRowLayout.baseMinimumTitleWidth)
        #expect(layout.minimumTitleWidth == 96)
    }

    /// The floor is a width for *text*, and the text scales while the row is still inline, so a
    /// fixed 96pt shrank to about five characters at xxxLarge and the title went on truncating.
    @Test func theTitleFloorScalesWithTheTitle() {
        // xxxLarge is the largest size that still lays out inline.
        let scaled = MonacoRowLayout(dynamicTypeSize: .xxxLarge, scaledTitleWidthFloor: 130)
        #expect(scaled.minimumTitleWidth == 130)
        #expect(scaled.isStacked == false)
    }

    /// A stacked row has a full-width title, so a floor would only fight the layout.
    @Test func aStackedRowHasNoTitleFloorEvenWhenOneIsPassedIn() {
        let stacked = MonacoRowLayout(dynamicTypeSize: .accessibility3, scaledTitleWidthFloor: 130)
        #expect(stacked.minimumTitleWidth == nil)
    }
}

struct CircleActionMetricsTests {
    /// `GroupActionRow` gives each of its four actions about 97pt on a 390pt screen. Unclamped,
    /// a 56pt disc reaches ~99pt at AX1 and ~189pt at AX5, so Add money / Propose / Cash out /
    /// Chat drew over each other from AX1 upwards.
    @Test func theDiscStopsBeforeItOutgrowsItsColumn() {
        let narrowestColumn: CGFloat = 97
        #expect(CircleActionMetrics.maximumDiscSize < narrowestColumn)

        // The scaled values @ScaledMetric produces at AX1 and AX5 for a 56pt base on .footnote.
        for scaled in [CGFloat(99), 189] {
            #expect(CircleActionMetrics.discSize(scaled: scaled) < narrowestColumn)
            #expect(CircleActionMetrics.discSize(scaled: scaled) == CircleActionMetrics.maximumDiscSize)
        }
    }

    @Test func theGlyphStopsWithTheDisc() {
        #expect(CircleActionMetrics.glyphSize(scaled: 68) == CircleActionMetrics.maximumGlyphSize)
        // A glyph that outgrew its disc would spill over the circle's edge.
        #expect(CircleActionMetrics.maximumGlyphSize < CircleActionMetrics.maximumDiscSize)
    }

    /// The point of the PR was that these used to be frozen at their design size while every
    /// label around them grew. Clamping must not put them back there.
    @Test func bothStillScaleUpToTheirCeiling() {
        #expect(CircleActionMetrics.discSize(scaled: 56) == 56)
        #expect(CircleActionMetrics.discSize(scaled: 70) == 70)
        #expect(CircleActionMetrics.glyphSize(scaled: 20) == 20)
        #expect(CircleActionMetrics.glyphSize(scaled: 26) == 26)
    }
}

struct StockMarkTests {
    @Test(arguments: [
        ("AAPL", "AAPL"),
        ("aapl", "AAPL"),
        ("AMZN", "AMZN"),
        ("AVGO", "AVGO"),
        ("ABBV", "ABBV"),
        ("V", "V"),
        ("GOOGL", "GOOG"),
        (" nvda ", "NVDA"),
        ("", ""),
        // A class suffix cut at four characters left the separator hanging: "BRK." read as an
        // abbreviation of itself rather than as Berkshire.
        ("BRK.B", "BRK"),
        ("BRK.A", "BRK"),
        ("brk.b", "BRK"),
        ("RDS-A", "RDS"),
    ])
    func tileTextKeepsTheWholeTicker(ticker: String, expected: String) {
        #expect(StockMark.tileText(forTicker: ticker) == expected)
    }

    /// `StockMark(symbol:)` is handed the wire symbol by most rows and the display ticker by a
    /// few. Either way the tile shows the ticker: the Base token suffix never reaches it.
    @Test(arguments: [
        ("AAPLc", "AAPL"),
        ("AAPL", "AAPL"),
        ("GOOGLc", "GOOG"),
        ("Fc", "F"),
        ("GEc", "GE"),
        ("BRK.Bc", "BRK"),
        ("BF.Bc", "BF.B"),
        (" NVDAc ", "NVDA"),
        // Legacy suffix still reads the same way.
        ("Fx", "F"),
    ])
    func theTileShowsTheTickerForAWireSymbol(symbol: String, expected: String) {
        #expect(StockMark.content(forSymbol: symbol) == .letter(expected))
    }

    /// Only the lowercase suffix is dropped; a ticker that really ends in "C" keeps it.
    @Test func aTickerEndingInACapitalCKeepsIt() {
        #expect(StockMark.content(forSymbol: "INTC") == .letter("INTC"))
        #expect(StockMark.content(forSymbol: "ABC") == .letter("ABC"))
    }

    /// Cash is a dollar sign, not a "USDC" tile, however the symbol arrives.
    @Test(arguments: ["USDC", "usdc", " USDC "])
    func cashShowsADollarSign(symbol: String) {
        #expect(StockMark.content(forSymbol: symbol) == .symbol("dollarsign"))
    }

    /// A separator inside the first four characters is content, not a dangling edge.
    @Test func aSeparatorThatIsNotAtTheEndIsKept() {
        #expect(StockMark.tileText(forTicker: "BF.B") == "BF.B")
    }

    @Test func tickersStartingWithTheSameLetterGetDifferentTiles() {
        // Nine catalog tickers start with "A"; they used to render as nine identical grey tiles.
        let tickers = ["AAPL", "ABBV", "ABT", "ACN", "AMBR", "AMZN", "APP", "AVGO", "AZN"]
        let tiles = Set(tickers.map { StockMark.tileText(forTicker: $0) })
        #expect(tiles.count == tickers.count)
    }

    @Test func longerTickersAreSetSmaller() {
        #expect(StockMark.textScale(for: "A") > StockMark.textScale(for: "AA"))
        #expect(StockMark.textScale(for: "AAP") > StockMark.textScale(for: "AAPL"))
    }
}

/// `MoneyFont` builds its `@ScaledMetric` bases from `MoneyStyle.baseSize`, so these assertions
/// are over the numbers that actually render. They used to be over a parallel copy: the sizes were
/// restated as literals in `MoneyFont` and nothing tied the two together.
struct MoneyStyleScalingTests {
    @Test func everyStyleHasTheDesignSizeItUsedToPreScale() {
        #expect(MoneyStyle.hero.baseSize == 44)
        #expect(MoneyStyle.large.baseSize == 28)
        #expect(MoneyStyle.row.baseSize == 17)
        #expect(MoneyStyle.caption.baseSize == 13)
    }

    @Test func stylesScaleAgainstTheSameTextStylesAsBefore() {
        #expect(MoneyStyle.hero.textStyle == .largeTitle)
        #expect(MoneyStyle.large.textStyle == .title)
        #expect(MoneyStyle.row.textStyle == .body)
        #expect(MoneyStyle.caption.textStyle == .footnote)
    }

    @Test func weightsMatchTheOldFonts() {
        #expect(MoneyStyle.hero.weight == .semibold)
        #expect(MoneyStyle.large.weight == .semibold)
        #expect(MoneyStyle.row.weight == .semibold)
        #expect(MoneyStyle.caption.weight == .medium)
    }
}
