import Foundation

/// Every word the matchup surfaces say, in one place so the host tests can hold it to the
/// product's vocabulary.
public enum MatchupCopy {
    public static let homeSectionTitle = "This week"
    public static let cabalSectionTitle = "Matchup"
    public static let screenTitle = "Matchup"
    public static let tableTitle = "Season table"
    public static let howItWorks = "Every Monday your cabal is paired with another. Highest return for the week wins."
    public static let byeTitle = "Bye week"
    public static let byeDetail = "No opponent this week. Back in the draw on Monday."
    public static let notInDrawTitle = "Not in this week's draw"
    public static let notInDrawDetail = "Cabals with two or more members and more than $1 in the pot are paired every Monday."
    public static let drawPendingTitle = "The draw is on its way"
    public static let drawPendingDetail = "This week's pairs land in a few minutes."
    public static let homeEmptyTitle = "No matchups this week"
    public static let recentTitle = "Recent results"
    public static let noResultsYet = "Results show up here after your first week."
    public static let challengeTitle = "Challenge a cabal"
    public static let challengeHint = "Pick a cabal to play next week. If one of their members accepts, you skip the draw."
    public static let challengeAction = "Challenge"
    public static let challengeSentLabel = "Sent"
    public static let acceptAction = "Accept"
    public static let challengesTitle = "Challenges"
    public static let tableEmptyTitle = "No results yet"
    public static let tableEmptyDetail = "The table fills in after the first week ends."
    public static let loadFailedTitle = "Couldn't load the matchup"
    public static let tableLoadFailedTitle = "Couldn't load the table"
    public static let tryAgain = "Try again"
    public static let searchPrompt = "Search cabals"
    public static let vs = "vs"

    /// The fixed strings, for the copy audit.
    public static let allFixedStrings: [String] = [
        homeSectionTitle, cabalSectionTitle, screenTitle, tableTitle, howItWorks, byeTitle, byeDetail,
        notInDrawTitle, notInDrawDetail, drawPendingTitle, drawPendingDetail, homeEmptyTitle,
        recentTitle, noResultsYet, challengeTitle, challengeHint, challengeAction, challengeSentLabel,
        acceptAction, challengesTitle, tableEmptyTitle, tableEmptyDetail, loadFailedTitle,
        tableLoadFailedTitle, tryAgain, searchPrompt,
    ]

    // MARK: Time

    /// "5 days left", "Last day", and "Week over" once it has ended and waits to be scored.
    public static func daysLeft(_ days: Int) -> String {
        switch days {
        case ..<1: return "Week over"
        case 1: return "Last day"
        default: return "\(days) days left"
        }
    }

