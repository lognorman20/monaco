import MonacoCore
import SwiftUI

/// What the sample harness opens the alert sheet with. Nil everywhere else.
struct PriceAlertPrefill: Equatable {
    var direction: PriceAlertDirection = .above
    /// A preset to hold chosen, or nil to type `amountText` instead.
    var presetPercent: Int?
    var amountText = ""
}

/// "Tell me when Alphabet is above $360.00": pick a side, pick a move or type a price, save.
///
/// The presets are measured from the price the stock screen is showing, so they are always on
/// the far side of it; a typed price that is already past says so in words and keeps Save off.
/// Below the form, the alerts already waiting on this stock, each one a swipe from gone.
struct PriceAlertSheet: View {
    let currentPriceUsdcMicros: Int64?
    let assetKind: AssetKind
    let logoURL: URL?
    /// Called with the confirmation when an alert is saved; the sheet closes itself.
    let onSaved: (MonacoToast) -> Void

    @State private var model: PriceAlertSheetModel
    @FocusState private var fieldFocused: Bool
    @Environment(\.dismiss) private var dismiss
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    init(
        symbol: String,
        name: String,
        currentPriceUsdcMicros: Int64?,
        assetKind: AssetKind,
        logoURL: URL?,
        dataSource: PriceAlertDataSource,
        prefill: PriceAlertPrefill? = nil,
        onCountChange: @escaping (Int) -> Void,
        onSaved: @escaping (MonacoToast) -> Void
    ) {
        self.currentPriceUsdcMicros = currentPriceUsdcMicros
        self.assetKind = assetKind
        self.logoURL = logoURL
        self.onSaved = onSaved
        let model = PriceAlertSheetModel(
            symbol: symbol,
            name: name,
            currentPriceUsdcMicros: currentPriceUsdcMicros,
            dataSource: dataSource,
            onCountChange: onCountChange
        )
        if let prefill {
            model.choose(direction: prefill.direction)
            if let percent = prefill.presetPercent {
                model.choose(percent: percent)
            } else {
                model.type(prefill.amountText)
            }
        }
        _model = State(initialValue: model)
    }

