import AppKit

@MainActor
final class LockPolicy {
    static let screenIsLocked = Notification.Name("com.apple.screenIsLocked")

    private let defaults: UserDefaults
    private let idle: any IdleTimeSource
    private let isVaultUnlocked: @MainActor () -> Bool
    private let onLock: @MainActor (LockReason) -> Void
    private let observers = NotificationObservers()
    private var poll: Task<Void, Never>?
    private var isStopped = false

    init(defaults: UserDefaults,
         idle: any IdleTimeSource,
         workspace: NotificationCenter,
         distributed: NotificationCenter,
         appCenter: NotificationCenter,
         pollInterval: Duration = .seconds(15),
         isVaultUnlocked: @escaping @MainActor () -> Bool,
         onLock: @escaping @MainActor (LockReason) -> Void) {
        self.defaults = defaults
        self.idle = idle
        self.isVaultUnlocked = isVaultUnlocked
        self.onLock = onLock
        observe(distributed, Self.screenIsLocked, .screenLocked)
        observe(workspace, NSWorkspace.willSleepNotification, .sleep)
        observe(workspace, NSWorkspace.sessionDidResignActiveNotification, .sessionResigned)
        observe(appCenter, NSApplication.willTerminateNotification, .quit)
        poll = Task { @MainActor [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: pollInterval)
                guard !Task.isCancelled, let self else { return }
                self.checkIdle()
            }
        }
    }

    func checkIdle() {
        guard !isStopped, isVaultUnlocked() else { return }
        let minutes = AutoLock.minutes(in: defaults)
        guard minutes > AutoLock.never else { return }
        guard idle.secondsSinceLastInput() >= TimeInterval(minutes) * 60 else { return }
        onLock(.idle)
    }

    func stop() {
        isStopped = true
        poll?.cancel()
        poll = nil
        observers.stop()
    }

    private func observe(_ center: NotificationCenter, _ name: Notification.Name, _ reason: LockReason) {
        observers.add(center, center.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.fire(reason) }
        })
    }

    private func fire(_ reason: LockReason) {
        guard !isStopped else { return }
        onLock(reason)
    }
}
