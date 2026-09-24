import Foundation

/// A `URLProtocol` that accepts a request and then says nothing for `stall` seconds before
/// answering 200. `MockURLProtocol` answers immediately, so it can only record the timeout
/// a request asked for — it can never show which deadline URLSession actually enforces.
/// This one lets the session's own timer run, which is the only way to prove that a money
/// write really gets its longer budget and a read really gets the short one.
final class StallingURLProtocol: URLProtocol {
    /// How long every request hangs before it is answered.
    static var stall: TimeInterval = 0

    private var work: DispatchWorkItem?

    override class func canInit(with request: URLRequest) -> Bool { true }

    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let work = DispatchWorkItem { [weak self] in
            guard let self, let url = self.request.url else { return }
            let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: nil)!
            self.client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            self.client?.urlProtocol(self, didLoad: Data("{}".utf8))
            self.client?.urlProtocolDidFinishLoading(self)
        }
        self.work = work
        DispatchQueue.global().asyncAfter(deadline: .now() + Self.stall, execute: work)
    }

    override func stopLoading() {
        work?.cancel()
        work = nil
    }
}
