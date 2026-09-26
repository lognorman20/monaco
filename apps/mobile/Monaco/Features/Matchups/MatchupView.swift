import MonacoCore
import SwiftUI

/// A cabal's matchup: this week's pair large, how it works in a line, next week's challenges,
/// recent results, and the way to challenge a cabal or open the season table.
struct MatchupView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let groupName: String
    let source: any MatchupDataSource

    @State private var model: GroupMatchupModel
    @State private var toast: MonacoToast?

    init(
        auth: PrivyAuthService,
        groupId: String,
        groupName: String,
        source: any MatchupDataSource,
        initial: GroupMatchupDTO? = nil
    ) {
        self.auth = auth
        self.groupId = groupId
        self.groupName = groupName
        self.source = source
        _model = State(initialValue: GroupMatchupModel(groupId: groupId, initial: initial))
    }

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
            .monacoCanvas()
            .navigationTitle(MatchupCopy.screenTitle)
            .navigationBarTitleDisplayMode(.inline)
            .task { try? await model.load(from: source) }
            .refreshable { try? await model.load(from: source) }
            .pollWhileVisible(every: LiveRefreshCadence.resting, isActive: model.state.value != nil) {
                try await model.load(from: source, quiet: true)
            }
            .monacoToast($toast)
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .loading, .hidden:
            ScrollView { MatchupScreenSkeleton() }
                .accessibilityIdentifier("matchup-loading")
        case .failed:
            ScrollView {
                EmptyState(
                    title: MatchupCopy.loadFailedTitle,
                    message: "Pull down or tap below to try again.",
                    actionTitle: MatchupCopy.tryAgain,
                    action: { Task { try? await model.load(from: source) } }
                )
                .padding(.top, MonacoTheme.Space.xl)
            }
            .accessibilityIdentifier("matchup-error")
        case .loaded(let dto):
            loaded(dto)
        }
    }

    private func loaded(_ dto: GroupMatchupDTO) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
                    if let current = dto.current {
                        MatchupHero(matchup: current)
                    } else {
                        notInDraw
                    }
                    Text(MatchupCopy.howItWorks)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.secondaryText)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(.horizontal, MonacoTheme.Space.gutter)
                        .accessibilityIdentifier("matchup-how-it-works")
                }

                if dto.canChallenge, !dto.challenges.isEmpty {
                    challengesSection(dto.challenges)
                }

                resultsSection(dto)

                actions(dto)
            }
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .accessibilityIdentifier("matchup-screen")
    }

    private var notInDraw: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            HStack(spacing: MonacoTheme.Space.sm) {
                CabalMark(groupId: groupId, name: groupName, size: 56)
                VStack(alignment: .leading, spacing: 2) {
                    Text(groupName)
                        .font(MonacoTheme.Typo.title)
                        .foregroundStyle(MonacoTheme.ink)
                        .lineLimit(2)
                    Text(MatchupCopy.notInDrawTitle)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
            }
            Text(MatchupCopy.notInDrawDetail)
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.secondaryText)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.horizontal, MonacoTheme.Space.gutter)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("matchup-not-in-draw")
    }

    private func challengesSection(_ challenges: [MatchupChallengeDTO]) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader(MatchupCopy.challengesTitle)
                .padding(.horizontal, MonacoTheme.Space.m)
            MonacoGroupedList {
                ForEach(challenges) { challenge in
                    MatchupChallengeRow(
                        challenge: challenge,
                        isAccepting: model.accepting.contains(challenge.id),
                        isLast: challenge.id == challenges.last?.id,
                        onAccept: {
                            Task {
                                if let result = await model.accept(challenge, from: source) {
                                    if result.isSuccess { Haptics.success() }
                                    toast = result
                                }
                            }
                        }
                    )
                }
            }
        }
        .accessibilityIdentifier("matchup-challenges")
    }

    private func resultsSection(_ dto: GroupMatchupDTO) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            HStack(alignment: .firstTextBaseline) {
                MonacoSectionHeader(MatchupCopy.recentTitle)
                if dto.record.hasPlayed {
                    Text(MatchupCopy.record(dto.record))
                        .font(MonacoTheme.Typo.dataStrong)
                        .foregroundStyle(MonacoTheme.ink)
                        .accessibilityLabel(MatchupCopy.recordSpoken(dto.record))
                        .accessibilityIdentifier("matchup-record")
                }
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            if dto.recent.isEmpty {
                MonacoGroupedList {
                    Text(MatchupCopy.noResultsYet)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.secondaryText)
                        .frame(maxWidth: .infinity, minHeight: 60, alignment: .leading)
                        .padding(.horizontal, MonacoTheme.Space.m)
                }
                .accessibilityIdentifier("matchup-results-empty")
            } else {
                MonacoGroupedList {
                    ForEach(dto.recent) { result in
                        MatchupResultRow(result: result, isLast: result.id == dto.recent.last?.id)
                    }
                }
                .accessibilityIdentifier("matchup-results")
            }
        }
    }

    private func actions(_ dto: GroupMatchupDTO) -> some View {
        MonacoGroupedList {
            if dto.canChallenge {
                NavigationLink {
                    ChallengeCabalView(
                        groupId: groupId,
                        groupName: groupName,
                        source: source,
                        alreadyChallenged: Set(dto.challenges.filter { $0.direction == .outgoing }.map(\.opponent.groupID))
                    )
                } label: {
                    MonacoRow(title: MatchupCopy.challengeTitle, subtitle: "Play a cabal of your choosing next week", chevron: true) {
                        actionGlyph("flag")
                    }
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier("matchup-challenge")
            }
            NavigationLink {
                MatchupTableView(source: source)
            } label: {
                MonacoRow(title: MatchupCopy.tableTitle, subtitle: "Every cabal by wins", chevron: true, isLast: true) {
                    actionGlyph("list.number")
                }
            }
            .buttonStyle(.monacoRow)
            .accessibilityIdentifier("matchup-season-table")
        }
    }

    private func actionGlyph(_ name: String) -> some View {
        Image(systemName: name)
            .font(MonacoTheme.Typo.bodyStrong)
            .foregroundStyle(MonacoTheme.brand)
            .frame(width: 44, height: 44)
            .background(MonacoTheme.surfaceSunken, in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.tile, style: .continuous))
            .accessibilityHidden(true)
    }
}

