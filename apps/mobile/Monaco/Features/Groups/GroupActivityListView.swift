import SwiftUI

/// Every activity row for one cabal, pushed from the group screen's "See all".
struct GroupActivityListView: View {
    @ObservedObject var auth: PrivyAuthService
    let items: [GroupActivityItemDTO]
    let retryingTransactionIDs: Set<String>
    let onRetry: (GroupActivityItemDTO) -> Void

    var body: some View {
        ScrollView {
            GroupActivityList(
                auth: auth,
                items: items,
                retryingTransactionIDs: retryingTransactionIDs,
                onRetry: onRetry,
                isLazy: true
            )
            .padding(.horizontal, 20)
            .padding(.vertical, 16)
        }
        .monacoCanvas()
        .navigationTitle("Activity")
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("group-activity-list")
    }
}
