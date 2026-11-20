import Foundation
import CoreGraphics

@MainActor
protocol IdleTimeSource: AnyObject {
    func secondsSinceLastInput() -> TimeInterval
}

@MainActor
final class SystemIdleTimeSource: IdleTimeSource {
    private let anyInput: CGEventType

    init() {
        guard let anyInput = CGEventType(rawValue: ~0) else {
            preconditionFailure("CGEventType cannot represent kCGAnyInputEventType")
        }
        self.anyInput = anyInput
    }

    func secondsSinceLastInput() -> TimeInterval {
        CGEventSource.secondsSinceLastEventType(.combinedSessionState, eventType: anyInput)
    }
}
