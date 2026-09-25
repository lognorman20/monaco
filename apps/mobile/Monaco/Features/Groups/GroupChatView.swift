import SwiftUI
import MonacoCore

/// Cabal chat: members-only message thread with a composer. Polls for new messages while visible.
///
/// Two rules the thread keeps. It only jumps to the newest message when the viewer sent it or
/// was already reading the bottom — anyone scrolled up in the history keeps their place and
/// gets a pill instead. And the poll is the shared `pollWhileVisible`, not a timer of its own,
/// so it pauses off-tab and in the background, backs off when the server stops answering, and
/// stands down while a pull-to-refresh is in flight.
struct GroupChatView: View {
    let groupId: String
    let groupName: String?
    /// Returns the chat transport for the current session, or nil when signed out.
    let makeService: () -> (any GroupChatService)?

    @State private var timeline = GroupChatTimeline()
    @State private var isLoadingOlder = false
    @State private var loadError: String?
    /// Whether this thread is closed to the viewer (removed from the cabal, cabal deleted).
    /// Parks the poll and the composer — so it takes more than one background tick to enter,
    /// and there is always something on screen that asks the server again.
    @State private var closure = GroupChatClosureTracker()
    @State private var toast: MonacoToast?
    @State private var unreadCount = 0
    /// Bumped to ask the thread to scroll to the newest message.
    @State private var scrollToBottomRequests = 0
    /// Set to the row that must stay put after older messages are prepended above it.
    @State private var keepInViewRowID: String?
    /// Whether the reader has taken the thread over: true from the moment they drag away from
    /// the newest message until they come back to it, by hand or by taking the pill. While it
    /// is false the thread follows the conversation, which is both what decides an arrival's
    /// scroll and what keeps a LazyVStack's estimated layout pinned to the end.
    @State private var readerControlsScroll = false
    /// Where the thread is resting, kept as state rather than read back out of a geometry
    /// *transition*: a derived `Bool` is only reported when it changes, and a drag that never
    /// leaves the `pinnedSlack` window — swiping down to dismiss the keyboard, a flick to
    /// check for new messages, a pull to refresh on a thread shorter than the screen —
    /// produces no transition at all. Without somewhere to remember the answer, those drags
    /// latched `readerControlsScroll` with nothing able to clear it.
    @State private var position = ThreadPosition(offset: 0, isAtEnd: true)
    /// What the thread looked like when the drag in progress began; nil between drags.
    @State private var dragOrigin: DragOrigin?
    @State private var refreshGate = RefreshGate()
    @FocusState private var composerFocused: Bool
    @Environment(\.scenePhase) private var scenePhase

    private let pageSize = 30
    private let pollInterval: Duration = .seconds(4)
    private static let bottomAnchor = "group-chat-bottom"
    /// How close to the end counts as "reading the bottom", in points.
    private static let pinnedSlack: CGFloat = 40