    var body: some View {
        NavigationStack {
            List {
                formRow { stockLine }
                formRow {
                    MonacoSegmented(PriceAlertDirection.allCases, selection: Binding(
                        get: { model.direction },
                        set: { model.choose(direction: $0) }
                    ), label: AlertCopy.directionTitle)
                    .accessibilityIdentifier("price-alert-direction")
                }
                formRow { presetChips }
                formRow { priceField }
                formRow { lineSentence }
                formRow { saveButton }
                existingSection
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .scrollDismissesKeyboard(.interactively)
            .monacoCanvas()
            .navigationTitle(AlertCopy.sheetTitle)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close") { dismiss() }
                        .accessibilityIdentifier("price-alert-close")
                }
                if fieldFocused {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { fieldFocused = false }
                            .font(MonacoTheme.Typo.bodyStrong)
                    }
                }
            }
            .monacoToast($model.toast)
        }
        .tint(MonacoTheme.ink)
        .presentationDetents([.large])
        .task { await model.load() }
        .onChange(of: currentPriceUsdcMicros) { _, price in
            if let price { model.currentPriceUsdcMicros = price }
        }
        .accessibilityIdentifier("price-alert-sheet")
    }

    // MARK: Form

    /// A row of the form: no rule, the page's inset, the paper behind it.
    private func formRow<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        content()
            .frame(maxWidth: .infinity, alignment: .leading)
            .listRowInsets(EdgeInsets(top: MonacoTheme.Space.s, leading: MonacoTheme.Space.m, bottom: MonacoTheme.Space.s, trailing: MonacoTheme.Space.m))
            .listRowSeparator(.hidden)
            .listRowBackground(MonacoTheme.canvas)
    }

    /// The stock and where it trades now: the figure every preset is measured from. At the
    /// accessibility sizes the price drops under the name rather than squeezing it.
    private var stockLine: some View {
        let identity = HStack(spacing: MonacoTheme.Space.sm) {
            StockMark(symbol: model.symbol, displayName: model.name, assetKind: assetKind, size: 44, logoURL: logoURL)
                .frame(width: 44, height: 44)
            VStack(alignment: .leading, spacing: 2) {
                Text(model.name)
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(2)
                Text(AssetSymbolFormatter.display(model.symbol, kind: assetKind))
                    .font(MonacoTheme.Typo.dataCaption)
                    .foregroundStyle(MonacoTheme.muted)
            }
        }
        let price = HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
            if let current = model.currentPriceUsdcMicros {
                MoneyText(micros: current, style: .row, voice: .market)
            } else {
                Text("—").moneyFont(.row, voice: .market).foregroundStyle(MonacoTheme.muted)
            }
            Text("Now")
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
        }
        return Group {
            if dynamicTypeSize.isAccessibilitySize {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    identity
                    price
                }
            } else {
                HStack(spacing: MonacoTheme.Space.sm) {
                    identity
                    Spacer(minLength: MonacoTheme.Space.s)
                    price
                }
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("price-alert-stock")
    }

    /// ±2, ±5, ±10 percent from the price above, as the market's chips.
    private var presetChips: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            ForEach(AlertCopy.presetPercents, id: \.self) { percent in
                let line = model.presetLine(percent)
                let selected = model.presetPercent == percent && line != nil
                Button {
                    Haptics.selection()
                    model.choose(percent: percent)
                } label: {
                    Text(AlertCopy.presetLabel(percent: percent, direction: model.direction))
                        .font(MonacoTheme.Typo.dataStrong)
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                        .foregroundStyle(line == nil ? MonacoTheme.disabledLabel : (selected ? MonacoTheme.onBrand : MonacoTheme.ink))
                        .padding(.horizontal, MonacoTheme.Space.m)
                        .frame(maxWidth: .infinity, minHeight: 44)
                        .background(Capsule().fill(selected ? MonacoTheme.brandFill : MonacoTheme.surfaceSunken))
                        .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .disabled(line == nil)
                .accessibilityLabel(presetSpoken(percent: percent, line: line))
                .accessibilityAddTraits(selected ? .isSelected : [])
                .accessibilityIdentifier("price-alert-preset-\(percent)")
            }
        }
    }

    private func presetSpoken(percent: Int, line: Int64?) -> String {
        let move = "\(model.direction == .above ? "Up" : "Down") \(percent) percent"
        guard let line else { return move }
        return "\(move), \(UsdAmountFormatter.format(micros: line))"
    }

    /// A typed price, in the market's voice, on the one field anatomy.
    private var priceField: some View {
        HStack(spacing: 1) {
            Text("$")
                .font(MonacoTheme.Typo.quote)
                .foregroundStyle(model.amountText.isEmpty ? MonacoTheme.disabledLabel : MonacoTheme.ink)
            TextField(
                "",
                text: Binding(get: { model.amountText }, set: { model.type($0) }),
                prompt: Text(AlertCopy.customPlaceholder).foregroundStyle(MonacoTheme.disabledLabel)
            )
            .font(MonacoTheme.Typo.quote)
            .foregroundStyle(MonacoTheme.ink)
            .tint(MonacoTheme.ink)
            .keyboardType(.decimalPad)
            .focused($fieldFocused)
            .accessibilityLabel(AlertCopy.customPlaceholder)
            .accessibilityIdentifier("price-alert-custom-field")
        }
        .monacoFieldChrome(isFocused: fieldFocused, isInvalid: model.problem != nil)
        .contentShape(Rectangle())
        .onTapGesture { fieldFocused = true }
    }

    /// The line in words, or why it will not do.
    @ViewBuilder
    private var lineSentence: some View {
        Group {
            if let problem = model.problem {
                Text(problem)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.loss)
                    .accessibilityIdentifier("price-alert-problem")
            } else if let sentence = model.sentence {
                Text(sentence)
                    .font(MonacoTheme.Typo.bodyStrong)
                    .foregroundStyle(MonacoTheme.ink)
                    .accessibilityIdentifier("price-alert-sentence")
            } else {
                Text(AlertCopy.formHint)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.muted)
            }
        }
        .fixedSize(horizontal: false, vertical: true)
        .frame(minHeight: 44, alignment: .leading)
    }

    private var saveButton: some View {
        Button {
            fieldFocused = false
            Haptics.tap()
            Task {
                guard let saved = await model.save() else { return }
                Haptics.success()
                onSaved(saved)
                dismiss()
            }
        } label: {
            if model.isSaving {
                ProgressView().tint(MonacoTheme.onBrand)
            } else {
                Text(AlertCopy.saveTitle)
            }
        }
        .buttonStyle(.monacoPrimary)
        .monacoFullWidthButtons()
        .disabled(!model.canSave)
        .accessibilityIdentifier("price-alert-save")
    }

    // MARK: Waiting alerts

    @ViewBuilder
    private var existingSection: some View {
        formRow {
            MonacoSectionHeader(AlertCopy.existingHeader(name: model.name))
                .padding(.top, MonacoTheme.Space.l)
        }
        switch model.listState {
        case .loading where model.waiting.isEmpty:
            ForEach(0..<2, id: \.self) { index in
                formRow {
                    HStack {
                        SkeletonBlock(width: 128, height: 16, radius: 4)
                        Spacer()
                        SkeletonBlock(width: 72, height: 12, radius: 3)
                    }
                    .frame(minHeight: 44)
                }
                .accessibilityHidden(true)
            }
        case .failed where model.waiting.isEmpty:
            formRow {
                EmptyState(
                    title: AlertCopy.loadFailed,
                    actionTitle: "Retry",
                    action: { Task { await model.load() } }
                )
                .accessibilityIdentifier("price-alert-list-failed")
            }
        default:
            if model.waiting.isEmpty {
                formRow {
                    Text(AlertCopy.emptyListLine)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.muted)
                        .accessibilityIdentifier("price-alert-list-empty")
                }
            } else {
                ForEach(Array(model.waiting.enumerated()), id: \.element.id) { index, alert in
                    PriceAlertLedgerRow(alert: alert, isFirst: index == 0)
                        .listRowInsets(EdgeInsets(top: 0, leading: MonacoTheme.Space.m, bottom: 0, trailing: MonacoTheme.Space.m))
                        .listRowSeparator(.hidden)
                        .listRowBackground(MonacoTheme.canvas)
                        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                            Button(role: .destructive) {
                                Task { await model.delete(alert) }
                            } label: {
                                Label("Remove", systemImage: "trash")
                            }
                        }
                        .accessibilityAction(named: Text("Remove")) {
                            Task { await model.delete(alert) }
                        }
                        .accessibilityIdentifier("price-alert-row-\(alert.id)")
                }
            }
        }
    }
}

