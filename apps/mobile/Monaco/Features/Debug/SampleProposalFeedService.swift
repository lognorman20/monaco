#if DEBUG
import Foundation
import MonacoCore
import SwiftUI

/// Debug-only in-memory proposal backend for simulator QA without a Privy session.
/// Launch with `-MonacoProposalFeedSample` to open the feed on sample data (see docs/qa/149).
///
/// The cabal is five people: the viewer and four friends. Every ballot the detail read returns is
/// named and dated, and the vote counts are derived from those ballots, so a card's tally, its
/// faces and the Votes list always agree.
@MainActor
final class SampleProposalFeedService: ProposalFeedService {
    static let launchArgument = "-MonacoProposalFeedSample"

    static var isRequested: Bool {
        ProcessInfo.processInfo.arguments.contains(launchArgument)
    }

    private struct Record {
        var proposal: ProposalDTO
        /// The rest of the cabal's ballots, named, as the detail read returns them.
        var ballots: [ProposalVoteDTO] = []
        /// The viewer's own ballot once cast: "yes" or "no".
        var viewerChoice: String?
        var viewerCastAt: String?
    }

    /// What the sample backend does with a read: answers it (the default), finds nothing open or
    /// closed (`-MonacoProposalSampleEmpty`), fails it (`-MonacoProposalSampleFailing`), or never
    /// answers (`-MonacoProposalSampleLoading`) — so the feed's and the proposal screen's empty,
    /// failed and loading states can be shot as well as the loaded ones.
    private enum ReadMode {
        case answers, empty, fails, hangs

        static var requested: ReadMode {
            let arguments = ProcessInfo.processInfo.arguments
            if arguments.contains("-MonacoProposalSampleEmpty") { return .empty }
            if arguments.contains("-MonacoProposalSampleFailing") { return .fails }
            if arguments.contains("-MonacoProposalSampleLoading") { return .hangs }
            return .answers
        }
    }

    private let readMode = ReadMode.requested

    /// Every read goes through here first, so a failing or hanging launch does it everywhere.
    private func beforeRead() async throws {
        switch readMode {
        case .fails:
            throw MonacoCore.MonacoAPIError.httpStatus(503)
        case .hangs:
            try await Task.sleep(for: .seconds(3600))
        case .answers, .empty:
            return
        }
    }

    private let viewerName = "Logan Norman"
    let viewerId: String? = "viewer"
    /// Detail reads per proposal, so a passed buy can move from Buying to Done while the screen polls.
    private var detailReads: [String: Int] = [:]
    private var records: [Record]
    private var comments: [String: [ProposalCommentDTO]]
    private let iso = ISO8601DateFormatter()

    private static let members = ["Ada Park", "Ben Ortiz", "Cy Lin", "Dee Shah"]
    private static let eligible = members.count + 1

    private static let theses: [Int: String] = [
        0: "Earnings Thursday. Services revenue keeps compounding and we're underweight big tech.",
        1: "Up a lot since we bought. Taking half off before the print.",
        2: "Scout rebalances once a week so none of us has to babysit the pot.",
        3: "Cloud keeps beating. Small position before the call.",
        4: "Cloud margins are back. I'd rather own the picks and shovels than guess the winner.",
        20: "Prime Day looked strong. Adding before the holidays.",
        21: "Search is holding up better than the headlines say.",
        22: "Ad prices are up two quarters running.",
        23: "Index core so the pot isn't all single names.",
        24: "Adding to Apple on the dip.",
    ]

    /// Closed votes, spelled out: who voted which way, and how the viewer voted. Passed ones
    /// carry a majority, the others do not.
    private static let closedBallots: [Int: (yes: [String], no: [String], viewer: String?)] = [
        20: (["Ada Park", "Ben Ortiz"], ["Cy Lin"], "yes"),
        21: (["Ben Ortiz"], ["Cy Lin", "Dee Shah"], "no"),
        22: (["Cy Lin", "Dee Shah"], [], "yes"),
        23: (["Dee Shah"], ["Ada Park", "Ben Ortiz"], nil),
        24: (["Ada Park", "Ben Ortiz"], ["Dee Shah"], "yes"),
    ]

