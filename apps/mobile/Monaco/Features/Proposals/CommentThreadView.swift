import MonacoCore
import SwiftUI

/// Threaded discussion under a proposal: a ruled list on the paper, one row per comment, replies
/// indented under their parent with a thin rule running down from the parent's face. Nesting stops
/// indenting at `ProposalCommentThread.maxIndentLevel`.
struct CommentThreadView: View {
    /// Already in display order. The thread is built once, where the comments are stored, so a
    /// re-render of the detail screen does not rebuild it.
    let rows: [ProposalCommentThreadRow]
    var isLoading = false
    var errorMessage: String?
    /// What an empty thread says, in the words of the proposal it sits under.
    var emptyMessage = ProposalFeedCopy.emptyThread
    var onRetry: () -> Void = {}
    var onReply: (ProposalCommentDTO) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader(
                ProposalFeedCopy.commentsTitle,
                trailing: rows.isEmpty ? nil : ProposalFeedCopy.commentCount(rows.count)
            )
            .padding(.horizontal, MonacoTheme.Space.m)

            if isLoading && rows.isEmpty {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
                    ForEach(0..<2, id: \.self) { _ in
                        HStack(alignment: .top, spacing: MonacoTheme.Space.sm) {
                            SkeletonBlock(width: CommentThreadLayout.avatarSize, height: CommentThreadLayout.avatarSize, radius: CommentThreadLayout.avatarSize / 2)
                            VStack(alignment: .leading, spacing: 6) {
                                SkeletonBlock(width: 110, height: 13)
                                SkeletonBlock(height: 13)
                                SkeletonBlock(width: 160, height: 13)
                            }
                        }
                    }
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .padding(.top, MonacoTheme.Space.s)
            } else if let errorMessage, rows.isEmpty {
                HStack(spacing: MonacoTheme.Space.sm) {
                    Text(errorMessage)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                    Spacer(minLength: MonacoTheme.Space.s)
                    Button(ProposalFeedCopy.tryAgain, action: onRetry)
                        .font(MonacoTheme.Typo.calloutStrong)
                        .foregroundStyle(MonacoTheme.brand)
                        .frame(minHeight: 44)
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .accessibilityIdentifier("comment-thread-error")
            } else if rows.isEmpty {
                Text(emptyMessage)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, MonacoTheme.Space.m)
                    .accessibilityIdentifier("comment-thread-empty")
            } else {
                MonacoGroupedList {
                    ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                        CommentRow(
                            row: row,
                            hasReplies: CommentThreadLayout.hasReplies(rows, at: index),
                            isLast: index == rows.count - 1,
                            onReply: { onReply(row.comment) }
                        )
                    }
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("comment-thread")
    }
}

/// Where a comment row puts its face, its words and its rules for how deep it sits.
enum CommentThreadLayout {
    static let avatarSize: CGFloat = 32
    /// How far each level of reply steps in: its face starts under the middle of its parent's.
    static let indentWidth: CGFloat = 28

    /// Leading edge of the face.
    static func avatarLeading(level: Int) -> CGFloat {
        MonacoTheme.Space.m + CGFloat(max(level, 0)) * indentWidth
    }

    /// Where the words start. The rule under a row starts here too, the way a ledger's rules
    /// start at the text rather than at the mark.
    static func textLeading(level: Int) -> CGFloat {
        avatarLeading(level: level) + avatarSize + MonacoTheme.Space.sm
    }

    /// The thin rules a reply draws down its left edge: one under the middle of each ancestor's
    /// face, so a run of replies hangs off the comment it answers.
    static func threadRuleOffsets(level: Int) -> [CGFloat] {
        (0..<max(level, 0)).map { avatarLeading(level: $0) + avatarSize / 2 }
    }

    /// True when the next row answers this one, so this row starts the rule under its own face.
    /// Measured on the indent, not the raw depth: past the deepest indent a reply sits level
    /// with its parent, and a rule leading into it would lead nowhere.
    static func hasReplies(_ rows: [ProposalCommentThreadRow], at index: Int) -> Bool {
        let next = index + 1
        guard rows.indices.contains(index), rows.indices.contains(next) else { return false }
        return rows[next].indentLevel > rows[index].indentLevel
    }
}

