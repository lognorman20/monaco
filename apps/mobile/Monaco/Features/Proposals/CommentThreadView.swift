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
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text(ProposalFeedCopy.commentsTitle)
                    .font(.headline)
                Spacer()
                if !comments.isEmpty {
                    Text(ProposalFeedCopy.commentCount(comments.count))
                        .font(.caption)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
            }

            if isLoading && comments.isEmpty {
                ProgressView()
                    .tint(MonacoTheme.accent)
                    .frame(maxWidth: .infinity)
            } else if let errorMessage, comments.isEmpty {
                HStack {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.warning)
                    Spacer()
                    Button("Try again", action: onRetry)
                        .font(.footnote.weight(.semibold))
                }
                .accessibilityIdentifier("comment-thread-error")
            } else if rows.isEmpty {
                Text(ProposalFeedCopy.emptyThread)
                    .font(.subheadline)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .accessibilityIdentifier("comment-thread-empty")
            } else {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(rows) { row in
                        CommentRow(row: row, onReply: { onReply(row.comment) })
                    }
                }
            }
        }
        .monacoSurfaceCard()
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("comment-thread")
    }
}

struct CommentRow: View {
    let row: ProposalCommentThreadRow
    let onReply: () -> Void

    private static let indentWidth: CGFloat = 14

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            ForEach(0..<row.indentLevel, id: \.self) { _ in
                Rectangle()
                    .fill(MonacoTheme.border)
                    .frame(width: 1)
                    .padding(.leading, Self.indentWidth - 1)
            }

            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    Text(row.comment.authorName)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(MonacoTheme.primaryText)
                    Text(ProposalTimeFormatter.ageLabel(row.comment.createdAt))
                        .font(.caption)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
                Text(row.comment.body)
                    .font(.subheadline)
                    .foregroundStyle(MonacoTheme.primaryText)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
                Button(ProposalFeedCopy.reply, action: onReply)
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(MonacoTheme.accent)
                    .buttonStyle(.borderless)
                    .frame(minHeight: 32)
                    .accessibilityIdentifier("comment-reply-\(row.comment.id)")
            }
            .padding(.leading, row.indentLevel > 0 ? 10 : 0)
            .padding(.vertical, 8)
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("comment-row-\(row.comment.id)")
    }
}

/// Bottom composer. Posts top-level comments, or a reply when `replyTarget` is set.
struct CommentComposer: View {
    @Binding var text: String
    let replyTarget: ProposalCommentDTO?
    let isPosting: Bool
    let onCancelReply: () -> Void
    let onPost: (String) -> Void

    @FocusState private var focused: Bool

    private var draft: ProposalCommentDraft {
        ProposalCommentDraft(text: text)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let replyTarget {
                HStack {
                    Text(ProposalFeedCopy.replyingTo(replyTarget.authorName))
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(MonacoTheme.secondaryText)
                    Spacer()
                    Button {
                        onCancelReply()
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .foregroundStyle(MonacoTheme.secondaryText)
                    }
                    .accessibilityLabel("Cancel reply")
                    .accessibilityIdentifier("comment-composer-cancel-reply")
                }
            }

            HStack(alignment: .bottom, spacing: 8) {
                TextField(ProposalFeedCopy.composerPlaceholder, text: $text, axis: .vertical)
                    .lineLimit(1...5)
                    .focused($focused)
                    .monacoFormTextField()
                    .padding(.horizontal, 12)
                    .padding(.vertical, 10)
                    .background(MonacoTheme.background, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                    .accessibilityIdentifier("comment-composer-field")

                Button {
                    if let body = draft.body {
                        focused = false
                        onPost(body)
                    }
                } label: {
                    if isPosting {
                        ProgressView().tint(MonacoTheme.primaryButtonLabel)
                    } else {
                        Text(ProposalFeedCopy.send)
                    }
                }
                .buttonStyle(.monacoPrimary)
                .disabled(draft.body == nil || isPosting)
                .accessibilityIdentifier("comment-composer-send")
            }

            if case .tooLong = draft {
                Text(ProposalFeedCopy.commentTooLong)
                    .font(.caption)
                    .foregroundStyle(MonacoTheme.warning)
                    .accessibilityIdentifier("comment-composer-too-long")
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .background(MonacoTheme.surface)
        .overlay(alignment: .top) {
            Rectangle().fill(MonacoTheme.border).frame(height: 1)
        }
        .onChange(of: replyTarget?.id) { _, newValue in
            if newValue != nil { focused = true }
        }
    }
}
