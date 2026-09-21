#if DEBUG
import SwiftUI

/// Every design primitive on one screen, for review and acceptance screenshots.
///
/// Launch arguments (Debug builds only):
/// - `-MonacoDesignGallery` — opens the gallery instead of the app.
/// - `-MonacoDesignGalleryTab <primitives|money|controls|toast>` — initial tab.
enum MonacoDesignGallery {
    static var isEnabled: Bool { ProcessInfo.processInfo.arguments.contains("-MonacoDesignGallery") }

    static var initialTab: GalleryTab {
        let arguments = ProcessInfo.processInfo.arguments
        guard let index = arguments.firstIndex(of: "-MonacoDesignGalleryTab"),
              arguments.indices.contains(index + 1),
              let tab = GalleryTab(rawValue: arguments[index + 1])
        else { return .primitives }
        return tab
    }

    static func rootView() -> some View {
        MonacoDesignGalleryRoot(selection: initialTab)
    }
}

enum GalleryTab: String {
    case primitives, money, controls, toast
}

private struct MonacoDesignGalleryRoot: View {
    @State var selection: GalleryTab

    var body: some View {
        TabView(selection: $selection) {
            NavigationStack { GalleryPrimitivesPage() }
                .tabItem { Label("Primitives", systemImage: "square.grid.2x2") }
                .tag(GalleryTab.primitives)
            NavigationStack { GalleryMoneyPage() }
                .tabItem { Label("Money", systemImage: "dollarsign") }
                .tag(GalleryTab.money)
            NavigationStack { GalleryControlsPage() }
                .tabItem { Label("Controls", systemImage: "slider.horizontal.3") }
                .tag(GalleryTab.controls)
            NavigationStack { GalleryToastPage() }
                .tabItem { Label("Toast", systemImage: "text.bubble") }
                .tag(GalleryTab.toast)
        }
    }
}

// MARK: - Pages

private struct GalleryPrimitivesPage: View {
    private let cabals: [(id: String, name: String)] = [
        ("8f1c2d3e-0001", "Weekend investors"),
        ("8f1c2d3e-0002", "Semis or bust"),
        ("8f1c2d3e-0003", "Index huggers"),
        ("8f1c2d3e-0004", "🚀🚀🚀"),
        ("8f1c2d3e-0005", "The extremely long cabal name for testing"),
    ]

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                    MonacoSectionHeader("Cabal marks")
                    HStack(spacing: MonacoTheme.Space.sm) {
                        ForEach(cabals, id: \.id) { cabal in
                            CabalMark(groupId: cabal.id, name: cabal.name)
                        }
                    }
                    HStack(spacing: MonacoTheme.Space.sm) {
                        CabalMark(groupId: cabals[0].id, name: cabals[0].name, size: 20)
                        CabalMark(groupId: cabals[0].id, name: cabals[0].name, size: 28)
                        CabalMark(groupId: cabals[0].id, name: cabals[0].name, size: 56)
                        CabalMark(groupId: cabals[0].id, name: cabals[0].name, size: 64)
                    }
                }

                VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                    MonacoSectionHeader("Stock marks")
                    HStack(spacing: MonacoTheme.Space.sm) {
                        StockMark(symbol: "AAPLc")
                        StockMark(symbol: "NVDAc")
                        StockMark(symbol: "TSLAc")
                        StockMark(symbol: "USDC")
                        StockMark(systemImage: "cpu")
                    }
                }

                VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                    MonacoSectionHeader("Your cabals", trailing: "See all") {}
                    MonacoGroupedList {
                        ForEach(Array(cabals.enumerated()), id: \.offset) { index, cabal in
                            NavigationLink {
                                GalleryPushedPage(name: cabal.name)
                            } label: {
                                MonacoRow(
                                    title: cabal.name,
                                    subtitle: "Your slice $311.50 · 57%",
                                    chevron: true,
                                    isLast: index == cabals.count - 1
                                ) {
                                    CabalMark(groupId: cabal.id, name: cabal.name)
                                } trailing: {
                                    PnLText(dollarPnl: ["+48.20", "-7.60", "+0.00", "-0.001", "+1204.5"][index], style: .row)
                                    PercentText(percentReturn: ["0.096", "-0.036", "0", nil, "0.412"][index], style: .caption)
                                }
                            }
                            .buttonStyle(.monacoRow)
                        }
                    }
                }

                VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                    MonacoSectionHeader("Holdings")
                    MonacoGroupedList {
                        MonacoRow(title: "Apple", subtitle: "1.2034 shares · $231.40") {
                            StockMark(symbol: "AAPLc")
                        } trailing: {
                            MoneyText(decimalString: "278.47", style: .row)
                            PnLText(dollarPnl: "+12.10", style: .caption)
                        }
                        MonacoRow(title: "Cash", isLast: true) {
                            StockMark(symbol: "USDC")
                        } trailing: {
                            MoneyText(decimalString: "270.03", style: .row)
                        }
                    }
                }

                VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                    MonacoSectionHeader("Empty state")
                    MonacoGroupedList {
                        EmptyState(
                            title: "Nothing bought yet",
                            message: "Add money, then propose the first buy.",
                            actionTitle: "Add money"
                        ) {}
                    }
                }

                VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                    MonacoSectionHeader("Loading")
                    VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                        SkeletonBlock(width: 180, height: 44)
                        SkeletonBlock(height: 60, radius: MonacoTheme.Radius.tile)
                        SkeletonBlock(height: 60, radius: MonacoTheme.Radius.tile)
                        MonacoRow(title: "Placeholder cabal", subtitle: "Your slice $000.00") {
                            CabalMark(groupId: "x", name: "Placeholder")
                        }
                        .skeleton(true)
                    }
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .monacoCanvas()
        .navigationTitle("Primitives")
        .navigationBarTitleDisplayMode(.large)
    }
}

