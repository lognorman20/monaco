import SwiftUI
import UIKit

/// "Cabal details" from the cabal screen's toolbar.
///
/// The invite code is what a member comes here for, so it is the one card: the code, Copy and
/// Share. The cabal's account on Solana follows as a quiet ruled section for developers — a raw
/// address is the most "crypto" thing in the app, so it is here and not on the cabal screen, and
/// even here it comes second. Leave sits at the bottom.
struct GroupDetailsSheet: View {
    let groupId: String
    let treasuryAddress: String?
    let isLeaving: Bool
    let onLeave: () -> Void

    @Environment(\.dismiss) private var dismiss
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @State private var copiedField: CopiedField?

    private enum CopiedField { case address, invite }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                    inviteCard
                        .padding(.horizontal, MonacoTheme.Space.m)

                    if let treasuryAddress, !treasuryAddress.isEmpty {
                        developerSection(treasuryAddress)
                    }

                    Button(role: .destructive, action: onLeave) {
                        Text(isLeaving ? "Leaving…" : "Leave cabal")
                    }
                    .buttonStyle(.monacoDestructive)
                    .monacoFullWidthButtons()
                    .disabled(isLeaving)
                    .padding(.horizontal, MonacoTheme.Space.m)
                    .accessibilityIdentifier("group-action-leave")
                }
                .padding(.top, MonacoTheme.Space.m)
                .padding(.bottom, MonacoTheme.Space.xl)
            }
            .monacoCanvas()
            .navigationTitle("Cabal details")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Done") { dismiss() }
                        .font(MonacoTheme.Typo.bodyStrong)
                        .accessibilityIdentifier("group-details-done")
                }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .accessibilityIdentifier("group-details-sheet")
    }

    // MARK: - Invite code

    private var inviteCard: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                Text(JoinCabalCopy.codeLabel)
                    .font(MonacoTheme.Typo.captionStrong)
                    .foregroundStyle(MonacoTheme.muted)
                inviteCode
                    .accessibilityIdentifier("group-invite-code")
            }
            Text(CabalDetailsCopy.inviteHint)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)

            let actions = dynamicTypeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(spacing: MonacoTheme.Space.sm))
                : AnyLayout(HStackLayout(spacing: MonacoTheme.Space.sm))
            actions {
                Button {
                    copy(.invite, value: groupId)
                } label: {
                    copyLabel(.invite, idle: CabalDetailsCopy.copyCode)
                }
                .buttonStyle(.monacoPrimary)
                .accessibilityIdentifier("group-invite-copy-button")

                ShareLink(item: groupId, subject: Text(CabalDetailsCopy.shareSubject)) {
                    Label(CabalDetailsCopy.share, systemImage: "square.and.arrow.up")
                }
                .buttonStyle(.monacoSecondary)
                .accessibilityIdentifier("group-invite-share-button")
            }
            .monacoFullWidthButtons()
            .padding(.top, MonacoTheme.Space.xs)
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
    }

    /// One line of mono at the default sizes, shrinking a little on the narrowest phones. At the
    /// accessibility sizes it wraps by character instead of shrinking past legibility — and
    /// never at a hyphen of its own making, which would read as part of the code.
    @ViewBuilder
    private var inviteCode: some View {
        if dynamicTypeSize.isAccessibilitySize {
            MonacoWalletAddressText(address: groupId, textStyle: .subheadline)
        } else {
            Text(groupId)
                .font(MonacoTheme.Typo.data)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
                .textSelection(.enabled)
        }
    }

    // MARK: - For developers

    private func developerSection(_ address: String) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader(CabalDetailsCopy.developersTitle)
                .padding(.horizontal, MonacoTheme.Space.m)

            MonacoGroupedList {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                    Text(CabalDetailsCopy.accountTitle)
                        .font(MonacoTheme.Typo.rowTitle)
                        .foregroundStyle(MonacoTheme.ink)
                    MonacoWalletAddressText(address: address, textStyle: .footnote, foreground: MonacoTheme.secondaryText)
                        .accessibilityIdentifier("group-treasury-address-value")
                    developerActions(address)
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .padding(.top, MonacoTheme.Space.sm)
                .padding(.bottom, MonacoTheme.Space.xs)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("group-treasury-address-block")
        }
    }

    @ViewBuilder
    private func developerActions(_ address: String) -> some View {
        let layout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 0))
            : AnyLayout(HStackLayout(spacing: MonacoTheme.Space.l))
        layout {
            Button {
                copy(.address, value: address)
            } label: {
                copyLabel(.address, idle: CabalDetailsCopy.copy)
                    .imageScale(.small)
            }
            .buttonStyle(.plain)
            .textAction()
            .accessibilityIdentifier("group-treasury-copy-button")

            if let url = URL(string: "https://solscan.io/account/\(address)") {
                Link(destination: url) {
                    Label(CabalDetailsCopy.viewOnSolscan, systemImage: "arrow.up.right")
                        .labelStyle(TrailingIconLabelStyle())
                }
                .textAction()
                .accessibilityIdentifier("group-treasury-solscan")
            }
        }
    }

    // MARK: - Copy

    /// "Copied" with a tick for two seconds, then back to the idle label.
    private func copyLabel(_ field: CopiedField, idle: String) -> some View {
        let copied = copiedField == field
        return Label(copied ? CabalDetailsCopy.copied : idle, systemImage: copied ? "checkmark" : "doc.on.doc")
    }

    private func copy(_ field: CopiedField, value: String) {
        UIPasteboard.general.string = value
        Haptics.selection()
        copiedField = field
        Task {
            try? await Task.sleep(for: .seconds(2))
            if copiedField == field { copiedField = nil }
        }
    }
}

/// The details sheet in the member's words.
enum CabalDetailsCopy {
    static let inviteHint = "Friends paste this code to join the cabal."
    static let copyCode = "Copy code"
    static let copy = "Copy"
    static let copied = "Copied"
    static let share = "Share"
    static let shareSubject = "Monaco invite code"
    static let developersTitle = "For developers"
    static let accountTitle = "Cabal account on Solana"
    static let viewOnSolscan = "View on Solscan"

    static let auditedStrings: [String] = [
        inviteHint, copyCode, copy, copied, share, shareSubject, developersTitle, accountTitle, viewOnSolscan,
    ]
}

private extension View {
    /// A demoted action: brand ink, no capsule, still a 44pt target.
    func textAction() -> some View {
        font(MonacoTheme.Typo.calloutStrong)
            .foregroundStyle(MonacoTheme.brand)
            .frame(minHeight: 44)
            .contentShape(Rectangle())
    }
}

private struct TrailingIconLabelStyle: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 4) {
            configuration.title
            configuration.icon.imageScale(.small)
        }
    }
}