    init(now: Date = Date()) {
        let symbols = ["AAPLx", "NVDAx", "TSLAx", "MSFTx", "AMZNx", "GOOGLx", "METAx", "SPYx"]
        let iso = ISO8601DateFormatter()
        var built: [Record] = []
        for index in 0..<25 {
            let open = index < 20
            let proposer = Self.members[index % Self.members.count]
            let created = now.addingTimeInterval(TimeInterval(-900 * (index + 1)))
            let plan = Self.ballotPlan(index: index, proposer: proposer)
            // Ballots come in a few minutes apart after the proposal went up.
            let ballots = (plan.yes.map { ($0, "yes") } + plan.no.map { ($0, "no") })
                .enumerated()
                .map { order, ballot in
                    ProposalVoteDTO(
                        voterId: ballot.0.lowercased().replacingOccurrences(of: " ", with: "-"),
                        displayName: ballot.0,
                        choice: ballot.1,
                        castAt: iso.string(from: created.addingTimeInterval(TimeInterval(240 * (order + 1))))
                    )
                }
            let viewerCastAt = plan.viewer.map { _ in
                iso.string(from: created.addingTimeInterval(TimeInterval(240 * (ballots.count + 1))))
            }
            let record = Record(
                proposal: ProposalDTO(
                    id: "sample-\(index)",
                    symbol: symbols[index % symbols.count],
                    status: open ? "open" : (index % 2 == 0 ? "passed" : "failed"),
                    kind: index == 1 ? "sell" : (index == 2 ? "add_agent" : "buy"),
                    usdcMicros: index == 1 || index == 2 ? nil : String((index + 1) * 12_500_000),
                    tokenAmount: index == 1 ? "50000000" : nil,
                    agentDisplayName: index == 2 ? "Scout" : nil,
                    allocationUsdcMicros: index == 2 ? "500000000" : nil,
                    // sample-3 is read-only (a seeded ghost proposal): tally only, no buttons.
                    canVote: open && index != 3,
                    thesis: Self.theses[index],
                    proposerName: proposer,
                    createdAt: iso.string(from: created),
                    expiresAt: iso.string(from: now.addingTimeInterval(TimeInterval(3600 * (20 - index) + 1200)))
                ),
                ballots: ballots,
                viewerChoice: plan.viewer,
                viewerCastAt: viewerCastAt
            )
            built.append(record)
        }
        records = built
        comments = [
            "sample-0": [
                ProposalCommentDTO(id: "c-1", proposalId: "sample-0", authorId: "ben", authorName: "Ben Ortiz",
                                   body: "Why Apple over Nvidia this week?", createdAt: iso.string(from: now.addingTimeInterval(-1800))),
                ProposalCommentDTO(id: "c-2", proposalId: "sample-0", parentId: "c-1", authorId: "ada", authorName: "Ada Park",
                                   body: "Smaller drawdown for our first buy. Nvidia can be next.", createdAt: iso.string(from: now.addingTimeInterval(-1200))),
            ],
            "sample-20": [
                ProposalCommentDTO(id: "c-20-1", proposalId: "sample-20", authorId: "cy-lin", authorName: "Cy Lin",
                                   body: "I'm a no. We already hold a lot of retail through the index.", createdAt: iso.string(from: now.addingTimeInterval(-17_400))),
                ProposalCommentDTO(id: "c-20-2", proposalId: "sample-20", parentId: "c-20-1", authorId: "ada-park", authorName: "Ada Park",
                                   body: "Fair, but this is the cloud business as much as the store.", createdAt: iso.string(from: now.addingTimeInterval(-16_800))),
                ProposalCommentDTO(id: "c-20-3", proposalId: "sample-20", parentId: "c-20-2", authorId: "cy-lin", authorName: "Cy Lin",
                                   body: "Okay. Small is fine.", createdAt: iso.string(from: now.addingTimeInterval(-16_200))),
                ProposalCommentDTO(id: "c-20-4", proposalId: "sample-20", authorId: "ben-ortiz", authorName: "Ben Ortiz",
                                   body: "Bought. Nice one, Ada.", createdAt: iso.string(from: now.addingTimeInterval(-540))),
            ],
        ]
        for index in records.indices {
            records[index].proposal = withTally(records[index], commentCount: comments[records[index].proposal.id]?.count ?? 0)
        }
    }

    /// Who voted which way on proposal `index`. Open votes follow a simple rotation from the
    /// proposer, who is always their own first yes; no ballots come from the members after.
    /// sample-0 starts untouched, which the vote-from-the-card UI test depends on.
    private static func ballotPlan(index: Int, proposer: String) -> (yes: [String], no: [String], viewer: String?) {
        if let closed = closedBallots[index] { return closed }
        // lane: notifications — the viewer has voted yes on the open ones, so the proposal screen
        // shows "Remind them" (`-MonacoProposalSampleViewerVoted`).
        let viewerVoted = ProcessInfo.processInfo.arguments.contains("-MonacoProposalSampleViewerVoted") && index != 0
        let yesCount = index % 3
        let noCount = index % 2
        let start = members.firstIndex(of: proposer) ?? 0
        let rotation = (0..<members.count).map { members[(start + $0) % members.count] }
        let yes = Array(rotation.prefix(yesCount))
        let no = Array(rotation.dropFirst(max(yesCount, 1)).prefix(noCount))
        return (yes, no, viewerVoted ? "yes" : nil)
    }

