import SwiftUI

/// Group screen: pot, you slice, member board, and actions.
struct GroupDetailView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let initialView: GroupViewDTO?

    private let apiClient = MonacoAPIClient()

    @State private var groupView: GroupViewDTO?
    @State private var errorMessage: String?
    @State private var isLoading = false

    init(auth: PrivyAuthService, groupId: String, initialView: GroupViewDTO? = nil) {
        self.auth = auth
        self.groupId = groupId
        self.initialView = initialView
    }

    var body: some View {
        Group {
            if isLoading && groupView == nil {
                ProgressView("Loading club…")
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .tint(MonacoTheme.accent)
            } else if let groupView {
                groupContent(groupView)
            } else if let errorMessage {
                VStack(alignment: .leading, spacing: 12) {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(MonacoTheme.warning)
                    Button("Try again") {
                        Task { await loadGroup() }
                    }
                    .buttonStyle(.monacoPrimary)
                }
                .padding()
                .monacoSurfaceCard()
                .padding()
            }
        }
        .navigationTitle(groupView?.name ?? "Club")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            if let initialView {
                groupView = initialView
            } else {
                await loadGroup()
            }
        }
        .refreshable {
            await loadGroup()
        }
    }

    @ViewBuilder
    private func groupContent(_ view: GroupViewDTO) -> some View {
        List {
            PotSectionView(pot: view.pot)
            YouSectionView(slice: view.you)
            MemberBoardSection(members: view.members)

            Section("Actions") {
                NavigationLink {
                    DepositView(auth: auth, groupId: groupId)
                } label: {
                    Label("Add money", systemImage: "plus.circle")
                }
                .accessibilityIdentifier("group-action-deposit")

                NavigationLink {
                    ProposeBuyView(auth: auth, groupId: groupId)
                } label: {
                    Label("Propose buy", systemImage: "chart.line.uptrend.xyaxis")
                }
                .accessibilityIdentifier("group-action-propose")
            }

            if let proposals = view.proposals, !proposals.isEmpty {
                Section("Proposals") {
                    ForEach(proposals) { proposal in
                        NavigationLink {
                            ProposalDetailView(auth: auth, proposal: proposal)
                        } label: {
                            HStack {
                                Text(proposal.symbol)
                                Spacer()
                                ProposalStatusChip(status: proposal.status)
                            }
                        }
                    }
                }
            }
        }
        .monacoInsetList()
        .background(MonacoTheme.background)
    }

    private func loadGroup() async {
        guard let token = auth.accessToken else { return }
        isLoading = true
        errorMessage = nil
        defer { isLoading = false }

        do {
            groupView = try await apiClient.getGroupView(accessToken: token, groupId: groupId)
        } catch MonacoAPIError.httpStatus(let code) {
            errorMessage = "Could not load club (HTTP \(code))."
        } catch {
            errorMessage = "Could not load club."
        }
    }
}
