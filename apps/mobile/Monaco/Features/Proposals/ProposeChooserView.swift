import SwiftUI

struct ProposeChooserView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let groupView: GroupViewDTO

    var body: some View {
        List {
            NavigationLink {
                ProposeBuyView(auth: auth, groupId: groupId)
            } label: {
                Label("Buy", systemImage: "chart.line.uptrend.xyaxis")
            }
            .accessibilityIdentifier("propose-kind-buy")

            NavigationLink {
                ProposeSellView(auth: auth, groupId: groupId, holdings: heldStocks)
            } label: {
                Label("Sell", systemImage: "chart.line.downtrend.xyaxis")
            }
            .disabled(heldStocks.isEmpty)
            .accessibilityIdentifier("propose-kind-sell")

            if heldStocks.isEmpty {
                Text("No stocks to sell yet.")
                    .foregroundStyle(.secondary)
            }
        }
        .navigationTitle("Propose")
    }

    private var heldStocks: [PotRowDTO] {
        groupView.pot.filter { row in
            row.symbol.uppercased() != "USDC" && (Int64(row.tokenAmount ?? "0") ?? 0) > 0
        }
    }
}