    var body: some View {
        VStack(spacing: 0) {
            messagesArea
            if let closedMessage = closure.message {
                // Only once there is a thread behind it; before that the error state says
                // the same thing across the whole screen.
                if timeline.hasLoadedNewest {
                    closedBanner(closedMessage)
                }
            } else {
                GroupChatComposer(focus: $composerFocused, send: send)
            }
        }
        .background(MonacoTheme.background)
        .navigationTitle(GroupChatCopy.title(groupName: groupName))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                HStack(spacing: 8) {
                    CabalMark(groupId: groupId, name: GroupChatCopy.title(groupName: groupName), size: 28)
                        .accessibilityHidden(true)
                    Text(GroupChatCopy.title(groupName: groupName))
                        .font(MonacoTheme.Typo.bodyStrong)
                        .foregroundStyle(MonacoTheme.ink)
                        .lineLimit(1)
                }
                .accessibilityElement(children: .combine)
                .accessibilityAddTraits(.isHeader)
            }
        }
        .task { await loadNewest() }
        .pollWhileVisible(every: pollInterval, isActive: !closure.isClosed, gate: refreshGate) {
            try await pollNewest()
        }
        .onChange(of: scenePhase) { _, phase in
            // The poll is parked while the thread is closed, so without this the screen would
            // keep telling a member they are out of their cabal until they navigate away.
            // Coming back to the app is the cheapest moment to ask the server once more.
            guard phase == .active, closure.isClosed else { return }
            Task { await loadNewest() }
        }
        .monacoToast($toast)
        // `.contain`, not a bare identifier: an identifier on its own was being applied to
        // every descendant, so the composer, the send button and the thread all reported
        // themselves as "group-chat-view" and nothing on this screen could be addressed.
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("group-chat-view")
    }

    // MARK: - Messages

    @ViewBuilder
    private var messagesArea: some View {
        if !timeline.hasLoadedNewest {
            if let loadError {
                statusMessage {
                    Text(loadError)
                        .font(MonacoTheme.Typo.body)
                        .foregroundStyle(MonacoTheme.ink)
                        .multilineTextAlignment(.center)
                        .fixedSize(horizontal: false, vertical: true)
                    // Offered even when the thread reads as closed. Being removed from a cabal
                    // and a cabal that briefly answered 404 look identical from here, and a
                    // member told they were thrown out of theirs needs something to tap.
                    Button("Try again") { Task { await loadNewest() } }
                        .buttonStyle(.monacoSecondary)
                        .accessibilityIdentifier("group-chat-retry")
                }
                // `.contain` again: a bare identifier on this container was being handed to
                // the Try again button inside it, so the one control on the screen could not
                // be addressed — by a UI test or by anything else.
                .accessibilityElement(children: .contain)
                .accessibilityIdentifier("group-chat-error")
            } else {
                GroupChatSkeleton()
                    .accessibilityIdentifier("group-chat-loading")
            }
        } else if timeline.rows.isEmpty {
            // The start of the conversation: whose chat this is, then the invitation to open it.
            statusMessage {
                CabalMark(groupId: groupId, name: GroupChatCopy.title(groupName: groupName), size: 56)
                Text(GroupChatCopy.title(groupName: groupName))
                    .font(MonacoTheme.Typo.section)
                    .foregroundStyle(MonacoTheme.ink)
                    .multilineTextAlignment(.center)
                Text(GroupChatCopy.emptyState)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .contentShape(Rectangle())
            .onTapGesture { composerFocused = true }
            .accessibilityIdentifier("group-chat-empty")
        } else {
            thread
        }
    }

    private var thread: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 3) {
                    if timeline.hasOlder {
                        loadEarlierButton
                    }

                    ForEach(timeline.rows) { row in
                        if let separator = row.timeSeparatorLabel() {
                            GroupChatDayRule(label: separator)
                                .padding(.top, row.id == timeline.rows.first?.id ? 8 : 16)
                                .padding(.bottom, 4)
                                .accessibilityIdentifier("group-chat-separator-\(row.id)")
                        }
                        GroupChatBubble(row: row)
                            .padding(.top, row.startsRun && !row.showsTimeSeparator ? 10 : 0)
                            .id(row.id)
                    }

                    Color.clear.frame(height: 1).id(Self.bottomAnchor)
                }
                .padding(.horizontal, 16)
                .padding(.bottom, 12)
            }
            // Only the opening position. A plain `.defaultScrollAnchor(.bottom)` also anchors
            // *size changes* to the bottom, which drags the view down whenever a message is
            // appended — the yank this screen is meant to stop, under the explicit scroll.
            // Following the thread is a decision now, made in `apply`, not an anchor.
            .defaultScrollAnchor(.bottom, for: .initialOffset)
            .scrollDismissesKeyboard(.interactively)
            .refreshable {
                await refreshGate.runNow { await loadNewest() }
            }
            .onScrollGeometryChange(for: ThreadPosition.self) { geometry in
                ThreadPosition(
                    offset: geometry.contentOffset.y,
                    isAtEnd: geometry.contentOffset.y + geometry.containerSize.height
                        >= geometry.contentSize.height - Self.pinnedSlack
                )
            } action: { _, updated in
                // Remembered in both directions: the phase handler needs the answer at the
                // moment a drag ends, and by then there may be no transition left to read.
                position = updated
                // Reaching the end is the reader rejoining the conversation, whether they
                // dragged there or we took them. Nothing here may set `readerControlsScroll`:
                // content growing pushes the end away for a frame or two, and that is the
                // thread working, not the reader leaving.
                guard updated.isAtEnd else { return }
                readerControlsScroll = false
                unreadCount = 0
            }
            .onScrollGeometryChange(for: CGFloat.self) { $0.contentSize.height } action: { _, _ in
                // A LazyVStack lays out from an estimated content size, so the opening
                // anchor comes to rest short of the newest message and every realised row
                // moves it again. Until the reader takes the thread over, keep them at the
                // end — which is also the right behaviour for a message arriving while
                // they sit there.
                guard !readerControlsScroll else { return }
                proxy.scrollTo(Self.bottomAnchor, anchor: .bottom)
            }
            .onScrollPhaseChange { _, phase in
                switch phase {
                case .interacting:
                    // A drag, not our own animated scroll: from here the position is theirs.
                    if dragOrigin == nil {
                        dragOrigin = DragOrigin(offset: position.offset, wasFollowing: !readerControlsScroll)
                    }
                    readerControlsScroll = true
                case .idle:
                    // …unless the drag left them where it found them. That is what the
                    // geometry handler cannot answer on its own: its derived value never
                    // leaves `true` for a drag inside the pinned window, so it has no
                    // transition to report and the latch would stay set for the life of the
                    // view — the stranded thread again, pill drawn over the message that just
                    // landed.
                    //
                    // "Where it found them" is compared as an *offset*, not as end-ness: a
                    // message arriving mid-drag moves the end away, and an arrival is not the
                    // reader going anywhere. End-ness alone would hand the latch straight back
                    // on a busy cabal, which is the one place this matters.
                    let origin = dragOrigin
                    dragOrigin = nil
                    if position.isAtEnd {
                        readerControlsScroll = false
                        unreadCount = 0
                        return
                    }
                    // A gesture that barely moved the thread did not hand it over, so give
                    // back whatever control the reader had before it. That is the whole of
                    // the bug: swiping down to dismiss the keyboard, a flick to check for new
                    // messages, or a pull-to-refresh on a short thread all latched the reader
                    // in control of a thread they never left, and nothing could clear it
                    // again — every arrival counted unread, the pill drawn over the message
                    // that had just landed, and the correction that keeps a LazyVStack at its
                    // end switched off for the life of the view.
                    //
                    // Measured as movement rather than as "is it at the end", because the end
                    // moves on its own: a message arriving mid-drag pushes it away, and that
                    // is not the reader going anywhere.
                    guard let origin, origin.wasFollowing,
                          abs(position.offset - origin.offset) <= Self.pinnedSlack
                    else { return }
                    readerControlsScroll = false
                    unreadCount = 0
                default:
                    return
                }
            }
            .onChange(of: scrollToBottomRequests) { _, _ in
                withAnimation(.easeOut(duration: 0.2)) {
                    proxy.scrollTo(Self.bottomAnchor, anchor: .bottom)
                }
            }
            .onChange(of: keepInViewRowID) { _, rowID in
                // "Load earlier" prepended a page above the reader. Put the row they were
                // on back where it was, with no animation, so nothing appears to move.
                guard let rowID else { return }
                proxy.scrollTo(rowID, anchor: .top)
                keepInViewRowID = nil
            }
            // Identifier on the scroll view itself, before the overlay: applied after, it
            // would be handed to the pill too and the pill could not be addressed.
            .accessibilityIdentifier("group-chat-thread")
            .overlay(alignment: .bottom) {
                if unreadCount > 0 {
                    newMessagesPill
                }
            }
        }
    }

    private var loadEarlierButton: some View {
        Button {
            Task { await loadOlder() }
        } label: {
            if isLoadingOlder {
                ProgressView().tint(MonacoTheme.accent)
            } else {
                Text(GroupChatCopy.loadEarlier)
                    .font(MonacoTheme.Typo.captionStrong)
            }
        }
        .buttonStyle(.borderless)
        .foregroundStyle(MonacoTheme.accent)
        .frame(maxWidth: .infinity)
        .padding(.vertical, 8)
        .disabled(isLoadingOlder)
        .accessibilityIdentifier("group-chat-load-earlier")
    }

    private var newMessagesPill: some View {
        Button {
            followThread()
        } label: {
            HStack(spacing: 6) {
                Image(systemName: "arrow.down")
                    .font(.caption.weight(.bold))
                Text(GroupChatCopy.newMessagesPill(count: unreadCount))
                    .font(MonacoTheme.Typo.captionStrong)
            }
            .foregroundStyle(MonacoTheme.primaryButtonLabel)
            .padding(.horizontal, 14)
            .padding(.vertical, 9)
            .background(Capsule().fill(MonacoTheme.primaryButtonFill))
        }
        .buttonStyle(.plain)
        .padding(.bottom, 10)
        .transition(.move(edge: .bottom).combined(with: .opacity))
        .animation(.snappy, value: unreadCount)
        .accessibilityIdentifier("group-chat-new-messages")
    }

    /// Replaces the composer on a closed thread. It keeps a way back: this is the only thing
    /// on screen once the poll is parked, and the reason behind it may have been a blip.
    private func closedBanner(_ message: String) -> some View {
        HStack(alignment: .center, spacing: MonacoTheme.Space.sm) {
            Label(message, systemImage: "lock.fill")
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.secondaryText)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)

            Button("Try again") { Task { await loadNewest() } }
                .font(MonacoTheme.Typo.captionStrong)
                .foregroundStyle(MonacoTheme.brand)
                .frame(minWidth: 44, minHeight: 44)
                .contentShape(Rectangle())
                .accessibilityIdentifier("group-chat-closed-retry")
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, MonacoTheme.Space.xs)
        .background(MonacoTheme.background)
        .overlay(alignment: .top) {
            MonacoRule()
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("group-chat-closed")
    }

    private func statusMessage<Content: View>(@ViewBuilder content: () -> Content) -> some View {
        VStack(spacing: 12) {
            content()
        }
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: - Loading

    private func loadNewest() async {
        guard let service = makeService() else {
            loadError = "Sign in to read this cabal's chat."
            return
        }
        do {
            let page = try await service.listGroupMessages(groupId: groupId, before: nil, limit: pageSize)
            apply(page)
            loadError = nil
            // The server just handed over the thread, which reopens it if we had it shut.
            closure.succeeded()
        } catch is CancellationError {
            return
        } catch {
            closure.memberLoadFailed(error)
            if timeline.hasLoadedNewest {
                toast = MonacoToast(message: GroupChatCopy.refreshFailure(error))
            } else {
                loadError = GroupChatCopy.loadFailure(error)
            }
        }
    }

    /// A tick. Throws so the shared schedule backs off; a thread the viewer can no longer read
    /// is not a failure to retry, so it parks the loop instead.
    private func pollNewest() async throws {
        guard let service = makeService() else { return }
        do {
            let page = try await service.listGroupMessages(groupId: groupId, before: nil, limit: pageSize)
            apply(page)
            if loadError != nil { loadError = nil }
            closure.succeeded()
        } catch {
            closure.pollFailed(error)
            // A closed answer this loop has not corroborated yet is not something to back off
            // from — it is the one thing worth asking again promptly, on the normal cadence,
            // to find out whether it was a blip. Once it is believed, `isActive` parks us.
            guard GroupChatCopy.chatClosed(error) == nil else { return }
            throw error
        }
    }

    /// Merges a newest page and decides what the arrival means for the viewer's scroll position.
    private func apply(_ page: GroupMessagesPageDTO) {
        let added = timeline.mergeNewest(page)
        guard !added.isEmpty else { return }
        if GroupChatTimeline.shouldAutoScroll(added: added, isFollowingThread: !readerControlsScroll) {
            followThread()
        } else {
            unreadCount += added.count
        }
    }

    /// The thread following the newest message again: the reader took the pill, sent something,
    /// or was already at the end when this arrived.
    ///
    /// Handing scroll control back matters as much as the scroll itself. A LazyVStack lays out
    /// from an estimated content size, so one `scrollTo` the moment a row is appended comes to
    /// rest short of the end and every row realised afterwards moves it further. While
    /// `readerControlsScroll` is false the thread corrects itself on each content-size change;
    /// latched true from an earlier drag, it would leave the reader stranded just above the
    /// newest message, never counted as "at the bottom", with the pill climbing again behind
    /// them.
    private func followThread() {
        unreadCount = 0
        readerControlsScroll = false
        scrollToBottomRequests += 1
    }

    private func loadOlder() async {
        guard let cursor = timeline.olderCursor, !isLoadingOlder, let service = makeService() else { return }
        isLoadingOlder = true
        defer { isLoadingOlder = false }
        // The row at the top right now is the one the reader is looking at.
        let topRowID = timeline.rows.first?.id
        // Asking for history is the reader taking the thread over, whether or not they had to
        // drag to reach the button — on a thread barely taller than the screen it is reachable
        // without one. Said before the prepend, because the content-size correction fires on
        // it: otherwise it races the place-keeping scroll below and wins, dropping the reader
        // at the bottom of the very history they just asked for.
        readerControlsScroll = true
        do {
            let page = try await service.listGroupMessages(groupId: groupId, before: cursor, limit: pageSize)
            timeline.mergeOlder(page)
            keepInViewRowID = topRowID
        } catch is CancellationError {
            return
        } catch {
            closure.memberLoadFailed(error)
            toast = MonacoToast(message: GroupChatCopy.earlierFailure(error))
        }
    }

    /// Posts `body`, and says what the composer should do with the member's text.
    private func send(_ body: String) async -> GroupChatSendOutcome {
        guard let service = makeService() else {
            toast = MonacoToast(message: GroupChatCopy.sendFailure(MonacoCore.MonacoAPIError.httpStatus(401)))
            return .failed
        }
        do {
            let sent = try await service.postGroupMessage(groupId: groupId, body: body)
            timeline.appendSent(sent)
            followThread()
            return .sent
        } catch is CancellationError {
            return .failed
        } catch {
            closure.memberLoadFailed(error)
            toast = MonacoToast(message: GroupChatCopy.sendFailure(error))
            return GroupChatCopy.isSendUnconfirmed(error) ? .unconfirmed : .failed
        }
    }
}

