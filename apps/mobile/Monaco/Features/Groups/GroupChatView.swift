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
    /// The cabal's members, for the avatars beside each run and the face stack in the toolbar.
    ///
    /// Resolved entirely client-side, with no backend work at all: `GroupMessageDTO.authorId`
    /// joins against the `GroupViewDTO.members` array the cabal screen has already loaded, and
    /// `LeaderboardRowDTO` carries `userId`, `displayName` and `profilePhotoUrl`. A member who is
    /// not in the array — someone who left after writing — falls back to the message's own
    /// `authorName`, which is the only honest answer there is.
    var members: [LeaderboardRowDTO] = []
    /// This cabal's resolved tint. Your own bubbles take it, which is the clearest possible
    /// signal of which room you are standing in.
    var tint: MonacoTheme.CabalTint?
    /// Returns the chat transport for the current session, or nil when signed out.
    let makeService: () -> (any GroupChatService)?

    /// Author id to member.
    ///
    /// Built **once per body pass**, in `thread`, next to `dayLabels` — never read from inside the
    /// `ForEach` closure. As a computed property read per row it was rebuilt once per bubble,
    /// O(rows x members) on a body that every composer keystroke invalidates, which is the exact
    /// per-row work `GroupChatRow` exists to keep off this path.
    private func membersById(_ members: [LeaderboardRowDTO]) -> [String: LeaderboardRowDTO] {
        Dictionary(members.map { ($0.userId, $0) }, uniquingKeysWith: { first, _ in first })
    }

    private var resolvedTint: MonacoTheme.CabalTint {
        tint ?? .forGroupId(groupId)
    }

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
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

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
                    CabalMark(tint: resolvedTint, name: GroupChatCopy.title(groupName: groupName), size: 28)
                        .accessibilityHidden(true)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(GroupChatCopy.title(groupName: groupName))
                            .font(.headline)
                            .foregroundStyle(MonacoTheme.fgPrimary)
                            .lineLimit(1)
                        // Who is in the room, under its name. Initials until the members'
                        // photos are there; `MonacoAvatar` already renders them.
                        MonacoFaceStack(
                            faces: members.map {
                                MonacoFace(id: $0.userId, displayName: $0.displayName, photoURL: $0.profilePhotoUrl)
                            },
                            size: 16,
                            maxVisible: 5,
                            ringColor: MonacoTheme.bgBase
                        )
                    }
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
                    Label(loadError, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(MonacoTheme.warning)
                    // Offered even when the thread reads as closed. Being removed from a cabal
                    // and a cabal that briefly answered 404 look identical from here, and a
                    // member told they were thrown out of theirs needs something to tap.
                    Button("Try again") { Task { await loadNewest() } }
                        .buttonStyle(.monacoPrimary)
                        .accessibilityIdentifier("group-chat-retry")
                }
                // `.contain` again: a bare identifier on this container was being handed to
                // the Try again button inside it, so the one control on the screen could not
                // be addressed — by a UI test or by anything else.
                .accessibilityElement(children: .contain)
                .accessibilityIdentifier("group-chat-error")
            } else {
                statusMessage {
                    ProgressView("Loading messages…")
                        .tint(MonacoTheme.controlTint)
                        .foregroundStyle(MonacoTheme.fgMuted)
                }
                .accessibilityIdentifier("group-chat-loading")
            }
        } else if timeline.rows.isEmpty {
            statusMessage {
                Text(GroupChatCopy.emptyState)
                    .font(.subheadline)
                    .foregroundStyle(MonacoTheme.fgMuted)
                    .multilineTextAlignment(.center)
            }
            .contentShape(Rectangle())
            .onTapGesture { composerFocused = true }
            .accessibilityIdentifier("group-chat-empty")
        } else {
            thread
        }
    }

    private var thread: some View {
        let dayLabels = GroupChatCopy.dayDividerLabels(for: timeline.rows)
        let authors = membersById(members)
        return ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 3) {
                    if timeline.hasOlder {
                        loadEarlierButton
                    }

                    ForEach(timeline.rows) { row in
                        if let separator = dayLabels[row.id] {
                            // A day divider, not a stray caption: a capsule on the quiet fill,
                            // centred, so the eye reads it as a break in the conversation.
                            Text(separator)
                                .font(MonacoTheme.Typo.micro)
                                .foregroundStyle(MonacoTheme.fgMuted)
                                .padding(.horizontal, 10)
                                .padding(.vertical, 4)
                                .background(Capsule().fill(MonacoTheme.fillQuiet))
                                .frame(maxWidth: .infinity)
                                .padding(.top, row.id == timeline.rows.first?.id ? 8 : 20)
                                .padding(.bottom, 6)
                                .accessibilityIdentifier("group-chat-separator-\(row.id)")
                        }
                        GroupChatBubble(
                            row: row,
                            author: authors[row.message.authorId],
                            tint: resolvedTint
                        )
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
                withAnimation(MonacoMotion.glide.reduced(reduceMotion)) {
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
                ProgressView().tint(MonacoTheme.controlTint)
            } else {
                Text(GroupChatCopy.loadEarlier)
                    .font(.footnote.weight(.semibold))
            }
        }
        .buttonStyle(.borderless)
        .foregroundStyle(MonacoTheme.brand)
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
                    .font(.footnote.weight(.semibold))
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
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Label(message, systemImage: "lock.fill")
                .font(.footnote)
                .foregroundStyle(MonacoTheme.fgMuted)
                .frame(maxWidth: .infinity, alignment: .leading)

            Button("Try again") { Task { await loadNewest() } }
                .font(.footnote.weight(.semibold))
                .foregroundStyle(MonacoTheme.brand)
                .accessibilityIdentifier("group-chat-closed-retry")
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 14)
        .background(MonacoTheme.background)
        .overlay(alignment: .top) {
            Rectangle().fill(MonacoTheme.border).frame(height: 0.5)
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
    /// Chat screen wired to the live API using the current session token.
    init(
        auth: DynamicAuthService,
        groupId: String,
        groupName: String?,
        members: [LeaderboardRowDTO] = [],
        tint: MonacoTheme.CabalTint? = nil
    ) {
        self.init(groupId: groupId, groupName: groupName, members: members, tint: tint) { [weak auth] in
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
            HStack(alignment: .bottom, spacing: 8) {
                TextField(GroupChatCopy.composerPlaceholder, text: $draft, axis: .vertical)
                    .lineLimit(1...5)
                    .focused(focus)
                    // The caret is ink, deliberately not brand: blue means tap, and a caret is
                    // not a thing you tap.
                    .tint(MonacoTheme.controlTint)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 11)
                    .frame(minHeight: 44)
                    .background(MonacoTheme.fillQuiet, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
                    .accessibilityIdentifier("group-chat-composer")

                Button {
                    Haptics.tap()
                    Task { await submit() }
                } label: {
                    ZStack {
                        Circle()
                            .fill(canSend || isSending ? MonacoTheme.primaryButtonFill : MonacoTheme.disabled)
                        if isSending {
                            ProgressView()
                                .tint(MonacoTheme.primaryButtonLabel)
                        } else {
                            Image(systemName: "arrow.up")
                                .font(.system(size: 17, weight: .semibold))
                                .foregroundStyle(MonacoTheme.primaryButtonLabel)
                        }
                    }
                    .frame(width: 44, height: 44)
                }
                .disabled(!canSend)
                .accessibilityLabel("Send message")
                .accessibilityIdentifier("group-chat-send")
            }

            if trimmedCount > GroupChatDraft.maxCharacters - 200 {
                Text("\(trimmedCount)/\(GroupChatDraft.maxCharacters)")
                    .font(.caption2.monospacedDigit())
                    .foregroundStyle(trimmedCount > GroupChatDraft.maxCharacters ? MonacoTheme.destructive : MonacoTheme.fgMuted)
                    .accessibilityIdentifier("group-chat-char-count")
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 8)
        .background(MonacoTheme.background)
        .overlay(alignment: .top) {
            Rectangle().fill(MonacoTheme.border).frame(height: 0.5)
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
    /// The member who wrote it, joined client-side. Nil for someone who has left the cabal.
    let author: LeaderboardRowDTO?
    /// The cabal's colour. Your own bubbles carry it, so the room you are standing in is
    /// readable from a single bubble.
    let tint: MonacoTheme.CabalTint

    @Environment(\.colorScheme) private var colorScheme

    private var message: GroupMessageDTO { row.message }

    /// The avatar column, reserved on every row from another member whether or not a face is
    /// drawn, so a run's second bubble does not shift left and nothing reflows when a photo
    /// finishes loading.
    private static let avatarSize: CGFloat = 24
    private static let avatarGutter: CGFloat = 32

    /// `cta`, not `fill`: a bubble carries body text at 17pt, which is not "large text" under
    /// WCAG, and only the deeper pair clears 4.5:1 against white in both schemes.
    private var mineFill: Color { tint.cta }

    var body: some View {
        VStack(alignment: message.mine ? .trailing : .leading, spacing: 4) {
            if !message.mine, row.startsRun {
                Text(author?.displayName ?? message.authorName)
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(MonacoTheme.fgMuted)
                    .padding(.leading, Self.avatarGutter + 14)
            }
            // The avatar is level with the bubble, not with the name above it and not with the
            // timestamp below it — so it belongs in an HStack with the bubble alone.
            HStack(alignment: .bottom, spacing: 0) {
                if message.mine {
                    Spacer(minLength: 56)
                } else {
                    avatarColumn
                }
                bubble
                if !message.mine { Spacer(minLength: 56) }
            }
            if row.endsRun, let timestamp {
                // The thread showed no time at all before v3 — it existed only in the VoiceOver
                // label. One stamp at the end of a run, not one per bubble.
                Text(timestamp)
                    .font(MonacoTheme.Typo.micro)
                    .foregroundStyle(MonacoTheme.fgSubtle)
                    .padding(.leading, message.mine ? 0 : Self.avatarGutter + 6)
                    .padding(.trailing, message.mine ? 6 : 0)
                    .accessibilityHidden(true)
            }
        }
        .frame(maxWidth: .infinity, alignment: message.mine ? .trailing : .leading)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(accessibilityText)
        .accessibilityIdentifier("group-chat-message-\(message.id)")
    }

    private var bubble: some View {
        Text(message.body)
            .font(.body)
            .foregroundStyle(message.mine ? Color.white : MonacoTheme.fgPrimary)
            .textSelection(.enabled)
            .padding(.horizontal, 14)
            .padding(.vertical, 9)
            .background(bubbleShape.fill(message.mine ? mineFill : MonacoTheme.bgRaised))
            .overlay {
                // A `bgRaised` bubble on `bgBase` is a 1.2:1 step in dark and would be an
                // invisible bubble; in light the two are already separable.
                if !message.mine, colorScheme == .dark {
                    bubbleShape.strokeBorder(MonacoTheme.line, lineWidth: 1)
                }
            }
    }

    private var timestamp: String? {
        row.date?.formatted(date: .omitted, time: .shortened)
    }

    @ViewBuilder
    private var avatarColumn: some View {
        Group {
            if row.startsRun {
                MonacoAvatar(
                    photoURL: author?.profilePhotoUrl,
                    displayName: author?.displayName ?? message.authorName,
                    size: Self.avatarSize
                )
            } else {
                Color.clear.frame(width: Self.avatarSize, height: 1)
            }
        }
        // Reserved on every row from another member, drawn only on the first of a run. The
        // gutter is what stops a run's second bubble sliding left, and a photo finishing loading
        // from reflowing the thread under the reader's thumb.
        .frame(width: Self.avatarGutter, alignment: .leading)
        .accessibilityHidden(true)
    }

    /// Rounded 20 all round, with a tighter corner on the sender's side at the end of a run.
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
        // The same resolved name the bubble draws. A member who renamed themselves must not be
        // one person to a sighted reader and another to VoiceOver on the same bubble.
        let who = message.mine ? "You" : (author?.displayName ?? message.authorName)
        guard let date = row.date else { return "\(who): \(message.body)" }
        return "\(who), \(date.formatted(date: .omitted, time: .shortened)): \(message.body)"
    }
}