    /// The record's proposal with its vote summary counted from its ballots.
    private func withTally(_ record: Record, commentCount: Int) -> ProposalDTO {
        let p = record.proposal
        let yes = record.ballots.filter { $0.choice == "yes" }.count + (record.viewerChoice == "yes" ? 1 : 0)
        let no = record.ballots.filter { $0.choice == "no" }.count + (record.viewerChoice == "no" ? 1 : 0)
        return ProposalDTO(
            id: p.id, symbol: p.symbol, status: p.status, kind: p.kind, usdcMicros: p.usdcMicros, tokenAmount: p.tokenAmount,
            agentDisplayName: p.agentDisplayName, allocationUsdcMicros: p.allocationUsdcMicros,
            canVote: p.canVote == true && record.viewerChoice == nil, thesis: p.thesis,
            proposerName: p.proposerName, createdAt: p.createdAt, expiresAt: p.expiresAt,
            voteSummary: ProposalVoteSummaryDTO(yesCount: yes, noCount: no, eligibleCount: Self.eligible, threshold: "majority"),
            commentCount: commentCount
        )
    }

    func listProposals(groupId: String, tab: ProposalFeedTab) async throws -> [ProposalDTO] {
        try await beforeRead()
        guard readMode != .empty else { return [] }
        return records.map(\.proposal).filter { ($0.status == "open") == (tab == .open) }
    }

    func proposal(id: String) async throws -> ProposalDTO {
        try await beforeRead()
        guard let record = records.first(where: { $0.proposal.id == id }) else {
            throw MonacoCore.MonacoAPIError.httpStatus(404)
        }
        var votes = record.ballots
        if let choice = record.viewerChoice {
            votes.append(ProposalVoteDTO(voterId: "viewer", displayName: viewerName, choice: choice, castAt: record.viewerCastAt))
        }
        let p = record.proposal
        return ProposalDTO(
            id: p.id, symbol: p.symbol, status: p.status, kind: p.kind, usdcMicros: p.usdcMicros, tokenAmount: p.tokenAmount, agentDisplayName: p.agentDisplayName, allocationUsdcMicros: p.allocationUsdcMicros, canVote: p.canVote, thesis: p.thesis,
            proposerName: p.proposerName, createdAt: p.createdAt, expiresAt: p.expiresAt,
            votes: votes, voteSummary: p.voteSummary, execution: execution(for: p),
            commentCount: p.commentCount
        )
    }

    /// Passed buys: sample-20 has landed; sample-22 is mid-swap for two reads, then lands;
    /// sample-24's swap failed.
    private func execution(for p: ProposalDTO) -> ProposalExecutionDTO {
        guard p.status == "passed", p.isTrade else { return ProposalExecutionDTO(state: "not_applicable") }
        let reads = detailReads[p.id, default: 0]
        detailReads[p.id] = reads + 1
        if p.id == "sample-24" { return ProposalExecutionDTO(state: "failed", failureReason: "No route at this size") }
        if p.id == "sample-22", reads < 2 { return ProposalExecutionDTO(state: "pending") }
        return ProposalExecutionDTO(state: "confirmed", executedAt: iso.string(from: Date().addingTimeInterval(-600)))
    }

    func castVote(proposalId: String, choice: ProposalVoteChoice) async throws {
        guard let index = records.firstIndex(where: { $0.proposal.id == proposalId }) else {
            throw MonacoCore.MonacoAPIError.httpStatus(404)
        }
        guard records[index].proposal.canVote == true else { throw MonacoCore.MonacoAPIError.httpStatus(403) }
        records[index].viewerChoice = choice.rawValue
        records[index].viewerCastAt = iso.string(from: Date())
        records[index].proposal = withTally(records[index], commentCount: records[index].proposal.commentCount ?? 0)
    }

    func comments(proposalId: String) async throws -> [ProposalCommentDTO] {
        try await beforeRead()
        return comments[proposalId] ?? []
    }

    func postComment(proposalId: String, body: String, parentId: String?) async throws -> ProposalCommentDTO {
        guard case .ready(let trimmed) = ProposalCommentDraft(text: body) else {
            throw MonacoCore.MonacoAPIError.httpStatus(400)
        }
        let comment = ProposalCommentDTO(
            id: UUID().uuidString, proposalId: proposalId, parentId: parentId, authorId: "viewer",
            authorName: viewerName, body: trimmed, createdAt: iso.string(from: Date())
        )
        comments[proposalId, default: []].append(comment)
        if let index = records.firstIndex(where: { $0.proposal.id == proposalId }) {
            records[index].proposal = withTally(records[index], commentCount: comments[proposalId]?.count ?? 0)
        }
        return comment
    }
}

