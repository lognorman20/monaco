import MonacoCore
import SwiftUI
import UIKit

/// Signed-in self-profile: centred identity, a 3-stat row, account balance with money
/// actions, and joined cabals. Reads `AppSessionStore`; no extra fetches.
struct ProfileTabView: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session

    /// Debug sample harness only: prefill the name field in the edit sheet.
    var initialNameDraft: String?
    /// Debug sample harness only: open the edit sheet immediately (to screenshot validation).
    var initiallyShowEditProfile = false

    @State private var toast: MonacoToast?
    @State private var showEditProfile = false

    private var displayName: String {
        let name = session.me?.displayName.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return name.isEmpty ? "Member" : name
    }

    private var memberSince: String {
        guard let createdAt = session.me?.createdAt else { return "Your profile" }
        return MemberSinceFormatter.format(createdAt)
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
        .sheet(isPresented: $showEditProfile) {
            NavigationStack {
                Form {
                    ProfileNameEditor(auth: auth, initialDraft: initialNameDraft) { toast = $0 }
                        .listRowInsets(EdgeInsets())
                        .listRowBackground(Color.clear)
                }
                .monacoFormScreen()
                .navigationTitle("Edit profile")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { showEditProfile = false }
                    }
                }
            }
            .presentationDetents([.medium])
        }
        .accessibilityIdentifier("profile-root")
        .onAppear {
            if initiallyShowEditProfile { showEditProfile = true }
        }
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

                statRow

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
        VStack(spacing: MonacoTheme.Space.s) {
            ProfilePhotoPicker(auth: auth, size: 96) { toast = $0 }

            HStack(spacing: MonacoTheme.Space.xs) {
                Text(displayName)
                    .font(MonacoTheme.Typo.display)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)

                Button {
                    showEditProfile = true
                } label: {
                    Image(systemName: "pencil")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(MonacoTheme.muted)
                        .frame(width: 44, height: 44)
                }
                .accessibilityLabel("Edit profile")
                .accessibilityIdentifier("profile-edit-button")
            }

            Text(memberSince)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
        }
        .frame(maxWidth: .infinity)
        .padding(.top, MonacoTheme.Space.m)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("profile-header")
    }

    private var statRow: some View {
        HStack(spacing: 0) {
            statItem(label: "Total in cabals") {
                MoneyText(decimalString: session.dashboard?.netWorthUsd ?? "0", style: .row)
            }
            statDivider
            statItem(label: "All-time") {
                PercentText(percentReturn: session.dashboard?.netWorthPercentReturn, style: .row)
            }
            statDivider
            statItem(label: "Cabals") {
                Text("\(cabalRows.count)")
                    .font(MonacoTheme.Typo.moneyRow)
                    .foregroundStyle(MonacoTheme.ink)
            }
        }
        .padding(.vertical, MonacoTheme.Space.s)
        .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous))
    }

    private var statDivider: some View {
        Rectangle()
            .fill(MonacoTheme.hairline)
            .frame(width: 1)
            .padding(.vertical, MonacoTheme.Space.s)
    }

    private func statItem<Value: View>(label: String, @ViewBuilder value: () -> Value) -> some View {
        VStack(spacing: 2) {
            value()
            Text(label)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
        }
        .frame(maxWidth: .infinity)
    }

    private var accountCard: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            VStack(alignment: .leading, spacing: 2) {
                Text("Account balance")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                if let balance = session.platformBalance {
                    MoneyText(micros: balance.availableUsdcMicros, style: .large)
                        .accessibilityIdentifier("profile-balance-value")
                } else if session.isBalanceLoading {
                    ProgressView()
                        .tint(MonacoTheme.accent)
                        .accessibilityIdentifier("profile-balance-loading")
                } else {
                    Text("Unavailable. Pull to refresh.")
                        .font(MonacoTheme.Typo.body)
                        .foregroundStyle(MonacoTheme.muted)
                        .accessibilityIdentifier("profile-balance-unavailable")
                }
            }

            HStack(spacing: MonacoTheme.Space.s) {
                NavigationLink {
                    DepositView(auth: auth, joinedCabals: session.joinedCabals)
                } label: {
                    Text("Add money")
                        .frame(maxWidth: .infinity)
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                }
                .buttonStyle(.monacoPrimary)
                .accessibilityIdentifier("profile-add-money-link")

                NavigationLink {
                    WithdrawView(auth: auth)
                } label: {
                    Text("Cash out")
                        .frame(maxWidth: .infinity)
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                }
                .buttonStyle(.monacoSecondary)
                .accessibilityIdentifier("profile-cash-out-link")
            }
        }
        .padding(MonacoTheme.Space.m)
        .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous))
    }

    /// #210 moved Settings into Profile: block explorers and sign out sit under the cabals.
    /// Withdraw is the balance card's "Cash out".
    private var accountActions: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Account")
            MonacoGroupedList {
                NavigationLink {
                    AdvancedSettingsView()
                } label: {
                    MonacoRow(
                        title: "Advanced",
                        subtitle: "Block explorers",
                        chevron: true,
                        isLast: true,
                        leading: { StockMark(systemImage: "link", size: 40) }
                    )
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier("profile-advanced-link")
            }

            Button("Sign out") {
                Task { await auth.logout() }
            }
            .buttonStyle(.monacoDestructive)
            .frame(maxWidth: .infinity)
            .padding(.top, MonacoTheme.Space.s)
            .accessibilityIdentifier("profile-sign-out")
        }
    }
}
