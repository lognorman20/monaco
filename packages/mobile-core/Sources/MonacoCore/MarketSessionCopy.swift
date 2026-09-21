import Foundation

/// What the chip under the hero price says about the exchange.
///
/// The point of this chip is the product's own argument: the underlying stock stops
/// trading at the bell and its B20 token does not. The token is an ERC-20 on Base
/// and its pools have no opening hours, so every closed state names the close *and*
/// says the token still trades there, and the open state says nothing about Base at
/// all — there is nothing to explain while the market is open.
///
/// "Still trades" is only said when the detail's own Kyber probe found a route. A
/// pool with no route is not trading, and the chip does not claim otherwise; it
/// never says "24/7" either, because nothing checks a pool around the clock.
///
/// Copy lives here rather than in the view so it can be read back in a host test,
/// and so that "what does the app say at 4:01pm on a half day" has one answer.
public struct MarketSessionChipCopy: Equatable, Sendable {
    /// "Market open", "After hours", "Closed for Thanksgiving Day".
    public let title: String
    /// The second half, when there is one: when the next session starts, or that the
    /// token keeps trading.
    public let detail: String?
    /// True only during the regular cash session — the one state where the equity
    /// behind the curve is still printing. The chart's end-of-line pulse reads this.
    public let isLive: Bool

    public init(title: String, detail: String?, isLive: Bool) {
        self.title = title
        self.detail = detail
        self.isLive = isLive
    }

    /// One line for VoiceOver, so the chip is not read as two unrelated fragments.
    public var spoken: String {
        guard let detail else { return title }
        return "\(title), \(detail)"
    }
}

public enum MarketSessionCopy {
    /// Nil when the backend did not say what session it priced in — an older backend,
    /// or a session name this build does not know. A chip that guesses is worse than
    /// no chip on the screen where someone decides to trade.
    ///
    /// `tokenRoutable` is the detail's Kyber buy probe (`liquidity.routable`). It
    /// defaults to false so a caller that does not know stays quiet about the pools.
    public static func chip(
        for market: MarketStatusDTO?,
        tokenRoutable: Bool = false,
        locale: Locale = .autoupdatingCurrent,
        timeZone: TimeZone = .autoupdatingCurrent
    ) -> MarketSessionChipCopy? {
        guard let market, market.session != .unknown else { return nil }
        let tokenLine = tokenRoutable ? tokenTradesOnBase : nil
        switch market.session {
        case .open:
            return MarketSessionChipCopy(
                title: "Market open",
                detail: market.earlyClose ? "Closes early today" : closeTimeDetail(market, locale: locale, timeZone: timeZone),
                isLive: true
            )
        case .preMarket:
            return MarketSessionChipCopy(
                title: "Pre-market",
                detail: openTimeDetail(market, locale: locale, timeZone: timeZone) ?? tokenLine,
                isLive: false
            )
        case .afterHours:
            return MarketSessionChipCopy(title: "After hours", detail: tokenLine, isLive: false)
        case .closed:
            if let holiday = market.holiday, !holiday.isEmpty {
                return MarketSessionChipCopy(title: "Closed for \(holiday)", detail: tokenLine, isLive: false)
            }
            return MarketSessionChipCopy(title: "Market closed", detail: tokenLine, isLive: false)
        case .unknown:
            return nil
        }
    }

    /// The second line whenever the exchange is shut and the token's pools routed.
    public static let tokenTradesOnBase = "Token still trades on Base"

    /// "Closes 4:00 PM" — only when the server said when, and only while that is still
    /// ahead of the `asOf` it priced at. A transition already in the past is a stale
    /// payload, and "closes 4:00 PM" at 5pm is worse than no second line.
    private static func closeTimeDetail(_ market: MarketStatusDTO, locale: Locale, timeZone: TimeZone) -> String? {
        guard let transition = upcomingTransition(market) else { return nil }
        return "Closes \(time(transition, locale: locale, timeZone: timeZone))"
    }

    private static func openTimeDetail(_ market: MarketStatusDTO, locale: Locale, timeZone: TimeZone) -> String? {
        guard market.nextSession == .open, let transition = upcomingTransition(market) else { return nil }
        return "Opens \(time(transition, locale: locale, timeZone: timeZone))"
    }

    private static func upcomingTransition(_ market: MarketStatusDTO) -> Date? {
        guard let transition = market.nextTransition else { return nil }
        guard let asOf = market.asOf else { return transition }
        return transition > asOf ? transition : nil
    }

    /// The exchange's clock rendered in the reader's own, from a UTC instant. `j` is
    /// the locale's hour field, so a 24-hour locale gets "16:00" and not "4:00 PM".
    private static func time(_ date: Date, locale: Locale, timeZone: TimeZone) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.locale = locale
        calendar.timeZone = timeZone
        return SharedFormatters.string(
            from: date,
            pattern: .template("jmm"),
            locale: locale,
            calendar: calendar,
            timeZone: timeZone
        )
    }
}