/// Root for the sample-data launch (Debug only): the feed under its real title, "Proposals", so it can be
/// recorded as is. The launch argument, not the screen, is what marks it as sample data.
/// Extra arguments open other proposal screens on the same sample data:
/// `-MonacoProposeSample` (a cabal screen with the Propose sheet),
/// `-MonacoProposeSampleStock` (the Stock detail entry: the buy flow jumped straight to a stock,
/// with `-MonacoProposePotFails` to make the first pot read fail),
/// `-MonacoProposalSampleDetail <id>` (one proposal's detail, e.g. `sample-22` for the swap tracker,
/// `sample-24` for a swap that failed; add `-MonacoProposalSampleBottom` to open it scrolled to
/// the ballots and the discussion) and `-MonacoProposalSampleClosed` (the feed on its Closed tab).
/// `-MonacoProposalSampleEmpty`, `-MonacoProposalSampleFailing` and `-MonacoProposalSampleLoading`
/// put either screen in its empty, failed or loading state (see `ReadMode`).
struct SampleProposalFeedRoot: View {
    @State private var service = SampleProposalFeedService()

    private var arguments: [String] { ProcessInfo.processInfo.arguments }

    private var detailId: String? {
        guard let index = arguments.firstIndex(of: "-MonacoProposalSampleDetail"), arguments.indices.contains(index + 1) else { return nil }
        return arguments[index + 1]
    }

    /// The lower half of the proposal screen — the ballots and the discussion — is below the fold,
    /// so the gallery needs a way to open on it.
    private var opensAtBottom: Bool { arguments.contains("-MonacoProposalSampleBottom") }

    private var opensOnClosed: Bool { arguments.contains("-MonacoProposalSampleClosed") }

    var body: some View {
        Group {
            if arguments.contains("-MonacoProposeSample") {
                SampleProposeRoot()
            } else if arguments.contains("-MonacoProposeSampleStock") {
                SampleProposeFromStockRoot()
            } else if let detailId {
                NavigationStack {
                    ProposalDetailView(service: service, proposalId: detailId)
                }
                .defaultScrollAnchor(opensAtBottom ? .bottom : nil)
            } else {
                NavigationStack {
                    ProposalFeedView(service: service, groupId: "sample", initialTab: opensOnClosed ? .closed : .open)
                }
            }
        }
        .tint(MonacoTheme.accent)
    }
}

/// A stand-in cabal screen with the Propose sheet, wired the way Group detail wires it.
///
/// For the screenshot gallery, one more flag opens it already on a step, on canned state. Each
/// step has its own flag, so `scripts/qa/screens.sh --check` holds every one of them to a line in
/// the manifest: `-MonacoProposeSampleBuy` (pick a stock), `-MonacoProposeSampleAmount` ($25 and a
/// reason), `-MonacoProposeSampleReview` (the receipt), `-MonacoProposeSampleSell` (what the cabal
/// owns), `-MonacoProposeSampleSellAmount` (half the Apple holding), `-MonacoProposeSampleSellReview`,
/// `-MonacoProposeSampleAddBot`, `-MonacoProposeSamplePauseBot` (the question a bot proposal asks),
/// `-MonacoProposeSampleCashOnly` (the chooser for a cabal that holds only cash and runs a bot:
/// Sell greyed out with its reason, and the bot's pause and remove rows), and the stock screen's
/// cabal picker for a buy or a sell, `-MonacoProposeSamplePickCabal` and
/// `-MonacoProposeSamplePickCabalSell`.
private struct SampleProposeRoot: View {
    @State private var service = SampleProposeService()
    @State private var showsPropose = false
    @State private var toast: MonacoToast?

    private let step = SampleProposeStep.requested

    var body: some View {
        if let kind = step?.pickerKind {
            SampleProposePickerRoot(kind: kind, service: service)
        } else {
            cabalScreen
        }
    }