/// One alert as a ruled line: "Above $360.00", then its stamp in the market's voice. A fired
/// alert says what it reached under the line.
struct PriceAlertLedgerRow: View {
    let alert: PriceAlertDTO
    var isFirst = false
    var now = Date()

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        Group {
            if dynamicTypeSize.isAccessibilitySize {
                // The stamp goes under the line rather than truncating beside it.
                VStack(alignment: .leading, spacing: 4) {
                    lineAndReach
                    stamp
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            } else {
                HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
                    lineAndReach
                    Spacer(minLength: MonacoTheme.Space.s)
                    stamp
                }
            }
        }
        .padding(.vertical, 12)
        .frame(minHeight: 52)
        .overlay(alignment: .top) { if isFirst { MonacoRule() } }
        .overlay(alignment: .bottom) { MonacoRule() }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spoken)
    }

    private var lineAndReach: some View {
        VStack(alignment: .leading, spacing: 2) {
            (Text(AlertCopy.directionTitle(alert.direction) + " ")
                .font(MonacoTheme.Typo.bodyStrong)
             + Text(UsdAmountFormatter.format(micros: alert.priceUsdcMicros))
                .font(MonacoTheme.Typo.dataStrong))
                .foregroundStyle(alert.hasFired ? MonacoTheme.muted : MonacoTheme.ink)
            if let reached = alert.triggeredPriceUsdcMicros, alert.hasFired {
                Text(AlertCopy.reached(priceUsdcMicros: reached))
                    .font(MonacoTheme.Typo.dataCaption)
                    .foregroundStyle(MonacoTheme.muted)
            }
        }
    }

    private var stamp: some View {
        Text(AlertCopy.stamp(for: alert, now: now))
            .font(MonacoTheme.Typo.stamp)
            .foregroundStyle(alert.hasFired ? MonacoTheme.ink : MonacoTheme.tertiaryText)
            .lineLimit(1)
            .minimumScaleFactor(0.8)
    }

    private var spoken: String {
        var sentence = AlertCopy.line(direction: alert.direction, lineUsdcMicros: alert.priceUsdcMicros)
        if let reached = alert.triggeredPriceUsdcMicros, alert.hasFired {
            sentence += ", \(AlertCopy.reached(priceUsdcMicros: reached))"
        }
        return sentence + ", " + AlertCopy.spokenStamp(for: alert, now: now)
    }
}

