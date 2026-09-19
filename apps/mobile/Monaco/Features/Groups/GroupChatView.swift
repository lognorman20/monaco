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

                    ForEach(Array(timeline.messages.enumerated()), id: \.element.id) { index, message in
                        let previous = index > 0 ? timeline.messages[index - 1] : nil
                        let next = index + 1 < timeline.messages.count ? timeline.messages[index + 1] : nil
                        let separator = separatorLabel(for: message, previous: previous)
                        let startsRun = separator != nil || previous?.authorId != message.authorId
                        let endsRun = next?.authorId != message.authorId
                            || next.map { separatorLabel(for: $0, previous: message) != nil } == true
                        if let separator {
                            Text(separator)
                                .font(MonacoTheme.Typo.micro)
                                .foregroundStyle(MonacoTheme.muted)
                                .frame(maxWidth: .infinity)
                                .padding(.top, index == 0 ? 8 : 16)
                                .padding(.bottom, 4)
                                .accessibilityIdentifier("group-chat-separator-\(message.id)")
                        }
                        GroupChatBubble(
                            message: message,
                            showsAuthor: !message.mine && startsRun,
                            endsRun: endsRun
                        )
                        .padding(.top, startsRun && separator == nil ? 10 : 0)
                        .id(message.id)
                    }

                    Color.clear.frame(height: 1).id(Self.bottomAnchor)
                }
                .padding(.horizontal, 16)
                .padding(.bottom, 12)
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

    private func separatorLabel(for message: GroupMessageDTO, previous: GroupMessageDTO?) -> String? {
        guard let date = message.createdAtDate else { return nil }
        guard GroupChatCopy.showsTimeSeparator(previous: previous?.createdAtDate, current: date) else { return nil }
        return GroupChatCopy.timeSeparatorLabel(date)
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
                    .padding(.horizontal, 16)
                    .padding(.vertical, 11)
                    .frame(minHeight: 44)
                    .background(MonacoTheme.surfaceSunken, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
                    .accessibilityIdentifier("group-chat-composer")

                Button {
                    Haptics.tap()
                    Task { await send() }
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
    let endsRun: Bool

    var body: some View {
        HStack {
            if message.mine { Spacer(minLength: 56) }
            VStack(alignment: message.mine ? .trailing : .leading, spacing: 4) {
                if showsAuthor {
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
        let tail: CGFloat = endsRun ? 6 : radius
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
        guard let date = message.createdAtDate else { return "\(who): \(message.body)" }
        return "\(who), \(date.formatted(date: .omitted, time: .shortened)): \(message.body)"
    }
}