    private static let weekFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        // A week starts at Monday 00:00 UTC; read in UTC it is that Monday everywhere.
        formatter.timeZone = TimeZone(identifier: "UTC")
        formatter.dateFormat = "MMM d"
        return formatter
    }()

    /// The Monday a week started: "Sep 21".
    public static func weekLabel(_ weekStart: Date) -> String {
        weekFormatter.string(from: weekStart)
    }

    // MARK: Scores

    /// A week's return as "+1.2%", "−0.8%", "0.0%", or "—" when there is none.
    public static func score(_ ratio: String?) -> String {
        formatRatio(ratio, decimals: 1)
    }

    /// Both scores of a pair. When they read the same at one decimal but are not equal, both get
    /// a second decimal, so a winner never shows the same number as the cabal it beat.
    public static func scores(_ a: String?, _ b: String?) -> (a: String, b: String) {
        let short = (formatRatio(a, decimals: 1), formatRatio(b, decimals: 1))
        guard short.0 == short.1, short.0 != "—",
              let x = ratioValue(a), let y = ratioValue(b), x != y
        else { return short }
        return (formatRatio(a, decimals: 2), formatRatio(b, decimals: 2))
    }

    static func ratioValue(_ raw: String?) -> Double? {
        guard let raw else { return nil }
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
            .replacingOccurrences(of: typographicMinus, with: "-")
        guard let value = Double(trimmed), value.isFinite else { return nil }
        return value
    }

    static func formatRatio(_ raw: String?, decimals: Int) -> String {
        guard let value = ratioValue(raw) else { return "—" }
        let percent = value * 100
        let magnitude = String(format: "%.\(decimals)f", abs(percent))
        if Double(magnitude) == 0 { return magnitude + "%" }
        return (percent < 0 ? typographicMinus : "+") + magnitude + "%"
    }

    /// Where the lead bar's split sits, 0...1, as side a's share. Even is 0.5; a lead of two
    /// points or more pushes it to 0.9, so the trailing side never disappears from the bar.
    public static func leadFraction(a: String?, b: String?) -> Double {
        guard let x = ratioValue(a), let y = ratioValue(b) else { return 0.5 }
        let lead = max(-1, min(1, (x - y) / 0.02))
        return 0.5 + lead * 0.4
    }

    // MARK: Record and results

    /// "4–1", or "4–1–1" once there has been a tie.
    public static func record(_ record: MatchupRecordDTO) -> String {
        var parts = [record.wins, record.losses]
        if record.ties > 0 { parts.append(record.ties) }
        return parts.map(String.init).joined(separator: "\u{2013}")
    }

    /// "4 wins, 1 loss" for VoiceOver.
    public static func recordSpoken(_ record: MatchupRecordDTO) -> String {
        func count(_ n: Int, _ one: String, _ many: String) -> String { "\(n) \(n == 1 ? one : many)" }
        var parts = [count(record.wins, "win", "wins"), count(record.losses, "loss", "losses")]
        if record.ties > 0 { parts.append(count(record.ties, "tie", "ties")) }
        return parts.joined(separator: ", ")
    }

    /// "W", "L", "T", or "Bye".
    public static func outcomeLetter(_ outcome: MatchupOutcome?) -> String {
        switch outcome {
        case .win: return "W"
        case .loss: return "L"
        case .tie: return "T"
        case .bye: return "Bye"
        case nil: return "—"
        }
    }

    /// "W · vs Semis or bust · +1.2% to +0.8% · Sep 15"; a bye reads "Bye · Sep 8".
    public static func resultLine(_ result: MatchupResultDTO) -> String {
        let week = weekLabel(result.weekStart)
        guard let opponent = result.opponent, result.result != .bye else {
            return "\(outcomeLetter(.bye)) · \(week)"
        }
        let pair = scores(result.score, result.opponentScore)
        return "\(outcomeLetter(result.result)) · \(vs) \(opponent.name) · \(pair.a) to \(pair.b) · \(week)"
    }

    // MARK: Challenges

    public static func challengeSent(to name: String) -> String { "Challenge sent to \(name)" }

    public static func challengeAccepted(opponent name: String) -> String { "You play \(name) next week" }

    /// "Semis or bust wants to play you next week."
    public static func incomingChallenge(from name: String) -> String { "\(name) wants to play you next week" }

    /// "Waiting on Semis or bust."
    public static func outgoingChallenge(to name: String) -> String { "Waiting on \(name)" }

    /// "Next week: Semis or bust."
    public static func nextOpponent(_ name: String) -> String { "Next week: \(name)" }

    public static func refusal(_ refusal: MatchupChallengeRefusal?) -> String {
        switch refusal {
        case .alreadyMatched: return "One of you already has next week's opponent"
        case .incomingChallenge: return "They challenged you first. Accept it on your matchup"
        case .tooManyChallenges: return "Five challenges are waiting on an answer this week"
        case .challengeClosed: return "That challenge closed when the week was drawn"
        case nil: return "Couldn't send the challenge. Try again"
        }
    }

    public static let acceptFailed = "Couldn't accept the challenge. Try again"

    // MARK: VoiceOver

    /// "Weekend investors, up 1.2%, against Semis or bust, up 0.8%. 3 days left."
    public static func spoken(_ matchup: MatchupDTO) -> String {
        guard let b = matchup.b else {
            return "\(matchup.a.name), \(byeTitle)"
        }
        let pair = scores(matchup.a.score, b.score)
        return "\(matchup.a.name), \(pair.a), against \(b.name), \(pair.b). \(daysLeft(matchup.daysLeft))."
    }
}
