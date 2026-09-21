import MonacoCore
import SwiftUI

/// Screens pushed from the group screen's action row and section headers.
enum GroupDetailRoute: Hashable {
    case addMoney
    /// Cash out, carrying the slice as it stood when the member tapped.
    ///
    /// The figure travels in the route rather than being read live from the cabal, because
    /// cashing everything out turns the slice to nothing: a screen reading it live rewrites
    /// itself as "Nothing to cash out yet" at the very moment it succeeds, a blink before it
    /// dismisses.
    case cashOut(shareUnits: Int64, equityUsd: String)
    case chat
    case proposals
    case activity
}

/// What a refresh of the cabal screen is allowed to show while it runs.
private enum GroupDetailRefreshMode {
    /// The screen has nothing yet: a skeleton while it waits, and an error if it fails.
    case initial
    /// The member pulled down. No skeleton over content they can already see, but a failure is
    /// theirs to hear about.
    case userInitiated
    /// Nobody asked. Write what changed, say nothing, and leave the screen alone on failure.
    case quiet
}

/// Group screen: hero, action row, open votes, holdings, leaderboard, and activity.
/// Owns loading, polling, leave, retry, and join-request state; `GroupDetailContent` is the layout.
struct GroupDetailView: View {
    @ObservedObject var auth: DynamicAuthService
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
    /// Which leave is running, so the progress cover can say whether the slice is being sold.
    @State private var leavingSellsSlice = false
    @State private var activityItems: [GroupActivityItemDTO] = []
    @State private var activityLoading = true
    @State private var activityError: String?
    @State private var retryingTransactionIDs: Set<String> = []
    @State private var errorMessage: String?
    @State private var toast: MonacoToast?
    @State private var isLoading: Bool
    /// The blocking first load has run at least once; re-appearing is the poll loop's job.
    @State private var didInitialLoad = false
    @State private var joinRequests: [JoinRequestDTO] = []
    /// Cleared the first time the server refuses the admin-only list, so a plain member's screen
    /// stops sending a request it already knows will be refused on every load and every tick.
    @State private var viewerMayBeAdmin = true
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
    /// A vote closed within the settling window, so its outcome is still on its way to the pot.
    @State private var isWatchingVoteOutcome = false
    /// Bumped each time the last open vote closes; drives the settling-window timer.
    @State private var voteOutcomeWatch = 0
    /// Pull-to-refresh and the background poll share it, so a tick stands down while the member
    /// is refreshing by hand.
    @State private var refreshGate = RefreshGate()

    /// Votes land, swaps settle and deposits reach the pot in seconds; a quiet cabal only needs
    /// its balances kept current.
    private var pollInterval: Duration {
        GroupDetailCadence.interval(for: GroupDetailCadence.Inputs(
            hasOpenVotes: hasOpenVotes,
            hasPendingActivity: activityItems.contains { GroupDetailCadence.isStillGoingThrough(status: $0.status) },
            isWatchingVoteOutcome: isWatchingVoteOutcome
        ))
    }