    private var cabalScreen: some View {
        NavigationStack {
            VStack(spacing: MonacoTheme.Space.l) {
                CabalMark(groupId: SampleProposeService.groupView.id, name: SampleProposeService.groupView.name, size: 64)
                CircleAction("Propose", systemImage: "arrow.up.right") { showsPropose = true }
                    .accessibilityIdentifier("group-action-propose")
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(MonacoTheme.canvas.ignoresSafeArea())
            .navigationTitle(SampleProposeService.groupView.name)
            .navigationBarTitleDisplayMode(.inline)
            .sheet(isPresented: $showsPropose) {
                if step == .cashOnly {
                    ProposeSheet(
                        service: service,
                        groupId: SampleProposeService.cashOnlyView.id,
                        groupView: SampleProposeService.cashOnlyView,
                        onProposed: proposed
                    )
                } else if let step {
                    SampleProposeStepSheet(step: step, service: service, onProposed: proposed)
                } else {
                    ProposeSheet(
                        service: service,
                        groupId: SampleProposeService.groupView.id,
                        groupView: SampleProposeService.groupView,
                        onProposed: proposed
                    )
                }
            }
            .monacoToast($toast)
        }
        .onAppear {
            if step != nil || ProcessInfo.processInfo.arguments.contains("-MonacoProposeSampleOpen") { showsPropose = true }
        }
    }

    private func proposed(_ proposalId: String) {
        showsPropose = false
        Haptics.success()
        toast = MonacoToast(message: ProposeFlowCopy.proposalSent(SampleProposeService.groupView.name), isSuccess: true)
    }
}

/// Which step `-MonacoProposeSample` opens on, one launch flag each.
private enum SampleProposeStep: CaseIterable, Hashable {
    case buy, amount, review, sell, sellAmount, sellReview, addBot, pauseBot, cashOnly, pickCabal, pickCabalSell

    var launchArgument: String {
        switch self {
        case .buy: "-MonacoProposeSampleBuy"
        case .amount: "-MonacoProposeSampleAmount"
        case .review: "-MonacoProposeSampleReview"
        case .sell: "-MonacoProposeSampleSell"
        case .sellAmount: "-MonacoProposeSampleSellAmount"
        case .sellReview: "-MonacoProposeSampleSellReview"
        case .addBot: "-MonacoProposeSampleAddBot"
        case .pauseBot: "-MonacoProposeSamplePauseBot"
        case .cashOnly: "-MonacoProposeSampleCashOnly"
        case .pickCabal: "-MonacoProposeSamplePickCabal"
        case .pickCabalSell: "-MonacoProposeSamplePickCabalSell"
        }
    }

    /// The picker is reached from a stock's screen, not from the sheet.
    var pickerKind: ProposalPickKind? {
        switch self {
        case .pickCabal: .buy
        case .pickCabalSell: .sell
        default: nil
        }
    }

    static var requested: SampleProposeStep? {
        let arguments = ProcessInfo.processInfo.arguments
        return allCases.first { arguments.contains($0.launchArgument) }
    }
}

/// The Propose sheet with one step already pushed over the chooser, so the step is shot in the
/// sheet it lives in, at the height the flow pins it to, with a way back.
private struct SampleProposeStepSheet: View {
    let step: SampleProposeStep
    let service: SampleProposeService
    let onProposed: (String) -> Void

    @State private var detents = ProposeSheetDetents()
    @State private var path: [SampleProposeStep] = []

    var body: some View {
        NavigationStack(path: $path) {
            ProposeChooserView(
                service: service,
                groupId: SampleProposeService.groupView.id,
                groupView: SampleProposeService.groupView,
                onProposed: onProposed,
                detents: $detents
            )
            .navigationDestination(for: SampleProposeStep.self) { step in
                SampleProposeStepScreen(step: step, service: service, onProposed: onProposed)
            }
        }
        .presentationDetents(detents.allowed, selection: $detents.selection)
        .task { path = [step] }
    }
}

/// Each step on its canned state: the Weekend investors pot, Apple at $231.40, and the reasons the
/// sample proposals give.
private struct SampleProposeStepScreen: View {
    let step: SampleProposeStep
    let service: SampleProposeService
    let onProposed: (String) -> Void

    private var groupId: String { SampleProposeService.groupView.id }
    private var pot: ProposePot { ProposePot(view: SampleProposeService.groupView) }

    var body: some View {
        switch step {
        case .buy:
            ProposeBuyView(service: service, groupId: groupId, pot: pot, onProposed: onProposed)
        case .amount:
            ProposeAmountView(
                service: service,
                groupId: groupId,
                stock: SampleProposeService.apple,
                pot: pot,
                initialAmount: "25",
                initialReason: SampleProposeService.buyReason,
                onProposed: onProposed
            )
        case .review:
            ProposeReviewView(service: service, groupId: groupId, review: SampleProposeService.buyReview, onProposed: onProposed)
        case .sell:
            ProposeSellView(service: service, groupId: groupId, pot: pot, onProposed: onProposed)
        case .sellAmount:
            ProposeSellAmountView(
                service: service,
                groupId: groupId,
                holding: SampleProposeService.appleHolding,
                pot: pot,
                initialAmount: "139.23",
                initialReason: SampleProposeService.sellReason,
                onProposed: onProposed
            )
        case .sellReview:
            ProposeSellReviewView(service: service, groupId: groupId, review: SampleProposeService.sellReview, onProposed: onProposed)
        case .addBot:
            ProposeAddAgentView(service: service, groupId: groupId, pot: pot, onProposed: onProposed)
        case .pauseBot:
            ProposeAgentLifecycleView(service: service, groupId: groupId, kind: "pause_agent", botName: "Scout", onProposed: onProposed)
        case .cashOnly, .pickCabal, .pickCabalSell:
            EmptyView()
        }
    }
}

/// The stock screen's way in: "Propose buy" or "Propose sell" on AAPL pushes the cabal picker.
/// The stock screen is a stand-in; the picker, its fan-out over three cabals and the amount step
/// behind it are the real ones.
private struct SampleProposePickerRoot: View {
    let kind: ProposalPickKind
    let service: SampleProposeService

