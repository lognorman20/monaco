import MonacoCore
import Observation
import SwiftUI

/// Reads headlines. The live source calls the API; tests and the sample harnesses swap
/// in canned feeds.
@MainActor
protocol NewsDataSource {
    func assetNews(symbol: String) async throws -> NewsFeedDTO
    func marketNews() async throws -> NewsFeedDTO
}

@MainActor
struct LiveNewsDataSource: NewsDataSource {
    let auth: PrivyAuthService

    func assetNews(symbol: String) async throws -> NewsFeedDTO {
        try await auth.withAccessToken { token in
            try await client(token).getAssetNews(symbol: symbol)
        }
    }

    func marketNews() async throws -> NewsFeedDTO {
        try await auth.withAccessToken { token in
            try await client(token).getMarketNews()
        }
    }

    private func client(_ token: String) -> MonacoCore.MonacoAPIClient {
        MonacoCore.MonacoAPIClient(baseURL: Config.apiBaseURL, accessTokenProvider: { token })
    }
}

/// One list of headlines — a stock's, or the market's — and how far reading it has got.
///
/// News is an addition to screens that are already useful without it, so it never holds
/// them up and never shouts. A first read shows the rows' shape; a read that fails with
/// nothing on screen says so in one line with a retry; a re-read that fails leaves the
/// rows the member is reading exactly where they are.
@Observable
@MainActor
final class NewsFeedModel {
    enum Phase: Equatable {
        /// Nothing has answered yet.
        case loading
        /// The server answered, with headlines or with none.
        case answered
        /// The read failed and there is nothing on screen it could have replaced.
        case failed
    }

    private(set) var phase: Phase = .loading
    /// The rows, built once per answer: the ages in them are worked out then, and the
    /// five-minute re-read brings them forward.
    private(set) var headlines: [NewsHeadline] = []

    private let fetch: @MainActor () async throws -> NewsFeedDTO
    private let now: () -> Date
    private var hasAnswer = false
    /// Issue order of reads, so only the newest may write — the same guard the social
    /// model keeps, for the same reason: the on-appear read and a retry overlap.
    private var requestSequence = 0

    init(fetch: @escaping @MainActor () async throws -> NewsFeedDTO, now: @escaping () -> Date = Date.init) {
        self.fetch = fetch
        self.now = now
    }

    var isEmpty: Bool { phase == .answered && headlines.isEmpty }

    /// The first read, and the retry a member taps: with nothing on screen yet, the rows'
    /// shape shows while it runs.
    func load() async {
        if !hasAnswer { phase = .loading }
        await read()
    }

    /// A read nobody asked for. Never puts a skeleton or an error over rows on screen.
    func refresh() async {
        await read()
    }

    private func read() async {
        requestSequence += 1
        let sequence = requestSequence
        do {
            let feed = try await fetch()
            guard requestSequence == sequence else { return }
            headlines = NewsHeadline.lines(feed.items, now: now())
            hasAnswer = true
            phase = .answered
        } catch {
            guard requestSequence == sequence else { return }
            if error.isRequestCancellation { return }
            // Rows already on screen are still the best answer we have.
            if !hasAnswer { phase = .failed }
        }
    }
}
