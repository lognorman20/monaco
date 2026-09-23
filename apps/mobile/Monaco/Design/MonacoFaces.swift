import MonacoCore
import SwiftUI

/// One person, as much of them as the API actually carries.
///
/// `photoURL` is nil for most people today and that is the honest state, not a gap to fill:
/// `ProposalVoteDTO` carries `voterId`, `displayName`, `choice` and `castAt` and no photo, so a
/// vote face is initials until `profilePhotoUrl` lands. `MonacoAvatar` already renders initials.
/// Nothing here invents a face.
struct MonacoFace: Identifiable, Equatable {
    let id: String
    let displayName: String
    var photoURL: String?

    init(id: String, displayName: String, photoURL: String? = nil) {
        self.id = id
        self.displayName = displayName
        self.photoURL = photoURL
    }
}

/// Overlapping faces with a ring in the surface colour, and a "+N" disc when there are more.
///
/// Used on strip cards, leaderboard rows, the propose review ("who will be asked to vote") and the
/// cabal toolbar. It is a *who*, never a count on its own — if the only thing available is a
/// number, this is the wrong component.
struct MonacoFaceStack: View {
    let faces: [MonacoFace]
    var size: CGFloat = 20
    var maxVisible: Int = 4

    /// The colour the ring punches out of. This is the surface behind the stack, so the faces
    /// read as separated rather than welded together.
    var ringColor: Color = MonacoTheme.bgRaised

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// The faces overlap by 30% of their width; at accessibility sizes they do not, because
    /// overlapping 34pt discs stop being separate people.
    private var overlap: CGFloat {
        dynamicTypeSize.isAccessibilitySize ? 2 : -size * 0.3
    }

    private var visible: [MonacoFace] {
        Array(faces.prefix(maxVisible))
    }

    private var overflow: Int {
        max(faces.count - visible.count, 0)
    }

    private var label: String {
        guard !faces.isEmpty else { return "" }
        let names = visible.map(\.displayName).filter { !$0.isEmpty }
        if overflow > 0 {
            return ([names.joined(separator: ", ")] + ["and \(overflow) more"]).joined(separator: " ")
        }
        return names.joined(separator: ", ")
    }

