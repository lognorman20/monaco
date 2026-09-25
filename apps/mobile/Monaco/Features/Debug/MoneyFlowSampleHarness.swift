#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: the money screens on canned data, with no sign-in and no backend.
/// Launch with `-MonacoMoneyFlowSample <scenario>`:
/// `addMoney` (address, a balance with a fund on its way, three cabals) · `addMoneyLoading` ·
/// `addMoneyFailed` · `fundCabal` (opened from a cabal, $50 typed) · `fundCabalPicker` (opened
/// from Add money, three cabals to pick from) · `fundCabalEmpty` (nothing to fund with yet) ·
/// `fundCabalLoading` · `withdraw` (Cash out to an address, amount and address typed) ·
/// `withdrawFailed` (the balance could not be read) · `withdrawConfirm` · `withdrawConfirmFailed` ·
/// `receiptPending` (money on its way into a pot) · `receiptLoading`.
///
/// Every screen is the product's own layout — `DepositContent`, `FundCabalContent`,
/// `WithdrawContent`, `WithdrawConfirmView`, `TransactionReceiptView` — fed sample values in
/// place of the network, so what is shot here is what ships.
enum MoneyFlowSampleScenario: String, CaseIterable {
    case addMoney
    case addMoneyLoading
    case addMoneyFailed
    case fundCabal
    case fundCabalPicker
    case fundCabalEmpty
    case fundCabalLoading
    case withdraw
    case withdrawFailed
    case withdrawConfirm
    case withdrawConfirmFailed
    case receiptPending
    case receiptLoading

    static let launchArgument = "-MonacoMoneyFlowSample"

    static var requested: MoneyFlowSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: launchArgument), arguments.indices.contains(flag + 1) else { return nil }
        return MoneyFlowSampleScenario(rawValue: arguments[flag + 1])
    }
}

struct MoneyFlowSampleHarness: View {
    let scenario: MoneyFlowSampleScenario
    @ObservedObject var auth: PrivyAuthService

    @State private var amountText: String
    @State private var destination: String
    @State private var selectedGroupId: String?

    init(scenario: MoneyFlowSampleScenario, auth: PrivyAuthService) {
        self.scenario = scenario
        self.auth = auth
        _amountText = State(initialValue: MoneyFlowSampleData.amountText(for: scenario))
        _destination = State(initialValue: scenario == .withdraw ? MoneyFlowSampleData.destination : "")
        _selectedGroupId = State(initialValue: MoneyFlowSampleData.cabals.first?.groupId)
    }

    var body: some View {
        NavigationStack {
            root
        }
        .tint(MonacoTheme.ink)
    }

    @ViewBuilder
    private var root: some View {
        switch scenario {
        case .addMoney, .addMoneyLoading, .addMoneyFailed:
            DepositContent(
                auth: auth,
                address: MoneyFlowSampleData.address(for: scenario),
                balance: .amount(MoneyFlowSampleData.balance.availableUsdcMicros),
                pendingAllocationMicros: MoneyFlowSampleData.pendingMicros,
                joinedCabals: MoneyFlowSampleData.cabals,
                onCopy: { _ in },
                onRetry: {}
            )
        case .fundCabal, .fundCabalPicker, .fundCabalEmpty, .fundCabalLoading:
            FundCabalContent(
                phase: MoneyFlowSampleData.fundPhase(for: scenario),
                joinedCabals: scenario == .fundCabalPicker ? MoneyFlowSampleData.cabals : [MoneyFlowSampleData.cabals[0]],
                isSingleCabalContext: scenario != .fundCabalPicker,
                selectedGroupId: $selectedGroupId,
                amountText: $amountText,
                isSubmitting: false,
                onSubmit: {},
                onRetry: {},
                onCopyAddress: { _ in }
            )
        case .withdraw, .withdrawFailed:
            WithdrawContent(
                phase: scenario == .withdrawFailed
                    ? .failed(PlatformBalanceLoader.message(for: URLError(.notConnectedToInternet)))
                    : .loaded(MoneyFlowSampleData.balance),
                amountText: $amountText,
                destinationAddress: $destination,
                onContinue: {},
                onRetry: {}
            )
        case .withdrawConfirm, .withdrawConfirmFailed:
            WithdrawConfirmView(
                destinationAddress: MoneyFlowSampleData.destination,
                amountText: "100",
                isSubmitting: false,
                failure: scenario == .withdrawConfirmFailed
                    ? MoneyFlowCopy.cashOutFailure(FlowErrorInput(isOffline: true))
                    : nil,
                onConfirm: {}
            )
        case .receiptPending:
            TransactionReceiptView(receipt: TransactionReceipt(deposit: MoneyFlowSampleData.pendingDeposit))
                .monacoCanvas()
                .navigationBarTitleDisplayMode(.inline)
        case .receiptLoading:
            TransactionReceiptSkeleton()
                .monacoCanvas()
                .navigationBarTitleDisplayMode(.inline)
        }
    }
}

enum MoneyFlowSampleData {
    /// The member's own deposit address, the same one the other harnesses use.
    static let depositAddress = "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"

    /// An outside Solana address that passes `SolanaAddress.validate`.
    static let destination = "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM"

    static let pendingMicros: Int64 = 50_000_000

    static let balance = PlatformBalanceDTO(
        availableUsdcMicros: 248_500_000,
        memberWalletAddress: depositAddress,
        pendingAllocationMicros: 0
    )

    static let emptyBalance = PlatformBalanceDTO(
        availableUsdcMicros: 0,
        memberWalletAddress: depositAddress,
        pendingAllocationMicros: 0
    )

    /// The Home harness's cabals, so their tints match across the gallery.
    static let cabals = [
        HomeGroupBoardRowDTO(groupId: "g1", name: "Weekend investors", potValueUsd: "548.20", percentReturn: "0.124", dollarPnl: "+48.20", isJoined: true),
        HomeGroupBoardRowDTO(groupId: "g2", name: "Semis or bust", potValueUsd: "2310.75", percentReturn: "-0.031", dollarPnl: "-73.90", isJoined: true),
        HomeGroupBoardRowDTO(groupId: "g3", name: "Index huggers", potValueUsd: "120.00", percentReturn: nil, dollarPnl: "+0.00", isJoined: true),
    ]

    static let pendingDeposit = GetDepositResponse(
        depositId: "d1",
        groupId: "g1",
        amount: 100_000_000,
        status: "pending",
        fromAddress: depositAddress,
        txSignature: nil,
        shareUnits: 0,
        createdAt: "2026-09-18T11:40:00Z"
    )

    static func address(for scenario: MoneyFlowSampleScenario) -> DepositAddressCard.Content {
        switch scenario {
        case .addMoneyLoading:
            return .loading
        case .addMoneyFailed:
            return .unavailable("No connection. Check your internet and try again.")
        default:
            return .ready(depositAddress)
        }
    }

    static func fundPhase(for scenario: MoneyFlowSampleScenario) -> PlatformBalanceLoader.Phase {
        switch scenario {
        case .fundCabalLoading:
            return .loading
        case .fundCabalEmpty:
            return .loaded(emptyBalance)
        default:
            return .loaded(balance)
        }
    }

    static func amountText(for scenario: MoneyFlowSampleScenario) -> String {
        switch scenario {
        case .fundCabal: return "50"
        case .withdraw: return "100"
        default: return ""
        }
    }
}
#endif
