import MonacoCore
import SwiftUI

/// Pick a joined cabal before opening propose-buy with a pre-selected stock.
struct GroupPickerForProposalView: View {
    @ObservedObject var auth: PrivyAuthService
    let home: HomeViewDTO
    @Environment(\.monacoSessionSnapshot) private var sharedSnapshot
    @Environment(\.refreshMonacoSession) private var refreshSession
    let symbol: String

    private var joinedGroups: [HomeGroupBoardRowDTO] {
        (sharedSnapshot?.home ?? home).groups.filter(\.isJoined)
    }

    var body: some View {
        List {
            if joinedGroups.isEmpty {
                MonacoEmptyStateCard(
                    message: "Join a cabal first to propose a buy.",
                    systemImage: "person.3"
                )
            } else {
                Section {
                    ForEach(joinedGroups) { row in
                        NavigationLink {
                            ProposeBuyView(auth: auth, groupId: row.groupId, initialSymbol: symbol)
                        } label: {
                            VStack(alignment: .leading, spacing: 4) {
                                Text(row.name)
                                    .font(.body.bold())
                                    .foregroundStyle(MonacoTheme.primaryText)
                                Text("Cabal pot $\(row.potValueUsd)")
                                    .font(.caption)
                                    .foregroundStyle(MonacoTheme.secondaryText)
                            }
                        }
                        .accessibilityIdentifier("group-picker-row-\(row.groupId)")
                    }
                } header: {
                    Text("Choose cabal")
                } footer: {
                    Text("Choose the cabal you want to buy \(AssetSymbolFormatter.format(symbol)) with.")
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
            }
        }
        .monacoInsetList()
        .background(MonacoTheme.background)
        .refreshable { await refreshSession() }
        .task { await refreshSession() }
        .navigationTitle("Choose a cabal")
        .navigationBarTitleDisplayMode(.inline)
    }
}

private enum AssetSymbolFormatter {
    static func format(_ symbol: String) -> String {
        let trimmed = symbol.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.lowercased().hasSuffix("x"), trimmed.count > 1 else { return trimmed }
        return String(trimmed.dropLast())
    }
}
