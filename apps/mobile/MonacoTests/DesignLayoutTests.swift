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
        #expect(layout.minimumTitleWidth == 96)
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
    ])
    func tileTextKeepsTheWholeTicker(ticker: String, expected: String) {
        #expect(StockMark.tileText(forTicker: ticker) == expected)
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