    var body: some View {
        if faces.isEmpty {
            EmptyView()
        } else {
            HStack(spacing: overlap) {
                ForEach(visible) { face in
                    MonacoAvatar(
                        photoURL: face.photoURL,
                        displayName: face.displayName,
                        size: size,
                        ring: ringColor,
                        ringWidth: 2
                    )
                }
                if overflow > 0 {
                    overflowDisc
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(label)
        }
    }

    private var overflowDisc: some View {
        Text("+\(overflow)")
            .font(.system(size: size * 0.38, weight: .bold))
            .foregroundStyle(MonacoTheme.fgMuted)
            .lineLimit(1)
            .minimumScaleFactor(0.5)
            .frame(width: size, height: size)
            .background(Circle().fill(MonacoTheme.fillQuiet))
            .overlay { Circle().strokeBorder(ringColor, lineWidth: 2) }
    }
}

// MARK: - The vote tally

/// One voter and how they voted. `state` is `ProposalVoteDot` — the same three-state enum the
/// tally has always used — so the faces and the dots cannot disagree about what a vote is.
struct MonacoVote: Identifiable, Equatable {
    let id: String
    let state: ProposalVoteDot
    var face: MonacoFace?

    init(id: String, state: ProposalVoteDot, face: MonacoFace? = nil) {
        self.id = id
        self.state = state
        self.face = face
    }
}

/// A single voter's face, **still carrying how they voted**.
///
/// This is the one thing all three v3 source directions got wrong. `VoteDot` encoded three states
/// by *shape as well as colour* — yes filled brand, no a 1.5pt loss outline, pending a grey
/// outline. Replacing it with a plain avatar says someone voted; it does not say how. On a screen
/// deciding whether real money gets spent, warmth is not worth that.
///
/// | state | render |
/// |---|---|
/// | yes | 22pt avatar + 2pt `brand` ring |
/// | no | 22pt avatar + 2pt `loss` ring |
/// | pending | hollow 22pt circle, 1.5pt `lineStrong`, dashed |
///
/// Colour is never the only carrier: yes and no are both filled circles with a ring, pending is
/// hollow and dashed, and the "2 of 5 voted · 3 yes to pass" caption remains the VoiceOver label
/// for the whole element.
struct MonacoVoteFace: View {
    let vote: MonacoVote
    var size: CGFloat = 22

    var body: some View {
        switch vote.state {
        case .yes, .no:
            if let face = vote.face {
                MonacoAvatar(
                    photoURL: face.photoURL,
                    displayName: face.displayName,
                    size: size,
                    ring: ringColor,
                    ringWidth: 2
                )
            } else {
                // A ballot with no voter identity attached. Still says how it was cast.
                Circle()
                    .fill(ringColor.opacity(0.18))
                    .overlay { Circle().strokeBorder(ringColor, lineWidth: 2) }
                    .frame(width: size, height: size)
            }
        case .pending:
            Circle()
                .strokeBorder(
                    MonacoTheme.lineStrong,
                    style: StrokeStyle(lineWidth: 1.5, dash: [3, 3])
                )
                .frame(width: size, height: size)
        }
    }

    private var ringColor: Color {
        switch vote.state {
        case .yes: return MonacoTheme.brand
        case .no: return MonacoTheme.loss
        case .pending: return MonacoTheme.lineStrong
        }
    }
}

/// The existing 8pt dot, lifted out of `ProposalCardView` so the faces have something to fall
/// back to that is not a second implementation of the encoding.
struct MonacoVoteDot: View {
    let state: ProposalVoteDot

    var body: some View {
        Circle()
            .fill(state == .yes ? MonacoTheme.brand : Color.clear)
            .overlay {
                Circle().strokeBorder(stroke, lineWidth: state == .no ? 1.5 : 1)
            }
            .frame(width: 8, height: 8)
            .scaleEffect(state == .pending ? 1 : 1.12)
    }

    private var stroke: Color {
        switch state {
        case .yes: return MonacoTheme.brand
        case .no: return MonacoTheme.loss
        case .pending: return MonacoTheme.fgSubtle.opacity(0.7)
        }
    }
}

/// When a tally shows faces and when it falls back to dots.
///
/// Pure, so the rule is testable without rendering: past `maximumFaces` voters a row of avatars
/// stops reading as a tally and starts reading as a crowd, and at an accessibility text size the
/// row cannot fit them at all. **The fallback ships with the faces, not after them** — the most
/// social component in the app must not break for the members who most need it to work.
enum MonacoVoteTallyLayout {
    /// Above this many voters, dots.
    static let maximumFaces = 8

    static func usesFaces(voterCount: Int, dynamicTypeSize: DynamicTypeSize) -> Bool {
        voterCount > 0 && voterCount <= maximumFaces && !dynamicTypeSize.isAccessibilitySize
    }
}

/// The tally row: faces when it can, the 8pt dots when it cannot.
///
/// The caption is the caller's — `ProposalVoteProgress.caption` carries the whole state to
/// VoiceOver and must not be dropped — so this renders the marks only and hides itself from
/// VoiceOver, leaving the caption as the label for the element as a whole.
struct MonacoVoteFaceRow: View {
    let votes: [MonacoVote]
    var faceSize: CGFloat = 22

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var usesFaces: Bool {
        MonacoVoteTallyLayout.usesFaces(voterCount: votes.count, dynamicTypeSize: dynamicTypeSize)
    }

    var body: some View {
        HStack(spacing: usesFaces ? -faceSize * 0.22 : 4) {
            ForEach(votes) { vote in
                if usesFaces {
                    MonacoVoteFace(vote: vote, size: faceSize)
                } else {
                    MonacoVoteDot(state: vote.state)
                }
            }
        }
        .animation(MonacoMotion.settle.reduced(reduceMotion), value: votes)
        .accessibilityHidden(true)
    }
}
