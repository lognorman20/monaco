import SwiftUI

/// Group screen: pot, you slice, member board, activity, proposals, and actions.
struct GroupDetailView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let groupName: String?
    let initialView: GroupViewDTO?
    var onLeft: () async -> Void = {}

    private let apiClient = MonacoAPIClient()
    @Environment(\.dismiss) private var dismiss

    @State private var groupView: GroupViewDTO?
    @State private var showLeaveConfirmation = false
    @State private var isLeaving = false
    @State private var activityItems: [GroupActivityItemDTO] = []
    @State private var activityLoading = true
    @State private var activityError: String?
    @State private var retryingTransactionIDs: Set<String> = []
    @State private var errorMessage: String?
    @State private var toast: MonacoToast?
    @State private var isLoading: Bool
    @State private var joinRequests: [JoinRequestDTO] = []
    @State private var joinRequestsLoading = false
    @State private var decidingRequestIDs: Set<String> = []

    private let activityPollInterval: Duration = .seconds(15)

    init(
        auth: PrivyAuthService,
        groupId: String,
        groupName: String? = nil,
        initialView: GroupViewDTO? = nil,
        onLeft: @escaping () async -> Void = {}
    ) {
        self.auth = auth
        self.groupId = groupId
        self.groupName = groupName
        self.initialView = initialView
        self.onLeft = onLeft
        _isLoading = State(initialValue: initialView == nil)
    }

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
            .background(MonacoTheme.background)
            .navigationTitle(groupView?.name ?? groupName ?? "Cabal")
            .navigationBarTitleDisplayMode(.inline)
            .task(id: loadTaskID) {
                if let initialView, groupView == nil {
                    groupView = initialView
                    isLoading = false
                } else {
                    await loadGroup()
                }
                await loadActivity()
                await loadJoinRequests()
                await pollActivityWhileVisible()
            }
            .refreshable {
                await loadGroup()
                await loadActivity()
                await loadJoinRequests()
            }
            .monacoToast($toast)
            .confirmationDialog("Leave this cabal?", isPresented: $showLeaveConfirmation, titleVisibility: .visible) {
                Button("Leave cabal", role: .destructive) { Task { await leaveGroup() } }
            } message: {
                Text("You will lose access to this cabal's board. Your deposit history stays on record.")
            }
    }

    @ViewBuilder
    private var content: some View {
        if let groupView {
            groupContent(groupView)
        } else if let errorMessage {
            statusCard {
                Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                    .foregroundStyle(MonacoTheme.warning)
                Button("Try again") {
                    Task { await loadGroup() }
                }
                .buttonStyle(.monacoPrimary)
            }
        } else if isLoading {
            ProgressView("Loading cabal…")
                .foregroundStyle(MonacoTheme.secondaryText)
                .tint(MonacoTheme.accent)
        } else {
            statusCard {
                Text("Could not load cabal.")
                    .foregroundStyle(MonacoTheme.secondaryText)
                Button("Try again") {
                    Task { await loadGroup() }
                }
                .buttonStyle(.monacoPrimary)
            }
        }
    }

    private func statusCard<Content: View>(@ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            content()
        }
        .padding()
        .monacoSurfaceCard()
        .padding()
    }

    private var loadTaskID: String {
        "\(groupId)-\(auth.accessToken ?? "")"
    }

    @ViewBuilder
    private func groupContent(_ view: GroupViewDTO) -> some View {
        List {
            PotSectionView(
                potTotalUsd: view.resolvedPotTotalUsd,
                pot: view.pot,
                treasuryAddress: view.treasuryAddress
            )
            YouSectionView(slice: view.you)
            MemberBoardSection(members: view.members)

            if joinRequestsLoading || !joinRequests.isEmpty {
                Section("Join requests") {
                    if joinRequestsLoading && joinRequests.isEmpty { ProgressView("Loading requests…") }
                    ForEach(joinRequests) { request in
                        VStack(alignment: .leading, spacing: 8) {
                            Text(request.displayName.isEmpty ? "Member" : request.displayName).font(.headline)
                            HStack {
                                Button("Approve") { Task { await decideJoinRequest(request, approve: true) } }
                                    .disabled(decidingRequestIDs.contains(request.id))
                                Button("Deny", role: .destructive) { Task { await decideJoinRequest(request, approve: false) } }
                                    .disabled(decidingRequestIDs.contains(request.id))
                            }
                        }
                    }
                }
            }

            GroupActivitySection(
                auth: auth,
                items: activityItems,
                isLoading: activityLoading,
                errorMessage: activityError,
                retryingTransactionIDs: retryingTransactionIDs,
                onRetry: { item in
                    Task { await retryTransaction(item) }
                }
            )

            ProposalHistorySection(auth: auth, groupId: groupId)

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
                Button(role: .destructive) { showLeaveConfirmation = true } label: {
                    Label(isLeaving ? "Leaving…" : "Leave cabal", systemImage: "rectangle.portrait.and.arrow.right")
                }
                .disabled(isLeaving)
                .accessibilityIdentifier("group-action-leave")
            }
        }
        .monacoInsetList()
        .background(MonacoTheme.background)
    }

    private func loadGroup() async {
        guard let token = auth.accessToken else {
            isLoading = false
            errorMessage = "Missing sign-in token."
            return
        }

        isLoading = true
        errorMessage = nil
        defer { isLoading = false }

        do {
            groupView = try await apiClient.getGroupView(accessToken: token, groupId: groupId)
        } catch is CancellationError {
            return
        } catch MonacoAPIError.httpStatus(let code) {
            errorMessage = "Could not load cabal (HTTP \(code))."
        } catch {
            errorMessage = "Could not load cabal."
        }
    }

    private func loadActivity(showLoadingIndicator: Bool = true) async {
        guard let token = auth.accessToken else {
            activityLoading = false
            activityError = "Missing sign-in token."
            return
        }

        if showLoadingIndicator {
            activityLoading = true
        }
        activityError = nil
        defer {
            if showLoadingIndicator {
                activityLoading = false
            }
        }

        do {
            let response = try await apiClient.getGroupActivity(accessToken: token, groupId: groupId)
            activityItems = response.items
            surfaceDepositFailureToasts(from: response.items)
        } catch is CancellationError {
            return
        } catch MonacoAPIError.httpStatus(let code) {
            activityError = "Could not load activity (HTTP \(code))."
        } catch {
            activityError = "Could not load activity."
        }
    }

    private func pollActivityWhileVisible() async {
        while !Task.isCancelled {
            try? await Task.sleep(for: activityPollInterval)
            guard !Task.isCancelled else { return }
            await loadActivity(showLoadingIndicator: false)
        }
    }

    private func surfaceDepositFailureToasts(from items: [GroupActivityItemDTO]) {
        guard let failure = DepositFailureToastTracker.consumeNewFailures(from: items).first else { return }
        toast = MonacoToast(message: DepositFailureToastTracker.message(for: failure))
    }

    private func leaveGroup() async {
        guard let token = auth.accessToken, !isLeaving else { return }
        isLeaving = true
        defer { isLeaving = false }
        do {
            try await apiClient.leaveGroup(accessToken: token, groupId: groupId)
            await onLeft()
            dismiss()
        } catch MonacoAPIError.leaveBlocked(let reason) {
            toast = MonacoToast(message: leaveBlockedMessage(for: reason))
        } catch MonacoAPIError.httpStatus(let code) {
            toast = MonacoToast(message: "Could not leave cabal (HTTP \(code)).")
        } catch {
            toast = MonacoToast(message: "Could not leave cabal.")
        }
    }

    private func leaveBlockedMessage(for reason: LeaveGroupBlockReason) -> String {
        switch reason {
        case .shareUnitsRemaining: return "Redeem your slice before leaving."
        case .lastMemberWithTreasury: return "You are the only member and the treasury still holds value."
        case .pendingRedeem: return "Finish your pending redeem before leaving."
        case .soleRemainingVote: return "Cast your vote on open proposals before leaving."
        case .creatorMustTransfer: return "Transfer cabal ownership before leaving."
        case .unknown: return "You cannot leave this cabal right now."
        }
    }

    private func retryTransaction(_ item: GroupActivityItemDTO) async {
        guard let token = auth.accessToken else {
            toast = MonacoToast(message: "Missing sign-in token.")
            return
        }
        guard !retryingTransactionIDs.contains(item.id) else { return }

        retryingTransactionIDs.insert(item.id)
        defer { retryingTransactionIDs.remove(item.id) }

        do {
            let result = try await apiClient.retryTransaction(accessToken: token, transactionId: item.id)
            await loadActivity(showLoadingIndicator: false)
            if result.status.lowercased() == "confirmed" {
                toast = MonacoToast(message: "Swap confirmed.")
            } else if result.status.lowercased() == "failed" {
                toast = MonacoToast(message: "Swap failed again. Try later.")
            }
        } catch is CancellationError {
            return
        } catch MonacoAPIError.httpStatus(let code) where code == 409 {
            toast = MonacoToast(message: "This swap can't be retried.")
        } catch MonacoAPIError.httpStatus(let code) {
            toast = MonacoToast(message: "Retry failed (HTTP \(code)).")
        } catch {
            toast = MonacoToast(message: "Retry failed. Try again.")
        }
    }

    private func loadJoinRequests() async {
        guard let token = auth.accessToken else { return }
        joinRequestsLoading = true
        defer { joinRequestsLoading = false }
        do {
            joinRequests = try await apiClient.listJoinRequests(accessToken: token, groupId: groupId)
        } catch MonacoAPIError.httpStatus(403) {
            joinRequests = []
        } catch {
            joinRequests = []
        }
    }

    private func decideJoinRequest(_ request: JoinRequestDTO, approve: Bool) async {
        guard let token = auth.accessToken else {
            toast = MonacoToast(message: "Missing sign-in token.")
            return
        }
        guard !decidingRequestIDs.contains(request.id) else { return }
        decidingRequestIDs.insert(request.id)
        defer { decidingRequestIDs.remove(request.id) }
        do {
            if approve {
                try await apiClient.approveJoinRequest(accessToken: token, groupId: groupId, requestId: request.id)
            } else {
                try await apiClient.denyJoinRequest(accessToken: token, groupId: groupId, requestId: request.id)
            }
            toast = MonacoToast(message: approve ? "Member approved." : "Join request denied.")
            await loadJoinRequests()
            await loadGroup()
        } catch {
            toast = MonacoToast(message: "Could not update request.")
        }
    }
}
