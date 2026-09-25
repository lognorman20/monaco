import MonacoCore
import SwiftUI

/// How tall the propose sheet may be. On the chooser the member can pull it between `.medium` and
/// `.large`; once a flow is pushed (search, amount with the keyboard, review) the sheet is pinned at
/// `.large`, so dragging content cannot collapse it mid-flow and hide "Add a reason" or Review.
struct ProposeSheetDetents: Equatable {
    var selection: PresentationDetent = .medium
    private(set) var isFlowActive = false

    var allowed: Set<PresentationDetent> {
        isFlowActive ? [.large] : [.medium, .large]
    }

    mutating func flowStarted() {
        isFlowActive = true
        selection = .large
    }

    mutating func returnedToChooser() {
        isFlowActive = false
        selection = .medium
    }
}

/// The whole propose sheet: its navigation stack, the chooser, and the sheet height. Present it from
/// `.sheet { ProposeSheet(…) }`; it owns its detents, so every presentation starts at `.medium` on the
/// chooser and each flow runs at `.large`.
struct ProposeSheet: View {
    private let service: ProposeService
    private let groupId: String
    private let groupView: GroupViewDTO
    private let onProposed: ((_ proposalId: String) -> Void)?

    @State private var detents = ProposeSheetDetents()

    init(auth: PrivyAuthService, groupId: String, groupView: GroupViewDTO, onProposed: ((_ proposalId: String) -> Void)? = nil) {
        self.init(service: LiveProposeService(auth: auth), groupId: groupId, groupView: groupView, onProposed: onProposed)
    }

    init(service: ProposeService, groupId: String, groupView: GroupViewDTO, onProposed: ((_ proposalId: String) -> Void)? = nil) {
        self.service = service
        self.groupId = groupId
        self.groupView = groupView
        self.onProposed = onProposed
    }

    var body: some View {
        NavigationStack {
            ProposeChooserView(service: service, groupId: groupId, groupView: groupView, onProposed: onProposed, detents: $detents)
        }
        .presentationDetents(detents.allowed, selection: $detents.selection)
    }
}

/// The propose sheet's first screen: the three things a member can ask the cabal to do, as ruled
/// rows on the paper. Present it through `ProposeSheet`; each flow pushes onto the sheet's stack
/// and calls `onProposed` with the new proposal id when the cabal has it.
///
/// Each row leads with an ink glyph in a sunken disc, the one the activity list draws for the same
/// kind of thing. It used to be a gold coin with a "+" or a "−" struck on it, and the coin is a
/// stock's mark: a row that is an action read as a stock called "+".
struct ProposeChooserView: View {
    let groupId: String
    let groupView: GroupViewDTO
    var onProposed: ((_ proposalId: String) -> Void)?

    private let service: ProposeService

    /// The presenting sheet's detents: pinned to `.large` while a flow is pushed, released on return.
    private let detents: Binding<ProposeSheetDetents>?

    init(
        auth: PrivyAuthService,
        groupId: String,
        groupView: GroupViewDTO,
        onProposed: ((_ proposalId: String) -> Void)? = nil,
        detents: Binding<ProposeSheetDetents>? = nil
    ) {
        self.init(service: LiveProposeService(auth: auth), groupId: groupId, groupView: groupView, onProposed: onProposed, detents: detents)
    }

    init(
        service: ProposeService,
        groupId: String,
        groupView: GroupViewDTO,
        onProposed: ((_ proposalId: String) -> Void)? = nil,
        detents: Binding<ProposeSheetDetents>? = nil
    ) {
        self.service = service
        self.detents = detents
        self.groupId = groupId
        self.groupView = groupView
        self.onProposed = onProposed
    }

    private var pot: ProposePot {
        ProposePot(view: groupView)
    }

    private var canSell: Bool {
        !pot.holdings.isEmpty
    }