    @EnvironmentObject private var auth: PrivyAuthService
    @State private var session: AppSessionStore = {
        let session = AppSessionStore()
        session.isLoading = false
        session.home = HomeViewDTO(groups: SampleProposeService.joinedCabals, people: [])
        return session
    }()
    @State private var path: [ProposalPickKind] = []
    @State private var toast: MonacoToast?

    var body: some View {
        NavigationStack(path: $path) {
            VStack(spacing: MonacoTheme.Space.m) {
                StockMark(symbol: SampleProposeService.apple.symbol, size: 64)
                Text(SampleProposeService.apple.name)
                    .font(MonacoTheme.Typo.title)
                    .foregroundStyle(MonacoTheme.ink)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(MonacoTheme.canvas.ignoresSafeArea())
            .navigationTitle(SampleProposeService.apple.ticker)
            .navigationBarTitleDisplayMode(.inline)
            .navigationDestination(for: ProposalPickKind.self) { kind in
                GroupPickerForProposalView(
                    auth: auth,
                    symbol: SampleProposeService.apple.symbol,
                    kind: kind,
                    stock: SampleProposeService.apple,
                    onProposed: { cabalName in
                        path = []
                        Haptics.success()
                        toast = MonacoToast(message: ProposeFlowCopy.proposalSent(cabalName), isSuccess: true)
                    },
                    service: service,
                    holdingsDataSource: SampleCabalHoldings()
                )
            }
        }
        .environment(session)
        .monacoToast($toast)
        .task { path = [kind] }
    }
}

/// Each sample cabal's pot, read after a short wait so the picker's loading rows are real.
private struct SampleCabalHoldings: CabalHoldingsDataSource {
    func groupView(groupId: String) async throws -> GroupViewDTO {
        try await Task.sleep(for: .milliseconds(300))
        guard let view = SampleProposeService.cabalViews[groupId] else { throw URLError(.badServerResponse) }
        return view
    }
}

/// The Stock detail entry into the buy flow: no chooser sheet, no pot handed down, and the stock
/// already picked. With `-MonacoProposePotFails` the first pot read fails, which is the path where
/// the amount step used to strand the member with a disabled Review button.
private struct SampleProposeFromStockRoot: View {
    @State private var service = SampleProposeService()

    var body: some View {
        NavigationStack {
            ProposeBuyView(
                service: service,
                groupId: SampleProposeService.groupView.id,
                pot: nil,
                initialSymbol: "AAPLx"
            )
        }
    }
}

/// In-memory propose backend: a cabal with $548.20 in the pot, popular stocks with prices (one of
/// them not buyable), catalog search, quotes at the listed price, and proposals that always go through.
@MainActor
final class SampleProposeService: ProposeService {
    static let groupView = GroupViewDTO(
        id: "sample-weekend",
        name: "Weekend investors",
        treasuryAddress: nil,
        potTotalUsd: "548.20",
        pot: [
            PotRowDTO(symbol: "AAPLx", units: "1.2034", markUsd: "231.40", valueUsd: "278.47", dollarPnl: "+28.47", afterHours: false, tokenAmount: "120340000"),
            PotRowDTO(symbol: "NVDAx", units: "1.05", markUsd: "178.20", valueUsd: "187.11", dollarPnl: "+22.11", afterHours: false, tokenAmount: "105000000"),
            PotRowDTO(symbol: "USDC", units: "82.62", markUsd: "1.00", valueUsd: "82.62", dollarPnl: "+0.00", afterHours: nil, tokenAmount: nil),
        ],
        you: MemberSliceDTO(shareUnits: "311500000", equityUsd: "311.50", slicePercent: "0.568", dollarPnl: "+27.40", percentReturn: "0.096"),
        members: [],
        proposals: nil,
        agent: nil
    )

