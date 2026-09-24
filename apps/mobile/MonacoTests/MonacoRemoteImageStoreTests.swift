import UIKit
import XCTest
@testable import Monaco

@MainActor
final class MonacoRemoteImageStoreTests: XCTestCase {
    override func setUp() {
        super.setUp()
        AvatarStubProtocol.reset()
    }

    func testDownsample_capsTheLongestEdge() throws {
        let data = try XCTUnwrap(Self.jpeg(width: 1600, height: 1200))
        let image = try XCTUnwrap(MonacoRemoteImageStore.downsampledImage(from: data, maxPixelSize: MonacoRemoteImageStore.avatarMaxPixelSize))
        XCTAssertEqual(max(image.size.width, image.size.height), CGFloat(MonacoRemoteImageStore.avatarMaxPixelSize))
        XCTAssertEqual(image.size.width / image.size.height, 1600.0 / 1200.0, accuracy: 0.01)
    }

    func testDownsample_malformedDataIsNil() {
        XCTAssertNil(MonacoRemoteImageStore.downsampledImage(from: Data("<html>not an image</html>".utf8), maxPixelSize: 320))
        XCTAssertNil(MonacoRemoteImageStore.downsampledImage(from: Data(), maxPixelSize: 320))
    }

    func testSecondRequest_isServedFromMemoryWithoutTheNetwork() async throws {
        let url = URL(string: "https://avatars.test/maya.jpg")!
        AvatarStubProtocol.respond(to: url, status: 200, body: try XCTUnwrap(Self.jpeg(width: 640, height: 640)))
        let store = MonacoRemoteImageStore(session: AvatarStubProtocol.session())

        XCTAssertNil(store.cachedImage(for: url))
        let first = await store.image(for: url)
        XCTAssertNotNil(first)
        XCTAssertNotNil(store.cachedImage(for: url))

        let second = await store.image(for: url)
        XCTAssertTrue(first === second)
        XCTAssertEqual(AvatarStubProtocol.requestCount(for: url), 1)
    }

    func testConcurrentRequests_shareOneDownload() async throws {
        let url = URL(string: "https://avatars.test/jordan.jpg")!
        AvatarStubProtocol.respond(to: url, status: 200, body: try XCTUnwrap(Self.jpeg(width: 640, height: 640)))
        let store = MonacoRemoteImageStore(session: AvatarStubProtocol.session())

        async let first = store.image(for: url)
        async let second = store.image(for: url)
        let images = await [first, second]
        XCTAssertNotNil(images[0])
        XCTAssertNotNil(images[1])
        XCTAssertEqual(AvatarStubProtocol.requestCount(for: url), 1)
    }

    func testNon200_isNilAndNotCached() async {
        let url = URL(string: "https://avatars.test/missing.jpg")!
        AvatarStubProtocol.respond(to: url, status: 404, body: Data("not found".utf8))
        let store = MonacoRemoteImageStore(session: AvatarStubProtocol.session())

        let image = await store.image(for: url)
        XCTAssertNil(image)
        XCTAssertNil(store.cachedImage(for: url))
    }

    func testNetworkFailure_isNil() async {
        let url = URL(string: "https://avatars.test/offline.jpg")!
        AvatarStubProtocol.fail(url, with: URLError(.notConnectedToInternet))
        let store = MonacoRemoteImageStore(session: AvatarStubProtocol.session())

        let image = await store.image(for: url)
        XCTAssertNil(image)
    }

    func testMalformedBody_isNil() async {
        let url = URL(string: "https://avatars.test/html.jpg")!
        AvatarStubProtocol.respond(to: url, status: 200, body: Data("<html></html>".utf8))
        let store = MonacoRemoteImageStore(session: AvatarStubProtocol.session())

        let image = await store.image(for: url)
        XCTAssertNil(image)
    }

    private static func jpeg(width: CGFloat, height: CGFloat) -> Data? {
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        let renderer = UIGraphicsImageRenderer(size: CGSize(width: width, height: height), format: format)
        return renderer.jpegData(withCompressionQuality: 0.8) { context in
            UIColor.systemBlue.setFill()
            context.fill(CGRect(x: 0, y: 0, width: width, height: height))
        }
    }
}

/// Canned responses per URL, with a request counter.
private final class AvatarStubProtocol: URLProtocol, @unchecked Sendable {
    private enum Stub {
        case response(status: Int, body: Data)
        case failure(URLError)
    }

    nonisolated(unsafe) private static var stubs: [URL: Stub] = [:]
    nonisolated(unsafe) private static var counts: [URL: Int] = [:]
    private static let lock = NSLock()

    nonisolated static func session() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [AvatarStubProtocol.self]
        return URLSession(configuration: configuration)
    }

    nonisolated static func reset() {
        lock.lock()
        stubs = [:]
        counts = [:]
        lock.unlock()
    }

    nonisolated static func respond(to url: URL, status: Int, body: Data) {
        lock.lock()
        stubs[url] = .response(status: status, body: body)
        lock.unlock()
    }

    nonisolated static func fail(_ url: URL, with error: URLError) {
        lock.lock()
        stubs[url] = .failure(error)
        lock.unlock()
    }

    nonisolated static func requestCount(for url: URL) -> Int {
        lock.lock()
        defer { lock.unlock() }
        return counts[url] ?? 0
    }

    nonisolated override class func canInit(with request: URLRequest) -> Bool { true }
    nonisolated override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    nonisolated override func startLoading() {
        guard let url = request.url else { return }
        Self.lock.lock()
        Self.counts[url, default: 0] += 1
        let stub = Self.stubs[url]
        Self.lock.unlock()

        switch stub {
        case .response(let status, let body):
            let response = HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: nil)!
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: body)
            client?.urlProtocolDidFinishLoading(self)
        case .failure(let error):
            client?.urlProtocol(self, didFailWithError: error)
        case nil:
            client?.urlProtocol(self, didFailWithError: URLError(.unsupportedURL))
        }
    }

    nonisolated override func stopLoading() {}
}
