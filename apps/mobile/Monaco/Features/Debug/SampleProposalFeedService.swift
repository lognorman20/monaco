#if DEBUG
import Foundation
import MonacoCore
import SwiftUI

/// Debug-only in-memory proposal backend for simulator QA without a Privy session.
/// Launch with `-MonacoProposalFeedSample` to open the feed on sample data (see docs/qa/149).
@MainActor
final class SampleProposalFeedService: ProposalFeedService {
    static let launchArgument = "-MonacoProposalFeedSample"

    static var isRequested: Bool {
        ProcessInfo.processInfo.arguments.contains(launchArgument)
    }

    private struct Record {
        var proposal: ProposalDTO
        var viewerVoted = false
    }

    private let viewerName = "You"
    let viewerId: String? = "viewer"
    /// Detail reads per proposal, so a passed buy can move from Buying to Done while the screen polls.
    private var detailReads: [String: Int] = [:]
    private var records: [Record]
    private var comments: [String: [ProposalCommentDTO]]
    private let iso = ISO8601DateFormatter()

    private static let theses: [Int: String] = [
        0: "Earnings Thursday. Services revenue keeps compounding and we're underweight big tech.",
        3: "Deliveries beat last quarter and the chart is basing. Small position before the call.",
        4: "Cloud margins are back. I'd rather own the picks and shovels than guess the winner.",
        20: "Index core so the pot isn't all single names.",
    ]

    init(now: Date = Date()) {
        let symbols = ["AAPLx", "NVDAx", "TSLAx", "MSFTx", "AMZNx", "GOOGLx", "METAx", "SPYx"]
        let proposers = ["Ada Park", "Ben Ortiz", "Cy Lin", "Dee Shah"]
        var built: [Record] = []
        for index in 0..<24 {
            let open = index < 20
            let yes = index % 3
            let no = index % 2
            built.append(Record(proposal: ProposalDTO(
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
                proposerName: proposers[index % proposers.count],
                createdAt: iso.string(from: now.addingTimeInterval(TimeInterval(-900 * (index + 1)))),
                expiresAt: iso.string(from: now.addingTimeInterval(TimeInterval(3600 * (20 - index) + 1200))),
                voteSummary: ProposalVoteSummaryDTO(yesCount: yes, noCount: no, eligibleCount: 5, threshold: "majority"),
                commentCount: index == 0 ? 2 : 0
            )))
        }
        records = built
        comments = [
            "sample-0": [
                ProposalCommentDTO(id: "c-1", proposalId: "sample-0", authorId: "ben", authorName: "Ben Ortiz",
                                   body: "Why Apple over Nvidia this week?", createdAt: iso.string(from: now.addingTimeInterval(-1800))),
                ProposalCommentDTO(id: "c-2", proposalId: "sample-0", parentId: "c-1", authorId: "ada", authorName: "Ada Park",
                                   body: "Smaller drawdown for our first buy. Nvidia can be next.", createdAt: iso.string(from: now.addingTimeInterval(-1200))),
            ],
        ]
    }

    func listProposals(groupId: String, tab: ProposalFeedTab) async throws -> [ProposalDTO] {
        records.map(\.proposal).filter { ($0.status == "open") == (tab == .open) }
    }

    func proposal(id: String) async throws -> ProposalDTO {
        guard let record = records.first(where: { $0.proposal.id == id }) else {
            throw MonacoCore.MonacoAPIError.httpStatus(404)
        }
        let votes = record.viewerVoted
            ? [ProposalVoteDTO(voterId: "viewer", displayName: viewerName, choice: "yes")]
            : []
        let p = record.proposal
        return ProposalDTO(
            id: p.id, symbol: p.symbol, status: p.status, kind: p.kind, usdcMicros: p.usdcMicros, tokenAmount: p.tokenAmount, agentDisplayName: p.agentDisplayName, allocationUsdcMicros: p.allocationUsdcMicros, canVote: p.canVote, thesis: p.thesis,
            proposerName: p.proposerName, createdAt: p.createdAt, expiresAt: p.expiresAt,
            votes: votes, voteSummary: p.voteSummary, execution: execution(for: p),
            commentCount: p.commentCount
        )
    }

