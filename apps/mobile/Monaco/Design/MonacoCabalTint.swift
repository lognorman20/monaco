import SwiftUI

extension MonacoTheme {
    /// A cabal's identity colour. Picked from the group id, never from the name, so a rename keeps
    /// the colour.
    ///
    /// Seven hue-spaced tints, ordered so consecutive entries are maximally hue-distant —
    /// the minimum adjacent gap is 49°. The order **is** `CabalTintAssignment`'s clockwise walk,
    /// so changing it changes collision behaviour, not just the list.
    ///
    /// No purple (the 250–335° band is empty by rule), no brand blue (210–250°), no profit green
    /// (140–175°; sage sits at the 175° edge and is blue-green, profit is at 154°).
    ///
    /// **Where a tint may appear, and nowhere else:** the cabal's mark, strip-card top band and
    /// accent rail; the cabal hero band's `soft` overlay on ink; that cabal's line on the
    /// multi-series chart; your own chat bubbles inside that cabal; the rank badge for ranks 1–3
    /// (via `cta`); an `EmptyState` mark disc for an empty cabal surface (via `soft`).
    ///
    /// **A tint is never on a button, a segmented thumb, a link or any control** — blue means tap.
    /// **A tint is never the only identity signal** — a cabal is always tint *plus*
    /// initials-or-picture, and every chart line carries an inline name label at its terminus.
    enum CabalTint: CaseIterable {
        case sage, peach, sky, butter, clay, olive, brick

        /// Mark tile, strip-card band, accent rail, chart key.
        ///
        /// White on `fill` clears the 3:1 large-text minimum, not the 4.5:1 body minimum — in dark
        /// the lighter fills sit at 3.5:1. That is the right bar for what draws there (bold tile
        /// initials at 15pt and up), and it is exactly why `cta` exists for anything smaller.
        var fill: Color {
            switch self {
            case .sage: return Color.adaptive(light: 0x0D7D74, dark: 0x10938A)
            case .peach: return Color.adaptive(light: 0xC2570C, dark: 0xD9681A)
            case .sky: return Color.adaptive(light: 0x17627D, dark: 0x1E7A99)
            case .butter: return Color.adaptive(light: 0xA16207, dark: 0xBC7A10)
            case .clay: return Color.adaptive(light: 0xBE3455, dark: 0xD44467)
            case .olive: return Color.adaptive(light: 0x5F6B12, dark: 0x74830F)
            case .brick: return Color.adaptive(light: 0xA33A20, dark: 0xBC4527)
            }
        }

        /// The deeper pair, for a tinted area carrying **small** white text.
        ///
        /// Mandatory, and it exists for one reason: `fill` clears only the large-text bar. The two
        /// places a tint carries small white text are the rank badge on the Cabals leaderboard (a
        /// 13pt bold numeral, which is not "large text" under WCAG) and your own chat bubbles.
        /// Both take `cta`. In dark, `cta` is the light-mode `fill`, which is already deep enough;
        /// every pair clears 4.5:1 against white in both schemes, the closest being peach in dark
        /// at exactly 4.50. `CabalTintRampTests` pins that from below.
        var cta: Color {
            switch self {
            case .sage: return Color.adaptive(light: 0x0A6B63, dark: 0x0D7D74)
            case .peach: return Color.adaptive(light: 0xA84B0A, dark: 0xC2570C)
            case .sky: return Color.adaptive(light: 0x13536B, dark: 0x17627D)
            case .butter: return Color.adaptive(light: 0x8A5406, dark: 0xA16207)
            case .clay: return Color.adaptive(light: 0xA32C49, dark: 0xBE3455)
            case .olive: return Color.adaptive(light: 0x515C0F, dark: 0x5F6B12)
            case .brick: return Color.adaptive(light: 0x8C3119, dark: 0xA33A20)
            }
        }

        /// Initials and glyphs drawn on `fill` or `cta`.
        var onFill: Color { .white }

        /// Low-alpha wash for a tinted surface that still carries `fgPrimary` text.
        /// 15.5–16.0:1 in light, 13.2–14.2:1 in dark.
        var soft: Color {
            Color.adaptive(
                light: lightFillHex,
                lightAlpha: 0.12,
                dark: darkFillHex,
                darkAlpha: 0.16
            )
        }

