import MonacoCore
import SwiftUI
import UIKit

/// Signed-in self-profile: photo, editable name, account balance, deposit address,
/// and joined cabals with the viewer's position. Reads `AppSessionStore`; no extra fetches.
struct ProfileTabView: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session

    /// Debug sample harness only: prefill the name field.
    var initialNameDraft: String?

    @State private var toast: MonacoToast?

    private var displayName: String {
        let name = session.me?.displayName.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return name.isEmpty ? "Member" : name
    }

    private var memberSince: String {
        guard let createdAt = session.me?.createdAt else { return "Your profile" }
        return MemberSinceFormatter.format(createdAt)
    }

    private var depositAddress: String? {
        let address = session.me?.memberWalletAddress.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !address.isEmpty, !address.hasPrefix("FAKE") else { return nil }
        return address
    }

    private var cabalRows: [ProfileCabalRow] {
        ProfileCabalRow.rows(home: session.home, dashboard: session.dashboard)
    }

    var body: some View {
        MonacoScreen {
            if session.me == nil, session.isLoading {
                ProgressView("Loading your profile…")
                    .tint(MonacoTheme.accent)
                    .foregroundStyle(MonacoTheme.muted)
                    .accessibilityIdentifier("profile-loading")
            } else if session.me == nil {
                loadError
            } else {
                profileScroll
            }
        }
        .navigationTitle("Profile")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable {
            await session.refresh(auth: auth)
        }
        .monacoToast($toast)
        .accessibilityIdentifier("profile-root")
    }

    private var loadError: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            Text(session.errorMessage ?? "Could not load your profile.")
                .font(MonacoTheme.TypeRole.body)
                .foregroundStyle(MonacoTheme.destructive)
            Button("Try again") {
                Task { await session.bootstrap(auth: auth) }
            }
            .buttonStyle(.monacoPrimary)
        }
        .padding(MonacoTheme.Space.m)
        .accessibilityIdentifier("profile-error")
    }

    private var profileScroll: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                header

                ProfileNameEditor(auth: auth, initialDraft: initialNameDraft) { toast = $0 }

                accountCard

                ProfileCabalsSection(
                    auth: auth,
                    rows: cabalRows,
                    onLeft: { await session.refresh(auth: auth) }
                )

                accountActions

                if let errorMessage = session.errorMessage {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(MonacoTheme.TypeRole.caption)
                        .foregroundStyle(MonacoTheme.warning)
                }
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.l)
        }
    }

    private var header: some View {
        HStack(alignment: .center, spacing: MonacoTheme.Space.m) {
            ProfilePhotoPicker(auth: auth, size: 88) { toast = $0 }
            MonacoHeroHeader(title: displayName, caption: memberSince)
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("profile-header")
        }
        .padding(.top, MonacoTheme.Space.m)
    }

    private var accountCard: some View {
        MonacoCard {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Account balance")
                        .font(MonacoTheme.TypeRole.caption)
                        .foregroundStyle(MonacoTheme.muted)
                    if let balance = session.platformBalance {
                        Text(UsdAmountFormatter.format(micros: balance.availableUsdcMicros))
                            .font(.title3.monospacedDigit().weight(.semibold))
                            .foregroundStyle(MonacoTheme.ink)
                            .accessibilityIdentifier("profile-balance-value")
                    } else if session.isBalanceLoading {
                        ProgressView()
                            .tint(MonacoTheme.accent)
                            .accessibilityIdentifier("profile-balance-loading")
                    } else {
                        Text("Unavailable. Pull to refresh.")
                            .font(MonacoTheme.TypeRole.body)
                            .foregroundStyle(MonacoTheme.muted)
                            .accessibilityIdentifier("profile-balance-unavailable")
                    }
                }

                Divider().overlay(MonacoTheme.hairline)

                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    Text("Deposit address")
                        .font(MonacoTheme.TypeRole.caption)
                        .foregroundStyle(MonacoTheme.muted)
                    if let depositAddress {
                        MonacoWalletAddressText(address: depositAddress, textStyle: .callout)
                            .accessibilityIdentifier("profile-deposit-address")
                        Text("Send USDC on Solana here to add to your account balance.")
                            .monacoSecondaryCaption()
                        Button {
                            UIPasteboard.general.string = depositAddress
                            toast = MonacoToast(message: "Address copied.", isSuccess: true)
                        } label: {
                            Label("Copy address", systemImage: "doc.on.doc")
                        }
                        .buttonStyle(.monacoSecondary)
                        .accessibilityIdentifier("profile-deposit-address-copy")
                    } else {
                        Text("Your deposit address is not ready yet. Pull to refresh.")
                            .font(MonacoTheme.TypeRole.body)
                            .foregroundStyle(MonacoTheme.muted)
                    }
                }
            }
        }
    }

    private var accountActions: some View {
        MonacoCard {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
                NavigationLink {
                    WithdrawView(auth: auth)
                } label: {
                    Label("Withdraw", systemImage: "arrow.up.right")
                        .foregroundStyle(MonacoTheme.primaryText)
                }
                .accessibilityIdentifier("profile-withdraw-link")

                Divider().overlay(MonacoTheme.hairline)

                NavigationLink {
                    AdvancedSettingsView()
                } label: {
                    Label("Advanced", systemImage: "link")
                        .foregroundStyle(MonacoTheme.primaryText)
                }
                .accessibilityIdentifier("profile-advanced-link")

                Divider().overlay(MonacoTheme.hairline)

                Button("Sign out") {
                    Task { await auth.logout() }
                }
                .buttonStyle(.monacoDestructive)
                .frame(maxWidth: .infinity)
                .accessibilityIdentifier("profile-sign-out")
            }
        }
    }
}
