#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: the group screen and its pushed screens on canned data, no sign-in or backend.
/// Launch with `-MonacoGroupDetailSample <scenario>`:
/// `populated` · `empty` · `loading` · `details` (Cabal details sheet open) · `propose` (chooser sheet open)
/// · `cashOut` · `receipt` (bought) · `receiptFailed` (failed sell) · `activity` (full list).
enum GroupDetailSampleScenario: String, CaseIterable {
    case populated
    case empty
    case loading
    case details
    case propose
    case cashOut
    case receipt
    case receiptFailed
    case activity

    static let launchArgument = "-MonacoGroupDetailSample"

    static var requested: GroupDetailSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: launchArgument), arguments.indices.contains(flag + 1) else { return nil }
        return GroupDetailSampleScenario(rawValue: arguments[flag + 1])
    }
}

struct GroupDetailSampleHarness: View {
    let scenario: GroupDetailSampleScenario
    @ObservedObject var auth: PrivyAuthService

    @State private var session: AppSessionStore = {
        let session = AppSessionStore()
        session.isLoading = false
        session.me = MeResponse(
            userId: GroupDetailSampleData.viewerId,
            displayName: "Logan Norman",
            memberWalletAddress: "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
            profilePhotoUrl: nil,
            createdAt: nil
        )
        return session
    }()
    @State private var proposalService = SampleProposalFeedService()
    @State private var showDetails = false
    @State private var showPropose = false
    @State private var route: GroupDetailRoute?
    @State private var toast: MonacoToast?
    @State private var heroScrolledAway = false

    var body: some View {
        NavigationStack {
            root
        }
        .tint(MonacoTheme.ink)
        .environment(session)
    }

    @ViewBuilder
    private var root: some View {
        switch scenario {
        case .cashOut:
            SellCabalView(auth: auth, groupId: "g1", maxShareUnits: 311_500_000, equityUsd: "311.50")
        case .receipt:
            TransactionReceiptView(receipt: TransactionReceipt(transaction: GroupDetailSampleData.boughtApple))
                .monacoCanvas()
                .navigationBarTitleDisplayMode(.inline)
        case .receiptFailed:
            TransactionReceiptView(
                receipt: TransactionReceipt(transaction: GroupDetailSampleData.failedSell),
                onRetry: {}
            )
            .monacoCanvas()
            .navigationBarTitleDisplayMode(.inline)
        case .activity:
            GroupActivityListView(auth: auth, items: GroupDetailSampleData.activity, retryingTransactionIDs: [], onRetry: { _ in })
        case .loading:
            GroupDetailSkeleton()
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
                .monacoCanvas()
                .navigationTitle("Weekend investors")
                .navigationBarTitleDisplayMode(.inline)
        case .populated, .empty, .details, .propose:
            groupScreen(scenario == .empty ? GroupDetailSampleData.emptyView : GroupDetailSampleData.view)
        }
    }