        /// Brighter than `fill` so the tint still reads as a mark or a rule on an ink surface.
        /// 6.9–10.2:1 on `Ink.base`.
        var onInk: Color {
            switch self {
            case .sage: return Color(hex: 0x2CC3B4)
            case .peach: return Color(hex: 0xFF9248)
            case .sky: return Color(hex: 0x46B3DB)
            case .butter: return Color(hex: 0xEBB13C)
            case .clay: return Color(hex: 0xFF6C8B)
            case .olive: return Color(hex: 0xB9C93A)
            case .brick: return Color(hex: 0xFF8C6B)
            }
        }

        /// This cabal's line on a chart: the light `fill` on paper, `onInk` in dark.
        /// Chart lines only — a chart line needs to read against its own plot area, not a card.
        var stroke: Color {
            Color.adaptive(light: lightFillHex, dark: onInkHex)
        }

        /// The name a member would say. Used in VoiceOver strings where the tint is the only thing
        /// distinguishing two rows and for debug surfaces; never shown as product copy.
        var name: String {
            switch self {
            case .sage: return "sage"
            case .peach: return "peach"
            case .sky: return "sky"
            case .butter: return "butter"
            case .clay: return "clay"
            case .olive: return "olive"
            case .brick: return "brick"
            }
        }

        // MARK: Hash assignment

        /// The tint a cabal hashes to before collision resolution.
        ///
        /// Stable across launches: FNV-1a 64 over the UTF-8 bytes of the trimmed, lowercased id,
        /// mod the case count. Lowercased because Swift's `UUID.uuidString` is uppercase while the
        /// API sends lowercase. Never `String.hashValue`, which is randomised per process.
        ///
        /// Prefer `CabalTintAssignment.resolve` where the viewer's own cabal list is in hand: this
        /// function alone gives a member of four cabals a real chance of two sharing a colour.
        static func forGroupId(_ groupId: String) -> CabalTint {
            let all = CabalTint.allCases
            return all[Int(fnv1a64(normalisedId(groupId)) % UInt64(all.count))]
        }

        /// Background fill for a cabal: `CabalTint.forGroupId(groupId).fill`.
        static func fill(forGroupId groupId: String) -> Color {
            forGroupId(groupId).fill
        }

        /// Low-alpha wash for a cabal: `CabalTint.forGroupId(groupId).soft`.
        static func soft(forGroupId groupId: String) -> Color {
            forGroupId(groupId).soft
        }

        /// Chart line colour for a cabal: `CabalTint.forGroupId(groupId).stroke`.
        static func stroke(forGroupId groupId: String) -> Color {
            forGroupId(groupId).stroke
        }

        /// The one spelling of a group id the tint system compares.
        static func normalisedId(_ groupId: String) -> String {
            groupId.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        }

        static func fnv1a64(_ string: String) -> UInt64 {
            var hash: UInt64 = 0xCBF2_9CE4_8422_2325
            for byte in string.utf8 {
                hash ^= UInt64(byte)
                hash = hash &* 0x0000_0100_0000_01B3
            }
            return hash
        }

        // MARK: Internals

        private var lightFillHex: UInt32 {
            switch self {
            case .sage: return 0x0D7D74
            case .peach: return 0xC2570C
            case .sky: return 0x17627D
            case .butter: return 0xA16207
            case .clay: return 0xBE3455
            case .olive: return 0x5F6B12
            case .brick: return 0xA33A20
            }
        }

        private var darkFillHex: UInt32 {
            switch self {
            case .sage: return 0x10938A
            case .peach: return 0xD9681A
            case .sky: return 0x1E7A99
            case .butter: return 0xBC7A10
            case .clay: return 0xD44467
            case .olive: return 0x74830F
            case .brick: return 0xBC4527
            }
        }

        private var onInkHex: UInt32 {
            switch self {
            case .sage: return 0x2CC3B4
            case .peach: return 0xFF9248
            case .sky: return 0x46B3DB
            case .butter: return 0xEBB13C
            case .clay: return 0xFF6C8B
            case .olive: return 0xB9C93A
            case .brick: return 0xFF8C6B
            }
        }
    }
}