    /// Passed buys: sample-20 has landed; sample-22 is mid-swap for two reads, then lands.
    private func execution(for p: ProposalDTO) -> ProposalExecutionDTO {
        guard p.status == "passed", p.isTrade else { return ProposalExecutionDTO(state: "not_applicable") }
        let reads = detailReads[p.id, default: 0]
        detailReads[p.id] = reads + 1
        if p.id == "sample-22", reads < 2 { return ProposalExecutionDTO(state: "pending") }
        return ProposalExecutionDTO(state: "confirmed", executedAt: iso.string(from: Date().addingTimeInterval(-600)))
    }

    func castVote(proposalId: String, choice: ProposalVoteChoice) async throws {
        guard let index = records.firstIndex(where: { $0.proposal.id == proposalId }) else {
            throw MonacoCore.MonacoAPIError.httpStatus(404)
        }
        var record = records[index]
        guard record.proposal.canVote == true else { throw MonacoCore.MonacoAPIError.httpStatus(403) }
        let p = record.proposal
        let summary = p.voteSummary ?? ProposalVoteSummaryDTO(yesCount: 0, noCount: 0, eligibleCount: 5, threshold: "majority")
        record.viewerVoted = true
        record.proposal = ProposalDTO(
            id: p.id, symbol: p.symbol, status: p.status, kind: p.kind, usdcMicros: p.usdcMicros, tokenAmount: p.tokenAmount, agentDisplayName: p.agentDisplayName, allocationUsdcMicros: p.allocationUsdcMicros, canVote: false, thesis: p.thesis,
            proposerName: p.proposerName, createdAt: p.createdAt, expiresAt: p.expiresAt,
            voteSummary: ProposalVoteSummaryDTO(
                yesCount: summary.yesCount + (choice == .yes ? 1 : 0),
                noCount: summary.noCount + (choice == .no ? 1 : 0),
                eligibleCount: summary.eligibleCount,
                threshold: summary.threshold
            ),
            commentCount: p.commentCount
        )
        records[index] = record
    }

    func comments(proposalId: String) async throws -> [ProposalCommentDTO] {
        comments[proposalId] ?? []
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
            let p = records[index].proposal
            records[index].proposal = ProposalDTO(
                id: p.id, symbol: p.symbol, status: p.status, kind: p.kind, usdcMicros: p.usdcMicros, tokenAmount: p.tokenAmount, agentDisplayName: p.agentDisplayName, allocationUsdcMicros: p.allocationUsdcMicros, canVote: p.canVote, thesis: p.thesis,
                proposerName: p.proposerName, createdAt: p.createdAt, expiresAt: p.expiresAt,
                voteSummary: p.voteSummary, commentCount: (p.commentCount ?? 0) + 1
            )
        }
        return comment
    }
}

/// Root for the sample-data launch: a labelled feed so screenshots can't be mistaken for live data.
/// Extra arguments open other proposal screens on the same sample data:
/// `-MonacoProposeSample` (a cabal screen with the Propose sheet) and
/// `-MonacoProposalSampleDetail <id>` (one proposal's detail, e.g. `sample-22` for the swap tracker).
struct SampleProposalFeedRoot: View {
    @State private var service = SampleProposalFeedService()

    private var arguments: [String] { ProcessInfo.processInfo.arguments }

    private var detailId: String? {
        guard let index = arguments.firstIndex(of: "-MonacoProposalSampleDetail"), arguments.indices.contains(index + 1) else { return nil }
        return arguments[index + 1]
    }

    var body: some View {
        Group {
            if arguments.contains("-MonacoProposeSample") {
                SampleProposeRoot()
            } else if let detailId {
                NavigationStack {
                    ProposalDetailView(service: service, proposalId: detailId)
                }
            } else {
                NavigationStack {
                    ProposalFeedView(service: service, groupId: "sample", title: "Sample data")
                }
            }
        }
        .tint(MonacoTheme.accent)
    }
}

/// A stand-in cabal screen with the Propose sheet, wired the way Group detail wires it.
private struct SampleProposeRoot: View {
    @State private var service = SampleProposeService()
    @State private var showsPropose = false
    @State private var proposeDetent: PresentationDetent = .medium
    @State private var toast: MonacoToast?

