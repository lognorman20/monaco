import SwiftUI
import Testing
@testable import Monaco

struct ProposeSheetDetentsTests {
    @Test func chooserStartsAtMediumAndCanBePulledToLarge() {
        let detents = ProposeSheetDetents()
        #expect(detents.selection == .medium)
        #expect(detents.allowed == [.medium, .large])
    }

    @Test func aPushedFlowPinsTheSheetAtLarge() {
        var detents = ProposeSheetDetents()
        detents.flowStarted()
        #expect(detents.selection == .large)
        #expect(detents.allowed == [.large])
    }

    @Test func returningToTheChooserReleasesTheSheet() {
        var detents = ProposeSheetDetents()
        detents.flowStarted()
        detents.returnedToChooser()
        #expect(detents.selection == .medium)
        #expect(detents.allowed == [.medium, .large])
    }

    @Test func selectionIsAlwaysAnAllowedDetent() {
        var detents = ProposeSheetDetents()
        #expect(detents.allowed.contains(detents.selection))
        detents.selection = .large
        detents.flowStarted()
        #expect(detents.allowed.contains(detents.selection))
        detents.returnedToChooser()
        #expect(detents.allowed.contains(detents.selection))
    }
}
