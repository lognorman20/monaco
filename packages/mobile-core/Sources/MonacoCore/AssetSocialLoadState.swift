import Foundation

/// How far the "what are my cabals doing with this stock" read has got.
///
/// Three states, not two. A failed read and an answer that came back empty look the
/// same to a boolean, and on this screen they are opposite claims about someone's
/// money: one says "we could not check", the other says "no cabal of yours holds
/// this". The backend already refuses to conflate them — a cabal it could not value
/// is counted into `unvaluedGroups` rather than dropped — and the app has to hold the
/// same line for the call as a whole.
public enum AssetSocialLoadState: Equatable, Sendable {
    /// Nothing has come back yet. Nothing may be asserted about the holdings.
    case loading
    /// An answer landed. Its holdings are the truth, empty or not.
    case answered
    /// The read failed and there is nothing to fall back on. Still not a fact about
    /// the holdings.
    case failed

    /// True only when the holdings on hand are a real answer to the question.
    public var isAnswered: Bool { self == .answered }
}

/// What the screen says when the social read did not come back.
///
/// Here rather than in the view so the wording is tested, and so the card and the
/// trade bar cannot drift into saying different things about the same failure.
public enum AssetSocialFailureCopy {
    /// The failed card keeps the slot's own title: the member is looking for their
    /// cabals' position, and that is still what this card is about.
    public static let cardTitle = "Your cabals' position"

    /// One sentence covering the whole read — the holdings, the votes and the
    /// activity all come from the same call, so one retry brings all three back.
    public static func message(symbol: String) -> String {
        "Couldn't load what your cabals hold, or what they've done with \(AssetSymbolFormatter.format(symbol))."
    }

    public static let retryTitle = "Retry"

    /// VoiceOver reads the card as one sentence, the way the other cards on this
    /// screen do.
    public static func spoken(symbol: String) -> String {
        "\(cardTitle). \(message(symbol: symbol))"
    }

    /// The line the trade bar puts where Sell would be. Without it, a missing Sell
    /// button is read as "no cabal of yours holds this", which is the lie.
    public static let sellUnknown = "Couldn't check what your cabals hold"
}
