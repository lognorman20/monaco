import SwiftUI
import UIKit

/// "Cabal details" from the group toolbar: the cabal account, its invite code, and Leave.
/// Kept off the main screen on purpose: a raw address is the most "crypto" thing in the app.
struct GroupDetailsSheet: View {
    let groupId: String
    let treasuryAddress: String?
    let isLeaving: Bool
    let onLeave: () -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var copiedField: CopiedField?

    private enum CopiedField { case address, invite }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 28) {
                    if let treasuryAddress, !treasuryAddress.isEmpty {
                        field(title: "Cabal account") {
                            MonacoWalletAddressText(address: treasuryAddress, textStyle: .footnote)
                                .accessibilityIdentifier("group-treasury-address-value")
                            HStack(spacing: 20) {
                                copyButton(.address, value: treasuryAddress)
                                    .accessibilityIdentifier("group-treasury-copy-button")
                            }
                        }
                        .accessibilityIdentifier("group-treasury-address-block")
                    }

                    field(title: "Invite code") {
                        Text(groupId)
                            .font(.system(.footnote, design: .monospaced))
                            .foregroundStyle(MonacoTheme.ink)
                            .textSelection(.enabled)
                            .accessibilityIdentifier("group-invite-code")
                        copyButton(.invite, value: groupId)
                            .accessibilityIdentifier("group-invite-copy-button")
                    }

                    Button(role: .destructive, action: onLeave) {
                        Text(isLeaving ? "Leaving…" : "Leave cabal")
                            .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.monacoDestructive)
                    .disabled(isLeaving)
                    .padding(.top, 8)
                    .accessibilityIdentifier("group-action-leave")
                }
                .padding(.horizontal, MonacoTheme.Space.gutter)
                .padding(.vertical, MonacoTheme.Space.m)
            }
            .monacoCanvas()
            .navigationTitle("Cabal details")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Done") { dismiss() }
                        .fontWeight(.semibold)
                        .accessibilityIdentifier("group-details-done")
                }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .accessibilityIdentifier("group-details-sheet")
    }

    private func field<Content: View>(title: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title)
                .font(MonacoTheme.Typo.caption.weight(.semibold))
                .foregroundStyle(MonacoTheme.muted)
            content()
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func copyButton(_ field: CopiedField, value: String) -> some View {
        Button {
            UIPasteboard.general.string = value
            Haptics.selection()
            copiedField = field
            Task {
                try? await Task.sleep(for: .seconds(2))
                if copiedField == field { copiedField = nil }
            }
        } label: {
            Label(copiedField == field ? "Copied" : "Copy", systemImage: copiedField == field ? "checkmark" : "doc.on.doc")
        }
        .font(.subheadline.weight(.semibold))
        .foregroundStyle(MonacoTheme.ink)
        .frame(minHeight: 44)
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
