import Foundation

enum SEKeyError: Error, Equatable, Sendable {
    case notAvailable
    case noWrap
    case keyItemMissing(SEKeyID)
    case readBackMismatch(SEKeyID)
    case verifyFailed
    case deriveFailed
    case malformedWrap
}
