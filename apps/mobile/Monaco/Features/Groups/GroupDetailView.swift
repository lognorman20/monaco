import MonacoCore
import SwiftUI

/// Screens pushed from the group screen's action row and section headers.
enum GroupDetailRoute: Hashable {
    case addMoney
    case cashOut
    case chat
    case proposals
    case activity
}

/// Group screen: hero, action row, open votes, holdings, leaderboard, and activity.
/// Owns loading, polling, leave, retry, and join-request state; `GroupDetailContent` is the layout.
struct GroupDetailView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let groupName: String?
    let initialView: GroupViewDTO?
    var onLeft: () async -> Void = {}

    private let apiClient = MonacoAPIClient()
    // NB: no `@Environment(\.dismiss)` here on purpose — see `DismissWhenActive`.
    @Environment(AppSessionStore.self) private var session: AppSessionStore?

    @State private var groupView: GroupViewDTO?
    @State private var showLeaveConfirmation = false
    @State private var showWithdrawLeaveConfirmation = false
    @State private var isLeaving = false
    /// Idempotency key for the leave request in flight; a retry after a lost response reuses it.
    @State private var leaveSubmission = IdempotentSubmission()
    /// One idempotency key holder per transaction being retried; retries of different rows overlap.
    @State private var retrySubmissions: [String: IdempotentSubmission] = [:]
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
    @State private var proposalService: LiveProposalFeedService
    @State private var proposalRefreshCount = 0
    @State private var route: GroupDetailRoute?
    @State private var showProposeSheet = false
    /// The propose sheet's height; the chooser raises it to `.large` while a flow is pushed.
    @State private var showDetailsSheet = false
    @State private var leaveRequestedFromDetails = false
    @State private var heroScrolledAway = false
    /// Set once leaving succeeded; `DismissWhenActive` pops the screen.
    @State private var hasLeft = false

    /// Whether the open-votes preview has a proposal collecting votes right now.
    @State private var hasOpenVotes = false
    /// Pull-to-refresh and the background poll share it, so a tick stands down while the member
    /// is refreshing by hand.
    @State private var refreshGate = RefreshGate()

    /// Votes land and swaps settle in seconds; a quiet cabal only needs its balances kept current.
    /// A deposit on its way into the pot is watched at the sweep cadence so the pot updates as it lands.
    private var pollInterval: Duration {
        if activityHasPendingDeposits { return DepositPolling.sweepStatusInterval }
        let swapInFlight = activityItems.contains { $0.status.lowercased() == "pending" }
        return hasOpenVotes || swapInFlight ? LiveRefreshCadence.inPlay : LiveRefreshCadence.resting
    }

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
        _proposalService = State(initialValue: LiveProposalFeedService(auth: auth))
    }

    private var displayName: String {
        groupView?.name ?? groupName ?? "Cabal"
    }

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
            .monacoCanvas()
            // The hero carries the name; the bar only shows it once the hero scrolls away.
            .navigationTitle(groupView == nil || heroScrolledAway ? displayName : "")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if groupView != nil {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button {
                            showDetailsSheet = true
                        } label: {
                            Image(systemName: "info.circle")
                        }
                        .accessibilityLabel("Cabal details")
                        .accessibilityIdentifier("group-details-button")
                    }
                }
            }
            .navigationDestination(item: $route) { route in
                destination(for: route)
            }
            .background(DismissWhenActive(isActive: hasLeft))
            .task(id: loadTaskID) {
                if let initialView, groupView == nil {
                    groupView = initialView
                    isLoading = false
                } else {
                    await loadGroup()
                }
                await loadActivity()
                await loadJoinRequests()
            }
            .pollWhileVisible(every: pollInterval, isActive: groupView != nil && !hasLeft, gate: refreshGate) {
                try await pollGroupAndActivity()
            }
            .refreshable {
                await refreshGate.runNow {
                    proposalRefreshCount += 1
                    await loadGroup()
                    await loadActivity()
                    await loadJoinRequests()
                }
            }
            .sheet(isPresented: $showProposeSheet, onDismiss: {
                proposalRefreshCount += 1
            }) {
                if let groupView {
                    ProposeSheet(auth: auth, groupId: groupId, groupView: groupView, onProposed: proposalSent)
                }
            }
            .sheet(isPresented: $showDetailsSheet, onDismiss: presentLeaveConfirmationIfRequested) {
                if let groupView {
                    GroupDetailsSheet(
                        groupId: groupId,
                        treasuryAddress: groupView.treasuryAddress,
                        isLeaving: isLeaving,
                        onLeave: {
                            leaveRequestedFromDetails = true
                            showDetailsSheet = false
                        }
                    )
                }
            }
            .monacoToast($toast)
            .confirmationDialog("Leave \(displayName)?", isPresented: $showLeaveConfirmation, titleVisibility: .visible) {
                Button("Leave cabal", role: .destructive) { Task { await leaveGroup(withdrawStake: false) } }
            } message: {
                Text("You'll lose access to this cabal's votes and chat.")
            }
            .confirmationDialog("Leave \(displayName)?", isPresented: $showWithdrawLeaveConfirmation, titleVisibility: .visible) {
                Button("Sell and leave", role: .destructive) { Task { await leaveGroup(withdrawStake: true) } }
            } message: {
                Text("We'll sell your slice at today's prices and move the cash to your account balance.")
            }
    }

    @ViewBuilder
    private var content: some View {
        if let groupView {
            GroupDetailContent(
                auth: auth,
                view: groupView,
                currentUserId: session?.me?.userId,
                proposalService: proposalService,
                proposalRefreshToken: "\(proposalRefreshCount)",
                onOpenVotesChange: { hasOpenVotes = $0 },
                activityItems: activityItems,
                activityLoading: activityLoading,
                activityError: activityError,
                retryingTransactionIDs: retryingTransactionIDs,
                joinRequests: joinRequests,
                decidingRequestIDs: decidingRequestIDs,
                onRoute: { route = $0 },
                onPropose: { showProposeSheet = true },
                onRetry: { item in Task { await retryTransaction(item) } },
                onDecideJoinRequest: { request, approve in
                    Task { await decideJoinRequest(request, approve: approve) }
                },
                onToast: { toast = $0 },
                onHeroScrolledAway: { heroScrolledAway = $0 }
            )
        } else if let errorMessage {
            statusCard {
                Text(errorMessage)
                    .font(.body)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .multilineTextAlignment(.center)
                Button("Try again") {
                    Task { await loadGroup() }
                }
                .buttonStyle(.monacoSecondary)
            }
            .accessibilityIdentifier("group-detail-error")
        } else if isLoading {
            GroupDetailSkeleton()
        } else {
            statusCard {
                Text("Couldn't load this cabal. Pull down to try again")
                    .font(.body)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .multilineTextAlignment(.center)
                Button("Try again") {
                    Task { await loadGroup() }
                }
                .buttonStyle(.monacoSecondary)
            }
        }
    }

    /// Scrollable so pull-to-refresh works from the error state too.
    private func statusCard<Content: View>(@ViewBuilder content: () -> Content) -> some View {
        ScrollView {
            VStack(spacing: 16) {
                content()
            }
            .padding(24)
            .frame(maxWidth: .infinity)
            .padding(.top, 48)
        }
    }

    @ViewBuilder
    private func destination(for route: GroupDetailRoute) -> some View {
        switch route {
        case .addMoney:
            FundCabalView(
                auth: auth,
                joinedCabals: [HomeGroupBoardRowDTO(
                    groupId: groupId,
                    name: displayName,
                    potValueUsd: groupView?.resolvedPotTotalUsd ?? "0",
                    percentReturn: nil,
                    dollarPnl: groupView?.you.dollarPnl ?? "+0.00",
                    isJoined: true
                )],
                preselectedGroupId: groupId,
                onFunded: {
                    await loadGroup()
                    await loadActivity()
                }
            )
        case .cashOut:
            SellCabalView(
                auth: auth,
                groupId: groupId,
                maxShareUnits: Int64(groupView?.you.shareUnits ?? "") ?? 0,
                equityUsd: groupView?.you.equityUsd ?? "0",
                onSold: {
                    await loadGroup()
                    await loadActivity()
                },
                onToast: { toast = $0 }
            )
        case .chat:
            GroupChatView(auth: auth, groupId: groupId, groupName: displayName)
        case .proposals:
            ProposalFeedView(service: proposalService, groupId: groupId)
        case .activity:
            GroupActivityListView(
                auth: auth,
                items: activityItems,
                retryingTransactionIDs: retryingTransactionIDs,
                onRetry: { item in Task { await retryTransaction(item) } }
            )
        }
    }

    private var loadTaskID: String {
        "\(groupId)-\(auth.accessToken ?? "")"
    }

    /// Called by the propose sheet once the cabal has the proposal: close the sheet and confirm.
    /// Closing the sheet reloads the open votes.
    private func proposalSent(_ proposalId: String) {
        showProposeSheet = false
        Haptics.success()
        toast = MonacoToast(message: "Proposal sent to \(displayName)", isSuccess: true)
    }

    /// The details sheet asks to leave; the confirmation shows once the sheet is gone.
    private func presentLeaveConfirmationIfRequested() {
        guard leaveRequestedFromDetails, let groupView else { return }
        leaveRequestedFromDetails = false
        if hasDeployedStake(in: groupView) {
            showWithdrawLeaveConfirmation = true
        } else {
            showLeaveConfirmation = true
        }
    }

    private func loadGroup() async {
        guard let token = auth.accessToken else {
            isLoading = false
            errorMessage = "Sign in again to see this cabal."
            return
        }

        isLoading = true
        errorMessage = nil
        defer { isLoading = false }

        do {
            groupView = try await apiClient.getGroupView(accessToken: token, groupId: groupId)
        } catch is CancellationError {
            return
        } catch MonacoAPIError.httpStatus {
            if Task.isCancelled { return }
            errorMessage = "Couldn't load this cabal. Pull down to try again"
        } catch {
            if error.isRequestCancellation { return }
            errorMessage = "Couldn't load this cabal. Pull down to try again"
        }
    }

    private func loadActivity(showLoadingIndicator: Bool = true) async {
        guard let token = auth.accessToken else {
            activityLoading = false
            activityError = "Sign in again to see activity."
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
        } catch MonacoAPIError.httpStatus {
            if Task.isCancelled { return }
            activityError = "Couldn't load activity. Pull down to try again"
        } catch {
            if error.isRequestCancellation { return }
            activityError = "Couldn't load activity. Pull down to try again"
        }
    }

    private var activityHasPendingDeposits: Bool {
        activityItems.contains { item in
            item.kind.lowercased() == "deposit" && DepositStatusNormalizer.isPending(item.status)
        }
    }

    /// Background re-read of the cabal and its activity: the pot, the member's slice, holdings,
    /// the leaderboard, and swaps settling. Writes only what changed and never a loading or error
    /// state; a throw leaves the screen as it is and lets the loop back off.
    private func pollGroupAndActivity() async throws {
        guard let token = auth.accessToken, !isLeaving else { return }
        async let viewLoad = apiClient.getGroupView(accessToken: token, groupId: groupId)
        async let activityLoad = apiClient.getGroupActivity(accessToken: token, groupId: groupId)
        let loadedView = try await viewLoad
        let loadedActivity = try? await activityLoad
        guard !Task.isCancelled, !hasLeft else { return }
        QuietUpdate.apply(loadedView, over: groupView) { groupView = $0 }
        if let loadedActivity {
            QuietUpdate.apply(loadedActivity.items, over: activityItems) { activityItems = $0 }
            if activityError != nil { activityError = nil }
            surfaceDepositFailureToasts(from: loadedActivity.items)
        }
    }

    private func surfaceDepositFailureToasts(from items: [GroupActivityItemDTO]) {
        guard let failure = DepositFailureToastTracker.consumeNewFailures(from: items).first else { return }
        toast = MonacoToast(message: DepositFailureToastTracker.message(for: failure))
    }

    private func hasDeployedStake(in view: GroupViewDTO) -> Bool {
        (Int64(view.you.shareUnits) ?? 0) > 0
    }

    private func leaveGroup(withdrawStake: Bool) async {
        guard let token = auth.accessToken, !isLeaving else { return }
        isLeaving = true
        defer { isLeaving = false }
        do {
            try await apiClient.leaveGroup(accessToken: token, groupId: groupId, withdrawStake: withdrawStake, submission: leaveSubmission)
            if withdrawStake {
                toast = MonacoToast(message: "Cash moved to your account balance", isSuccess: true)
            }
            await onLeft()
            hasLeft = true
        } catch MonacoAPIError.leaveBlocked(let reason) {
            toast = MonacoToast(message: leaveBlockedMessage(for: reason))
        } catch MonacoAPIError.httpStatus {
            toast = MonacoToast(message: "Couldn't leave this cabal. Try again")
        } catch {
            toast = MonacoToast(message: "Couldn't leave this cabal. Try again")
        }
    }

    private func leaveBlockedMessage(for reason: LeaveGroupBlockReason) -> String {
        switch reason {
        case .shareUnitsRemaining: return "Cash out your slice first."
        case .lastMemberWithTreasury: return "You're the last member and the pot still has money in it."
        case .pendingRedeem: return "Your cash out is still finishing. Try again in a minute."
        case .soleRemainingVote: return "Vote on the open proposals before you leave."
        case .creatorMustTransfer: return "Hand the cabal to another member before you leave."
        case .unknown: return "You can't leave this cabal right now."
        }
    }

    private func retryTransaction(_ item: GroupActivityItemDTO) async {
        guard let token = auth.accessToken else {
            toast = MonacoToast(message: "Sign in again to retry.")
            return
        }
        guard !retryingTransactionIDs.contains(item.id) else { return }

        retryingTransactionIDs.insert(item.id)
        defer { retryingTransactionIDs.remove(item.id) }

        let submission = retrySubmissions[item.id] ?? IdempotentSubmission()
        retrySubmissions[item.id] = submission

        do {
            let result = try await apiClient.retryTransaction(accessToken: token, transactionId: item.id, submission: submission)
            await loadActivity(showLoadingIndicator: false)
            if result.status.lowercased() == "confirmed" {
                let done = item.kind.lowercased() == "sell" ? "Sold" : "Bought"
                toast = MonacoToast(message: "\(done). Holdings updated", isSuccess: true)
            } else if result.status.lowercased() == "failed" {
                toast = MonacoToast(message: "It didn't go through again. Try later")
            }
        } catch is CancellationError {
            return
        } catch MonacoAPIError.httpStatus(let code) where code == 409 {
            toast = MonacoToast(message: "This one can't be retried")
        } catch MonacoAPIError.httpStatus {
            toast = MonacoToast(message: "Retry didn't go through. Try again")
        } catch {
            toast = MonacoToast(message: "Retry didn't go through. Try again")
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
            toast = MonacoToast(message: "Sign in again to answer requests.")
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
            toast = MonacoToast(message: approve ? "\(request.displayName.isEmpty ? "Member" : request.displayName) is in" : "Request declined", isSuccess: true)
            await loadJoinRequests()
            await loadGroup()
        } catch {
            toast = MonacoToast(message: "Couldn't update the request. Try again")
        }
    }
}