    /// Same content and chrome as `GroupDetailView`, fed from sample data.
    private func groupScreen(_ view: GroupViewDTO) -> some View {
        GroupDetailContent(
            auth: auth,
            view: view,
            currentUserId: GroupDetailSampleData.viewerId,
            proposalService: proposalService,
            proposalRefreshToken: "0",
            activityItems: scenario == .empty ? [] : GroupDetailSampleData.activity,
            activityLoading: false,
            activityError: nil,
            retryingTransactionIDs: [],
            joinRequests: scenario == .empty ? [] : GroupDetailSampleData.joinRequests,
            decidingRequestIDs: [],
            onRoute: { route = $0 },
            onPropose: { showPropose = true },
            onRetry: { _ in },
            onDecideJoinRequest: { _, _ in },
            onToast: { toast = $0 },
            onHeroScrolledAway: { heroScrolledAway = $0 }
        )
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .monacoCanvas()
        .navigationTitle(heroScrolledAway ? view.name : "")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { showDetails = true } label: { Image(systemName: "info.circle") }
                    .accessibilityLabel("Cabal details")
            }
        }
        .navigationDestination(item: $route) { route in
            switch route {
            case .cashOut:
                SellCabalView(auth: auth, groupId: view.id, maxShareUnits: Int64(view.you.shareUnits) ?? 0, equityUsd: view.you.equityUsd)
            case .activity:
                GroupActivityListView(auth: auth, items: GroupDetailSampleData.activity, retryingTransactionIDs: [], onRetry: { _ in })
            case .proposals:
                ProposalFeedView(service: proposalService, groupId: view.id)
            case .addMoney, .chat:
                Text("Not in the sample harness")
                    .foregroundStyle(MonacoTheme.muted)
            }
        }
        .sheet(isPresented: $showPropose) {
            NavigationStack {
                ProposeChooserView(auth: auth, groupId: view.id, groupView: view)
            }
            .presentationDetents([.medium, .large])
        }
        .sheet(isPresented: $showDetails) {
            GroupDetailsSheet(groupId: view.id, treasuryAddress: view.treasuryAddress, isLeaving: false, onLeave: {})
        }
        .monacoToast($toast)
        .task {
            if scenario == .details { showDetails = true }
            if scenario == .propose { showPropose = true }
        }
    }
}

enum GroupDetailSampleData {
    static let viewerId = "u2"

    static let view = GroupViewDTO(
        id: "5b1f0c9e-0001-4c55-9a51-000000000001",
        name: "Weekend investors",
        treasuryAddress: "9fQeWbKc3pZ1rJtM7xVn2aLhYd8sGu4oTk6ReXbN1cPq",
        potTotalUsd: "548.20",
        pot: [
            PotRowDTO(symbol: "USDC", units: "82.62", markUsd: "1.00", valueUsd: "82.62", dollarPnl: "+0.00", afterHours: nil, tokenAmount: nil),
            PotRowDTO(symbol: "AAPLx", units: "1.2034", markUsd: "231.40", valueUsd: "278.47", dollarPnl: "+28.47", afterHours: false, tokenAmount: "120340000"),
            PotRowDTO(symbol: "NVDAx", units: "1.05", markUsd: "178.20", valueUsd: "187.11", dollarPnl: "-7.60", afterHours: true, tokenAmount: "105000000"),
        ],
        you: MemberSliceDTO(shareUnits: "311500000", equityUsd: "311.50", slicePercent: "0.568", dollarPnl: "+27.40", percentReturn: "0.096"),
        members: [
            LeaderboardRowDTO(rank: 1, userId: "u1", displayName: "Ana Ruiz", percentReturn: "0.142", dollarPnl: "+14.20"),
            LeaderboardRowDTO(rank: 2, userId: "u2", displayName: "Logan Norman", percentReturn: "0.096", dollarPnl: "+27.40"),
            LeaderboardRowDTO(rank: 3, userId: "u3", displayName: "Leo Park", percentReturn: "0.012", dollarPnl: "+1.10"),
            LeaderboardRowDTO(rank: 4, userId: "u4", displayName: "Mia Chen", percentReturn: "-0.021", dollarPnl: "-2.10"),
            LeaderboardRowDTO(rank: 5, userId: "u5", displayName: "Sam Okafor", percentReturn: nil, dollarPnl: "+0.00"),
        ],
        proposals: nil,
        agent: GroupAgentDTO(id: "a1", status: "active", agentDisplayName: "Scout", allocationUsdcMicros: "100000000")
    )

    static let emptyView = GroupViewDTO(
        id: "5b1f0c9e-0009-4c55-9a51-000000000009",
        name: "Rent money",
        treasuryAddress: "9fQeWbKc3pZ1rJtM7xVn2aLhYd8sGu4oTk6ReXbN1cPq",
        potTotalUsd: "0.00",
        pot: [PotRowDTO(symbol: "USDC", units: "0.00", markUsd: "1.00", valueUsd: "0.00", dollarPnl: "+0.00", afterHours: nil, tokenAmount: nil)],
        you: MemberSliceDTO(shareUnits: "0", equityUsd: "0.00", slicePercent: "0", dollarPnl: "+0.00", percentReturn: nil),
        members: [LeaderboardRowDTO(rank: 1, userId: "u2", displayName: "Logan Norman", percentReturn: nil, dollarPnl: "+0.00")],
        proposals: nil,
        agent: nil
    )

