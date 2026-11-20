import AppKit

final class AppDelegate: NSObject, NSApplicationDelegate {
    /// Scene id of the main window (see TesseraApp), used to reopen the right one.
    static let mainWindowID = "main"

    private var quitFlush: Task<Void, Never>?

    func applicationDidFinishLaunching(_ notification: Notification) {
        if SelfTest.runIfRequested() { return }
        if SelfTestSE.runIfRequested() { return }
        #if DEBUG
        if MarketingShot.runIfRequested() {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { NSApp.terminate(nil) }
            return
        }
        if Screenshots.isRequested {
            Task { @MainActor in
                await Screenshots.run()
                NSApp.terminate(nil)
            }
        }
        #endif
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        guard !flag else { return true }
        // Target the main scene by id: "first window that can become key" can be
        // the Settings window, which would reopen to the wrong surface.
        let window = sender.windows.first { $0.identifier?.rawValue == Self.mainWindowID }
            ?? sender.windows.first { $0.canBecomeKey }
        window?.makeKeyAndOrderFront(nil)
        return true
    }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        guard quitFlush == nil else { return .terminateLater }
        let flush = QuitFlush(clock: ContinuousClock())
        guard let model = AppSession.shared?.model, flush.shouldWait(for: model) else { return .terminateNow }
        quitFlush = Task { @MainActor in
            _ = await flush.run(model)
            NSApp.reply(toApplicationShouldTerminate: true)
        }
        return .terminateLater
    }
}

enum QuitFlushOutcome: Equatable {
    case settled
    case timedOut
}

@MainActor
struct QuitFlush<C: Clock> where C.Duration == Duration {
    let clock: C
    let bound: Duration

    init(clock: C, bound: Duration = .seconds(5)) {
        self.clock = clock
        self.bound = bound
    }

    func shouldWait(for model: (any PendingCommitFlushing)?) -> Bool {
        model?.hasPendingCommits ?? false
    }

    func run(_ model: any PendingCommitFlushing) async -> QuitFlushOutcome {
        let race = QuitRace()
        let clock = clock, bound = bound
        return await withCheckedContinuation { continuation in
            race.continuation = continuation
            let deadline = Task { @MainActor in
                do { try await clock.sleep(for: bound) } catch { return }
                race.finish(.timedOut)
            }
            Task { @MainActor in
                await model.settlePendingCommits()
                deadline.cancel()
                race.finish(.settled)
            }
        }
    }
}

@MainActor
private final class QuitRace {
    var continuation: CheckedContinuation<QuitFlushOutcome, Never>?

    func finish(_ outcome: QuitFlushOutcome) {
        continuation?.resume(returning: outcome)
        continuation = nil
    }
}
