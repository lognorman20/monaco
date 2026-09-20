import SwiftUI
import MonacoCore

/// Cabal chat: members-only message thread with a composer. Polls for new messages while visible.
///
/// The draft lives in `GroupChatComposer`, not here: a keystroke must never re-run the thread.
struct GroupChatView: View {
    let groupId: String
    let groupName: String?
    /// Returns the chat transport for the current session, or nil when signed out.
    let makeService: () -> (any GroupChatService)?

    @State private var timeline = GroupChatTimeline()
    @State private var isLoadingOlder = false
    @State private var loadError: String?
    @State private var toast: MonacoToast?
    @State private var refreshGate = RefreshGate()
    /// The send in flight, so the next one waits its turn and messages post in the order typed.
    @State private var lastSend: Task<Void, Never>?
    @FocusState private var composerFocused: Bool

    private let pageSize = 30
    private let pollInterval: Duration = .seconds(4)
    private static let bottomAnchor = "group-chat-bottom"

    var body: some View {
        VStack(spacing: 0) {
            messagesArea
            GroupChatComposer(focus: $composerFocused) { body in send(body) }
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
        .task {
            await refreshGate.runNow { await loadNewest() }
        }
        .pollWhileVisible(every: pollInterval, gate: refreshGate) {
            try await pollNewest()
        }
        .monacoToast($toast)
        .accessibilityIdentifier("group-chat-view")
    }

    // MARK: - Messages

    @ViewBuilder
    private var messagesArea: some View {
        if !timeline.hasLoadedNewest, timeline.rows.isEmpty {
            if let loadError {
                statusMessage {
                    Label(loadError, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(MonacoTheme.warning)
                    Button(GroupChatCopy.tryAgain) { Task { await refreshGate.runNow { await loadNewest() } } }
                        .buttonStyle(.monacoPrimary)
                        .accessibilityIdentifier("group-chat-retry")
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

                    // Rows arrive with their separator and run flags worked out at merge time.
                    ForEach(timeline.rows) { row in
                        if let separatorDate = row.separatorDate {
                            Text(GroupChatCopy.timeSeparatorLabel(separatorDate))
                                .font(MonacoTheme.Typo.micro)
                                .foregroundStyle(MonacoTheme.muted)
                                .frame(maxWidth: .infinity)
                                .padding(.top, row.id == timeline.rows.first?.id ? 8 : 16)
                                .padding(.bottom, 4)
                                .accessibilityIdentifier("group-chat-separator-\(row.serverId ?? row.id)")
                        }
                        GroupChatBubble(
                            row: row,
                            onRetry: { retry(clientId: row.id) },
                            onDiscard: { timeline.discardFailed(clientId: row.id) }
                        )
                        .equatable()
                        .padding(.top, row.startsRun && row.separatorDate == nil ? 10 : 0)
                        .id(row.id)
                    }

                    Color.clear.frame(height: 1).id(Self.bottomAnchor)
                }
                .padding(.horizontal, 16)
                .padding(.bottom, 12)
            }
            .defaultScrollAnchor(.bottom)
            .scrollDismissesKeyboard(.interactively)
            .refreshable { await refreshGate.runNow { await loadNewest() } }
            .onChange(of: timeline.rows.last?.id) { _, _ in
                withAnimation(.easeOut(duration: 0.2)) {
                    proxy.scrollTo(Self.bottomAnchor, anchor: .bottom)
                }
            }
            .accessibilityIdentifier("group-chat-thread")
        }
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
            timeline.mergeNewest(page)
            loadError = nil
        } catch is CancellationError {
            return
        } catch {
            if timeline.hasLoadedNewest {
                toast = MonacoToast(message: GroupChatCopy.loadFailure(error))
            } else {
                loadError = GroupChatCopy.loadFailure(error)
            }
        }
    }

    private func loadOlder() async {
        guard let cursor = timeline.olderCursor, !isLoadingOlder, let service = makeService() else { return }
        isLoadingOlder = true
        defer { isLoadingOlder = false }
        do {
            let page = try await service.listGroupMessages(groupId: groupId, before: cursor, limit: pageSize)
            timeline.mergeOlder(page)
        } catch is CancellationError {
            return
        } catch {
            toast = MonacoToast(message: GroupChatCopy.loadFailure(error))
        }
    }

    /// One quiet tick of `pollWhileVisible`. The API pages backwards only (`before`), so a tick
    /// reads the newest page; the timeline is written only when that page held something new.
    /// Throwing is how a tick reports failure, which is what makes the loop back off.
    private func pollNewest() async throws {
        guard let service = makeService() else { return }
        let page = try await service.listGroupMessages(groupId: groupId, before: nil, limit: pageSize)
        var merged = timeline
        merged.mergeNewest(page)
        QuietUpdate.apply(merged, over: timeline) { timeline = $0 }
        if loadError != nil { loadError = nil }
    }

    // MARK: - Sending

    /// Shows the message at once, then posts it. `body` is already validated by the composer.
    private func send(_ body: String) {
        let clientId = UUID().uuidString
        timeline.beginSend(clientId: clientId, body: body)
        deliver(clientId: clientId, body: body)
    }

    private func retry(clientId: String) {
        guard let body = timeline.retrySend(clientId: clientId) else { return }
        Haptics.tap()
        deliver(clientId: clientId, body: body)
    }

    /// Posts after the previous send has settled. Not tied to the view's lifetime: a message the
    /// member sent still goes out if they leave the screen straight after.
    private func deliver(clientId: String, body: String) {
        let previous = lastSend
        lastSend = Task {
            await previous?.value
            guard let service = makeService() else {
                fail(clientId: clientId, error: MonacoCore.MonacoAPIError.httpStatus(401))
                return
            }
            do {
                let sent = try await service.postGroupMessage(groupId: groupId, body: body)
                timeline.confirmSent(clientId: clientId, message: sent)
            } catch {
                fail(clientId: clientId, error: error)
            }
        }
    }

    private func fail(clientId: String, error: Error) {
        // A poll may already have delivered it (the answer was lost, not the message): say nothing.
        guard timeline.failSend(clientId: clientId) == .markedFailed else { return }
        toast = MonacoToast(message: GroupChatCopy.sendFailure(error))
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

/// The text field and send button. Owns the draft so typing re-renders this view only.
private struct GroupChatComposer: View {
    let focus: FocusState<Bool>.Binding
    /// Receives the trimmed, validated body. The field clears as soon as it is handed over.
    let onSend: (String) -> Void

    @State private var draft = ""

    var body: some View {
        let sendable = try? GroupChatDraft.validate(draft).get()
        let count = draft.trimmingCharacters(in: .whitespacesAndNewlines).unicodeScalars.count

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
                    guard let sendable else { return }
                    Haptics.tap()
                    draft = ""
                    onSend(sendable)
                } label: {
                    ZStack {
                        Circle()
                            .fill(sendable != nil ? MonacoTheme.primaryButtonFill : MonacoTheme.disabled)
                        Image(systemName: "arrow.up")
                            .font(.system(size: 17, weight: .semibold))
                            .foregroundStyle(MonacoTheme.primaryButtonLabel)
                    }
                    .frame(width: 44, height: 44)
                }
                .disabled(sendable == nil)
                .accessibilityLabel("Send message")
                .accessibilityIdentifier("group-chat-send")
            }

            if count > GroupChatDraft.maxCharacters - 200 {
                Text("\(count)/\(GroupChatDraft.maxCharacters)")
                    .font(.caption2.monospacedDigit())
                    .foregroundStyle(count > GroupChatDraft.maxCharacters ? MonacoTheme.destructive : MonacoTheme.secondaryText)
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
}

/// Equatable on the row alone so SwiftUI skips bubbles whose row did not change; the closures
/// only ever act on `row.id`.
private struct GroupChatBubble: View, Equatable {
    let row: GroupChatRow
    let onRetry: () -> Void
    let onDiscard: () -> Void

    static func == (lhs: GroupChatBubble, rhs: GroupChatBubble) -> Bool {
        lhs.row == rhs.row
    }

    var body: some View {
        HStack {
            if row.mine { Spacer(minLength: 56) }
            VStack(alignment: row.mine ? .trailing : .leading, spacing: 4) {
                if !row.mine && row.startsRun {
                    Text(row.authorName)
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(MonacoTheme.muted)
                        .padding(.horizontal, 14)
                }
                Text(row.body)
                    .font(.body)
                    .foregroundStyle(row.mine ? MonacoTheme.primaryButtonLabel : MonacoTheme.ink)
                    .textSelection(.enabled)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 9)
                    .background(bubbleShape.fill(row.mine ? MonacoTheme.primaryButtonFill : MonacoTheme.surface))
                    .opacity(row.delivery == .delivered ? 1 : 0.55)
                deliveryNote
            }
            if !row.mine { Spacer(minLength: 56) }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(accessibilityText)
        .accessibilityIdentifier("group-chat-message-\(row.serverId ?? row.id)")
    }

    @ViewBuilder
    private var deliveryNote: some View {
        switch row.delivery {
        case .delivered:
            EmptyView()
        case .sending:
            Text(GroupChatCopy.sending)
                .font(.caption2)
                .foregroundStyle(MonacoTheme.muted)
                .padding(.horizontal, 14)
                .accessibilityIdentifier("group-chat-sending-\(row.id)")
        case .failed:
            Button(action: onRetry) {
                Label(GroupChatCopy.notSent, systemImage: "exclamationmark.circle.fill")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(MonacoTheme.destructive)
                    .padding(.horizontal, 14)
                    .frame(minHeight: 44, alignment: .top)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .contextMenu {
                Button(GroupChatCopy.tryAgain, systemImage: "arrow.clockwise", action: onRetry)
                Button(GroupChatCopy.deleteUnsent, systemImage: "trash", role: .destructive, action: onDiscard)
            }
            .accessibilityIdentifier("group-chat-retry-\(row.id)")
        }
    }

    /// Rounded 20 all round, with a tighter corner on the sender's side at the end of a run.
    private var bubbleShape: UnevenRoundedRectangle {
        let radius = MonacoTheme.Radius.bubble
        let tail: CGFloat = row.endsRun ? 6 : radius
        return UnevenRoundedRectangle(
            topLeadingRadius: radius,
            bottomLeadingRadius: row.mine ? radius : tail,
            bottomTrailingRadius: row.mine ? tail : radius,
            topTrailingRadius: radius,
            style: .continuous
        )
    }

    private var accessibilityText: String {
        let who = row.mine ? "You" : row.authorName
        switch row.delivery {
        case .sending:
            return "\(who), \(GroupChatCopy.sending): \(row.body)"
        case .failed:
            return "\(who), \(GroupChatCopy.notSent): \(row.body)"
        case .delivered:
            guard let date = row.date else { return "\(who): \(row.body)" }
            return "\(who), \(date.formatted(date: .omitted, time: .shortened)): \(row.body)"
        }
    }
}
