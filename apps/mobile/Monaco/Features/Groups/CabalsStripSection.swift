import MonacoCore
import SwiftUI

/// Whether the viewer's cabals are known yet. An empty list only means "no
/// cabals" once the server has answered; before that it means "not loaded".
enum CabalsStripState {
    /// The list is on its way.
    case loading
    /// The server answered; an empty list is genuinely empty.
    case loaded
    /// We have no list and nothing is in flight — the load did not land.
    case unavailable
}

/// Horizontal snapping strip of the viewer's cabals. Each card leads with a solid band in that
/// cabal's tint carrying its mark, then the name, the pot and how it is doing.
///
/// The band is the identity: five of these side by side are five different rooms, and the tints
/// come from `CabalTintAssignment` so no two of the viewer's own cabals can be the same colour.
struct CabalsStripSection: View {
    let rows: [HomeGroupBoardRowDTO]
    var state: CabalsStripState = .loaded
    /// Resolved across the viewer's own cabals. Empty falls back to the plain hash per cabal.
    var tints: [String: MonacoTheme.CabalTint] = [:]
    /// The zoom transition's namespace, when the owning screen has one.
    var zoomNamespace: Namespace.ID?
    var onSelect: (CabalsRoute) -> Void
    var onRetry: () -> Void = {}

    static let cardSize = CGSize(width: 200, height: 176)

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// Snapping and scroll transitions are decoration on a horizontal strip and they fight the
    /// vertical stack the row falls back to at an accessibility size — the same gate
    /// `StockMoverStrip` already applies to the movers.
    private var scrollsHorizontally: Bool { !dynamicTypeSize.isAccessibilitySize }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.headerToContent) {
            MonacoSectionHeader("Your cabals")

            if !rows.isEmpty {
                if scrollsHorizontally {
                    strip
                } else {
                    stack
                }
            } else {
                switch state {
                case .loading:
                    placeholderStrip
                case .unavailable:
                    // Deliberately no cause: the store swallows the reason, so a
                    // missing token, a 401 and a 500 all arrive here and naming
                    // the connection would be a guess. See "Needs from other
                    // areas" — a load state on the store is what fixes this.
                    EmptyState(
                        title: "Couldn't load your cabals",
                        message: "Give it another go.",
                        actionTitle: "Try again",
                        action: onRetry
                    )
                    .accessibilityIdentifier("cabals-strip-error")
                case .loaded:
                    EmptyState(
                        title: "No cabals yet",
                        message: "Search above or start one with the + button."
                    )
                    .accessibilityIdentifier("cabals-strip-empty")
                }
            }
        }
    }

    private func tint(for row: HomeGroupBoardRowDTO) -> MonacoTheme.CabalTint {
        CabalTintAssignment.tint(forGroupId: row.groupId, in: tints)
    }

    private var strip: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            LazyHStack(spacing: MonacoTheme.Space.sm) {
                ForEach(rows) { row in
                    cardButton(row)
                        .scrollTransition(.interactive, axis: .horizontal) { view, phase in
                            view
                                .opacity(phase.isIdentity ? 1 : 0.55)
                                .scaleEffect(phase.isIdentity ? 1 : 0.94)
                        }
                }
                newCabalButton
            }
            .scrollTargetLayout()
            .padding(.vertical, 2)
        }
        .scrollTargetBehavior(.viewAligned)
        .accessibilityIdentifier("cabals-strip")
    }

    /// At an accessibility text size the cards are taller than the screen is wide; stacking them
    /// is the only layout that still shows a whole cabal at once.
    private var stack: some View {
        VStack(spacing: MonacoTheme.Space.sm) {
            ForEach(rows) { row in
                cardButton(row)
            }
            newCabalButton
        }
        .accessibilityIdentifier("cabals-strip")
    }

    private func cardButton(_ row: HomeGroupBoardRowDTO) -> some View {
        Button {
            onSelect(.cabal(id: row.groupId, name: row.name))
        } label: {
            CabalStripCard(
                row: row,
                tint: tint(for: row),
                size: Self.cardSize,
                fillsWidth: !scrollsHorizontally
            )
        }
        .buttonStyle(.plain)
        .zoomSource(id: row.groupId, in: zoomNamespace)
        .accessibilityIdentifier("cabals-strip-card-\(row.groupId)")
    }

    private var newCabalButton: some View {
        Button {
            onSelect(.create)
        } label: {
            NewCabalStripCard(size: Self.cardSize, fillsWidth: !scrollsHorizontally)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("cabals-strip-new")
    }

    /// Two cards in the real shape while the list loads, so the section does not
    /// jump from a message to content.
    private var placeholderStrip: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            ForEach(0..<2, id: \.self) { _ in
                VStack(alignment: .leading, spacing: 0) {
                    SkeletonBlock(height: CabalStripCard.bandHeight)
                    VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                        SkeletonBlock(width: 104, height: 14)
                        Spacer(minLength: 0)
                        SkeletonBlock(width: 84, height: 22)
                        SkeletonBlock(width: 64, height: 12)
                    }
                    .padding(MonacoTheme.Space.m)
                }
                .frame(width: Self.cardSize.width, height: Self.cardSize.height, alignment: .topLeading)
                .clipShape(RoundedRectangle(cornerRadius: MonacoTheme.Radius.container, style: .continuous))
                .monacoElevation(.card)
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement()
        .accessibilityLabel("Loading your cabals")
        .accessibilityIdentifier("cabals-strip-loading")
    }
}

