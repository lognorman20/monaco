#if DEBUG
import MonacoCore
import SwiftUI
import UIKit

/// Debug-only: the group screen and its pushed screens on canned data, no sign-in or backend.
/// Launch with `-MonacoGroupDetailSample <scenario>`:
/// `populated` · `empty` · `loading` · `details` (Cabal details sheet open) · `propose` (chooser sheet open)
/// · `cashOut` · `receipt` (bought) · `receiptFailed` (failed sell) · `activity` (full list)
/// · `sellAndLeave` (the screen while the slice is being sold)
/// · `picture` (cabal with a picture, viewer is its creator) · `noPicture` (creator, tinted
/// initials, nothing to remove) · `pictureNotCreator` (has a picture, viewer is a plain member,
/// so no controls) · `pictureUploadFailure` (every write is refused).
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
    case sellAndLeave
    case picture
    case noPicture
    case pictureNotCreator
    case pictureUploadFailure

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
    /// Every scenario gets an editor; only the ones whose sample view says the
    /// viewer is the creator actually show its controls.
    @StateObject private var pictureEditor: CabalPictureEditor

    init(scenario: GroupDetailSampleScenario, auth: PrivyAuthService) {
        self.scenario = scenario
        self.auth = auth
        _pictureEditor = StateObject(wrappedValue: CabalPictureEditor(
            groupId: GroupDetailSampleData.pictureGroupId,
            pictureUrl: GroupDetailSampleData.initialPictureURL(for: scenario),
            writer: SampleCabalPictureWriter(alwaysFails: scenario == .pictureUploadFailure)
        ))
    }

    /// The one scenario that stands in for a leave in flight, read wherever the product reads
    /// `isLeaving`, so the harness and the product gate on the same thing.
    private var isLeaving: Bool { scenario == .sellAndLeave }

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
        case .populated, .empty, .details, .propose, .sellAndLeave:
            groupScreen(scenario == .empty ? GroupDetailSampleData.emptyView : GroupDetailSampleData.view)
        case .picture, .noPicture, .pictureNotCreator, .pictureUploadFailure:
            groupScreen(GroupDetailSampleData.pictureView(for: scenario))
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
            onHeroScrolledAway: { heroScrolledAway = $0 },
            pictureEditor: pictureEditor,
            heroChart: scenario == .empty ? .sparse : .curve(GroupDetailSampleData.pnlPoints),
            heroRange: .oneMonth
        )

        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .monacoCanvas()
        // The same cover the real screen puts up while a leave is running.
        .groupLeaveProgress(isLeaving: isLeaving, isSellingSlice: true)
        .navigationTitle(heroScrolledAway ? view.name : "")
        .navigationBarTitleDisplayMode(.inline)
        .cabalHeroNavigationBar(isOverHero: !heroScrolledAway)

        .toolbar {
            // `GroupDetailView` drops this item entirely while a leave runs, so the harness
            // drops it under the same condition. Rendering it regardless would leave the
            // leave-in-progress test asserting against an item the product never shows.
            if !isLeaving {
                ToolbarItem(placement: .topBarTrailing) {
                    Button { showDetails = true } label: { Image(systemName: "info.circle") }
                        .accessibilityLabel("Cabal details")
                        .accessibilityIdentifier("group-details-button")
                }
            }
        }
        .navigationDestination(item: $route) { route in
            switch route {
            case .cashOut(let shareUnits, let equityUsd):
                SellCabalView(auth: auth, groupId: view.id, maxShareUnits: shareUnits, equityUsd: equityUsd)
            case .activity:
                GroupActivityListView(auth: auth, items: GroupDetailSampleData.activity, retryingTransactionIDs: [], onRetry: { _ in })
            case .proposals:
                ProposalFeedView(service: proposalService, groupId: view.id)
            case .stock(let symbol):
                AssetDetailView(auth: auth, symbol: symbol)
            case .addMoney, .chat:
                Text("Not in the sample harness")
                    .foregroundStyle(MonacoTheme.muted)
            }
        }
        .sheet(isPresented: $showPropose) {
            ProposeSheet(auth: auth, groupId: view.id, groupView: view)
        }
        .sheet(isPresented: $showDetails) {
            // lane: invites
            GroupDetailsSheet(groupId: view.id, groupName: view.name, invites: SampleInviteSource(), treasuryAddress: view.treasuryAddress, isLeaving: false, onLeave: {})
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

    /// A month of the pot's P&L, three days a sample, ending on the hero's all-time figure.
    static var pnlPoints: [GroupPnLPointDTO] {
        let path: [Double] = [0, 4.1, 9.8, 7.2, 15.5, 12.0, 19.4, 24.7, 18.9, 20.87]
        let step: TimeInterval = 3 * 24 * 3600
        let now = Date()
        return path.enumerated().map { index, value in
            GroupPnLPointDTO(
                at: now.addingTimeInterval(-step * Double(path.count - 1 - index)),
                potValueUsd: "548.20",
                netInUsd: "527.33",
                dollarPnl: String(format: "%+.2f", value)
            )
        }
    }


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
        agent: GroupAgentDTO(id: "a1", status: "active", agentDisplayName: "Scout", allocationUsdcMicros: "100000000", apiKey: "scout")
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

    /// One request with a profile photo, one falling back to initials.
    static let joinRequests = [
        JoinRequestDTO(
            id: "jr1", userId: "u9", displayName: "Priya Nair",
            profilePhotoUrl: ProfileSampleHarness.samplePhotoURL()?.absoluteString,
            requestedAt: "2026-09-18T10:00:00Z"
        ),
        JoinRequestDTO(id: "jr2", userId: "u10", displayName: "Jordan Hale", requestedAt: "2026-09-18T11:00:00Z"),
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

    // MARK: - Cabal picture scenarios

    static let pictureGroupId = "5b1f0c9e-0001-4c55-9a51-000000000001"

    /// The picture a scenario starts with. A file URL, so the mark draws a real
    /// image through the same image store with no network.
    static func initialPictureURL(for scenario: GroupDetailSampleScenario) -> String? {
        switch scenario {
        case .picture, .pictureNotCreator, .pictureUploadFailure:
            samplePictureURL()?.absoluteString
        default:
            nil
        }
    }

    /// The group view behind each picture scenario. Only the two creator
    /// scenarios say the viewer created the cabal, so the others must not offer
    /// the controls at all.
    static func pictureView(for scenario: GroupDetailSampleScenario) -> GroupViewDTO {
        let view = self.view
        return GroupViewDTO(
            id: view.id,
            name: view.name,
            treasuryAddress: view.treasuryAddress,
            potTotalUsd: view.potTotalUsd,
            pot: view.pot,
            you: view.you,
            members: view.members,
            proposals: view.proposals,
            agent: view.agent,
            pictureUrl: initialPictureURL(for: scenario),
            isCreator: scenario != .pictureNotCreator
        )
    }

    /// A generated square written to tmp, so the cabal mark shows a picture with
    /// no backend and no network.
    static func samplePictureURL() -> URL? {
        let size = CGSize(width: 256, height: 256)
        let image = UIGraphicsImageRenderer(size: size).image { context in
            let cg = context.cgContext
            let colors = [
                UIColor(red: 0.16, green: 0.36, blue: 0.75, alpha: 1).cgColor,
                UIColor(red: 0.52, green: 0.22, blue: 0.70, alpha: 1).cgColor,
            ] as CFArray
            if let gradient = CGGradient(colorsSpace: CGColorSpaceCreateDeviceRGB(), colors: colors, locations: [0, 1]) {
                cg.drawLinearGradient(gradient, start: .zero, end: CGPoint(x: 256, y: 256), options: [.drawsAfterEndLocation])
            }
            UIColor(white: 1, alpha: 0.85).setFill()
            cg.fillEllipse(in: CGRect(x: 78, y: 62, width: 100, height: 100))
            UIColor(white: 1, alpha: 0.55).setFill()
            cg.fill(CGRect(x: 48, y: 176, width: 160, height: 22))
        }
        guard let data = image.pngData() else { return nil }
        let url = FileManager.default.temporaryDirectory.appending(path: "monaco-sample-cabal-picture.png")
        do {
            try data.write(to: url, options: .atomic)
            return url
        } catch {
            return nil
        }
    }
}

/// Stands in for the backend in the picture scenarios: a short delay so the
/// spinner is visible, then either a new picture or the failure the
/// `pictureUploadFailure` scenario exists to show.
@MainActor
struct SampleCabalPictureWriter: CabalPictureWriting {
    var alwaysFails = false

    func uploadPicture(groupId: String, imageData: Data, mimeType: String) async throws -> String? {
        try? await Task.sleep(for: .milliseconds(700))
        if alwaysFails {
            throw MonacoCore.MonacoAPIError.rejected(status: 413, message: "picture must be at most 2MB")
        }
        return GroupDetailSampleData.samplePictureURL()?.absoluteString
    }

    func removePicture(groupId: String) async throws -> String? {
        try? await Task.sleep(for: .milliseconds(400))
        if alwaysFails {
            throw MonacoCore.MonacoAPIError.httpStatus(503)
        }
        return nil
    }
}
#endif