    private static let catalog: [ProposeStock] = [
        ProposeStock(symbol: "AAPLx", name: "Apple", priceMicros: 231_400_000, change24h: "0.012"),
        ProposeStock(symbol: "NVDAx", name: "Nvidia", priceMicros: 178_200_000, change24h: "-0.008"),
        ProposeStock(symbol: "TSLAx", name: "Tesla", priceMicros: 342_100_000, change24h: "0.034"),
        ProposeStock(symbol: "MSFTx", name: "Microsoft", priceMicros: 438_900_000, change24h: "0.004"),
        ProposeStock(symbol: "SPYx", name: "S&P 500", priceMicros: 612_300_000, change24h: "0.002"),
        ProposeStock(symbol: "GOOGLx", name: "Alphabet", priceMicros: 201_000_000, change24h: "-0.015"),
        ProposeStock(symbol: "AMBRx", name: "Amber", priceMicros: 12_400_000, change24h: nil, isTradable: false),
    ]

    // MARK: Canned state for the step scenarios

    static var apple: ProposeStock { catalog[0] }

    static var appleHolding: PotRowDTO { groupView.pot[0] }

    static let buyReason = "Earnings Thursday. Services revenue keeps compounding and we're underweight big tech."
    static let sellReason = "Take some profit before earnings."

    /// $25 of Apple, priced the way `buyQuote` prices it.
    static var buyReview: ProposeBuyReview {
        let usdcMicros: Int64 = 25_000_000
        return ProposeBuyReview(
            stock: apple,
            usdcMicros: usdcMicros,
            quote: buyQuote(symbol: apple.symbol, usdcMicros: usdcMicros),
            fallbackPriceMicros: apple.priceMicros,
            cabalId: groupView.id,
            cabalName: groupView.name,
            thesis: buyReason,
            potMicros: ProposePot(view: groupView).totalMicros
        )
    }

    /// Half the Apple holding, priced the way `sellQuote` prices it.
    static var sellReview: ProposeSellReview {
        let held = Int64(appleHolding.tokenAmount ?? "0") ?? 0
        let sold = held / 2
        return ProposeSellReview(
            symbol: appleHolding.symbol,
            name: apple.name,
            tokenAmount: sold,
            estimateMicros: sellQuote(symbol: appleHolding.symbol, tokenAmount: sold).outputUsdcMicros.flatMap { Int64($0) },
            cabalId: groupView.id,
            cabalName: groupView.name,
            thesis: sellReason,
            heldTokenAmount: held
        )
    }

    /// The same cabal before it bought anything, with a bot running: nothing to sell, and the bot's
    /// own rows in the chooser.
    static let cashOnlyView = cabalView(
        id: groupView.id,
        name: groupView.name,
        potTotalUsd: "548.20",
        pot: [
            PotRowDTO(symbol: "USDC", units: "548.20", markUsd: "1.00", valueUsd: "548.20", dollarPnl: "+0.00", afterHours: nil, tokenAmount: nil),
        ],
        agent: GroupAgentDTO(id: "sample-scout", status: "active", agentDisplayName: "Scout", allocationUsdcMicros: "100000000")
    )

    /// The member's cabals for the stock screen's picker: two hold Apple, one does not.
    static let joinedCabals: [HomeGroupBoardRowDTO] = [
        HomeGroupBoardRowDTO(groupId: groupView.id, name: groupView.name, potValueUsd: "548.20", percentReturn: "0.096", dollarPnl: "+27.40", isJoined: true),
        HomeGroupBoardRowDTO(groupId: "sample-chips", name: "Semis or bust", potValueUsd: "2310.75", percentReturn: "-0.036", dollarPnl: "-86.20", isJoined: true),
        HomeGroupBoardRowDTO(groupId: "sample-index", name: "Index huggers", potValueUsd: "120.00", percentReturn: "0.0", dollarPnl: "+0.00", isJoined: true),
    ]

    static let cabalViews: [String: GroupViewDTO] = [
        groupView.id: groupView,
        "sample-chips": cabalView(id: "sample-chips", name: "Semis or bust", potTotalUsd: "2310.75", pot: [
            PotRowDTO(symbol: "NVDAx", units: "8.4", markUsd: "178.20", valueUsd: "1496.88", dollarPnl: "-61.40", afterHours: false, tokenAmount: "840000000"),
            PotRowDTO(symbol: "AAPLx", units: "3.1", markUsd: "231.40", valueUsd: "717.34", dollarPnl: "-24.80", afterHours: false, tokenAmount: "310000000"),
            PotRowDTO(symbol: "USDC", units: "96.53", markUsd: "1.00", valueUsd: "96.53", dollarPnl: "+0.00", afterHours: nil, tokenAmount: nil),
        ]),
        "sample-index": cabalView(id: "sample-index", name: "Index huggers", potTotalUsd: "120.00", pot: [
            PotRowDTO(symbol: "USDC", units: "120.00", markUsd: "1.00", valueUsd: "120.00", dollarPnl: "+0.00", afterHours: nil, tokenAmount: nil),
        ]),
    ]

