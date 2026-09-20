import MonacoCore
import SwiftUI

/// Threaded discussion under a proposal. Replies sit under their parent, indented up to
/// `ProposalCommentThread.maxIndentLevel` levels.
struct CommentThreadView: View {
    let comments: [ProposalCommentDTO]
    var isLoading = false
    var errorMessage: String?
    var onRetry: () -> Void = {}
    var onReply: (ProposalCommentDTO) -> Void

    private var rows: [ProposalCommentThreadRow] {
        ProposalCommentThread.rows(from: comments)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader(
                ProposalFeedCopy.commentsTitle,
                trailing: comments.isEmpty ? nil : ProposalFeedCopy.commentCount(comments.count)
            )

            if isLoading && comments.isEmpty {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
                    ForEach(0..<2, id: \.self) { _ in
                        HStack(alignment: .top, spacing: MonacoTheme.Space.sm) {
                            SkeletonBlock(width: 28, height: 28, radius: 14)
                            VStack(alignment: .leading, spacing: 6) {
                                SkeletonBlock(width: 100, height: 12)
                                SkeletonBlock(height: 12)
                            }
                        }
                    }
                }
            } else if let errorMessage, comments.isEmpty {
                HStack {
                    Text(errorMessage)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.muted)
                    Spacer()
                    Button(ProposalFeedCopy.tryAgain, action: onRetry)
                        .font(MonacoTheme.Typo.callout.weight(.semibold))
                        .foregroundStyle(MonacoTheme.ink)
                        .frame(minHeight: 44)
                }
                .accessibilityIdentifier("comment-thread-error")
            } else if rows.isEmpty {
                Text(ProposalFeedCopy.emptyThread)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.muted)
                    .accessibilityIdentifier("comment-thread-empty")
            } else {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(rows) { row in
                        CommentRow(row: row, onReply: { onReply(row.comment) })
                    }
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("comment-thread")
    }
}

struct CommentRow: View {
    let row: ProposalCommentThreadRow
    let onReply: () -> Void

    private static let indentWidth: CGFloat = 18

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            ForEach(0..<row.indentLevel, id: \.self) { _ in
                Rectangle()
                    .fill(MonacoTheme.hairline)
                    .frame(width: 2)
                    .padding(.trailing, Self.indentWidth - 2)
            }

            HStack(alignment: .top, spacing: MonacoTheme.Space.sm) {
                MonacoAvatar(photoURL: nil, displayName: row.comment.authorName, size: 28)
                    .padding(.top, 2)
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        Text(row.comment.authorName)
                            .font(MonacoTheme.Typo.callout.weight(.semibold))
                            .foregroundStyle(MonacoTheme.ink)
                            .lineLimit(1)
                        Text(RelativeTimeFormatter.label(iso: row.comment.createdAt))
                            .font(MonacoTheme.Typo.caption)
                            .foregroundStyle(MonacoTheme.tertiaryText)
                    }
                    Text(row.comment.body)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.ink)
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                }
                Spacer(minLength: 0)
                Button(action: onReply) {
                    Image(systemName: "arrowshape.turn.up.left")
                        .font(.system(size: 15, weight: .medium))
                        .foregroundStyle(MonacoTheme.muted)
                        .frame(width: 44, height: 44)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(ProposalFeedCopy.replyAccessibility)
                .accessibilityIdentifier("comment-reply-\(row.comment.id)")
                .padding(.top, -8)
                .padding(.trailing, -12)
            }
        }
        .padding(.vertical, MonacoTheme.Space.s)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("comment-row-\(row.comment.id)")
    }
}

/// Bottom composer: pill field and a round send button. Posts a top-level comment, or a reply when
/// `replyTarget` is set.
struct CommentComposer: View {
    let replyTarget: ProposalCommentDTO?
    let isPosting: Bool
    /// Comments posted from this screen so far. A new value means the draft went out: clear it.
    let postedCount: Int
    let onCancelReply: () -> Void
    let onPost: (String) -> Void

    /// Owned here, not by the screen: a keystroke re-renders the composer and nothing else.
    @State private var text = ""
    @FocusState private var focused: Bool

    private var draft: ProposalCommentDraft {
        ProposalCommentDraft(text: text)
    }

    private var canPost: Bool {
        draft.body != nil && !isPosting
    }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            if let replyTarget {
                HStack {
                    Text(ProposalFeedCopy.replyingTo(replyTarget.authorName))
                        .font(MonacoTheme.Typo.caption.weight(.semibold))
                        .foregroundStyle(MonacoTheme.muted)
                    Spacer()
                    Button {
                        onCancelReply()
                    } label: {
                        Image(systemName: "xmark")
                            .font(.system(size: 12, weight: .semibold))
                            .foregroundStyle(MonacoTheme.muted)
                            .frame(width: 32, height: 32)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Cancel reply")
                    .accessibilityIdentifier("comment-composer-cancel-reply")
                }
            }

            HStack(alignment: .bottom, spacing: MonacoTheme.Space.s) {
                TextField(
                    "",
                    text: $text,
                    prompt: Text(ProposalFeedCopy.composerPlaceholder).foregroundStyle(MonacoTheme.tertiaryText),
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
                    if let body = draft.body {
                        onPost(body)
                    }
                } label: {
                    ZStack {
                        Circle().fill(canPost ? MonacoTheme.primaryButtonFill : MonacoTheme.disabled)
                        if isPosting {
                            ProgressView().tint(MonacoTheme.primaryButtonLabel)
                        } else {
                            Image(systemName: "arrow.up")
                                .font(.system(size: 17, weight: .semibold))
                                .foregroundStyle(MonacoTheme.primaryButtonLabel)
                        }
                    }
                    .frame(width: 44, height: 44)
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
        .padding(.horizontal, MonacoTheme.Space.gutter)
        .padding(.vertical, MonacoTheme.Space.s)
        .background(MonacoTheme.canvas.ignoresSafeArea(edges: .bottom))
        .overlay(alignment: .top) {
            Rectangle().fill(MonacoTheme.hairline).frame(height: 1)
        }
        .onChange(of: replyTarget?.id) { _, newValue in
            if newValue != nil { focused = true }
        }
        .onChange(of: postedCount) { _, _ in
            text = ""
        }
    }
}