    var body: some View {
        NavigationStack {
            VStack(spacing: MonacoTheme.Space.l) {
                CabalMark(groupId: SampleProposeService.groupView.id, name: SampleProposeService.groupView.name, size: 64)
                Text("Sample data")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                CircleAction("Propose", systemImage: "arrow.up.right") { showsPropose = true }
                    .accessibilityIdentifier("group-action-propose")
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(MonacoTheme.canvas.ignoresSafeArea())
            .navigationTitle(SampleProposeService.groupView.name)
            .navigationBarTitleDisplayMode(.inline)
            .sheet(isPresented: $showsPropose) {
                NavigationStack {
                    ProposeChooserView(
                        service: service,
                        groupId: SampleProposeService.groupView.id,
                        groupView: SampleProposeService.groupView,
                        onProposed: { _ in
                            showsPropose = false
                            Haptics.success()
                            toast = MonacoToast(message: ProposeFlowCopy.proposalSent(SampleProposeService.groupView.name), isSuccess: true)
                        },
                        detent: $proposeDetent
                    )
                }
                .presentationDetents([.medium, .large], selection: $proposeDetent)
            }
            .monacoToast($toast)
        }
        .onAppear {
            if ProcessInfo.processInfo.arguments.contains("-MonacoProposeSampleOpen") { showsPropose = true }
        }
    }
}

/// In-memory propose backend: a cabal with $548.20 in the pot, popular stocks with prices,
/// catalog search, quotes at the listed price, and proposals that always go through.
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

    private let catalog: [ProposeStock] = [
        ProposeStock(symbol: "AAPLx", name: "Apple", priceMicros: 231_400_000, change24h: "0.012"),
        ProposeStock(symbol: "NVDAx", name: "Nvidia", priceMicros: 178_200_000, change24h: "-0.008"),
        ProposeStock(symbol: "TSLAx", name: "Tesla", priceMicros: 342_100_000, change24h: "0.034"),
        ProposeStock(symbol: "MSFTx", name: "Microsoft", priceMicros: 438_900_000, change24h: "0.004"),
        ProposeStock(symbol: "SPYx", name: "S&P 500", priceMicros: 612_300_000, change24h: "0.002"),
        ProposeStock(symbol: "GOOGLx", name: "Alphabet", priceMicros: 201_000_000, change24h: "-0.015"),
        ProposeStock(symbol: "AMBRx", name: "Amber", priceMicros: 12_400_000, change24h: nil, isTradable: false),
    ]

    func pot(groupId: String) async throws -> ProposePot {
        ProposePot(view: Self.groupView)
    }

    func popularStocks() async throws -> [ProposeStock] {
        try await Task.sleep(for: .milliseconds(300))
        return Array(catalog.prefix(6))
    }

    func searchStocks(groupId: String, query: String, offset: Int, limit: Int) async throws -> (stocks: [ProposeStock], hasMore: Bool) {
        let term = query.lowercased()
        let matches = catalog.filter { $0.name.lowercased().contains(term) || $0.ticker.lowercased().contains(term) }
        return (Array(matches.dropFirst(offset).prefix(limit)), false)
    }

    func priceMicros(symbol: String) async throws -> Int64? {
        catalog.first { $0.symbol == symbol }?.priceMicros
    }

    func buyQuote(groupId: String, symbol: String, usdcMicros: Int64) async throws -> BuyQuoteDTO {
        try await Task.sleep(for: .milliseconds(500))
        let price = catalog.first { $0.symbol == symbol }?.priceMicros ?? 100_000_000
        let atomics = Int64((Double(usdcMicros) / Double(price)) * 100_000_000)
        return BuyQuoteDTO(
            symbol: symbol, kind: "buy", usdcMicros: String(usdcMicros), tokenAmount: nil,
            routable: true, outputAmount: String(atomics), outputUsdcMicros: nil, priceUsdcMicros: String(price)
        )
    }

    func sellQuote(groupId: String, symbol: String, tokenAmount: Int64) async throws -> BuyQuoteDTO {
        try await Task.sleep(for: .milliseconds(500))
        let mark = Self.groupView.pot.first { $0.symbol == symbol }.flatMap { Double($0.markUsd) } ?? 1
        let usdc = Int64(Double(tokenAmount) / 100_000_000 * mark * 1_000_000)
        return BuyQuoteDTO(
            symbol: symbol, kind: "sell", usdcMicros: nil, tokenAmount: String(tokenAmount),
            routable: usdc >= 1_000_000, outputAmount: String(usdc), outputUsdcMicros: String(usdc), priceUsdcMicros: nil
        )
    }

    func propose(groupId: String, draft: ProposalDraft) async throws -> String {
        try await Task.sleep(for: .milliseconds(600))
        return UUID().uuidString
    }
}
#endif
