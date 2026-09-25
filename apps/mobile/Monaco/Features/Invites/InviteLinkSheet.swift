import MonacoCore
import SwiftUI

/// Where an invite link lands: the join screen with the code filled in, in a sheet over
/// whatever tab the member was on. Once they are in, the sheet shows the cabal itself, so
/// the link ends in the cabal and not on a spent form.
struct InviteLinkSheet: View {
    @ObservedObject var auth: PrivyAuthService
    /// The canonical code (or legacy cabal id) the link carried.
    let code: String
    var invites: InviteSource?
    var actions: CabalsActionSource?

    @Environment(\.dismiss) private var dismiss
    @State private var joined: JoinedCabal?

    private struct JoinedCabal: Equatable {
        let id: String
        let name: String?
    }

    var body: some View {
        NavigationStack {
            Group {
                if let joined {
                    GroupDetailView(auth: auth, groupId: joined.id, groupName: joined.name)
                } else {
                    JoinGroupView(
                        auth: auth,
                        initialCode: code,
                        actions: actions,
                        invites: invites
                    ) { groupId, name in
                        joined = JoinedCabal(id: groupId, name: name)
                    }
                }
            }
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button("Close") { dismiss() }
                        .font(MonacoTheme.Typo.bodyStrong)
                        .accessibilityIdentifier("invite-link-close")
                }
            }
        }
        .accessibilityIdentifier("invite-link-sheet")
    }
}

extension View {
    /// Presents `InviteLinkSheet` whenever an invite link is waiting in `store`: at once when
    /// the app is already signed in, or as soon as this view appears after sign-in and
    /// onboarding. Attach it to the signed-in root (`MainTabView`).
    /// `store` defaults to `PendingInviteStore.shared`, where the app's URL handlers put links.
    func inviteLinkSheet(
        auth: PrivyAuthService,
        store: PendingInviteStore? = nil,
        invites: InviteSource? = nil,
        actions: CabalsActionSource? = nil
    ) -> some View {
        modifier(InviteLinkSheetPresenter(auth: auth, store: store, invites: invites, actions: actions))
    }
}

private struct InviteLinkSheetPresenter: ViewModifier {
    @ObservedObject var auth: PrivyAuthService
    let customStore: PendingInviteStore?
    let invites: InviteSource?
    let actions: CabalsActionSource?

    init(auth: PrivyAuthService, store: PendingInviteStore?, invites: InviteSource?, actions: CabalsActionSource?) {
        self.auth = auth
        customStore = store
        self.invites = invites
        self.actions = actions
    }

    private var store: PendingInviteStore { customStore ?? .shared }

    @State private var presented: PendingInviteStore.Pending?

    func body(content: Content) -> some View {
        content
            .onAppear(perform: presentWaitingInvite)
            .onChange(of: store.pending) { _, _ in presentWaitingInvite() }
            // A link that arrived while a sheet was already up waits for it to close.
            .onChange(of: presented) { _, now in
                if now == nil { presentWaitingInvite() }
            }
            .sheet(item: $presented) { invite in
                InviteLinkSheet(auth: auth, code: invite.text, invites: invites, actions: actions)
            }
    }

    private func presentWaitingInvite() {
        guard presented == nil, store.pending != nil else { return }
        presented = store.take()
    }
}