/// Where the thread is resting: how far it is scrolled, and whether that counts as being at
/// the newest message. Both together, because deciding what a drag meant needs the offset —
/// end-ness moves on its own whenever a message arrives.
private struct ThreadPosition: Equatable {
    var offset: CGFloat
    var isAtEnd: Bool
}

/// Where the thread stood when a drag began, which is what says whether the drag meant
/// anything: who was in control, and how far it has moved since.
private struct DragOrigin: Equatable {
    var offset: CGFloat
    var wasFollowing: Bool
}

/// What a send leaves the composer to do.
private enum GroupChatSendOutcome {
    /// It landed. The composer stays empty.
    case sent
    /// It definitely did not land, so the member's text comes back.
    case failed
    /// It may well have landed, and chat has no delete. The text is deliberately *not* handed
    /// back: a composer refilled with a message that is already in the thread is one tap from
    /// the duplicate the warning is there to prevent.
    case unconfirmed
}

extension GroupChatView {
    /// Chat screen wired to the live API using the current Privy session token.
    init(auth: PrivyAuthService, groupId: String, groupName: String?) {
        self.init(groupId: groupId, groupName: groupName) { [weak auth] in
            guard let token = auth?.accessToken, !token.isEmpty else { return nil }
            return MonacoCore.MonacoAPIClient(baseURL: Config.apiBaseURL, accessTokenProvider: { token })
        }
    }
}

