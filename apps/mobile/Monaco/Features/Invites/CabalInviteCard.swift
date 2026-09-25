import MonacoCore
import SwiftUI
import UIKit

/// The invite card on a cabal's details sheet: the one card on the sheet, because sharing the
/// cabal is what a member opens it for.
///
/// The QR code of the link sits beside the short code, so a friend in the room scans it and a
/// friend elsewhere gets the link. "Share invite" is the primary action; Copy link and Copy
/// code follow; "New code" retires the current one after a confirmation.
struct CabalInviteCard: View {
    let cabalName: String
    @State private var model: CabalInviteModel
    /// Toasts go to the sheet, which owns the overlay.
    private let onToast: (MonacoToast) -> Void

    init(groupId: String, cabalName: String, source: InviteSource, onToast: @escaping (MonacoToast) -> Void = { _ in }) {
        self.cabalName = cabalName
        _model = State(initialValue: CabalInviteModel(groupId: groupId, source: source))
        self.onToast = onToast
    }

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @State private var copied: CopiedField?
    @State private var confirmNewCode = false

    private enum CopiedField { case link, code }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            switch model.state {
            case .loading:
                loadingBody
            case .failed(let message):
                failedBody(message)
            case .loaded(let invite):
                loadedBody(invite)
            }
        }
        .padding(MonacoTheme.Space.m)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            MonacoTheme.surface,
            in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
        }
        .task { await model.load() }
        .confirmationDialog(CabalInviteCopy.newCodeTitle, isPresented: $confirmNewCode, titleVisibility: .visible) {
            Button(CabalInviteCopy.newCodeConfirm, role: .destructive) {
                Task { await renew() }
            }
        } message: {
            Text(CabalInviteCopy.newCodeMessage)
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("group-invite-card")
    }

    // MARK: - Loaded

    @ViewBuilder
    private func loadedBody(_ invite: InviteDTO) -> some View {
        codeAndQR(invite)

        ShareLink(
            item: InviteShareText.message(cabalName: cabalName, link: invite.link),
            subject: Text(InviteShareText.subject(cabalName: cabalName)),
            preview: SharePreview(InviteShareText.subject(cabalName: cabalName))
        ) {
            Label(CabalInviteCopy.share, systemImage: "square.and.arrow.up")
        }
        .buttonStyle(.monacoPrimary)
        .monacoFullWidthButtons()
        .accessibilityIdentifier("group-invite-share-button")

        let copyRow = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(spacing: MonacoTheme.Space.sm))
            : AnyLayout(HStackLayout(spacing: MonacoTheme.Space.sm))
        copyRow {
            Button {
                copy(.link, value: invite.link.absoluteString)
            } label: {
                copyLabel(.link, idle: CabalInviteCopy.copyLink, systemImage: "link")
            }
            .buttonStyle(.monacoSecondary)
            .accessibilityIdentifier("group-invite-copy-link-button")

            Button {
                copy(.code, value: invite.code)
            } label: {
                copyLabel(.code, idle: CabalInviteCopy.copyCode, systemImage: "doc.on.doc")
            }
            .buttonStyle(.monacoSecondary)
            .accessibilityIdentifier("group-invite-copy-button")
        }
        .monacoFullWidthButtons()

        Button {
            confirmNewCode = true
        } label: {
            Text(model.isRenewing ? CabalInviteCopy.renewing : CabalInviteCopy.newCode)
                .font(MonacoTheme.Typo.calloutStrong)
                .foregroundStyle(MonacoTheme.brand)
                .frame(minHeight: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(model.isRenewing)
        .accessibilityIdentifier("group-invite-new-code-button")
    }

    /// The QR tile and the code side by side; stacked at the accessibility sizes, where the
    /// code needs the whole width.
    @ViewBuilder
    private func codeAndQR(_ invite: InviteDTO) -> some View {
        let layout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: MonacoTheme.Space.m))
            : AnyLayout(HStackLayout(alignment: .top, spacing: MonacoTheme.Space.m))
        layout {
            InviteQRCodeView(link: invite.link)
                .frame(width: 128, height: 128)
                .accessibilityIdentifier("group-invite-qr")

            VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                Text(CabalInviteCopy.codeLabel)
                    .font(MonacoTheme.Typo.captionStrong)
                    .foregroundStyle(MonacoTheme.muted)
                Text(InviteLink.displayCode(invite.code))
                    .font(MonacoTheme.Typo.data)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
                    .textSelection(.enabled)
                    .accessibilityLabel(spokenCode(invite.code))
                    .accessibilityIdentifier("group-invite-code")
                Text(CabalInviteCopy.hint(cabalName: cabalName))
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.top, MonacoTheme.Space.xs)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    /// VoiceOver reads a code symbol by symbol, not as two made-up words.
    private func spokenCode(_ code: String) -> String {
        code.map(String.init).joined(separator: " ")
    }

    // MARK: - Loading and failure

    private var loadingBody: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            HStack(alignment: .top, spacing: MonacoTheme.Space.m) {
                SkeletonBlock(width: 128, height: 128, radius: MonacoTheme.Radius.tile)
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    SkeletonBlock(width: 72, height: 12)
                    SkeletonBlock(width: 104, height: 18)
                    SkeletonBlock(height: 12)
                    SkeletonBlock(width: 120, height: 12)
                }
            }
            SkeletonBlock(height: MonacoButtonMetrics.minimumHeight, radius: MonacoButtonMetrics.minimumHeight / 2)
            HStack(spacing: MonacoTheme.Space.sm) {
                SkeletonBlock(height: MonacoButtonMetrics.minimumHeight, radius: MonacoButtonMetrics.minimumHeight / 2)
                SkeletonBlock(height: MonacoButtonMetrics.minimumHeight, radius: MonacoButtonMetrics.minimumHeight / 2)
            }
            // "New code", so the card does not grow when the code lands.
            SkeletonBlock(width: 84, height: 14)
                .frame(minHeight: 44)
        }
        .accessibilityElement()
        .accessibilityLabel("Loading the invite code")
        .accessibilityIdentifier("group-invite-loading")
    }

    private func failedBody(_ message: String) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            Text(CabalInviteCopy.codeLabel)
                .font(MonacoTheme.Typo.captionStrong)
                .foregroundStyle(MonacoTheme.muted)
            Text(message)
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.ink)
                .fixedSize(horizontal: false, vertical: true)
            Button(CabalInviteCopy.retry) {
                Task { await model.load() }
            }
            .buttonStyle(.monacoSecondary)
            .monacoFullWidthButtons()
            .accessibilityIdentifier("group-invite-retry")
        }
    }

    // MARK: - Actions

    private func renew() async {
        guard let toast = await model.renew() else { return }
        if toast.isSuccess { Haptics.success() }
        onToast(toast)
    }

    private func copyLabel(_ field: CopiedField, idle: String, systemImage: String) -> some View {
        let isCopied = copied == field
        return Label(isCopied ? CabalInviteCopy.copied : idle, systemImage: isCopied ? "checkmark" : systemImage)
    }

    private func copy(_ field: CopiedField, value: String) {
        UIPasteboard.general.string = value
        Haptics.selection()
        copied = field
        Task {
            try? await Task.sleep(for: .seconds(2))
            if copied == field { copied = nil }
        }
    }
}
