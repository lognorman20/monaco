import SwiftUI

/// Routes activity row to transaction or proposal detail when ids overlap.
struct ActivityDetailDestination: View {
    @ObservedObject var auth: DynamicAuthService
    let activityItem: GroupActivityItemDTO
    let onRetry: ((GroupActivityItemDTO) async -> RetryTransactionResponse?)?
    let isRetrying: Bool

    @State private var useProposalDetail = false

    private let apiClient = MonacoAPIClient()

    var body: some View {
        Group {
            if useProposalDetail {
                ProposalDetailView(auth: auth, proposalId: activityItem.id)
            } else {
                TransactionDetailView(
                    auth: auth,
                    activityItem: activityItem,
                    onRetry: onRetry,
                    isRetrying: isRetrying
                )
            }
        }
        .task(id: resolveTaskID) {
            await resolveDestination()
        }
    }

    private var resolveTaskID: String {
        "\(activityItem.id)-\(activityItem.kind)-\(auth.accessToken ?? "")"
    }

    private func resolveDestination() async {
        guard shouldResolveProposalFallback else { return }
        guard let token = auth.accessToken else { return }

        do {
            _ = try await apiClient.getTransactionDetail(accessToken: token, transactionId: activityItem.id)
            useProposalDetail = false
        } catch MonacoAPIError.httpStatus(404) {
            useProposalDetail = true
        } catch {
            useProposalDetail = false
        }
    }

    private var shouldResolveProposalFallback: Bool {
        GroupActivityRules.needsProposalFallback(activityItem)
    }
}
