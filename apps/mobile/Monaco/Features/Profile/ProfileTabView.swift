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
    /// Debug sample harness only: open the face sheet immediately.
    var initiallyShowFacePicker = false
    /// Debug sample harness only: stand in for the store's save so the *success* path — sheet
    /// closes, toast lands on the uncovered screen — can be exercised without a backend.
    var saveName: (any DisplayNameSaving)?

    @State private var toast: MonacoToast?
    @State private var showEditProfile = false
    @State private var confirmSignOut = false
    @State private var isSigningOut = false

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
        .pollWhileVisible(every: LiveRefreshCadence.resting) {
            try await session.pollLive(auth: auth)
        }
        .monacoToast($toast)
        .sheet(isPresented: $showEditProfile) {
            NavigationStack {
                ScrollView {
                    ProfileNameEditor(auth: auth, initialDraft: initialNameDraft, saveName: saveName) {
                        // Close first: the toast is an overlay on this screen, so it is
                        // only readable once the sheet is out of the way.
                        showEditProfile = false
                        toast = MonacoToast(message: "Name updated.", isSuccess: true)
                    }
                }
                .monacoCanvas()
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
        // `.contain` for the same reason as `profile-header` below and the chat root: a bare
        // identifier is handed to every descendant, so the whole profile tree reported itself
        // as "profile-root" and nothing inside it could be addressed.
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("profile-root")
        .onAppear {
            if initiallyShowEditProfile { showEditProfile = true }
        }
        .monacoFrameStats("Profile")
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
            // Edge to edge, like Home: the ruled lists run to the screen's edges and each
            // section insets its own header.
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                // One ruled table: the three figures, then the cash line under them.
                VStack(spacing: 0) {
                    header
                        .padding(.bottom, MonacoTheme.Space.l)
                    statRow
                    HomeBalanceRowSection(
                        auth: auth,
                        balance: session.platformBalance,
                        isBalanceLoading: session.isBalanceLoading,
                        joinedCabals: session.joinedCabals,
                        onRetryBalance: { Task { await session.refresh(auth: auth) } },
                        identifierPrefix: "profile",
                        balanceIdentifier: "profile-balance-value",
                        rules: .bottom
                    )
                }


                ProfileCabalsSection(
                    auth: auth,
                    rows: cabalRows,
                    onLeft: { await session.refresh(auth: auth) }
                )

                accountActions

                if let errorMessage = session.errorMessage {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.warning)
                        .padding(.horizontal, MonacoTheme.Space.m)
                }
            }
            .padding(.bottom, MonacoTheme.Space.xl)
        }
    }


    private var header: some View {
        VStack(spacing: MonacoTheme.Space.s) {
            ProfilePhotoPicker(auth: auth, size: 96, initiallyOpen: initiallyShowFacePicker) { toast = $0 }

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
                        .font(MonacoTheme.Typo.captionStrong)
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
        // `.contain`, not `.combine`: the header holds two buttons (change photo, edit
        // name). Combining collapsed them into one element that VoiceOver could only
        // activate one way, and hid both identifiers from UI tests.
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("profile-header")
    }

    /// Three figures in a ruled band: what the member has in cabals, how it has done, and how
    /// many cabals that is across. Typography does the separating; there is no card.
    private var statRow: some View {
        HStack(spacing: 0) {
            statItem(label: "In cabals") {
                MoneyText(decimalString: session.dashboard?.netWorthUsd ?? "0", style: .large)
            }
            statDivider
            statItem(label: "All time") {
                PercentText(percentReturn: session.dashboard?.netWorthPercentReturn, style: .large)
            }
            statDivider
            statItem(label: cabalRows.count == 1 ? "Cabal" : "Cabals") {
                Text("\(cabalRows.count)")
                    .moneyFont(.large)
                    .foregroundStyle(MonacoTheme.ink)
            }
        }
        .padding(.vertical, MonacoTheme.Space.sm)
        .overlay(alignment: .top) { MonacoRule() }
        .overlay(alignment: .bottom) { MonacoRule() }
    }

    private var statDivider: some View {
        Rectangle()
            .fill(MonacoTheme.hairline)
            .frame(width: 1)
            .padding(.vertical, MonacoTheme.Space.xs)
    }

    private func statItem<Value: View>(label: String, @ViewBuilder value: () -> Value) -> some View {
        VStack(spacing: 2) {
            value()
                .lineLimit(1)
                .minimumScaleFactor(0.6)
            Text(label)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
        }
        .frame(maxWidth: .infinity)
        .padding(.horizontal, MonacoTheme.Space.s)
    }


    /// #210 moved Settings into Profile: block explorers and sign out sit under the cabals.
    /// Withdraw is the balance card's "Cash out".
    private var accountActions: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Account")
                .padding(.horizontal, MonacoTheme.Space.m)
            MonacoGroupedList {
                // lane: settings
                NavigationLink {
                    SettingsView(auth: auth, userId: session.me?.userId)
                } label: {
                    MonacoRow(
                        title: "Settings",
                        subtitle: "App lock, notifications, your account",
                        chevron: true,
                        leading: { StockMark(systemImage: "gearshape", size: 40) }
                    )
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier("profile-settings-link")
                // lane: portfolio
                NavigationLink {
                    HistoryView(auth: auth)
                } label: {
                    MonacoRow(
                        title: PortfolioCopy.historyTitle,
                        subtitle: PortfolioCopy.historyCaption,
                        chevron: true,
                        leading: { StockMark(systemImage: "list.bullet", size: 40) }
                    )
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier("profile-history-row")

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

            // Signing out costs a fresh code by text to get back in, and the button sits
            // right under the Advanced row at the end of a scroll. Ask first, and keep it
            // disabled afterwards so a second tap can't start a second logout.
            Button("Sign out") {
                confirmSignOut = true
            }
            .buttonStyle(.monacoDestructive)
            .frame(maxWidth: .infinity)
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.top, MonacoTheme.Space.m)

            .disabled(isSigningOut)
            .accessibilityIdentifier("profile-sign-out")
            .confirmationDialog("Sign out of Monaco?", isPresented: $confirmSignOut, titleVisibility: .visible) {
                Button("Sign out", role: .destructive) {
                    guard !isSigningOut else { return }
                    isSigningOut = true
                    Task {
                        await auth.logout()
                        isSigningOut = false
                    }
                }
                .accessibilityIdentifier("profile-sign-out-confirm")
                Button("Cancel", role: .cancel) {}
                    .accessibilityIdentifier("profile-sign-out-cancel")
            } message: {
                Text("Your money stays where it is. You'll need a new code by text to sign back in.")
            }
        }
    }
}
