import Foundation
import MonacoCore
import Observation

/// What the join screen knows about what the member typed or pasted.
///
/// The text is parsed on every change (`InviteLink`): a short code, a link carrying one, the
/// share text around a link, or a legacy cabal id. A code is looked up on the public preview
/// once typing pauses, so the cabal's name, members and pot appear before the member commits.
/// A cabal id has no preview; it joins through the old route.
@MainActor
@Observable
final class InviteCodeEntryModel {
    enum Preview: Equatable {
        /// Nothing to look up: empty, half-typed, malformed, or a legacy id.
        case idle
        case loading(code: String)
        case loaded(InvitePreviewDTO)
        /// The code is malformed to the server, unknown, or replaced by a new one.
        case notFound(code: String)
        /// The lookup failed for another reason. Joining with the code may still work.
        case unavailable(code: String)
    }

    enum Tone: Equatable { case neutral, warning }

    struct Footer: Equatable {
        let text: String
        let tone: Tone
    }

    private(set) var text = ""
    private(set) var link: InviteLink?
    private(set) var preview: Preview = .idle

    @ObservationIgnored private let source: InviteSource
    @ObservationIgnored private let debounce: Duration
    @ObservationIgnored private var lookup: Task<Void, Never>?

    init(source: InviteSource, debounce: Duration = .milliseconds(350)) {
        self.source = source
        self.debounce = debounce
    }

    /// Call with every edit. `immediately` skips the typing pause, for a code that arrived
    /// whole (a link that opened the app).
    func update(text newText: String, immediately: Bool = false) {
        text = newText
        let parsed = InviteLink.parse(newText)
        link = parsed
        guard let code = parsed?.code else {
            lookup?.cancel()
            lookup = nil
            preview = .idle
            return
        }
        // "K7QM 4XPD" and "k7qm4xpd" are one code: a cosmetic edit keeps what is on screen.
        if previewCode == code, !isUnavailable { return }
        startLookup(code: code, immediately: immediately)
    }

    /// Looks the current code up again after a failure the member can retry.
    func retryPreview() {
        guard let code = link?.code else { return }
        startLookup(code: code, immediately: true)
    }

    private func startLookup(code: String, immediately: Bool) {
        lookup?.cancel()
        preview = .loading(code: code)
        let source = source
        let debounce = debounce
        lookup = Task { [weak self] in
            if !immediately {
                try? await Task.sleep(for: debounce)
            }
            guard !Task.isCancelled else { return }
            let outcome: Preview
            do {
                outcome = .loaded(try await source.preview(code: code))
            } catch {
                guard !Task.isCancelled, !error.isRequestCancellation else { return }
                outcome = InviteErrorStatus.of(error) == 404 ? .notFound(code: code) : .unavailable(code: code)
            }
            guard !Task.isCancelled, let self, self.link?.code == code else { return }
            self.preview = outcome
        }
    }

    // MARK: - Reading

    var loadedPreview: InvitePreviewDTO? {
        if case .loaded(let preview) = preview { return preview }
        return nil
    }

    var isLoadingPreview: Bool {
        if case .loading = preview { return true }
        return false
    }

    private var isUnavailable: Bool {
        if case .unavailable = preview { return true }
        return false
    }

    private var previewCode: String? {
        switch preview {
        case .idle: return nil
        case .loading(let code), .notFound(let code), .unavailable(let code): return code
        case .loaded(let preview): return InviteLink.normalizeCode(preview.code) ?? link?.code
        }
    }

    /// A code the server says is dead is not offered; anything else parseable is.
    var canJoin: Bool {
        guard link != nil else { return false }
        if case .notFound = preview { return false }
        return true
    }

    /// The line under the field. Nil once the cabal itself is on screen.
    var footer: Footer? {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty {
            return Footer(text: InviteEntryCopy.footer, tone: .neutral)
        }
        switch link {
        case nil:
            if InviteLink.isPartialCode(trimmed) {
                return Footer(text: InviteEntryCopy.typing, tone: .neutral)
            }
            return Footer(text: JoinCabalCopy.malformedCode, tone: .warning)
        case .groupId:
            return Footer(text: InviteEntryCopy.footer, tone: .neutral)
        case .code:
            switch preview {
            case .notFound: return Footer(text: InviteEntryCopy.notFound, tone: .warning)
            case .unavailable: return Footer(text: InviteEntryCopy.unavailable, tone: .neutral)
            case .loaded, .loading: return nil
            case .idle: return Footer(text: InviteEntryCopy.footer, tone: .neutral)
            }
        }
    }
}