/// The composer owns the draft so that typing invalidates 44 points of the screen instead of
/// the whole thread.
private struct GroupChatComposer: View {
    var focus: FocusState<Bool>.Binding
    /// Posts the trimmed body and says what to do with the member's text.
    let send: (String) async -> GroupChatSendOutcome

    @State private var draft = ""
    @State private var isSending = false

    private var trimmedCount: Int {
        draft.trimmingCharacters(in: .whitespacesAndNewlines).unicodeScalars.count
    }

    var body: some View {
        let canSend = !isSending && (try? GroupChatDraft.validate(draft).get()) != nil

        return VStack(alignment: .trailing, spacing: 4) {
            HStack(alignment: .bottom, spacing: MonacoTheme.Space.s) {
                // The title and the prompt are the same words: the title is what VoiceOver and
                // the UI tests read, the prompt is the placeholder drawn in the palette's own grey.
                TextField(
                    GroupChatCopy.composerPlaceholder,
                    text: $draft,
                    prompt: Text(GroupChatCopy.composerPlaceholder).foregroundStyle(MonacoTheme.disabledLabel),
                    axis: .vertical
                )
                    .font(MonacoTheme.Typo.body)
                    .foregroundStyle(MonacoTheme.ink)
                    .tint(MonacoTheme.ink)
                    .lineLimit(1...5)
                    .focused(focus)
                    .padding(.horizontal, MonacoTheme.Space.m)
                    .padding(.vertical, 11)
                    .frame(minHeight: 44)
                    .background(MonacoTheme.surfaceSunken, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
                    .accessibilityIdentifier("group-chat-composer")

                Button {
                    Haptics.tap()
                    Task { await submit() }
                } label: {
                    ComposerSendDisc(isLive: canSend || isSending, isSending: isSending)
                }
                .buttonStyle(.plain)
                .disabled(!canSend)
                .accessibilityLabel("Send message")
                .accessibilityIdentifier("group-chat-send")
            }

            if trimmedCount > GroupChatDraft.maxCharacters - 200 {
                Text("\(trimmedCount)/\(GroupChatDraft.maxCharacters)")
                    .font(MonacoTheme.Typo.stamp)
                    .foregroundStyle(trimmedCount > GroupChatDraft.maxCharacters ? MonacoTheme.destructive : MonacoTheme.secondaryText)
                    .accessibilityIdentifier("group-chat-char-count")
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, MonacoTheme.Space.s)
        .background(MonacoTheme.background)
        .overlay(alignment: .top) {
            MonacoRule()
        }
    }

    /// Clears the field before the request rather than subtracting the sent text afterwards:
    /// the field stayed editable mid-flight, so fixing a typo left the already-submitted text
    /// sitting in the composer, ready to be posted a second time.
    private func submit() async {
        guard !isSending else { return }
        let body: String
        switch GroupChatDraft.validate(draft) {
        case .success(let trimmed):
            body = trimmed
        case .failure:
            return
        }

        let submitted = draft
        draft = ""
        isSending = true
        defer { isSending = false }

        switch await send(body) {
        case .sent, .unconfirmed:
            return
        case .failed:
            // Give the text back. Anything typed while it was in flight is the member's too,
            // so the failed message goes in front of it rather than one of the two being
            // picked to throw away silently.
            draft = draft.isEmpty ? submitted : submitted + "\n" + draft
        }
    }
}

private struct GroupChatBubble: View {
    let row: GroupChatRow

    private var message: GroupMessageDTO { row.message }

    /// The face at the foot of someone else's run, the way a group chat marks who is talking.
    static let faceSize: CGFloat = 28

    var body: some View {
        HStack(alignment: .bottom, spacing: MonacoTheme.Space.s) {
            if message.mine {
                Spacer(minLength: 56)
            } else {
                face
            }
            VStack(alignment: message.mine ? .trailing : .leading, spacing: 4) {
                if !message.mine, row.startsRun {
                    Text(message.authorName)
                        .font(MonacoTheme.Typo.captionStrong)
                        .foregroundStyle(MonacoTheme.muted)
                        .lineLimit(1)
                        .padding(.horizontal, 14)
                }
                Text(message.body)
                    .font(MonacoTheme.Typo.body)
                    .foregroundStyle(message.mine ? MonacoTheme.onBrand : MonacoTheme.ink)
                    .textSelection(.enabled)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 9)
                    .background(bubbleShape.fill(message.mine ? MonacoTheme.brandFill : MonacoTheme.surface))
                    // Paper on paper needs an edge: white on cream is faint in light and all but
                    // gone in dark, where the surface and the canvas are two greens apart.
                    .overlay {
                        if !message.mine {
                            bubbleShape.strokeBorder(MonacoTheme.hairline, lineWidth: 1)
                        }
                    }
            }
            if !message.mine { Spacer(minLength: 56) }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(accessibilityText)
        .accessibilityIdentifier("group-chat-message-\(message.id)")
    }

    /// Only the last bubble of a run carries the face; the rest keep its column so the run
    /// lines up.
    @ViewBuilder
    private var face: some View {
        Group {
            if row.endsRun {
                MonacoAvatar(photoURL: nil, displayName: message.authorName, size: Self.faceSize)
            } else {
                Color.clear
            }
        }
        .frame(width: Self.faceSize, height: Self.faceSize)
    }

    /// `Radius.bubble` all round, with a tighter corner on the sender's side at the end of a run,
    /// which on someone else's run points at their face.
    private var bubbleShape: UnevenRoundedRectangle {
        let radius = MonacoTheme.Radius.bubble
        let tail: CGFloat = row.endsRun ? 6 : radius
        return UnevenRoundedRectangle(
            topLeadingRadius: radius,
            bottomLeadingRadius: message.mine ? radius : tail,
            bottomTrailingRadius: message.mine ? tail : radius,
            topTrailingRadius: radius,
            style: .continuous
        )
    }

    private var accessibilityText: String {
        let who = message.mine ? "You" : message.authorName
        guard let date = row.date else { return "\(who): \(message.body)" }
        return "\(who), \(date.formatted(date: .omitted, time: .shortened)): \(message.body)"
    }
}

/// The time between two rules, where the conversation picked up again after a gap.
private struct GroupChatDayRule: View {
    let label: String

    var body: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            MonacoRule()
            // The rules give way first; at the largest text sizes the stamp wraps rather than
            // running off the edge.
            Text(label)
                .font(MonacoTheme.Typo.stamp)
                .foregroundStyle(MonacoTheme.tertiaryText)
                .multilineTextAlignment(.center)
                .lineLimit(2)
                .layoutPriority(1)
            MonacoRule()
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(label)
    }
}

/// The thread in its own shape while the first page loads: a few runs from either side, settled
/// at the bottom where a chat opens.
private struct GroupChatSkeleton: View {
    private let bubbles: [(mine: Bool, width: CGFloat)] = [
        (false, 188), (false, 132), (true, 172), (false, 216), (true, 112),
    ]

    var body: some View {
        VStack(spacing: 6) {
            Spacer(minLength: 0)
            ForEach(Array(bubbles.enumerated()), id: \.offset) { _, bubble in
                HStack(alignment: .bottom, spacing: MonacoTheme.Space.s) {
                    if bubble.mine {
                        Spacer(minLength: 56)
                    } else {
                        SkeletonBlock(width: GroupChatBubble.faceSize, height: GroupChatBubble.faceSize, radius: GroupChatBubble.faceSize / 2)
                    }
                    SkeletonBlock(width: bubble.width, height: 38, radius: MonacoTheme.Radius.bubble)
                    if !bubble.mine { Spacer(minLength: 56) }
                }
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.bottom, MonacoTheme.Space.sm)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading messages")
    }
}
