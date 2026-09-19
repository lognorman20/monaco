import MonacoCore
import SwiftUI

/// Sheet content for Group detail's "Propose" action. The presenter wraps it in a
/// `NavigationStack` with `.presentationDetents([.medium, .large])`; each flow pushes onto that stack
/// and calls `onProposed` with the new proposal id when the cabal has it.
struct ProposeChooserView: View {
    let groupId: String
    let groupView: GroupViewDTO
    var onProposed: ((_ proposalId: String) -> Void)?

    private let service: ProposeService

    init(auth: PrivyAuthService, groupId: String, groupView: GroupViewDTO, onProposed: ((_ proposalId: String) -> Void)? = nil) {
        self.init(service: LiveProposeService(auth: auth), groupId: groupId, groupView: groupView, onProposed: onProposed)
    }

    init(service: ProposeService, groupId: String, groupView: GroupViewDTO, onProposed: ((_ proposalId: String) -> Void)? = nil) {
        self.service = service
        self.groupId = groupId
        self.groupView = groupView
        self.onProposed = onProposed
    }

    private var pot: ProposePot {
        ProposePot(view: groupView)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                MonacoGroupedList {
                    NavigationLink {
                        ProposeBuyView(service: service, groupId: groupId, pot: pot, onProposed: onProposed)
                    } label: {
                        ChooserRow(title: ProposeFlowCopy.buyRow, detail: ProposeFlowCopy.buyRowDetail, systemImage: "plus")
                    }
                    .buttonStyle(.monacoRow)
                    .accessibilityIdentifier("propose-kind-buy")

                    NavigationLink {
                        ProposeSellView(service: service, groupId: groupId, pot: pot, onProposed: onProposed)
                    } label: {
                        ChooserRow(
                            title: ProposeFlowCopy.sellRow,
                            detail: pot.holdings.isEmpty ? ProposeFlowCopy.sellRowEmpty : sellDetail,
                            systemImage: "minus",
                            isEnabled: !pot.holdings.isEmpty,
                            isLast: !showsAgentRows
                        )
                    }
                    .buttonStyle(.monacoRow)
                    .disabled(pot.holdings.isEmpty)
                    .accessibilityIdentifier("propose-kind-sell")

                    agentRows
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, MonacoTheme.Space.s)
            .padding(.bottom, MonacoTheme.Space.l)
        }
        .scrollBounceBehavior(.basedOnSize)
        .background(MonacoTheme.canvas.ignoresSafeArea())
        .navigationTitle(ProposeFlowCopy.chooserTitle)
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("propose-chooser")
    }

    private var sellDetail: String {
        let names = pot.holdings.prefix(3).map { ProposeStock.displayName(symbol: $0.symbol) }
        return names.joined(separator: ", ")
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
                        systemImage: kind == "pause_agent" ? "pause" : "play"
                    )
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier(kind == "pause_agent" ? "propose-kind-pause-agent" : "propose-kind-resume-agent")

                NavigationLink {
                    ProposeAgentLifecycleView(service: service, groupId: groupId, kind: "revoke_agent", botName: agent.agentDisplayName, onProposed: onProposed)
                } label: {
                    ChooserRow(title: ProposeFlowCopy.removeBotRow, detail: agent.agentDisplayName, systemImage: "xmark", isLast: true)
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier("propose-kind-revoke-agent")
            }
        } else {
            NavigationLink {
                ProposeAddAgentView(service: service, groupId: groupId, pot: pot, onProposed: onProposed)
            } label: {
                ChooserRow(title: ProposeFlowCopy.addBotRow, detail: ProposeFlowCopy.addBotRowDetail, systemImage: "cpu", isLast: true)
            }
            .buttonStyle(.monacoRow)
            .accessibilityIdentifier("propose-kind-add-agent")
        }
    }
}

/// One large chooser row: glyph tile, title, one muted line, chevron.
private struct ChooserRow: View {
    let title: String
    let detail: String
    let systemImage: String
    var isEnabled = true
    var isLast = false

    var body: some View {
        MonacoRow(title: title, subtitle: detail, chevron: isEnabled, isLast: isLast) {
            StockMark(systemImage: systemImage)
        }
        .opacity(isEnabled ? 1 : 0.45)
    }
}
