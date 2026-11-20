import XCTest
@testable import Tessera

@MainActor
private final class HitCounter {
    var hits = 0
}

@MainActor
final class NotificationObserversTests: XCTestCase {
    private let ping = Notification.Name("tessera.tests.ping")

    func testStopRemovesEveryObserver() {
        let first = NotificationCenter()
        let second = NotificationCenter()
        let observers = NotificationObservers()
        let counter = HitCounter()
        for center in [first, second] {
            observers.add(center, center.addObserver(forName: ping, object: nil, queue: .main) { _ in
                MainActor.assumeIsolated { counter.hits += 1 }
            })
        }
        first.post(name: ping, object: nil)
        second.post(name: ping, object: nil)
        XCTAssertEqual(counter.hits, 2)
        observers.stop()
        first.post(name: ping, object: nil)
        second.post(name: ping, object: nil)
        XCTAssertEqual(counter.hits, 2)
    }

    func testStopTwiceIsHarmless() {
        let center = NotificationCenter()
        let observers = NotificationObservers()
        let counter = HitCounter()
        observers.add(center, center.addObserver(forName: ping, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated { counter.hits += 1 }
        })
        observers.stop()
        observers.stop()
        center.post(name: ping, object: nil)
        XCTAssertEqual(counter.hits, 0)
    }
}