/// This week's pair, large: the two marks facing each other across "vs", the two scores, the lead
/// bar, and how long is left. A bye is the one cabal and the words for it.
struct MatchupHero: View {
    let matchup: MatchupDTO

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            if let b = matchup.b {
                let scores = MatchupCopy.scores(matchup.a.score, b.score)
                HStack(alignment: .top, spacing: MonacoTheme.Space.s) {
                    side(matchup.a, score: scores.a, key: .a, alignment: .leading)
                    Text(MatchupCopy.vs)
                        .font(MonacoTheme.Typo.stamp)
                        .foregroundStyle(MonacoTheme.tertiaryText)
                        .frame(height: 64)
                        .accessibilityHidden(true)
                    side(b, score: scores.b, key: .b, alignment: .trailing)
                }
                MatchupLeadBar(
                    fraction: MatchupCopy.leadFraction(a: matchup.a.score, b: b.score),
                    leading: matchup.leading,
                    groupA: matchup.a.groupID,
                    groupB: b.groupID,
                    height: 6
                )
                ViewThatFits(in: .horizontal) {
                    HStack {
                        Text(MatchupCopy.daysLeft(matchup.daysLeft))
                        Spacer()
                        Text("Week of \(MatchupCopy.weekLabel(matchup.weekStart))")
                    }
                    VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                        Text(MatchupCopy.daysLeft(matchup.daysLeft))
                        Text("Week of \(MatchupCopy.weekLabel(matchup.weekStart))")
                    }
                }
                .font(MonacoTheme.Typo.stamp)
                .foregroundStyle(MonacoTheme.tertiaryText)
            } else {
                bye
            }
        }
        .padding(.horizontal, MonacoTheme.Space.gutter)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(MatchupCopy.spoken(matchup))
        .accessibilityIdentifier(matchup.isBye ? "matchup-bye" : "matchup-hero")
    }

    private func side(_ side: MatchupSideDTO, score: String, key: MatchupLeader, alignment: HorizontalAlignment) -> some View {
        let tone = MatchupScoreTone.tone(for: key, leading: matchup.leading, score: side.score)
        let textAlignment: TextAlignment = alignment == .leading ? .leading : .trailing
        return VStack(alignment: alignment, spacing: MonacoTheme.Space.s) {
            CabalMark(groupId: side.groupID, name: side.name, size: 64, pictureUrl: side.pictureUrl)
            Text(side.name)
                .font(MonacoTheme.Typo.rowTitle)
                .foregroundStyle(MonacoTheme.ink)
                .multilineTextAlignment(textAlignment)
                .lineLimit(2)
                .fixedSize(horizontal: false, vertical: true)
            Text(score)
                .moneyFont(.large, weight: tone == .plain ? .medium : .semibold, voice: .market)
                .foregroundStyle(tone.color)
                .lineLimit(1)
                .minimumScaleFactor(0.6)
            Text(side.memberCount == 1 ? "1 member" : "\(side.memberCount) members")
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.secondaryText)
        }
        .frame(maxWidth: .infinity, alignment: alignment == .leading ? .leading : .trailing)
    }

    private var bye: some View {
        HStack(alignment: .top, spacing: MonacoTheme.Space.sm) {
            CabalMark(groupId: matchup.a.groupID, name: matchup.a.name, size: 64, pictureUrl: matchup.a.pictureUrl)
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                Text(matchup.a.name)
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(2)
                Text(MatchupCopy.byeTitle)
                    .font(MonacoTheme.Typo.title)
                    .foregroundStyle(MonacoTheme.ink)
                Text(MatchupCopy.byeDetail)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .fixedSize(horizontal: false, vertical: true)
                Text(MatchupCopy.daysLeft(matchup.daysLeft))
                    .font(MonacoTheme.Typo.stamp)
                    .foregroundStyle(MonacoTheme.tertiaryText)
            }
        }
    }
}