    var body: some View {
        ScrollView {
            // No side padding: the rows run edge to edge, the way every list in the app does.
            MonacoGroupedList {
                NavigationLink {
                    ProposeBuyView(service: service, groupId: groupId, pot: pot, onProposed: onProposed)
                } label: {
                    ChooserRow(
                        title: ProposeFlowCopy.buyRow,
                        detail: ProposeFlowCopy.buyRowDetail,
                        systemImage: ProposeGlyph.buy
                    )
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier("propose-kind-buy")

                NavigationLink {
                    ProposeSellView(service: service, groupId: groupId, pot: pot, onProposed: onProposed)
                } label: {
                    // Disabled, it still says why: the reason is the row's second line, set in the
                    // readable caption colour, while the title greys out.
                    ChooserRow(
                        title: ProposeFlowCopy.sellRow,
                        detail: canSell ? sellDetail : ProposeFlowCopy.sellRowEmpty,
                        systemImage: ProposeGlyph.sell,
                        isEnabled: canSell,
                        isLast: !showsAgentRows
                    )
                }
                .buttonStyle(.monacoRow)
                .disabled(!canSell)
                .accessibilityIdentifier("propose-kind-sell")

                agentRows
            }
            .padding(.top, MonacoTheme.Space.s)
            .padding(.bottom, MonacoTheme.Space.l)
        }
        .scrollBounceBehavior(.basedOnSize)
        .background(MonacoTheme.canvas.ignoresSafeArea())
        .navigationTitle(ProposeFlowCopy.chooserTitle)
        .navigationBarTitleDisplayMode(.inline)
        // The chooser disappears only when a flow is pushed over it (or the sheet closes, which
        // discards the sheet's state), and appears again when the member comes back to it.
        .onAppear { updateDetents { $0.returnedToChooser() } }
        .onDisappear { updateDetents { $0.flowStarted() } }
        .accessibilityIdentifier("propose-chooser")
    }

    private func updateDetents(_ change: (inout ProposeSheetDetents) -> Void) {
        guard let detents else { return }
        var next = detents.wrappedValue
        change(&next)
        guard next != detents.wrappedValue else { return }
        withAnimation(.snappy) { detents.wrappedValue = next }
    }

    private var sellDetail: String {
        ProposeScreenCopy.sellRowDetail(names: pot.holdings.map { ProposeStock.displayName(symbol: $0.symbol) })
    }

    private var agentStatus: String? {
        groupView.agent?.status.lowercased()
    }

    private var showsAgentRows: Bool {
        groupView.agent == nil || agentStatus == "active" || agentStatus == "paused"
    }

    @ViewBuilder
    private var agentRows: some View {
        if let agent = groupView.agent {
            if agentStatus == "active" || agentStatus == "paused" {
                let kind = agentStatus == "active" ? "pause_agent" : "resume_agent"
                NavigationLink {
                    ProposeAgentLifecycleView(service: service, groupId: groupId, kind: kind, botName: agent.agentDisplayName, onProposed: onProposed)
                } label: {
                    ChooserRow(
                        title: kind == "pause_agent" ? ProposeFlowCopy.pauseBotRow : ProposeFlowCopy.resumeBotRow,
                        detail: agent.agentDisplayName,
                        systemImage: ProposeGlyph.lifecycle(kind)
                    )
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier(kind == "pause_agent" ? "propose-kind-pause-agent" : "propose-kind-resume-agent")

                NavigationLink {
                    ProposeAgentLifecycleView(service: service, groupId: groupId, kind: "revoke_agent", botName: agent.agentDisplayName, onProposed: onProposed)
                } label: {
                    ChooserRow(
                        title: ProposeFlowCopy.removeBotRow,
                        detail: agent.agentDisplayName,
                        systemImage: ProposeGlyph.lifecycle("revoke_agent"),
                        isLast: true
                    )
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier("propose-kind-revoke-agent")
            }
        } else {
            NavigationLink {
                ProposeAddAgentView(service: service, groupId: groupId, pot: pot, onProposed: onProposed)
            } label: {
                ChooserRow(
                    title: ProposeFlowCopy.addBotRow,
                    detail: ProposeFlowCopy.addBotRowDetail,
                    systemImage: ProposeGlyph.bot,
                    isLast: true
                )
            }
            .buttonStyle(.monacoRow)
            .accessibilityIdentifier("propose-kind-add-agent")
        }
    }
}

/// One chooser row: the glyph, the title over one caption line, the chevron. Titles wrap rather
/// than truncate: "Sell something the cabal owns" does not fit one line on a small phone, and at
/// the accessibility sizes nothing here should be cut off.
private struct ChooserRow: View {
    let title: String
    let detail: String
    let systemImage: String
    var isEnabled = true
    var isLast = false

    var body: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            ProposeGlyph(systemImage: systemImage, isEnabled: isEnabled)
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(isEnabled ? MonacoTheme.ink : MonacoTheme.disabledLabel)
                    .fixedSize(horizontal: false, vertical: true)
                Text(detail)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if isEnabled {
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(MonacoTheme.tertiaryText)
                    .accessibilityHidden(true)
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, 10)
        .frame(minHeight: 64)
        .contentShape(Rectangle())
        .overlay(alignment: .bottom) {
            if !isLast {
                MonacoRule().padding(.leading, MonacoTheme.Space.m + ProposeGlyph.rowSize + MonacoTheme.Space.sm)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// The mark for a row that is an action rather than a stock or a cabal: an ink symbol in a sunken
/// disc. The activity list draws the same disc at 32pt for what already happened; a row the member
/// can tap draws it at 40pt.
struct ProposeGlyph: View {
    let systemImage: String
    var size: CGFloat = ProposeGlyph.rowSize
    var isEnabled = true

    /// The chooser's disc.
    static let rowSize: CGFloat = 40
    /// A line that explains rather than acts, the size of an activity row's glyph.
    static let noteSize: CGFloat = 32

    /// Buy and sell take the activity list's own glyphs, so a buy looks the same when it is
    /// proposed and when it lands in the cabal's history.
    static var buy: String { GroupActivityRules.glyph(for: "buy") }
    static var sell: String { GroupActivityRules.glyph(for: "sell") }
    static let bot = "cpu"

    static func lifecycle(_ kind: String) -> String {
        switch kind {
        case "pause_agent": "pause"
        case "resume_agent": "play"
        default: "xmark"
        }
    }

    var body: some View {
        Image(systemName: systemImage)
            .font(.system(size: (size * 0.4).rounded(), weight: .semibold))
            .foregroundStyle(isEnabled ? MonacoTheme.ink : MonacoTheme.disabledLabel)
            .frame(width: size, height: size)
            .background(Circle().fill(MonacoTheme.surfaceSunken))
            .accessibilityHidden(true)
    }
}

/// Words the propose screens add to `ProposeFlowCopy`, which lives in MonacoCore and is shared with
/// the proposal screens. Kept in one place and audited against `MainFlowCopyAudit` by
/// `ProposeRedesignTests`, for the same reason that table is.
enum ProposeScreenCopy {
    /// The sell row's second line: what the cabal could sell, by name. Three names at most, then
    /// how many more, so a long pot never silently drops a holding from the sentence.
    static func sellRowDetail(names: [String]) -> String {
        let shown = names.prefix(3).joined(separator: ", ")
        let more = names.count - 3
        guard more > 0 else { return shown }
        return "\(shown) and \(more) more"
    }

    // MARK: Pick a stock

    /// A stock that cannot be bought keeps its name and says why, in place of the name alone.
    static func cantBuyCaption(name: String) -> String {
        "\(name) · \(ProposeFlowCopy.cantBuy)"
    }

    // MARK: Receipt

    static let getsRow = "Cabal gets"
    static let potRow = "Pot"
    static let whoVotesRow = "Who votes"
    static let raisesRow = "Raises"
    static let keepsRow = "Cabal keeps"
    static let keepsNothing = "None"

    /// An estimate from the price check, marked as one: "about 0.108 shares".
    static func about(_ figure: String) -> String {
        "about \(figure)"
    }

    /// "Buy $25.00 of AAPL": the exact thing the cabal votes on, in dollars.
    static func buyHeadline(amount: String, ticker: String) -> String {
        ProposalFeedCopy.buyHeadline(symbol: ticker, amount: amount)
    }

    /// "Sell 0.6017 shares of AAPL": a sell is a number of shares, so that is the headline and
    /// the dollars it raises are the estimate underneath.
    static func sellHeadline(shares: String, ticker: String) -> String {
        "Sell \(shares) of \(ticker)"
    }

    /// How much of the pot a buy spends: "4.6% of $548.20". Nil without a pot to measure against.
    static func potShare(amountMicros: Int64, potMicros: Int64) -> String? {
        guard potMicros > 0, amountMicros > 0 else { return nil }
        // To the tenth, and whole numbers without the ".0". Not rounded to whole percents above
        // 10, the way a slice is: 99.6% of the pot is not "100%" on the receipt a vote rests on.
        let tenths = (Double(amountMicros) / Double(potMicros) * 1000).rounded() / 10
        let label: String
        if tenths < 0.1 {
            label = "under 0.1%"
        } else if tenths == tenths.rounded() {
            label = String(format: "%.0f%%", tenths)
        } else {
            label = String(format: "%.1f%%", tenths)
        }
        return "\(label) of \(UsdAmountFormatter.format(micros: potMicros))"
    }

    /// The reason's header on a receipt: the words the proposal's own screen heads it with.
    static func reasonTitle(isSell: Bool) -> String {
        isSell ? "Why sell" : "Why buy"
    }

    /// What the cabal still holds after a sell, in shares; "None" when the sell takes all of it.
    static func keeps(heldAtomics: Int64, soldAtomics: Int64) -> String {
        let left = heldAtomics - soldAtomics
        guard left > 0 else { return keepsNothing }
        return ProposalShareFormatter.sharesLabel(fromAtomics: String(left))
    }

    // MARK: Pick a cabal

    /// "Which cabal should buy " before the ticker, "?" after it: the ticker sets in the market's
    /// voice between them.
    static func pickerQuestion(kind: ProposalPickKind) -> (lead: String, tail: String) {
        switch kind {
        case .buy: ("Which cabal should buy ", "?")
        case .sell: ("Which cabal should sell ", "?")
        }
    }

    static let inThePot = "in the pot"

    // MARK: Trading bot

    static let botRulesTitle = "How it works"
    static let botTrades = "It buys and sells stocks for the cabal, only inside its budget."
    static let botAnswers = "Anyone in the cabal can propose pausing or removing it."

    /// Every string above, with representative arguments, for the copy audit.
    static let auditedStrings: [String] = [
        sellRowDetail(names: ["Apple", "Nvidia", "Tesla", "Microsoft"]),
        cantBuyCaption(name: "Amber"),
        getsRow, potRow, whoVotesRow, raisesRow, keepsRow, keepsNothing,
        about("0.108 shares"),
        buyHeadline(amount: "$25.00", ticker: "AAPL"),
        sellHeadline(shares: "0.6017 shares", ticker: "AAPL"),
        potShare(amountMicros: 25_000_000, potMicros: 548_200_000) ?? "",
        keeps(heldAtomics: 120_340_000, soldAtomics: 60_170_000),
        reasonTitle(isSell: false), reasonTitle(isSell: true),
        pickerQuestion(kind: .buy).lead + "AAPL" + pickerQuestion(kind: .buy).tail,
        pickerQuestion(kind: .sell).lead + "AAPL" + pickerQuestion(kind: .sell).tail,
        inThePot, botRulesTitle, botTrades, botAnswers,
    ]
}