    static let joinRequests = [
        JoinRequestDTO(id: "jr1", userId: "u9", displayName: "Priya Nair", requestedAt: "2026-09-18T10:00:00Z"),
    ]

    static var activity: [GroupActivityItemDTO] {
        let iso = ISO8601DateFormatter()
        func ago(_ minutes: Double) -> String { iso.string(from: Date().addingTimeInterval(-minutes * 60)) }
        return [
            GroupActivityItemDTO(id: "t1", kind: "buy", status: "pending", symbol: "AAPLx", amountMicros: 50_000_000, createdAt: ago(3), txSignature: "5h1Xk", tokenAmount: nil, proceedsUsdcMicros: nil, initiatedBy: "member", agentDisplayName: nil),
            GroupActivityItemDTO(id: "t2", kind: "deposit", status: "confirmed", symbol: nil, amountMicros: 100_000_000, createdAt: ago(55), txSignature: nil, tokenAmount: nil, proceedsUsdcMicros: nil, initiatedBy: nil, agentDisplayName: nil),
            GroupActivityItemDTO(id: "t3", kind: "sell", status: "failed", symbol: "TSLAx", amountMicros: 0, createdAt: ago(180), txSignature: nil, tokenAmount: "25000000", proceedsUsdcMicros: nil, initiatedBy: "member", agentDisplayName: nil),
            GroupActivityItemDTO(id: "t4", kind: "buy", status: "confirmed", symbol: "NVDAx", amountMicros: 194_710_000, createdAt: ago(60 * 26), txSignature: "3kQp", tokenAmount: nil, proceedsUsdcMicros: nil, initiatedBy: "agent", agentDisplayName: "Scout"),
            GroupActivityItemDTO(id: "t5", kind: "buy", status: "confirmed", symbol: "AAPLx", amountMicros: 250_000_000, createdAt: ago(60 * 50), txSignature: "4mZa", tokenAmount: nil, proceedsUsdcMicros: nil, initiatedBy: "member", agentDisplayName: nil),
            GroupActivityItemDTO(id: "t6", kind: "deposit", status: "confirmed", symbol: nil, amountMicros: 300_000_000, createdAt: ago(60 * 74), txSignature: nil, tokenAmount: nil, proceedsUsdcMicros: nil, initiatedBy: nil, agentDisplayName: nil),
        ]
    }

    static let boughtApple = TransactionDetailDTO(
        transactionId: "t5", groupId: "g1", action: "buy", status: "confirmed", amountMicros: 250_000_000,
        inputMint: nil, outputMint: nil, inputSymbol: "USDC", outputSymbol: "AAPLx",
        txSignature: "4mZaQ8nJv2kPp7sWfLr3bXy9TcHd6eUoGi1AqRsNmVtK", executeRequestId: nil, proposalId: "p1",
        costBasisPrice: 250_000_000, costBasisAmount: 108_034_000, createdAt: "2026-09-16T14:02:00Z",
        confirmedAt: "2026-09-16T14:02:09Z", failureReason: nil, proceedsUsdcMicros: nil
    )

    static let failedSell = TransactionDetailDTO(
        transactionId: "t3", groupId: "g1", action: "sell", status: "failed", amountMicros: 25_000_000,
        inputMint: nil, outputMint: nil, inputSymbol: "TSLAx", outputSymbol: "USDC",
        txSignature: nil, executeRequestId: nil, proposalId: "p2",
        costBasisPrice: nil, costBasisAmount: nil, createdAt: "2026-09-18T11:40:00Z",
        confirmedAt: nil, failureReason: "slippage", proceedsUsdcMicros: nil
    )
}
#endif
