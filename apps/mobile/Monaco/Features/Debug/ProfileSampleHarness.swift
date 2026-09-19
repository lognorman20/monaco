#if DEBUG
import MonacoCore
import SwiftUI
import UIKit

/// Debug-only: renders Profile from canned `AppSessionStore` data so QA can screenshot
/// each state without Privy or a backend. Launch with
/// `-MonacoProfileSample <placeholder|photo|validation|empty|loading|error>`.
enum ProfileSampleScenario: String, CaseIterable {
    case placeholder
    case photo
    case validation
    case empty
    case loading
    case error

    static var requested: ProfileSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: "-MonacoProfileSample"),
              arguments.indices.contains(flag + 1)
        else { return nil }
        return ProfileSampleScenario(rawValue: arguments[flag + 1])
    }
}

struct ProfileSampleHarness: View {
    let scenario: ProfileSampleScenario
    @ObservedObject var auth: PrivyAuthService
    @State private var session: AppSessionStore

    init(scenario: ProfileSampleScenario, auth: PrivyAuthService) {
        self.scenario = scenario
        self.auth = auth
        _session = State(initialValue: Self.makeSession(for: scenario))
    }

    var body: some View {
        NavigationStack {
            ProfileTabView(
                auth: auth,
                initialNameDraft: scenario == .validation ? "Logan Norman of the Weekend Investors" : nil
            )
        }
        .environment(session)
    }

    private static func makeSession(for scenario: ProfileSampleScenario) -> AppSessionStore {
        let session = AppSessionStore()
        session.isLoading = false
        switch scenario {
        case .loading:
            session.isLoading = true
            return session
        case .error:
            session.errorMessage = "Could not connect to Monaco."
            return session
        default:
            break
        }

        session.me = MeResponse(
            userId: "sample-user",
            displayName: "Logan Norman",
            memberWalletAddress: "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
            profilePhotoUrl: scenario == .photo ? samplePhotoURL()?.absoluteString : nil,
            createdAt: ISO8601DateFormatter().date(from: "2026-09-01T14:30:00Z")
        )
        session.platformBalance = PlatformBalanceDTO(
            availableUsdcMicros: 1_248_500_000,
            memberWalletAddress: "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
            pendingAllocationMicros: 0
        )

        let joined = scenario != .empty
        session.home = HomeViewDTO(
            groups: joined ? [
                HomeGroupBoardRowDTO(groupId: "g1", name: "Weekend investors", potValueUsd: "548.20", percentReturn: "0.124", dollarPnl: "+48.20", isJoined: true),
                HomeGroupBoardRowDTO(groupId: "g2", name: "Semis or bust", potValueUsd: "2310.75", percentReturn: "-0.031", dollarPnl: "-73.90", isJoined: true),
                HomeGroupBoardRowDTO(groupId: "g3", name: "Index huggers", potValueUsd: "120.00", percentReturn: nil, dollarPnl: "+0.00", isJoined: true),
            ] : [],
            people: []
        )
        session.dashboard = HomeDashboardDTO(
            netWorthUsd: joined ? "711.55" : "0.00",
            netWorthDollarPnl: joined ? "+12.40" : "+0.00",
            netWorthPercentReturn: joined ? "0.018" : nil,
            myGroups: joined ? [
                HomeMyGroupRowDTO(groupId: "g1", name: "Weekend investors", equityUsd: "311.50", slicePercent: "0.568", dollarPnl: "+27.40", percentReturn: "0.096"),
                HomeMyGroupRowDTO(groupId: "g2", name: "Semis or bust", equityUsd: "400.05", slicePercent: "0.173", dollarPnl: "-15.00", percentReturn: "-0.036"),
            ] : [],
            pnlSeries1H: [],
            leaderboard: HomeLeaderboardSectionDTO(range: "ALL", people: []),
            missedProposals: []
        )
        return session
    }

    /// Writes a generated image to tmp so AsyncImage loads it from a file URL.
    private static func samplePhotoURL() -> URL? {
        let size = CGSize(width: 256, height: 256)
        let image = UIGraphicsImageRenderer(size: size).image { context in
            let colors = [
                UIColor(red: 0.93, green: 0.55, blue: 0.36, alpha: 1).cgColor,
                UIColor(red: 0.36, green: 0.22, blue: 0.18, alpha: 1).cgColor,
            ] as CFArray
            if let gradient = CGGradient(colorsSpace: CGColorSpaceCreateDeviceRGB(), colors: colors, locations: [0, 1]) {
                context.cgContext.drawLinearGradient(gradient, start: .zero, end: CGPoint(x: size.width, y: size.height), options: [])
            }
            UIColor(white: 1, alpha: 0.85).setFill()
            context.cgContext.fillEllipse(in: CGRect(x: 88, y: 52, width: 80, height: 80))
            context.cgContext.fillEllipse(in: CGRect(x: 48, y: 150, width: 160, height: 150))
        }
        guard let data = image.pngData() else { return nil }
        let url = FileManager.default.temporaryDirectory.appending(path: "monaco-sample-avatar.png")
        do {
            try data.write(to: url, options: .atomic)
            return url
        } catch {
            return nil
        }
    }
}
#endif