private struct GalleryPushedPage: View {
    let name: String

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                Text(name)
                    .font(MonacoTheme.Typo.display)
                    .lineLimit(2)
                    .minimumScaleFactor(0.8)
                ForEach(0..<12, id: \.self) { index in
                    SkeletonBlock(height: 60, radius: MonacoTheme.Radius.tile)
                        .opacity(index.isMultiple(of: 2) ? 1 : 0.6)
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
        }
        .monacoCanvas()
        .navigationTitle(name)
        .navigationBarTitleDisplayMode(.inline)
    }
}

private struct GalleryMoneyPage: View {
    @State private var ticking: Decimal = 1248.50

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    Text("Your money in cabals")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                    MoneyText(ticking, style: .hero)
                    HStack(spacing: MonacoTheme.Space.s) {
                        PnLBadge(dollarPnl: "+48.20", percentReturn: "0.096")
                        Text("all time")
                            .font(MonacoTheme.Typo.caption)
                            .foregroundStyle(MonacoTheme.muted)
                    }
                }
                .accessibilityElement(children: .combine)

                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    MonacoSectionHeader("Hero")
                    MoneyText(Decimal(0), style: .hero)
                    MoneyText(decimalString: "1248.50", style: .hero)
                    MoneyText(decimalString: "12431180", style: .hero)
                    MoneyText(decimalString: "not a number", style: .hero)
                }

                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    MonacoSectionHeader("Sizes")
                    MoneyText(micros: 548_200_000, style: .large)
                    MoneyText(micros: 548_200_000, style: .row)
                    MoneyText(micros: 548_200_000, style: .caption)
                }

                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    MonacoSectionHeader("Profit and loss")
                    HStack(spacing: MonacoTheme.Space.l) {
                        PnLText(dollarPnl: "+48.20", style: .row)
                        PnLText(dollarPnl: "-7.60", style: .row)
                        PnLText(dollarPnl: "+0.00", style: .row)
                        PnLText(dollarPnl: "-0.00", style: .row)
                    }
                    HStack(spacing: MonacoTheme.Space.l) {
                        PercentText(percentReturn: "0.096", style: .row)
                        PercentText(percentReturn: "-0.036", style: .row)
                        PercentText(percentReturn: "0", style: .row)
                        PercentText(percentReturn: nil, style: .row)
                    }
                    VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                        PnLBadge(dollarPnl: "+48.20", percentReturn: "0.096")
                        PnLBadge(dollarPnl: "-7.60", percentReturn: "-0.036")
                        PnLBadge(dollarPnl: "-0.001", percentReturn: "0")
                        PnLBadge(dollarPnl: "+311.50", percentReturn: nil, style: .row)
                    }
                }

                Button("Change the hero figure") {
                    withAnimation(.snappy) { ticking += Decimal(Double.random(in: -80...120)).rounded2 }
                }
                .buttonStyle(.monacoSecondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .monacoCanvas()
        .navigationTitle("")
        .navigationBarTitleDisplayMode(.inline)
    }
}

