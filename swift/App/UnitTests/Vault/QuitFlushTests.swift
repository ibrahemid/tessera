import Foundation
import os
import XCTest
import TesseraCore
@testable import Tessera

final class ManualClock: Clock, Sendable {
    struct Instant: InstantProtocol {
        var offset: Duration

        func advanced(by duration: Duration) -> Instant { Instant(offset: offset + duration) }
        func duration(to other: Instant) -> Duration { other.offset - offset }
        static func < (lhs: Instant, rhs: Instant) -> Bool { lhs.offset < rhs.offset }
    }

    private struct Sleeper: Sendable {
        let deadline: Instant
        let continuation: CheckedContinuation<Void, any Error>
    }

    private struct State: Sendable {
        var now = Instant(offset: .zero)
        var sleepers: [UUID: Sleeper] = [:]
        var cancelled: Set<UUID> = []
        var deadlines: [Duration] = []
    }

    private let state = OSAllocatedUnfairLock(initialState: State())

    var now: Instant { state.withLock { $0.now } }
    var minimumResolution: Duration { .zero }
    var requestedDeadlines: [Duration] { state.withLock { $0.deadlines } }
    var sleeperCount: Int { state.withLock { $0.sleepers.count } }

    func sleep(until deadline: Instant, tolerance: Duration?) async throws {
        let id = UUID()
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, any Error>) in
                let early = state.withLock { s -> Result<Void, any Error>? in
                    s.deadlines.append(deadline.offset)
                    if s.cancelled.contains(id) { return .failure(CancellationError()) }
                    if deadline <= s.now { return .success(()) }
                    s.sleepers[id] = Sleeper(deadline: deadline, continuation: continuation)
                    return nil
                }
                if let early { continuation.resume(with: early) }
            }
        } onCancel: {
            let sleeper = state.withLock { s -> Sleeper? in
                s.cancelled.insert(id)
                return s.sleepers.removeValue(forKey: id)
            }
            sleeper?.continuation.resume(throwing: CancellationError())
        }
    }

    func advance(by duration: Duration) {
        let due = state.withLock { s -> [Sleeper] in
            s.now = s.now.advanced(by: duration)
            let now = s.now
            let due = s.sleepers.filter { $0.value.deadline <= now }
            for id in due.keys { s.sleepers.removeValue(forKey: id) }
            return Array(due.values)
        }
        due.forEach { $0.continuation.resume() }
    }
}

@MainActor
final class QuitFlushTests: XCTestCase {
    private func makeHost() throws -> (PersistHost, InMemoryVaultFile, Data) {
        let (envelope, dek) = try VaultFiles.makeEnvelope(accounts: [VaultFixture.account("a", issuer: "Alpha")])
        let file = InMemoryVaultFile(envelope: envelope)
        return (try PersistHost(file: file, dek: dek), file, dek)
    }

    private func addBeta(_ host: PersistHost) {
        XCTAssertTrue(host.mutate { $0.append(VaultFixture.account("b", issuer: "Beta")) })
    }

    func testQuitWithNothingPendingTerminatesAtOnce() throws {
        let (host, _, _) = try makeHost()
        let flush = QuitFlush(clock: ManualClock())

        XCTAssertFalse(flush.shouldWait(for: host))
        XCTAssertFalse(flush.shouldWait(for: nil))
        XCTAssertEqual(flush.bound, .seconds(5))
    }

    func testQuitWaitsForAnEditStagedJustBefore() async throws {
        let (host, file, dek) = try makeHost()
        let clock = ManualClock()
        let flush = QuitFlush(clock: clock, bound: .seconds(5))
        file.hold()
        addBeta(host)
        XCTAssertTrue(flush.shouldWait(for: host))

        let outcome = Task { await flush.run(host) }
        await waitUntil { file.waitingCommits == 1 && clock.sleeperCount == 1 }
        XCTAssertEqual(try file.accounts(dek: dek).map(\.id), ["a"])
        file.release()
        let result = await outcome.value

        XCTAssertEqual(result, .settled)
        XCTAssertEqual(try file.accounts(dek: dek).map(\.id), ["a", "b"])
        XCTAssertFalse(host.persistence.hasPendingCommits)
        XCTAssertEqual(clock.requestedDeadlines, [.seconds(5)])
        XCTAssertEqual(clock.sleeperCount, 0)
    }

    func testQuitStopsWaitingAtTheBound() async throws {
        let (host, file, dek) = try makeHost()
        let clock = ManualClock()
        let flush = QuitFlush(clock: clock, bound: .seconds(5))
        file.hold()
        addBeta(host)

        let outcome = Task { await flush.run(host) }
        await waitUntil { clock.sleeperCount == 1 }
        clock.advance(by: .seconds(4))
        XCTAssertEqual(clock.sleeperCount, 1)
        clock.advance(by: .seconds(1))
        let result = await outcome.value

        XCTAssertEqual(result, .timedOut)
        XCTAssertTrue(host.persistence.hasPendingCommits)
        XCTAssertEqual(try file.accounts(dek: dek).map(\.id), ["a"])
        file.release()
        await host.settlePendingCommits()
        XCTAssertEqual(try file.accounts(dek: dek).map(\.id), ["a", "b"])
    }

    func testQuitWaitsForAnExclusiveWrite() async throws {
        let (host, _, _) = try makeHost()
        let clock = ManualClock()
        let flush = QuitFlush(clock: clock, bound: .seconds(5))
        let gate = TestGate()
        let exclusive = Task { await host.runInWriteOrder { await gate.wait(); return 7 } }
        await waitUntil { gate.isWaiting }
        XCTAssertTrue(flush.shouldWait(for: host))

        let outcome = Task { await flush.run(host) }
        await waitUntil { clock.sleeperCount == 1 }
        XCTAssertTrue(host.hasPendingCommits)
        gate.open()
        let result = await outcome.value

        XCTAssertEqual(result, .settled)
        let value = await exclusive.value
        XCTAssertEqual(value, 7)
        XCTAssertFalse(host.hasPendingCommits)
        XCTAssertEqual(clock.sleeperCount, 0)
    }
}
