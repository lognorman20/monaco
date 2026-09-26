import MonacoCore
import SwiftUI
import UIKit

/// The inbox: everything Monaco told the member, newest first, under "Today" and "Earlier".
/// A ruled list on the paper; the only card is the ask to turn notifications on, because that is
/// the one thing on this screen the member acts on besides the rows.
struct InboxView: View {
    @ObservedObject var auth: PrivyAuthService
    let model: InboxModel
    var permission: PushPermissionSource = SystemPushPermission()
    /// "See your cabals" under an empty inbox. Nil hides it.
    var onOpenCabals: (() -> Void)?
    /// The time rows are aged and grouped against. Sample screens pin it.
    var clock: () -> Date = Date.init

    @State private var destination: NotificationDestination?
    @State private var toast: MonacoToast?
    @State private var permissionStatus: PushPermissionStatus?
    @State private var now = Date()
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        content
            .monacoCanvas()
            .navigationTitle(InboxCopy.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                // Only while something is unread: a disabled pill in the bar is noise.
                if model.unreadCount > 0 {
                    ToolbarItem(placement: .topBarTrailing) {
                        markAllButton
                    }
                }
            }
            .navigationDestination(item: $destination) { destination in
                InboxDestinationView(auth: auth, destination: destination)
            }
            .refreshable {
                if !(await model.refresh()) {
                    toast = MonacoToast(message: InboxCopy.refreshFailed)
                }
                now = clock()
            }
            .task {
                now = clock()
                if model.phase == .loaded {
                    try? await model.poll()
                } else {
                    await model.load()
                }
                now = clock()
            }
            .task(id: scenePhase) {
                // Coming back from Settings is the moment the answer may have changed.
                guard scenePhase == .active else { return }
                permissionStatus = await permission.status()
            }
            .pollWhileVisible(every: LiveRefreshCadence.resting) {
                try await model.poll()
                now = clock()
            }
            .monacoToast($toast)
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("inbox-view")
    }

    @ViewBuilder
    private var content: some View {
        if model.items.isEmpty {
            switch model.phase {
            case .loading:
                InboxSkeleton()
            case .failed:
                ScrollView {
                    EmptyState(
                        title: InboxCopy.loadFailedTitle,
                        message: InboxCopy.loadFailedMessage,
                        actionTitle: InboxCopy.tryAgain,
                        action: { Task { await model.load() } }
                    )
                    .padding(.top, MonacoTheme.Space.xl)
                    .accessibilityIdentifier("inbox-error")
                }
                .scrollBounceBehavior(.always)
            case .loaded:
                ScrollView {
                    VStack(spacing: MonacoTheme.Space.l) {
                        permissionCard
                        EmptyState(
                            title: InboxCopy.emptyTitle,
                            message: InboxCopy.emptyMessage,
                            actionTitle: onOpenCabals == nil ? nil : InboxCopy.openCabals,
                            action: onOpenCabals
                        )
                        .accessibilityIdentifier("inbox-empty")
                    }
                    .padding(.top, MonacoTheme.Space.l)
                }
                .scrollBounceBehavior(.always)
            }
        } else {
            list
        }
    }

    private var list: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                permissionCard
                ForEach(InboxGrouping.sections(model.items, now: now)) { section in
                    sectionView(section)
                }
                olderFooter
            }
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .accessibilityIdentifier("inbox-list")
    }

    private func sectionView(_ section: InboxSection) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader(section.title)
                .padding(.horizontal, MonacoTheme.Space.m)
            MonacoGroupedList {
                ForEach(section.items) { notification in
                    Button {
                        open(notification)
                    } label: {
                        InboxRow(notification: notification, now: now, isLast: notification.id == section.items.last?.id)
                    }
                    .buttonStyle(.monacoRow)
                    .accessibilityIdentifier("inbox-row-\(notification.id)")
                }
            }
        }
    }

    @ViewBuilder
    private var olderFooter: some View {
        if model.nextCursor != nil {
            Group {
                if model.olderFailed {
                    Button(InboxCopy.loadMore) {
                        Task { await model.loadOlder() }
                    }
                    .buttonStyle(.monacoSecondary)
                    .accessibilityIdentifier("inbox-load-older")
                } else {
                    ProgressView()
                        .tint(MonacoTheme.ink)
                        .accessibilityLabel("Loading older")
                        .task { await model.loadOlder() }
                }
            }
            .frame(maxWidth: .infinity)
        }
    }

    @ViewBuilder
    private var permissionCard: some View {
        switch permissionStatus {
        case .notDetermined:
            InboxPermissionCard(
                title: InboxCopy.permissionTitle,
                message: InboxCopy.permissionMessage,
                actionTitle: InboxCopy.permissionTurnOn,
                action: {
                    UserDefaults.standard.set(true, forKey: PushPromptGate.askedKey)
                    permissionStatus = await permission.request()
                }
            )
            .padding(.horizontal, MonacoTheme.Space.m)
        case .denied:
            InboxPermissionCard(
                title: InboxCopy.permissionDeniedTitle,
                message: InboxCopy.permissionDeniedMessage,
                actionTitle: InboxCopy.permissionOpenSettings,
                action: {
                    if let url = URL(string: UIApplication.openNotificationSettingsURLString) {
                        await UIApplication.shared.open(url)
                    }
                }
            )
            .padding(.horizontal, MonacoTheme.Space.m)
        case .authorized, .none:
            EmptyView()
        }
    }

    private var markAllButton: some View {
        Button {
            Task {
                if await model.markAllRead() {
                    Haptics.success()
                } else {
                    Haptics.warning()
                    toast = MonacoToast(message: InboxCopy.markReadFailed)
                }
            }
        } label: {
            Text(InboxCopy.markAllRead)
                .font(MonacoTheme.Typo.calloutStrong)
                .foregroundStyle(MonacoTheme.brand)
                .lineLimit(1)
                .fixedSize()
                .frame(minHeight: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("inbox-mark-all-read")
    }

    private func open(_ notification: NotificationDTO) {
        Haptics.selection()
        let target = NotificationDestination.of(notification)
        if target != .none {
            destination = target
        }
        Task { await model.open(notification) }
    }
}

/// One inbox row: the mark, the title in the row voice over the body, and the age in the
/// market's mono stamp with the unread dot under it.
struct InboxRow: View {
    let notification: NotificationDTO
    let now: Date
    let isLast: Bool

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var isStacked: Bool { dynamicTypeSize.isAccessibilitySize }

    var body: some View {
        HStack(alignment: .top, spacing: MonacoTheme.Space.sm) {
            InboxMark(mark: NotificationMark.of(notification))
                .frame(width: 44, height: 44)
            VStack(alignment: .leading, spacing: 2) {
                Text(notification.title)
                    .font(MonacoTheme.Typo.bodyStrong)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(isStacked ? nil : 3)
                    .fixedSize(horizontal: false, vertical: true)
                if !notification.body.isEmpty {
                    Text(notification.body)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                        .lineLimit(isStacked ? nil : 2)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if isStacked {
                    stamp
                        .padding(.top, MonacoTheme.Space.xs)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if !isStacked {
                stamp
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, MonacoTheme.Space.sm)
        .frame(minHeight: 60)
        .contentShape(Rectangle())
        .overlay(alignment: .bottom) {
            if !isLast {
                Rectangle()
                    .fill(MonacoTheme.hairline)
                    .frame(height: 1)
                    .padding(.leading, MonacoRowLayout(dynamicTypeSize: dynamicTypeSize).separatorLeadingInset)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityText)
        .accessibilityAddTraits(.isButton)
    }

    private var stamp: some View {
        VStack(alignment: isStacked ? .leading : .trailing, spacing: 6) {
            Text(NotificationAge.label(for: notification.createdAt, now: now))
                .font(MonacoTheme.Typo.stamp)
                .foregroundStyle(MonacoTheme.tertiaryText)
                .fixedSize()
            if notification.isUnread {
                Circle()
                    .fill(MonacoTheme.ink)
                    .frame(width: 8, height: 8)
                    .accessibilityHidden(true)
            }
        }
    }

    private var accessibilityText: String {
        var parts: [String] = []
        if notification.isUnread { parts.append(InboxCopy.unreadDot) }
        parts.append(notification.title)
        if !notification.body.isEmpty { parts.append(notification.body) }
        parts.append(NotificationAge.accessibilityLabel(for: notification.createdAt, now: now))
        return parts.joined(separator: ". ")
    }
}

/// The row's mark: the stock's coin for a buy or sell, the dollar coin for the member's own
/// money, the cabal's mark for everything else in a cabal, and a bell for the rest.
struct InboxMark: View {
    let mark: NotificationMark

    var body: some View {
        switch mark {
        case .stock(let symbol):
            StockMark(symbol: symbol)
        case .money:
            StockMark(symbol: "USDC")
        case .cabal(let groupId, let name, let pictureUrl):
            CabalMark(groupId: groupId, name: name, pictureUrl: pictureUrl)
        case .bell:
            SunkenGlyphMark(systemImage: "bell")
        }
    }
}

/// The ask to turn notifications on, or to open Settings when they were turned off.
struct InboxPermissionCard: View {
    let title: String
    let message: String
    let actionTitle: String
    let action: () async -> Void

    @State private var isWorking = false

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            HStack(alignment: .top, spacing: MonacoTheme.Space.sm) {
                SunkenGlyphMark(systemImage: "bell", size: 40)
                VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                    Text(title)
                        .font(MonacoTheme.Typo.bodyStrong)
                        .foregroundStyle(MonacoTheme.ink)
                        .fixedSize(horizontal: false, vertical: true)
                    Text(message)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            Button {
                guard !isWorking else { return }
                isWorking = true
                Task {
                    await action()
                    isWorking = false
                }
            } label: {
                Text(actionTitle)
            }
            .buttonStyle(.monacoPrimary)
            .monacoFullWidthButtons()
            .disabled(isWorking)
            .accessibilityIdentifier("inbox-permission-action")
        }
        .padding(MonacoTheme.Space.m)
        .background(
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                .fill(MonacoTheme.surface)
        )
        .overlay {
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("inbox-permission")
    }
}

/// Rows in the shape of the inbox: a section header's line, then mark, two lines and a stamp.
struct InboxSkeleton: View {
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                SkeletonBlock(width: 72, height: 18)
                    .frame(height: 28)
                    .padding(.horizontal, MonacoTheme.Space.m)
                VStack(spacing: 0) {
                    ForEach(0..<6, id: \.self) { index in
                        HStack(alignment: .top, spacing: MonacoTheme.Space.sm) {
                            SkeletonBlock(width: 44, height: 44, radius: MonacoTheme.Radius.tile)
                            VStack(alignment: .leading, spacing: 6) {
                                SkeletonBlock(width: index.isMultiple(of: 2) ? 220 : 180, height: 15)
                                SkeletonBlock(width: 140, height: 11)
                            }
                            .padding(.top, 4)
                            Spacer(minLength: MonacoTheme.Space.s)
                            SkeletonBlock(width: 24, height: 11)
                                .padding(.top, 4)
                        }
                        .padding(.horizontal, MonacoTheme.Space.m)
                        .padding(.vertical, MonacoTheme.Space.sm)
                        .overlay(alignment: .bottom) {
                            if index < 5 {
                                MonacoRule().padding(.leading, MonacoTheme.Space.m + 44 + MonacoTheme.Space.sm)
                            }
                        }
                    }
                }
                .overlay(alignment: .top) { MonacoRule() }
                .overlay(alignment: .bottom) { MonacoRule() }
            }
            .padding(.top, MonacoTheme.Space.m)
        }
        .scrollDisabled(true)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
        .accessibilityIdentifier("inbox-loading")
    }
}

/// Where a notification goes, built with the routes Home already uses.
struct InboxDestinationView: View {
    @ObservedObject var auth: PrivyAuthService
    let destination: NotificationDestination

    var body: some View {
        switch destination {
        case .proposal(let id):
            ProposalDetailView(auth: auth, proposalId: id)
        case .cabal(let id, let name):
            GroupDetailView(auth: auth, groupId: id, groupName: name)
        case .stock(let symbol):
            AssetDetailView(auth: auth, symbol: symbol)
        case .transaction(let id, let isSell):
            // The receipt reads everything it shows from the transaction itself; the row here
            // only names it. `confirmed` because the server tells members about fills, not tries.
            TransactionDetailView(
                auth: auth,
                activityItem: GroupActivityItemDTO(
                    id: id,
                    kind: isSell ? "sell" : "buy",
                    status: "confirmed",
                    symbol: nil,
                    amountMicros: 0,
                    createdAt: "",
                    txSignature: nil,
                    tokenAmount: nil,
                    proceedsUsdcMicros: nil,
                    initiatedBy: nil,
                    agentDisplayName: nil
                ),
                onRetry: nil,
                isRetrying: false
            )
        case .none:
            EmptyView()
        }
    }
}