private struct GalleryControlsPage: View {
    enum Range: String, CaseIterable { case open = "Open", closed = "Closed" }

    @State private var amount = "50"
    @State private var range: Range = .open
    @State private var name = ""
    @State private var email = "maya@example.com"

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                AmountEntry(
                    amountText: $amount,
                    max: Decimal(string: "548.20"),
                    presets: [.dollars(25), .dollars(50), .dollars(100), .fraction(1, label: "Max")],
                    helper: "The pot has $548.20",
                    overLimitHelper: "More than the pot has"
                )

                MonacoSegmented(Range.allCases, selection: $range) { $0.rawValue }

                VStack(spacing: MonacoTheme.Space.sm) {
                    MonacoTextField("Cabal name", text: $name)
                    MonacoTextField("Email", text: $email, keyboard: .emailAddress, contentType: .emailAddress)
                    MonacoSearchField(placeholder: "Search Apple, Tesla, NVDA…", text: .constant(""))
                }

                HStack(spacing: MonacoTheme.Space.sm) {
                    CircleAction("Add money", systemImage: "plus") {}
                    CircleAction("Propose", systemImage: "arrow.up.right") {}
                    CircleAction("Cash out", systemImage: "arrow.down.left") {}
                    CircleAction("Chat", systemImage: "bubble.left") {}
                }
                .frame(maxWidth: .infinity)

                VStack(spacing: MonacoTheme.Space.sm) {
                    Button("Add money") {}.buttonStyle(.monacoPrimary)
                    Button("Cash out") {}.buttonStyle(.monacoSecondary)
                    Button("Leave cabal") {}.buttonStyle(.monacoDestructive)
                    Button("Disabled") {}.buttonStyle(.monacoPrimary).disabled(true)
                }
                .frame(maxWidth: .infinity)
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .scrollDismissesKeyboard(.interactively)
        .monacoCanvas()
        .navigationTitle("Controls")
        .navigationBarTitleDisplayMode(.inline)
        .safeAreaInset(edge: .bottom) {
            BottomCTA {
                Button("Review") {}.buttonStyle(.monacoPrimary)
            }
        }
    }
}

private struct GalleryToastPage: View {
    @State private var toast: MonacoToast?
    @State private var isLoadingBalance = false

    private let failure = "We couldn't confirm that went through. Check your balance before trying again."

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                MonacoSectionHeader("Toasts")
                Text("A toast stays up for as long as its message takes to read. Swipe down or tap to dismiss.")
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.muted)
                Button("Show success") {
                    toast = MonacoToast(message: "Added $50 to Weekend investors", isSuccess: true)
                }
                .buttonStyle(.monacoSecondary)
                Button("Show a money failure") {
                    toast = MonacoToast(message: failure)
                }
                .buttonStyle(.monacoSecondary)
                // The money screens set a toast and reload a balance in the same update. Only the
                // toast should animate; the block below must swap without springing.
                Button("Show error while the balance reloads") {
                    toast = MonacoToast(message: "Couldn't load this. Pull down to try again")
                    isLoadingBalance.toggle()
                }
                .buttonStyle(.monacoSecondary)
                if isLoadingBalance {
                    ProgressView()
                        .tint(MonacoTheme.accent)
                        .frame(maxWidth: .infinity, minHeight: 96)
                } else {
                    MoneyText(Decimal(1248.5), style: .hero)
                        .frame(maxWidth: .infinity, minHeight: 96, alignment: .leading)
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
        }
        .monacoCanvas()
        .navigationTitle("Toast")
        .monacoToast($toast, placement: .aboveBottomCTA)
        .safeAreaInset(edge: .bottom) {
            BottomCTA {
                Button("Add money") {}.buttonStyle(.monacoPrimary)
            }
        }
    }
}

private extension Decimal {
    var rounded2: Decimal {
        var source = self
        var result = Decimal()
        NSDecimalRound(&result, &source, 2, .plain)
        return result
    }
}

#Preview("Primitives") { NavigationStack { GalleryPrimitivesPage() } }
#Preview("Money") { NavigationStack { GalleryMoneyPage() } }
#Preview("Controls") { NavigationStack { GalleryControlsPage() } }
#Preview("Toast") { MonacoDesignGallery.rootView() }
#endif
