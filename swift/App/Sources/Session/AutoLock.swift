import Foundation

enum AutoLock {
    static let minutesPrefKey = "tessera.autoLockMinutes"
    static let defaultMinutes = 15
    static let never = 0
    static let choices = [1, 5, 15, 30, 60, never]

    static func minutes(in defaults: UserDefaults) -> Int {
        guard defaults.object(forKey: minutesPrefKey) != nil else { return defaultMinutes }
        return defaults.integer(forKey: minutesPrefKey)
    }

    static func label(for minutes: Int) -> String {
        switch minutes {
        case ...never: return "Never"
        case 1: return "1 minute"
        case 60: return "1 hour"
        default: return "\(minutes) minutes"
        }
    }
}
