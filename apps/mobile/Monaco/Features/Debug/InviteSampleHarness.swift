#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: the invite screens on canned data, with no backend.
///
/// - `details`        — the cabal's details sheet: code, QR code, Share invite, Copy link, Copy code.
/// - `detailsLoading` — the invite card while the code loads.
/// - `detailsFailed`  — the invite card when the code could not be loaded, with a retry.
/// - `joinPreview`    — the join screen with a code filled in and its open cabal previewed.
/// - `joinTyping`     — the join screen halfway through typing a code.
/// - `joinRequest`    — a previewed cabal whose admin approves members.
/// - `joinUnknown`    — a code that no longer works.
/// - `joinLink`       — an invite link opened the app: the join sheet over the tabs.
///
/// Launch with `-MonacoInviteSample <scenario>`.
enum InviteSampleScenario: String, CaseIterable {
    case details
    case detailsLoading
    case detailsFailed
    case joinPreview
    case joinTyping
    case joinRequest
    case joinUnknown
    case joinLink

    static let launchArgument = "-MonacoInviteSample"

    static var requested: InviteSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: launchArgument), arguments.indices.contains(flag + 1) else { return nil }
        return InviteSampleScenario(rawValue: arguments[flag + 1])
    }
}

/// The canned cabals and codes behind the scenarios.
enum SampleInvites {
    static let groupId = "5b1f0c9e-0005-4c55-9a51-000000000005"
    static let invite = InviteDTO(code: "K7QM4XPD", url: "https://trymonaco.xyz/join/K7QM4XPD")
    static let renewed = InviteDTO(code: "R8WN3HQT", url: "https://trymonaco.xyz/join/R8WN3HQT")

    static let previews: [String: InvitePreviewDTO] = [
        "K7QM4XPD": InvitePreviewDTO(
            code: "K7QM4XPD", groupId: groupId, name: "Sunday Investors", memberCount: 9,
            tint: "indigo", pictureUrl: nil, joinPolicy: .open, potValueUsd: "1240.50"
        ),
        "H4TC9MWE": InvitePreviewDTO(
            code: "H4TC9MWE", groupId: "5b1f0c9e-0003-4c55-9a51-000000000003", name: "Semis or bust", memberCount: 4,
            tint: "moss", pictureUrl: nil, joinPolicy: .request, potValueUsd: "812.40"
        ),
    ]
}

/// Canned invite reads and writes. The details card's code can be made to hang or fail.
@MainActor
struct SampleInviteSource: InviteSource {
    enum CodeBehaviour { case answers, hangs, fails }
    var code: CodeBehaviour = .answers

    func currentInvite(groupId: String) async throws -> InviteDTO {
        switch code {
        case .answers:
            return SampleInvites.invite
        case .hangs:
            try await Task.sleep(for: .seconds(3600))
            throw CancellationError()
        case .fails:
            try await Task.sleep(for: .milliseconds(150))
            throw MonacoCore.MonacoAPIError.httpStatus(503)
        }
    }

    func newInvite(groupId: String) async throws -> InviteDTO {
        try await Task.sleep(for: .milliseconds(300))
        return SampleInvites.renewed
    }

    func preview(code: String) async throws -> InvitePreviewDTO {
        try await Task.sleep(for: .milliseconds(120))
        guard let canonical = InviteLink.normalizeCode(code), let preview = SampleInvites.previews[canonical] else {
            throw MonacoCore.MonacoAPIError.rejected(status: 404, message: "invite not found")
        }
        return preview
    }

    func join(code: String) async throws -> InviteJoinResult {
        let preview = try await preview(code: code)
        return InviteJoinResult(status: preview.joinPolicy == .request ? .pending : .joined, groupId: preview.groupId)
    }
}

struct InviteSampleHarness: View {
    let scenario: InviteSampleScenario
    @ObservedObject var auth: PrivyAuthService

    @State private var showDetails = false
    /// In memory, so the sample never leaves an invite waiting for the real app.
    @State private var linkStore: PendingInviteStore = {
        let defaults = UserDefaults(suiteName: "monaco.inviteSample") ?? .standard
        defaults.removePersistentDomain(forName: "monaco.inviteSample")
        return PendingInviteStore(defaults: defaults)
    }()

    private let actions = CabalsTabSampleData.Actions()

    var body: some View {
        switch scenario {
        case .details, .detailsLoading, .detailsFailed:
            details
        case .joinPreview:
            join(code: "K7QM4XPD")
        case .joinTyping:
            join(code: "K7QM 4")
        case .joinRequest:
            join(code: "h4tc-9mwe")
        case .joinUnknown:
            join(code: "ZZZZ2222")
        case .joinLink:
            linkOverTabs
        }
    }

    private var detailsSource: SampleInviteSource {
        switch scenario {
        case .detailsLoading: return SampleInviteSource(code: .hangs)
        case .detailsFailed: return SampleInviteSource(code: .fails)
        default: return SampleInviteSource()
        }
    }

    private var details: some View {
        NavigationStack {
            MonacoTheme.canvas
                .ignoresSafeArea()
                .navigationTitle("Sunday Investors")
                .navigationBarTitleDisplayMode(.inline)
        }
        .sheet(isPresented: $showDetails) {
            GroupDetailsSheet(
                groupId: SampleInvites.groupId,
                groupName: "Sunday Investors",
                invites: detailsSource,
                treasuryAddress: "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
                isLeaving: false,
                onLeave: {}
            )
        }
        .task { showDetails = true }
    }

    private func join(code: String) -> some View {
        NavigationStack {
            JoinGroupView(auth: auth, initialCode: code, actions: actions, invites: SampleInviteSource())
        }
    }

    /// The Cabals tab under the sheet an opened link presents.
    private var linkOverTabs: some View {
        TabView {
            NavigationStack {
                MonacoTheme.canvas
                    .ignoresSafeArea()
                    .navigationTitle("Cabals")
            }
            .tabItem { Label("Cabals", systemImage: "person.3") }
        }
        .tint(MonacoTheme.ink)
        .inviteLinkSheet(auth: auth, store: linkStore, invites: SampleInviteSource(), actions: actions)
        .task {
            if let url = URL(string: "https://trymonaco.xyz/join/K7QM4XPD") {
                linkStore.receive(url)
            }
        }
    }
}
#endif