/// One cabal: a solid tint band with the mark, then the name, the pot and the P&L badge.
///
/// There are no member faces here and no "2 open votes" status line, which the direction asks
/// for: `HomeGroupBoardRowDTO` carries the id, the name, the pot, the percent and the dollar
/// P&L, and nothing about who is in the cabal or what is happening in it. Faces and a live line
/// are filed as the API follow-up rather than invented.
struct CabalStripCard: View {
    let row: HomeGroupBoardRowDTO
    let tint: MonacoTheme.CabalTint
    let size: CGSize
    var fillsWidth = false

    static let bandHeight: CGFloat = 56

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            band
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                Text(row.name)
                    .displayFont(.section)
                    .foregroundStyle(MonacoTheme.fgPrimary)
                    .lineLimit(2)
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: MonacoTheme.Space.xs)
                MoneyText(decimalString: row.potValueUsd, style: .large)
                PnLBadge(dollarPnl: row.dollarPnl, percentReturn: row.percentReturn, style: .caption)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(MonacoTheme.Space.m)
        }
        .frame(width: fillsWidth ? nil : size.width, alignment: .topLeading)
        .frame(maxWidth: fillsWidth ? .infinity : nil, alignment: .topLeading)
        .frame(minHeight: size.height, alignment: .topLeading)
        .clipShape(RoundedRectangle(cornerRadius: MonacoTheme.Radius.container, style: .continuous))
        .monacoElevation(.card)
        .accessibilityElement(children: .combine)
    }

    /// The identity band. `fill` carries the mark, which is 40pt bold initials — large text under
    /// WCAG, which is the bar `fill` is built to clear.
    private var band: some View {
        ZStack(alignment: .bottomLeading) {
            Rectangle()
                .fill(tint.fill)
                .frame(height: Self.bandHeight)
            CabalMark(tint: tint, name: row.name, size: 40)
                .overlay {
                    RoundedRectangle(
                        cornerRadius: MonacoTheme.Radius.tile * 40 / 44,
                        style: .continuous
                    )
                    .strokeBorder(MonacoTheme.bgRaised, lineWidth: 2)
                }
                .padding(.leading, MonacoTheme.Space.m)
                // Half the mark hangs below the band onto the card, so the two read as one
                // object rather than a coloured header with a tile parked in it.
                .offset(y: 20)
        }
        .frame(height: Self.bandHeight)
        // The mark overhangs, so the band must not clip it — the card clips the whole stack.
        .zIndex(1)
    }
}

private struct NewCabalStripCard: View {
    let size: CGSize
    var fillsWidth = false

    var body: some View {
        VStack(spacing: MonacoTheme.Space.s) {
            Image(systemName: "plus")
                .font(.title2.weight(.semibold))
                .foregroundStyle(MonacoTheme.fgMuted)
            Text("New cabal")
                .font(MonacoTheme.Typo.callout.weight(.semibold))
                .foregroundStyle(MonacoTheme.fgMuted)
        }
        .frame(width: fillsWidth ? nil : size.width, height: size.height)
        .frame(maxWidth: fillsWidth ? .infinity : nil)
        .overlay {
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.container, style: .continuous)
                .strokeBorder(MonacoTheme.line, style: StrokeStyle(lineWidth: 1, dash: [6, 4]))
        }
        .accessibilityLabel("New cabal")
    }
}

extension View {
    /// Marks this view as the source of a zoom push, when the screen has a namespace to hang it
    /// on. Written as one modifier so a caller without a namespace — a preview, a harness — is
    /// not forced to invent one.
    @ViewBuilder
    func zoomSource(id: String, in namespace: Namespace.ID?) -> some View {
        if let namespace {
            matchedTransitionSource(id: id, in: namespace)
        } else {
            self
        }
    }

    /// The matching push. `.zoom` honours Reduce Motion itself, so there is no branch here.
    @ViewBuilder
    func zoomDestination(id: String, in namespace: Namespace.ID?) -> some View {
        if let namespace {
            navigationTransition(.zoom(sourceID: id, in: namespace))
        } else {
            self
        }
    }
}