/// The Reply target is a full 44pt; its word needs less. The row gives back only the difference,
/// so at the default size the word sits close under the comment, and at the largest sizes, where
/// the word fills the target itself, nothing overlaps the lines around it.
enum CommentRowMetrics {
    static let replyTarget: CGFloat = 44

    static func replyOverhang(lineHeight: CGFloat) -> CGFloat {
        max(0, (replyTarget - lineHeight) / 2)
    }
}

/// One comment: the author's face, their name strong with when they said it in the market's
/// voice, what they said, and Reply as a text action under it.
struct CommentRow: View {
    let row: ProposalCommentThreadRow
    var hasReplies = false
    var isLast = false
    let onReply: () -> Void

    /// One line of the Reply word at the current text size.
    @ScaledMetric(relativeTo: .footnote) private var replyLineHeight: CGFloat = 18

    private var level: Int { row.indentLevel }

    var body: some View {
        HStack(alignment: .top, spacing: MonacoTheme.Space.sm) {
            MonacoAvatar(photoURL: nil, displayName: row.comment.authorName, size: CommentThreadLayout.avatarSize)
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
                    Text(row.comment.authorName)
                        .font(MonacoTheme.Typo.calloutStrong)
                        .foregroundStyle(MonacoTheme.ink)
                        .lineLimit(1)
                    Text(RelativeTimeFormatter.label(iso: row.comment.createdAt))
                        .font(MonacoTheme.Typo.stamp)
                        .foregroundStyle(MonacoTheme.tertiaryText)
                        .fixedSize()
                }
                Text(row.comment.body)
                    .font(MonacoTheme.Typo.body)
                    .foregroundStyle(MonacoTheme.ink)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
                Button(action: onReply) {
                    // The word, not an arrow glyph: a bare arrow read as "share" as often as
                    // "reply". The target is a full 44pt; the layout gives most of that back so
                    // the word sits close under the comment it answers.
                    Text(ProposalFeedCopy.reply)
                        .font(MonacoTheme.Typo.captionStrong)
                        .foregroundStyle(MonacoTheme.brand)
                        .frame(minWidth: CommentRowMetrics.replyTarget, minHeight: CommentRowMetrics.replyTarget, alignment: .leading)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .padding(.vertical, -CommentRowMetrics.replyOverhang(lineHeight: replyLineHeight))
                .padding(.top, MonacoTheme.Space.xs)
                .accessibilityLabel(ProposalFeedCopy.replyAccessibility)
                .accessibilityIdentifier("comment-reply-\(row.comment.id)")
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.leading, CommentThreadLayout.avatarLeading(level: level))
        .padding(.trailing, MonacoTheme.Space.m)
        .padding(.top, MonacoTheme.Space.sm)
        .padding(.bottom, MonacoTheme.Space.sm)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(alignment: .topLeading) { threadRules }
        .overlay(alignment: .bottom) {
            if !isLast {
                MonacoRule().padding(.leading, CommentThreadLayout.textLeading(level: level))
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("comment-row-\(row.comment.id)")
    }

    /// The rules down the left edge: one per ancestor, the full height of the row so a run of
    /// replies joins up, and one under this row's own face when the next row answers it.
    private var threadRules: some View {
        ZStack(alignment: .topLeading) {
            ForEach(CommentThreadLayout.threadRuleOffsets(level: level), id: \.self) { x in
                MonacoTheme.hairline
                    .frame(width: 1)
                    .frame(maxHeight: .infinity)
                    .offset(x: x - 0.5)
            }
            if hasReplies {
                MonacoTheme.hairline
                    .frame(width: 1)
                    .frame(maxHeight: .infinity)
                    .padding(.top, MonacoTheme.Space.sm + CommentThreadLayout.avatarSize + MonacoTheme.Space.xs)
                    .offset(x: CommentThreadLayout.avatarLeading(level: level) + CommentThreadLayout.avatarSize / 2 - 0.5)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .accessibilityHidden(true)
    }
}

/// Bottom composer: a sunken capsule field and an ink send disc. Posts a top-level comment, or a
/// reply when `replyTarget` is set.
///
/// The draft lives here, not on the screen above: typing then invalidates the composer alone,
/// instead of the whole proposal detail body and its comment thread on every keystroke.
struct CommentComposer: View {
    let replyTarget: ProposalCommentDTO?
    let isPosting: Bool
    let onCancelReply: () -> Void
    /// Answers whether the comment was accepted. Only then is the draft cleared and the keyboard
    /// dropped, so the thread the comment landed in is readable again and Reply is reachable.
    let onPost: (String) async -> Bool
    /// Called once the composer has cleared its draft and given up focus, so the thread can scroll
    /// to what was just posted without racing the keyboard's safe-area inset.
    var onDidStandDown: () -> Void = {}

    @State private var text = ""
    @FocusState private var focused: Bool

    private var draft: ProposalCommentDraft {
        ProposalCommentDraft(text: text)
    }

    private var canPost: Bool {
        draft.body != nil && !isPosting
    }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
            if let replyTarget {
                HStack {
                    Text(ProposalFeedCopy.replyingTo(replyTarget.authorName))
                        .font(MonacoTheme.Typo.captionStrong)
                        .foregroundStyle(MonacoTheme.muted)
                    Spacer()
                    Button {
                        onCancelReply()
                    } label: {
                        Image(systemName: "xmark")
                            .font(.system(size: 12, weight: .semibold))
                            .foregroundStyle(MonacoTheme.muted)
                            .frame(width: 44, height: 44)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .padding(.trailing, -MonacoTheme.Space.sm)
                    .accessibilityLabel("Cancel reply")
                    .accessibilityIdentifier("comment-composer-cancel-reply")
                }
            }

            HStack(alignment: .bottom, spacing: MonacoTheme.Space.s) {
                TextField(
                    "",
                    text: $text,
                    prompt: Text(ProposalFeedCopy.composerPlaceholder).foregroundStyle(MonacoTheme.disabledLabel),
                    axis: .vertical
                )
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.ink)
                .tint(MonacoTheme.ink)
                .lineLimit(1...5)
                .focused($focused)
                .padding(.horizontal, MonacoTheme.Space.m)
                .padding(.vertical, 11)
                .frame(minHeight: 44)
                .background(MonacoTheme.surfaceSunken, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
                .accessibilityIdentifier("comment-composer-field")

                Button {
                    guard let body = draft.body else { return }
                    Task {
                        guard await onPost(body) else { return }
                        text = ""
                        focused = false
                        onDidStandDown()
                    }
                } label: {
                    ComposerSendDisc(isLive: canPost || isPosting, isSending: isPosting)
                }
                .buttonStyle(.plain)
                .disabled(!canPost)
                .accessibilityLabel(ProposalFeedCopy.postAccessibility)
                .accessibilityIdentifier("comment-composer-send")
            }

            if case .tooLong = draft {
                Text(ProposalFeedCopy.commentTooLong)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.loss)
                    .accessibilityIdentifier("comment-composer-too-long")
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, MonacoTheme.Space.s)
        .background(MonacoTheme.canvas.ignoresSafeArea(edges: .bottom))
        .overlay(alignment: .top) {
            MonacoRule()
        }
        .onChange(of: replyTarget?.id) { _, newValue in
            if newValue != nil { focused = true }
        }
    }
}

/// The send button both composers share: an ink disc with a cream arrow once there is something
/// to send, sunken with a quiet arrow until then — the primary button's own disabled state.
struct ComposerSendDisc: View {
    /// There is a draft that can go, or one is going.
    let isLive: Bool
    let isSending: Bool

    var body: some View {
        ZStack {
            Circle()
                .fill(isLive ? MonacoTheme.brandFill : MonacoTheme.surfaceSunken)
            if isSending {
                ProgressView()
                    .tint(MonacoTheme.onBrand)
            } else {
                Image(systemName: "arrow.up")
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundStyle(isLive ? MonacoTheme.onBrand : MonacoTheme.disabledLabel)
            }
        }
        .frame(width: 44, height: 44)
        .animation(.easeOut(duration: 0.15), value: isLive)
    }
}