/// Assigns tints across the viewer's own cabals so no two of them collide.
///
/// Hashing alone is not enough. With five buckets a member of four cabals had roughly a 70%
/// chance that two shared a colour, and on the strip and the multi-line chart the tint was the
/// only identity signal there was. Growing to seven buckets narrows that; it does not close it.
/// This closes it: walk the viewer's cabals in a deterministic order, keep each cabal's hashed
/// tint when it is still free, and take the next free tint clockwise when it is not.
///
/// The result is stable across launches (the order is the ids, not the API's response order) and
/// guarantees no two of *your* cabals share a tint until you are in eight of them.
enum CabalTintAssignment {
    /// Tints for one viewer's cabals, keyed by group id.
    ///
    /// The map is keyed by the normalised id **and** by each caller's original spelling when the
    /// two differ, so `map[group.id]` works whatever case the DTO arrived in. Prefer
    /// `tint(forGroupId:in:)`, which normalises and falls back for a cabal outside the set.
    ///
    /// - Parameter orderedGroupIds: the viewer's cabals in any order. The walk sorts them itself.
    static func resolve(orderedGroupIds: [String]) -> [String: MonacoTheme.CabalTint] {
        let all = MonacoTheme.CabalTint.allCases
        var seen = Set<String>()
        // Ascending normalised id, not the API's order: a cabal must not change colour because a
        // list came back sorted by activity today and by name tomorrow.
        let ordered = orderedGroupIds
            .map { (original: $0, key: MonacoTheme.CabalTint.normalisedId($0)) }
            .sorted { $0.key < $1.key }
            .filter { seen.insert($0.key).inserted }

        var taken = Set<MonacoTheme.CabalTint>()
        var assignment: [String: MonacoTheme.CabalTint] = [:]

        for entry in ordered {
            let preferred = MonacoTheme.CabalTint.forGroupId(entry.key)
            var chosen = preferred
            // Past seven cabals every tint is taken and a collision is arithmetic, not a bug.
            // Fall back to the hash so the eighth cabal at least keeps a stable colour.
            if taken.count < all.count, var index = all.firstIndex(of: preferred) {
                while taken.contains(all[index]) {
                    index = (index + 1) % all.count
                }
                chosen = all[index]
            }
            taken.insert(chosen)
            assignment[entry.key] = chosen
            if entry.original != entry.key {
                assignment[entry.original] = chosen
            }
        }
        return assignment
    }

    /// The resolved tint for a cabal, falling back to the plain hash for one outside the set —
    /// a cabal the viewer is not in (a public cabal being previewed, a leaderboard row) still
    /// needs a colour, and it needs the same one everywhere.
    static func tint(
        forGroupId groupId: String,
        in assignment: [String: MonacoTheme.CabalTint]
    ) -> MonacoTheme.CabalTint {
        if let resolved = assignment[groupId] { return resolved }
        if let resolved = assignment[MonacoTheme.CabalTint.normalisedId(groupId)] { return resolved }
        return MonacoTheme.CabalTint.forGroupId(groupId)
    }
}

private struct CabalTintKey: EnvironmentKey {
    static let defaultValue: MonacoTheme.CabalTint? = nil
}

extension EnvironmentValues {
    /// The tint of the cabal whose surface this is, set at the root of a cabal-owned screen.
    ///
    /// `nil` everywhere else, and that is the correct answer rather than a missing one: a surface
    /// that is not about one specific cabal has no tint, and a component that falls back to
    /// `brand` when this is `nil` is the reason blue still means tap.
    var cabalTint: MonacoTheme.CabalTint? {
        get { self[CabalTintKey.self] }
        set { self[CabalTintKey.self] = newValue }
    }
}

extension View {
    /// Declares the cabal whose surface this is. Scoped hard — see `CabalTint`'s list of the six
    /// places a tint may appear.
    func cabalTint(_ tint: MonacoTheme.CabalTint?) -> some View {
        environment(\.cabalTint, tint)
    }
}