/// A finished week: the outcome letter, who it was against and when, and both scores.
struct MatchupResultRow: View {
    let result: MatchupResultDTO
    let isLast: Bool

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        let isBye = result.result == .bye || result.opponent == nil
        let scores = MatchupCopy.scores(result.score, result.opponentScore)
        let layout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: MonacoTheme.Space.s))
            : AnyLayout(HStackLayout(spacing: MonacoTheme.Space.sm))
        layout {
            Text(MatchupCopy.outcomeLetter(result.result))
                .font(MonacoTheme.Typo.dataStrong)
                .foregroundStyle(result.result == .win ? MonacoTheme.ink : MonacoTheme.secondaryText)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
                .frame(width: 44, height: 44)
                .background(
                    result.result == .win ? MonacoTheme.brandWash : MonacoTheme.surfaceSunken,
                    in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.tile, style: .continuous)
                )
            VStack(alignment: .leading, spacing: 2) {
                Text(isBye ? MatchupCopy.byeTitle : "\(MatchupCopy.vs) \(result.opponent?.name ?? "")")
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
                Text(MatchupCopy.weekLabel(result.weekStart))
                    .font(MonacoTheme.Typo.stamp)
                    .foregroundStyle(MonacoTheme.tertiaryText)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if !isBye {
                VStack(alignment: dynamicTypeSize.isAccessibilitySize ? .leading : .trailing, spacing: 2) {
                    Text(scores.a)
                        .font(MonacoTheme.Typo.dataStrong)
                        .foregroundStyle(MonacoTheme.signed(scores.a))
                    Text("\(MatchupCopy.vs) \(scores.b)")
                        .font(MonacoTheme.Typo.dataCaption)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
                .lineLimit(1)
                .fixedSize()
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, 8)
        .frame(minHeight: 60)
        .overlay(alignment: .bottom) {
            if !isLast {
                MonacoRule().padding(.leading, MonacoTheme.Space.m + 44 + MonacoTheme.Space.sm)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(MatchupCopy.resultLine(result))
        .accessibilityIdentifier("matchup-result-\(result.id)")
    }
}

/// A challenge for next week: who, which way, and — when it was sent to this cabal and is still
/// open — an Accept button.
struct MatchupChallengeRow: View {
    let challenge: MatchupChallengeDTO
    let isAccepting: Bool
    let isLast: Bool
    let onAccept: () -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var line: String {
        switch (challenge.direction, challenge.status) {
        case (_, .accepted), (_, .scheduled): return MatchupCopy.nextOpponent(challenge.opponent.name)
        case (.incoming, _): return MatchupCopy.incomingChallenge(from: challenge.opponent.name)
        case (.outgoing, _): return MatchupCopy.outgoingChallenge(to: challenge.opponent.name)
        }
    }

    var body: some View {
        // At the accessibility sizes the button drops under the words instead of cutting them off.
        let layout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: MonacoTheme.Space.s))
            : AnyLayout(HStackLayout(spacing: MonacoTheme.Space.sm))
        layout {
            HStack(spacing: MonacoTheme.Space.sm) {
                CabalMark(groupId: challenge.opponent.groupID, name: challenge.opponent.name, size: 40, pictureUrl: challenge.opponent.pictureUrl)
                Text(line)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(dynamicTypeSize.isAccessibilitySize ? nil : 2)
                    .fixedSize(horizontal: false, vertical: dynamicTypeSize.isAccessibilitySize)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            if challenge.canAccept {
                Button(action: onAccept) {
                    Group {
                        if isAccepting {
                            ProgressView().tint(MonacoTheme.primaryButtonLabel)
                        } else {
                            Text(MatchupCopy.acceptAction)
                        }
                    }
                    .font(MonacoTheme.Typo.calloutStrong)
                    .foregroundStyle(MonacoTheme.primaryButtonLabel)
                    .padding(.horizontal, 14)
                    .frame(minHeight: 36)
                    .background(Capsule().fill(MonacoTheme.primaryButtonFill))
                    .frame(minHeight: 44)
                }
                .buttonStyle(.plain)
                .disabled(isAccepting)
                .accessibilityIdentifier("matchup-accept-\(challenge.id)")
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, 6)
        .frame(minHeight: 60)
        .overlay(alignment: .bottom) {
            if !isLast {
                MonacoRule().padding(.leading, MonacoTheme.Space.m + 40 + MonacoTheme.Space.sm)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// The matchup screen while it loads: the hero's two sides and bar, then ruled result rows.
struct MatchupScreenSkeleton: View {
    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
                HStack(alignment: .top) {
                    skeletonSide(alignment: .leading)
                    Spacer()
                    skeletonSide(alignment: .trailing)
                }
                SkeletonBlock(height: 6, radius: 3)
                SkeletonBlock(width: 96, height: 10)
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                SkeletonBlock(width: 140, height: 20)
                    .padding(.horizontal, MonacoTheme.Space.m)
                MatchupResultRowSkeleton(rows: 3)
            }
        }
        .padding(.top, MonacoTheme.Space.m)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading matchup")
    }

    private func skeletonSide(alignment: HorizontalAlignment) -> some View {
        VStack(alignment: alignment, spacing: MonacoTheme.Space.s) {
            SkeletonBlock(width: 64, height: 64, radius: MonacoTheme.Radius.tile * 64 / 44)
            SkeletonBlock(width: 120, height: 14)
            SkeletonBlock(width: 80, height: 26)
        }
    }
}

/// Result rows while they load: the outcome tile, the opponent and week, the two scores.
struct MatchupResultRowSkeleton: View {
    var rows = 3

    var body: some View {
        VStack(spacing: 0) {
            ForEach(0..<rows, id: \.self) { index in
                HStack(spacing: MonacoTheme.Space.sm) {
                    SkeletonBlock(width: 44, height: 44, radius: MonacoTheme.Radius.tile)
                    VStack(alignment: .leading, spacing: 6) {
                        SkeletonBlock(width: 150, height: 14)
                        SkeletonBlock(width: 56, height: 10)
                    }
                    Spacer(minLength: MonacoTheme.Space.s)
                    VStack(alignment: .trailing, spacing: 6) {
                        SkeletonBlock(width: 52, height: 14)
                        SkeletonBlock(width: 60, height: 10)
                    }
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .frame(minHeight: 60)
                .overlay(alignment: .bottom) {
                    if index < rows - 1 {
                        MonacoRule().padding(.leading, MonacoTheme.Space.m + 44 + MonacoTheme.Space.sm)
                    }
                }
            }
        }
        .overlay(alignment: .top) { MonacoRule() }
        .overlay(alignment: .bottom) { MonacoRule() }
        .accessibilityHidden(true)
    }
}
