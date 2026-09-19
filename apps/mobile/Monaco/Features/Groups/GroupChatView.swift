import SwiftUI
import MonacoCore

/// Cabal chat: members-only message thread with a composer. Polls for new messages while visible.
struct GroupChatView: View {
    let groupId: String
    let groupName: String?
    /// Returns the chat transport for the current session, or nil when signed out.
    let makeService: () -> (any GroupChatService)?

    @State private var timeline = GroupChatTimeline()
    @State private var draft = ""
    @State private var isSending = false
    @State private var isLoadingOlder = false
    @State private var loadError: String?
    @State private var toast: MonacoToast?
    @FocusState private var composerFocused: Bool

    private let pageSize = 30
    private let pollInterval: Duration = .seconds(4)
    private static let bottomAnchor = "group-chat-bottom"

    var body: some View {
        VStack(spacing: 0) {
            messagesArea
            composer
        }
        .background(MonacoTheme.background)
        .navigationTitle(GroupChatCopy.title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if let groupName, !groupName.isEmpty {
                ToolbarItem(placement: .principal) {
                    VStack(spacing: 0) {
                        Text(GroupChatCopy.title).font(.headline)
                        Text(groupName)
                            .font(.caption)
                            .foregroundStyle(MonacoTheme.secondaryText)
                    }
                    .accessibilityElement(children: .combine)
                }
            }
        }
        .task {
            await loadNewest()
            await pollWhileVisible()
        }
        .monacoToast($toast)
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
                    Button("Try again") { Task { await loadNewest() } }
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
        } else if timeline.messages.isEmpty {
            statusMessage {
                Image(systemName: "bubble.left.and.bubble.right")
                    .font(.title2)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .accessibilityHidden(true)
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
                LazyVStack(alignment: .leading, spacing: 2) {
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

                    ForEach(Array(timeline.messages.enumerated()), id: \.element.id) { index, message in
                        let previous = index > 0 ? timeline.messages[index - 1] : nil
                        GroupChatBubble(
                            message: message,
                            showsAuthor: !message.mine && previous?.authorId != message.authorId
                        )
                        .padding(.top, previous?.authorId == message.authorId ? 0 : 8)
                        .id(message.id)
                    }

                    Color.clear.frame(height: 1).id(Self.bottomAnchor)
                }
                .padding(.horizontal, 12)
                .padding(.bottom, 8)
            }
            .defaultScrollAnchor(.bottom)
            .scrollDismissesKeyboard(.interactively)
            .refreshable { await loadNewest() }
            .onChange(of: timeline.messages.last?.id) { _, _ in
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

    // MARK: - Composer

    private var composer: some View {
        let validation = GroupChatDraft.validate(draft)
        let canSend = !isSending && (try? validation.get()) != nil
        let count = draft.trimmingCharacters(in: .whitespacesAndNewlines).unicodeScalars.count

        return VStack(alignment: .trailing, spacing: 4) {
            HStack(alignment: .bottom, spacing: 8) {
                TextField(GroupChatCopy.composerPlaceholder, text: $draft, axis: .vertical)
                    .lineLimit(1...5)
                    .focused($composerFocused)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 9)
                    .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
                    .overlay {
                        RoundedRectangle(cornerRadius: 18, style: .continuous)
                            .strokeBorder(MonacoTheme.border, lineWidth: 1)
                    }
                    .accessibilityIdentifier("group-chat-composer")

                Button {
                    Task { await send() }
                } label: {
                    if isSending {
                        ProgressView()
                            .tint(MonacoTheme.accent)
                            .frame(width: 34, height: 34)
                    } else {
                        Image(systemName: "arrow.up.circle.fill")
                            .font(.system(size: 34))
                            .foregroundStyle(canSend ? MonacoTheme.primaryButtonFill : MonacoTheme.disabled)
                    }
                }
                .disabled(!canSend)
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
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(MonacoTheme.background)
        .overlay(alignment: .top) {
            Rectangle().fill(MonacoTheme.border).frame(height: 0.5)
        }
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

    /// Quietly fetches the newest page every few seconds; a failed poll waits for the next tick.
    private func pollWhileVisible() async {
        while !Task.isCancelled {
            try? await Task.sleep(for: pollInterval)
            guard !Task.isCancelled, let service = makeService() else { return }
            if let page = try? await service.listGroupMessages(groupId: groupId, before: nil, limit: pageSize) {
                timeline.mergeNewest(page)
                if loadError != nil { loadError = nil }
            }
        }
    }

    private func send() async {
        guard !isSending else { return }
        let body: String
        switch GroupChatDraft.validate(draft) {
        case .success(let trimmed):
            body = trimmed
        case .failure(let problem):
            toast = MonacoToast(message: GroupChatCopy.sendFailure(problem))
            return
        }
        guard let service = makeService() else {
            toast = MonacoToast(message: GroupChatCopy.sendFailure(MonacoCore.MonacoAPIError.httpStatus(401)))
            return
        }

        let submitted = draft
        isSending = true
        defer { isSending = false }
        do {
            let sent = try await service.postGroupMessage(groupId: groupId, body: body)
            timeline.appendSent(sent)
            // Keep anything typed while the request was in flight.
            if draft.hasPrefix(submitted) {
                draft = String(draft.dropFirst(submitted.count)).trimmingCharacters(in: .whitespaces)
            }
        } catch is CancellationError {
            return
        } catch {
            // Keep the draft so the member can retry without retyping.
            toast = MonacoToast(message: GroupChatCopy.sendFailure(error))
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

private struct GroupChatBubble: View {
    let message: GroupMessageDTO
    let showsAuthor: Bool

    var body: some View {
        HStack {
            if message.mine { Spacer(minLength: 48) }
            VStack(alignment: message.mine ? .trailing : .leading, spacing: 3) {
                if showsAuthor {
                    Text(message.authorName)
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(MonacoTheme.secondaryText)
                        .padding(.horizontal, 4)
                }
                Text(message.body)
                    .font(.body)
                    .foregroundStyle(message.mine ? MonacoTheme.primaryButtonLabel : MonacoTheme.primaryText)
                    .textSelection(.enabled)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .background(bubbleShape.fill(message.mine ? MonacoTheme.primaryButtonFill : MonacoTheme.surface))
                    .overlay {
                        if !message.mine {
                            bubbleShape.strokeBorder(MonacoTheme.border, lineWidth: 1)
                        }
                    }
                if let date = message.createdAtDate {
                    Text(Self.timestamp(date))
                        .font(.caption2)
                        .foregroundStyle(MonacoTheme.secondaryText)
                        .padding(.horizontal, 4)
                }
            }
            if !message.mine { Spacer(minLength: 48) }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(accessibilityText)
        .accessibilityIdentifier("group-chat-message-\(message.id)")
    }

    private var bubbleShape: RoundedRectangle {
        RoundedRectangle(cornerRadius: 16, style: .continuous)
    }

    private var accessibilityText: String {
        let who = message.mine ? "You" : message.authorName
        return "\(who): \(message.body)"
    }

    /// Local time on display; the API stores UTC.
    private static func timestamp(_ date: Date) -> String {
        if Calendar.current.isDateInToday(date) {
            return date.formatted(date: .omitted, time: .shortened)
        }
        return date.formatted(.dateTime.month(.abbreviated).day().hour().minute())
    }
}