    private static func cabalView(id: String, name: String, potTotalUsd: String, pot: [PotRowDTO], agent: GroupAgentDTO? = nil) -> GroupViewDTO {
        GroupViewDTO(
            id: id,
            name: name,
            treasuryAddress: nil,
            potTotalUsd: potTotalUsd,
            pot: pot,
            you: MemberSliceDTO(shareUnits: "0", equityUsd: "0", slicePercent: "0", dollarPnl: "+0.00", percentReturn: "0"),
            members: [],
            proposals: nil,
            agent: agent
        )
    }

    /// A buy priced at the catalog price, one share = 10^8 atomics.
    static func buyQuote(symbol: String, usdcMicros: Int64) -> BuyQuoteDTO {
        let price = catalog.first { $0.symbol == symbol }?.priceMicros ?? 100_000_000
        let atomics = Int64((Double(usdcMicros) / Double(price)) * 100_000_000)
        return BuyQuoteDTO(
            symbol: symbol,
            routable: true,
            kind: "buy",
            usdcMicros: String(usdcMicros),
            outputAmount: String(atomics),
            priceUsdcMicros: String(price)
        )
    }

    /// A sell priced at the holding's mark; under a dollar does not route.
    static func sellQuote(symbol: String, tokenAmount: Int64) -> BuyQuoteDTO {
        let mark = groupView.pot.first { $0.symbol == symbol }.flatMap { Double($0.markUsd) } ?? 1
        let usdc = Int64(Double(tokenAmount) / 100_000_000 * mark * 1_000_000)
        return BuyQuoteDTO(
            symbol: symbol,
            routable: usdc >= 1_000_000,
            kind: "sell",
            tokenAmount: String(tokenAmount),
            outputAmount: String(usdc),
            outputUsdcMicros: String(usdc)
        )
    }

    /// `-MonacoProposePotFails`: the first read fails, so a retry can be driven from a test.
    private var potReads = 0

    func pot(groupId: String) async throws -> ProposePot {
        potReads += 1
        if potReads == 1, ProcessInfo.processInfo.arguments.contains("-MonacoProposePotFails") {
            // The same error type `LiveProposeService` throws, so the sample harness exercises the
            // real `ProposeErrorCopy` mapping rather than only its type-agnostic fallbacks.
            throw Monaco.MonacoAPIError.httpStatus(503)
        }
        return ProposePot(view: Self.groupView)
    }

    func popularStocks() async throws -> [ProposeStock] {
        try await Task.sleep(for: .milliseconds(300))
        return Self.catalog
    }

    func searchStocks(groupId: String, query: String, offset: Int, limit: Int) async throws -> (stocks: [ProposeStock], hasMore: Bool) {
        let term = query.lowercased()
        let matches = Self.catalog.filter { $0.name.lowercased().contains(term) || $0.ticker.lowercased().contains(term) }
        return (Array(matches.dropFirst(offset).prefix(limit)), false)
    }

    func assetDetail(symbol: String) async throws -> AssetDetailDTO {
        let stock = Self.catalog.first { $0.symbol == symbol }
        return AssetDetailDTO(
            symbol: symbol,
            name: stock?.name ?? symbol,
            solanaMint: "",
            routable: stock?.isTradable ?? true,
            priceUsdcMicros: stock?.priceMicros,
            change24h: stock?.change24h,
            liquidity: AssetLiquidityDTO(label: "Via Jupiter", routable: true, buyProbeUsdcMicros: 25_000_000)
        )
    }

    func priceMicros(symbol: String) async throws -> Int64? {
        Self.catalog.first { $0.symbol == symbol }?.priceMicros
    }

    func buyQuote(groupId: String, symbol: String, usdcMicros: Int64) async throws -> BuyQuoteDTO {
        try await Task.sleep(for: .milliseconds(500))
        return Self.buyQuote(symbol: symbol, usdcMicros: usdcMicros)
    }

    func sellQuote(groupId: String, symbol: String, tokenAmount: Int64) async throws -> BuyQuoteDTO {
        try await Task.sleep(for: .milliseconds(500))
        return Self.sellQuote(symbol: symbol, tokenAmount: tokenAmount)
    }

    func propose(groupId: String, draft: ProposalDraft, submission: IdempotentSubmission) async throws -> String {
        try await Task.sleep(for: .milliseconds(600))
        return UUID().uuidString
    }
}
#endif