// MARK: - Stock screen entry points

extension View {
    /// Everything the watchlist adds to the stock screen except the alert button: the star in
    /// the navigation bar, the alert sheet, and the watch state read off each detail poll.
    func assetWatchChrome(
        watch: AssetWatchModel,
        detail: AssetDetailDTO?,
        name: String,
        alerts: PriceAlertDataSource,
        showsAlertSheet: Binding<Bool>,
        prefill: PriceAlertPrefill?,
        toast: Binding<MonacoToast?>
    ) -> some View {
        modifier(AssetWatchChrome(
            watch: watch,
            detail: detail,
            name: name,
            alerts: alerts,
            showsAlertSheet: showsAlertSheet,
            prefill: prefill,
            toast: toast
        ))
    }
}

private struct AssetWatchChrome: ViewModifier {
    let watch: AssetWatchModel
    let detail: AssetDetailDTO?
    let name: String
    let alerts: PriceAlertDataSource
    @Binding var showsAlertSheet: Bool
    let prefill: PriceAlertPrefill?
    @Binding var toast: MonacoToast?

    func body(content: Content) -> some View {
        content
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    WatchStarButton(watching: watch.watching, isSaving: watch.isSaving) {
                        Haptics.selection()
                        Task {
                            if let message = await watch.toggle() { toast = message }
                        }
                    }
                }
            }
            .onChange(of: detail, initial: true) { _, detail in
                watch.absorb(detail)
                if prefill != nil, detail != nil, !showsAlertSheet, !didOpenPrefill {
                    didOpenPrefill = true
                    showsAlertSheet = true
                }
            }
            .sheet(isPresented: $showsAlertSheet) {
                PriceAlertSheet(
                    symbol: detail?.symbol ?? watch.symbol,
                    name: name,
                    currentPriceUsdcMicros: detail?.priceUsdcMicros,
                    assetKind: detail?.resolvedKind ?? .stock,
                    logoURL: detail?.logoUrl.flatMap(URL.init(string:)),
                    dataSource: alerts,
                    prefill: prefill,
                    onCountChange: { watch.alertsChanged(count: $0) },
                    onSaved: { toast = $0 }
                )
            }
    }

    @State private var didOpenPrefill = false
}