/// Invisible helper that pops its screen once `isActive` turns true.
///
/// It exists so that `GroupDetailView` does not read `@Environment(\.dismiss)` itself.
/// `GroupDetailView` also declares `.navigationDestination(item:)` for the screens its
/// action row pushes, and SwiftUI recomputes a pushed view's `DismissAction` whenever the
/// stack's contents change. Reading both in one body makes pushing a screen invalidate the
/// very body that declares the push, which invalidates the dismiss action again: the group
/// screen spins the main thread instead of navigating, and Add money / Cash out / Chat do
/// nothing. Keeping the dismiss dependency in a leaf that renders nothing confines that
/// churn to a view with no navigation of its own.
///
/// `GroupNavSampleUITests` covers every entry path the product uses; it hangs without this.
private struct DismissWhenActive: View {
    let isActive: Bool
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        Color.clear
            .frame(width: 0, height: 0)
            .accessibilityHidden(true)
            .allowsHitTesting(false)
            .onChange(of: isActive) { _, nowActive in
                if nowActive { dismiss() }
            }
    }
}

/// Scrollable layout of the group screen. Pure: data in, actions out.
struct GroupDetailContent: View {
    @ObservedObject var auth: PrivyAuthService
    let view: GroupViewDTO
    let currentUserId: String?
    let proposalService: ProposalFeedService
    let proposalRefreshToken: String
    var onOpenVotesChange: (Bool) -> Void = { _ in }
    let activityItems: [GroupActivityItemDTO]
    let activityLoading: Bool
    let activityError: String?
    let retryingTransactionIDs: Set<String>
    let joinRequests: [JoinRequestDTO]
    let decidingRequestIDs: Set<String>
    let onRoute: (GroupDetailRoute) -> Void
    let onPropose: () -> Void
    let onRetry: (GroupActivityItemDTO) -> Void
    let onDecideJoinRequest: (JoinRequestDTO, Bool) -> Void
    let onToast: (MonacoToast) -> Void
    var onHeroScrolledAway: (Bool) -> Void = { _ in }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 32) {
                VStack(spacing: 20) {
                    GroupHeroSection(view: view)
                    GroupActionRow(onRoute: onRoute, onPropose: onPropose)
                }

                if !joinRequests.isEmpty {
                    GroupJoinRequestsCard(
                        requests: joinRequests,
                        decidingRequestIDs: decidingRequestIDs,
                        onDecide: onDecideJoinRequest
                    )
                }

                VStack(alignment: .leading, spacing: 0) {
                    ProposalHistorySection(
                        service: proposalService,
                        groupId: view.id,
                        refreshToken: proposalRefreshToken,
                        onOpenVotesChange: onOpenVotesChange,
                        onSeeAll: { onRoute(.proposals) },
                        onToast: onToast
                    )
                    PotSectionView(
                        pot: view.pot,
                        onAddMoney: { onRoute(.addMoney) }
                    )
                }

                if let agent = view.agent {
                    AgentSectionView(agent: agent) {
                        onToast(MonacoToast(message: ProposeFlowCopy.keyCopied, isSuccess: true))
                    }
                }

                MemberBoardSection(members: view.members, currentUserId: currentUserId)

                GroupActivitySection(
                    auth: auth,
                    items: activityItems,
                    isLoading: activityLoading,
                    errorMessage: activityError,
                    retryingTransactionIDs: retryingTransactionIDs,
                    onRetry: onRetry,
                    onSeeAll: { onRoute(.activity) }
                )
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, 8)
            .padding(.bottom, 32)
        }
        .scrollIndicators(.hidden)
        .onScrollGeometryChange(for: Bool.self) { geometry in
            geometry.contentOffset.y + geometry.contentInsets.top > 140
        } action: { _, scrolledAway in
            onHeroScrolledAway(scrolledAway)
        }
    }
}

