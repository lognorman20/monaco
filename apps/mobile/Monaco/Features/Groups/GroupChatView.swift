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
    /// Set once this thread is closed to the viewer (removed from the cabal, cabal deleted).
    /// Parks the poll and the composer: there is nothing left to ask the server for.
    @State private var closedMessage: String?
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
    @State private var refreshGate = RefreshGate()
    @FocusState private var composerFocused: Bool

    private let pageSize = 30
    private let pollInterval: Duration = .seconds(4)
    private static let bottomAnchor = "group-chat-bottom"
    /// How close to the end counts as "reading the bottom", in points.
    private static let pinnedSlack: CGFloat = 40

    var body: some View {
        VStack(spacing: 0) {
            messagesArea
            if let closedMessage {
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
                        .font(.headline)
                        .foregroundStyle(MonacoTheme.ink)
                        .lineLimit(1)
                }
                .accessibilityElement(children: .combine)
                .accessibilityAddTraits(.isHeader)
            }
        }
        .task { await loadNewest() }
        .pollWhileVisible(every: pollInterval, isActive: closedMessage == nil, gate: refreshGate) {
            try await pollNewest()
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
                    if closedMessage == nil {
                        Button("Try again") { Task { await loadNewest() } }
                            .buttonStyle(.monacoPrimary)
                            .accessibilityIdentifier("group-chat-retry")
                    }
                }
                .accessibilityIdentifier("group-chat-error")
            } else {
                statusMessage {
                    ProgressView("Loading messages…")
                        .tint(MonacoTheme.accent)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
                .accessibilityIdentifier("group-chat-loading")
            }
        } else if timeline.rows.isEmpty {
            statusMessage {
                Text(GroupChatCopy.emptyState)
                    .font(.subheadline)
                    .foregroundStyle(MonacoTheme.secondaryText)
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
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 3) {
                    if timeline.hasOlder {
                        loadEarlierButton
                    }

                    ForEach(timeline.rows) { row in
                        if let separator = row.timeSeparatorLabel() {
                            Text(separator)
                                .font(MonacoTheme.Typo.micro)
                                .foregroundStyle(MonacoTheme.muted)
                                .frame(maxWidth: .infinity)
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
            .onScrollGeometryChange(for: Bool.self) { geometry in
                geometry.contentOffset.y + geometry.containerSize.height
                    >= geometry.contentSize.height - Self.pinnedSlack
            } action: { _, isAtBottom in
                // Reaching the end is the reader rejoining the conversation, whether they
                // dragged there or we took them. Nothing here may set `readerControlsScroll`:
                // content growing pushes the end away for a frame or two, and that is the
                // thread working, not the reader leaving.
                guard isAtBottom else { return }
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
                // A drag, not our own animated scroll: from here the position is theirs.
                if phase == .interacting { readerControlsScroll = true }
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
                    .font(.footnote.weight(.semibold))
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

    private func closedBanner(_ message: String) -> some View {
        Label(message, systemImage: "lock.fill")
            .font(.footnote)
            .foregroundStyle(MonacoTheme.secondaryText)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 16)
            .padding(.vertical, 14)
            .background(MonacoTheme.background)
            .overlay(alignment: .top) {
                Rectangle().fill(MonacoTheme.border).frame(height: 0.5)
            }
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
        } catch is CancellationError {
            return
        } catch {
            if let closed = GroupChatCopy.chatClosed(error) {
                closedMessage = closed
            }
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
        } catch {
            if let closed = GroupChatCopy.chatClosed(error) {
                closedMessage = closed
                return
            }
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
        do {
            let page = try await service.listGroupMessages(groupId: groupId, before: cursor, limit: pageSize)
            timeline.mergeOlder(page)
            keepInViewRowID = topRowID
        } catch is CancellationError {
            return
        } catch {
            if let closed = GroupChatCopy.chatClosed(error) { closedMessage = closed }
            toast = MonacoToast(message: GroupChatCopy.earlierFailure(error))
        }
    }

    /// Posts `body`. Returns true once it has landed, which is what clears the composer.
    private func send(_ body: String) async -> Bool {
        guard let service = makeService() else {
            toast = MonacoToast(message: GroupChatCopy.sendFailure(MonacoCore.MonacoAPIError.httpStatus(401)))
            return false
        }
        do {
            let sent = try await service.postGroupMessage(groupId: groupId, body: body)
            timeline.appendSent(sent)
            followThread()
            return true
        } catch is CancellationError {
            return false
        } catch {
            if let closed = GroupChatCopy.chatClosed(error) { closedMessage = closed }
            toast = MonacoToast(message: GroupChatCopy.sendFailure(error))
            return false
        }
    }
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
    /// Posts the trimmed body; true once it landed.
    let send: (String) async -> Bool

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
                    .padding(.horizontal, 16)
                    .padding(.vertical, 11)
                    .frame(minHeight: 44)
                    .background(MonacoTheme.surfaceSunken, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
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
                    .foregroundStyle(trimmedCount > GroupChatDraft.maxCharacters ? MonacoTheme.destructive : MonacoTheme.secondaryText)
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
        if await send(body) { return }
        // Failed. Give the text back, unless something new was typed while it was in flight.
        if draft.isEmpty {
            draft = submitted
        }
    }
}

private struct GroupChatBubble: View {
    let row: GroupChatRow

    private var message: GroupMessageDTO { row.message }

    var body: some View {
        HStack {
            if message.mine { Spacer(minLength: 56) }
            VStack(alignment: message.mine ? .trailing : .leading, spacing: 4) {
                if !message.mine, row.startsRun {
                    Text(message.authorName)
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(MonacoTheme.muted)
                        .padding(.horizontal, 14)
                }
                Text(message.body)
                    .font(.body)
                    .foregroundStyle(message.mine ? MonacoTheme.primaryButtonLabel : MonacoTheme.ink)
                    .textSelection(.enabled)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 9)
                    .background(bubbleShape.fill(message.mine ? MonacoTheme.primaryButtonFill : MonacoTheme.surface))
            }
            if !message.mine { Spacer(minLength: 56) }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(accessibilityText)
        .accessibilityIdentifier("group-chat-message-\(message.id)")
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
        let who = message.mine ? "You" : message.authorName
        guard let date = row.date else { return "\(who): \(message.body)" }
        return "\(who), \(date.formatted(date: .omitted, time: .shortened)): \(message.body)"
    }
}