    init(
        auth: DynamicAuthService,
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
            .groupLeaveProgress(isLeaving: isLeaving, isSellingSlice: leavingSellsSlice)
            // The hero carries the name; the bar only shows it once the hero scrolls away.
            .navigationTitle(groupView == nil || heroScrolledAway ? displayName : "")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if groupView != nil, !isLeaving {
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
            // The first appearance loads; coming back from a pushed screen does not. The poll
            // loop below already knows how stale its data is and ticks straight away when it is.
            .task(id: loadTaskID) {
                if let initialView, groupView == nil {
                    groupView = initialView
                    isLoading = false
                }
                guard !didInitialLoad || groupView == nil else { return }
                didInitialLoad = true
                try? await refreshGate.runNow { try await refresh(.initial) }
            }
            .pollWhileVisible(every: pollInterval, isActive: groupView != nil && !hasLeft, gate: refreshGate) {
                try await refresh(.quiet)
            }
            // A vote that just closed is still landing: keep watching closely for a little while.
            .task(id: voteOutcomeWatch) {
                guard voteOutcomeWatch > 0 else { return }
                isWatchingVoteOutcome = true
                defer { isWatchingVoteOutcome = false }
                try? await Task.sleep(for: GroupDetailCadence.voteSettlingWindow)
            }
            .refreshable {
                await refreshGate.runNow {
                    proposalRefreshCount += 1
                    try? await refresh(.userInitiated)
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
                onOpenVotesChange: openVotesChanged,
                activityItems: activityItems,
                activityLoading: activityLoading,
                activityError: activityError,
                retryingTransactionIDs: retryingTransactionIDs,
                joinRequests: joinRequests,
                decidingRequestIDs: decidingRequestIDs,
                onRoute: { route = $0 },
                onPropose: { showProposeSheet = true },
                onRetry: { item in await retryTransaction(item) },
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
                    Task { await refreshGate.runNow { try? await refresh(.initial) } }
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
                    Task { await refreshGate.runNow { try? await refresh(.initial) } }
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
                onFunded: { await refreshQuietly() }
            )
        case .cashOut(let shareUnits, let equityUsd):
            SellCabalView(
                auth: auth,
                groupId: groupId,
                maxShareUnits: shareUnits,
                equityUsd: equityUsd,
                onSold: { await refreshQuietly() },
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
                onRetry: { item in await retryTransaction(item) }
            )
        }
    }

    private var loadTaskID: String {
        // Keyed on who is signed in, not on the token: Dynamic rotates the token under a
        // session that has not changed, and that must not reload the cabal.
        "\(groupId)-\(auth.sessionIdentity ?? "")"
    }

    /// The open-votes preview reporting what it is showing.
    ///
    /// A proposal leaves the open list the moment it passes, which is exactly when the swap it
    /// decided starts. Read the cabal straight away and keep watching closely for a while, so the
    /// pot, holdings and activity move while the member is still looking at the vote they cast.
    private func openVotesChanged(_ nowOpen: Bool) {
        let votesJustClosed = hasOpenVotes && !nowOpen
        hasOpenVotes = nowOpen
        guard votesJustClosed else { return }
        voteOutcomeWatch += 1
        Task { await refreshQuietly() }
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

    /// The one way this screen reads itself.
    ///
    /// The cabal, its activity and — for an admin — the people waiting to join are read together
    /// rather than one after another, and written through `QuietUpdate`, so a refresh that finds
    /// nothing new changes nothing the member can see. Every caller goes through `refreshGate`,
    /// which is what stops a resuming poll tick from racing the appear load back into the view
    /// with two near-identical snapshots: a tick asks with `run` and is dropped while anything
    /// else holds the gate. It does not serialise the reads the member asks for — `runNow` marks
    /// the gate busy but waits for nothing — so two of those (a pull-to-refresh over the
    /// follow-up to a retry, say) can still be in flight together and land last-writer-wins.
    ///
    /// Failure belongs to whoever asked. A `.quiet` read rethrows so the poll loop backs off and
    /// leaves the screen exactly as the member last saw it; the other modes say so.
    private func refresh(_ mode: GroupDetailRefreshMode) async throws {
        guard let token = auth.accessToken else {
            if mode != .quiet {
                isLoading = false
                activityLoading = false
                errorMessage = "Sign in again to see this cabal."
            }
            return
        }
        // Leaving owns the screen while it runs; refreshing would only race the pop.
        guard !isLeaving, !hasLeft else { return }

        if mode == .initial {
            isLoading = true
            activityLoading = true
        }
        if mode != .quiet {
            errorMessage = nil
            activityError = nil
        }
        defer {
            if mode == .initial {
                isLoading = false
                activityLoading = false
            }
        }

        async let viewLoad = apiClient.getGroupView(accessToken: token, groupId: groupId)
        async let activityLoad = apiClient.getGroupActivity(accessToken: token, groupId: groupId)
        async let joinLoad = readJoinRequests(token: token)

        let activity = try? await activityLoad
        let joinRequestsRead = await joinLoad
        var loadedView: GroupViewDTO?
        var viewFailure: Error?
        do {
            loadedView = try await viewLoad
        } catch {
            viewFailure = error
        }

        guard !Task.isCancelled, !hasLeft else { return }

        if let loadedView {
            QuietUpdate.apply(loadedView, over: groupView) { groupView = $0 }
            if errorMessage != nil { errorMessage = nil }
        }
        if let activity {
            QuietUpdate.apply(activity.items, over: activityItems) { activityItems = $0 }
            if activityError != nil { activityError = nil }
            surfaceDepositFailureToasts(from: activity.items)
        } else if mode != .quiet, activityItems.isEmpty {
            activityError = "Couldn't load activity. Pull down to try again"
        }
        apply(joinRequestsRead)

        guard let viewFailure, !viewFailure.isRequestCancellation else { return }
        if mode == .quiet { throw viewFailure }
        if groupView == nil {
            errorMessage = "Couldn't load this cabal. Pull down to try again"
        } else if mode == .userInitiated {
            toast = MonacoToast(message: "Couldn't refresh this cabal. Try again")
        }
    }

    /// The read that follows something the member just did — funding, cashing out, answering a
    /// request, retrying a swap, a vote closing, a refused leave. It goes through the gate (so a
    /// poll tick that lands on top of it stands down) but is never itself dropped, and it shows
    /// nothing either way: the action it follows has already said what happened.
    private func refreshQuietly() async {
        try? await refreshGate.runNow { try await refresh(.quiet) }
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
        leavingSellsSlice = withdrawStake
        isLeaving = true
        do {
            try await apiClient.leaveGroup(accessToken: token, groupId: groupId, withdrawStake: withdrawStake)
            if withdrawStake {
                toast = MonacoToast(message: "Cash moved to your account balance", isSuccess: true)
            }
            // The cover stays up until the screen is on its way out. `onLeft()` is a network
            // round trip at every call site, and clearing `isLeaving` here would hand back the
            // action row, the back button and the details item for the length of it — on a cabal
            // the member has just left, still showing the slice they left with, because nothing
            // has re-read it yet. Only the failure paths below put the screen back in the
            // member's hands, which is also all `refreshQuietly()` needs to run.
            await onLeft()
            hasLeft = true
            return
        } catch MonacoAPIError.leaveBlocked(let reason) {
            isLeaving = false
            toast = MonacoToast(message: leaveBlockedMessage(for: reason))
        } catch MonacoAPIError.missingAccessToken {
            // Thrown before anything is sent, when the session's token is blank: the server was
            // never asked, so nothing was sold. It is a sign-in problem, not an unconfirmed sale.
            isLeaving = false
            toast = MonacoToast(message: "Sign in again to leave.")
            return
        } catch {
            isLeaving = false
            // No idempotency key rides on this request, so another "Sell and leave" is a new
            // request rather than a replay of this one. When the sale may already have happened
            // the member is told to look at their slice first, never simply to try again.
            let failure = FlowErrorInput(error)
            toast = MonacoToast(message: GroupDetailRefreshPolicy.leaveFailureMessage(
                sellsSlice: withdrawStake,
                failureStatus: failure.status,
                neverSent: failure.isOffline
            ))
        }
        // A refused leave can still have sold the slice: the server sells first and checks the
        // cabal's rules afterwards. Re-read the cabal so what is on screen is what is true now.
        await refreshQuietly()
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

    private func retryTransaction(_ item: GroupActivityItemDTO) async -> RetryTransactionResponse? {
        guard let token = auth.accessToken else {
            toast = MonacoToast(message: "Sign in again to retry.")
            return nil
        }
        guard !retryingTransactionIDs.contains(item.id) else { return nil }

        retryingTransactionIDs.insert(item.id)
        defer { retryingTransactionIDs.remove(item.id) }

        do {
            let result = try await apiClient.retryTransaction(accessToken: token, transactionId: item.id)
            await refreshQuietly()
            if result.status.lowercased() == "confirmed" {
                let done = item.kind.lowercased() == "sell" ? "Sold" : "Bought"
                toast = MonacoToast(message: "\(done). Holdings updated", isSuccess: true)
            } else if result.status.lowercased() == "failed" {
                toast = MonacoToast(message: "It didn't go through again. Try later")
            }
            return result
        } catch is CancellationError {
            return nil
        } catch MonacoAPIError.missingAccessToken {
            toast = MonacoToast(message: "Sign in again to retry.")
            return nil
        } catch {
            // A retry places a brand-new swap and leaves the failed row as it was, and no
            // idempotency key rides on it, so tapping Retry again after a lost answer is a second
            // swap. When this one may have gone through, re-read the activity and send the member
            // there first rather than inviting another tap.
            let failure = FlowErrorInput(error)
            toast = MonacoToast(message: GroupDetailRefreshPolicy.swapRetryFailureMessage(
                failureStatus: failure.status,
                neverSent: failure.isOffline
            ))
            if GroupDetailRefreshPolicy.swapRetryMayHavePlacedSwap(
                failureStatus: failure.status,
                neverSent: failure.isOffline
            ) {
                await refreshQuietly()
            }
            return nil
        }
    }

    /// What one read of the admin-only join requests found.
    private struct JoinRequestsRead {
        var outcome: JoinRequestsLoadOutcome
        var requests: [JoinRequestDTO] = []
        var viewerMayStillBeAdmin = true
    }

    /// Reads the people waiting to join. Answers with what to do rather than throwing: a dropped
    /// request or an offline blip must never blink a pending request away from the admin who was
    /// about to answer it.
    private func readJoinRequests(token: String) async -> JoinRequestsRead {
        guard viewerMayBeAdmin else { return JoinRequestsRead(outcome: .keep) }
        do {
            let requests = try await apiClient.listJoinRequests(accessToken: token, groupId: groupId)
            return JoinRequestsRead(outcome: .replace, requests: requests)
        } catch {
            let status = Self.httpStatus(of: error)
            let wasCancelled = error.isRequestCancellation
            return JoinRequestsRead(
                outcome: GroupDetailRefreshPolicy.joinRequestsOutcome(failureStatus: status, wasCancelled: wasCancelled),
                viewerMayStillBeAdmin: wasCancelled
                    || GroupDetailRefreshPolicy.viewerMayBeAdmin(afterFailureStatus: status)
            )
        }
    }

    private func apply(_ read: JoinRequestsRead) {
        if !read.viewerMayStillBeAdmin { viewerMayBeAdmin = false }
        switch read.outcome {
        case .replace:
            QuietUpdate.apply(read.requests, over: joinRequests) { joinRequests = $0 }
        case .clear:
            if !joinRequests.isEmpty { joinRequests = [] }
        case .keep:
            break
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
        let name = request.displayName.isEmpty ? "Member" : request.displayName
        do {
            if approve {
                try await apiClient.approveJoinRequest(accessToken: token, groupId: groupId, requestId: request.id)
            } else {
                try await apiClient.denyJoinRequest(accessToken: token, groupId: groupId, requestId: request.id)
            }
            toast = MonacoToast(message: approve ? "\(name) is in" : "Request declined", isSuccess: true)
        } catch {
            if error.isRequestCancellation { return }
            guard GroupDetailRefreshPolicy.joinRequestAlreadyAnswered(failureStatus: Self.httpStatus(of: error)) else {
                // Still there to answer: keep the row so the admin can try again.
                toast = MonacoToast(message: "Couldn't update the request. Try again")
                return
            }
            // Answered on another device, or withdrawn. Take the row away rather than leave
            // buttons on screen that can only ever fail.
            joinRequests.removeAll { $0.id == request.id }
            toast = MonacoToast(message: "\(name)'s request was already answered")
        }
        // A new member changes the member board and everyone's slice; both come back quietly.
        await refreshQuietly()
    }

    /// The status the server answered with, or nil when the request never reached one.
    ///
    /// Read through `FlowErrorInput`, the one place the app reduces its API errors to a status,
    /// so a new error case lands there without this screen having to enumerate it.
    private static func httpStatus(of error: Error) -> Int? {
        FlowErrorInput(error).status
    }
}

extension View {
    /// The cabal screen while a leave is running.
    ///
    /// Leaving sells a slice and waits for the payout to confirm, which can take most of a
    /// minute. For that whole time the screen says what is happening and takes no taps: an idle
    /// looking screen invites a second tap, or a second money flow on a cabal being left.
    func groupLeaveProgress(isLeaving: Bool, isSellingSlice: Bool) -> some View {
        disabled(isLeaving)
            // `disabled()` stops taps but leaves the rows reachable by VoiceOver swipe, so the
            // member can still walk an action row that does nothing. Hide the content behind
            // the cover the same way the cover hides it visually.
            .accessibilityHidden(isLeaving)
            .overlay {
                if isLeaving {
                    GroupLeaveProgressCover(isSellingSlice: isSellingSlice)
                }
            }
            .animation(.easeInOut(duration: 0.2), value: isLeaving)
            .navigationBarBackButtonHidden(isLeaving)
    }
}

struct GroupLeaveProgressCover: View {
    let isSellingSlice: Bool

    var body: some View {
        ZStack {
            MonacoTheme.canvas.opacity(0.94)
                .ignoresSafeArea()
            VStack(spacing: 14) {
                ProgressView()
                    .tint(MonacoTheme.ink)
                Text(isSellingSlice ? "Selling your slice…" : "Leaving the cabal…")
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.ink)
                Text("This can take a minute. Keep the app open.")
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .multilineTextAlignment(.center)
            }
            .padding(24)
        }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.updatesFrequently)
        .accessibilityIdentifier("group-leaving-cover")
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
    @ObservedObject var auth: DynamicAuthService
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
    let onRetry: (GroupActivityItemDTO) async -> RetryTransactionResponse?
    let onDecideJoinRequest: (JoinRequestDTO, Bool) -> Void
    let onToast: (MonacoToast) -> Void
    var onHeroScrolledAway: (Bool) -> Void = { _ in }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 32) {
                VStack(spacing: 20) {
                    GroupHeroSection(view: view)
                    GroupActionRow(slice: view.you, onRoute: onRoute, onPropose: onPropose)
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
                    AgentSectionView(agent: agent)
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
    /// The member's slice, read here so Cash out is pushed with the figures that were on screen.
    let slice: MemberSliceDTO
    let onRoute: (GroupDetailRoute) -> Void
    let onPropose: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            action("Add money", systemImage: "plus", id: "group-action-fund") { onRoute(.addMoney) }
            action("Propose", systemImage: "arrow.up.right", id: "group-action-propose", perform: onPropose)
            action("Cash out", systemImage: "arrow.down.left", id: "group-action-sell") {
                onRoute(.cashOut(shareUnits: Int64(slice.shareUnits) ?? 0, equityUsd: slice.equityUsd))
            }
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