/// Add money · Propose · Cash out · Chat, directly under the hero.
struct GroupActionRow: View {
    let onRoute: (GroupDetailRoute) -> Void
    let onPropose: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            action("Add money", systemImage: "plus", id: "group-action-fund") { onRoute(.addMoney) }
            action("Propose", systemImage: "arrow.up.right", id: "group-action-propose", perform: onPropose)
            action("Cash out", systemImage: "arrow.down.left", id: "group-action-sell") { onRoute(.cashOut) }
            action("Chat", systemImage: "bubble.left", id: "group-action-chat") { onRoute(.chat) }
        }
    }

    private func action(_ title: String, systemImage: String, id: String, perform: @escaping () -> Void) -> some View {
        CircleAction(title, systemImage: systemImage, action: perform)
            .frame(maxWidth: .infinity)
            .accessibilityIdentifier(id)
    }
}

/// Admin-only: people waiting to join, answered inline.
struct GroupJoinRequestsCard: View {
    let requests: [JoinRequestDTO]
    let decidingRequestIDs: Set<String>
    let onDecide: (JoinRequestDTO, Bool) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            MonacoSectionHeader(requests.count == 1 ? "1 person wants to join" : "\(requests.count) people want to join")
            VStack(spacing: 0) {
                ForEach(requests) { request in
                    let name = request.displayName.isEmpty ? "Member" : request.displayName
                    HStack(spacing: 8) {
                        MonacoAvatar(photoURL: request.profilePhotoUrl, displayName: name, size: 36)
                        Text(name)
                            .font(MonacoTheme.Typo.rowTitle)
                            .foregroundStyle(MonacoTheme.ink)
                            .lineLimit(1)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .layoutPriority(1)
                        Button("Deny") { onDecide(request, false) }
                            .font(.subheadline.weight(.semibold))
                            .lineLimit(1)
                            .fixedSize()
                            .foregroundStyle(MonacoTheme.muted)
                            .frame(minWidth: 44, minHeight: 44)
                            .accessibilityIdentifier("join-request-deny-\(request.id)")
                        Button("Approve") {
                            Haptics.success()
                            onDecide(request, true)
                        }
                            .font(.subheadline.weight(.semibold))
                            .lineLimit(1)
                            .fixedSize()
                            .foregroundStyle(MonacoTheme.primaryButtonLabel)
                            .padding(.horizontal, 14)
                            .frame(minHeight: 36)
                            .background(Capsule().fill(MonacoTheme.primaryButtonFill))
                            .frame(minHeight: 44)
                            .accessibilityIdentifier("join-request-approve-\(request.id)")
                    }
                    .disabled(decidingRequestIDs.contains(request.id))
                    .opacity(decidingRequestIDs.contains(request.id) ? 0.5 : 1)
                    .padding(.vertical, 6)
                }
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous))
        }
        .accessibilityIdentifier("group-join-requests")
    }
}

/// Placeholder shaped like the loaded screen: hero, action row, three rows.
struct GroupDetailSkeleton: View {
    var body: some View {
        VStack(alignment: .leading, spacing: 32) {
            SkeletonBlock(height: 300, radius: MonacoTheme.Radius.hero)
            HStack {
                ForEach(0..<4, id: \.self) { _ in
                    SkeletonBlock(width: 56, height: 56, radius: 28)
                        .frame(maxWidth: .infinity)
                }
            }
            VStack(alignment: .leading, spacing: 12) {
                SkeletonBlock(width: 120, height: 22)
                ForEach(0..<3, id: \.self) { _ in
                    SkeletonBlock(height: 60, radius: 14)
                }
            }
        }
        .padding(.horizontal, MonacoTheme.Space.gutter)
        .padding(.top, 8)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading cabal")
        .accessibilityIdentifier("group-detail-loading")
    }
}
