import Foundation
import TesseraCore

struct LinkPrefill: Identifiable, Equatable {
    let id = UUID()
    let text: String
}

enum LinkIntake {
    static let schemes: Set<String> = ["otpauth", "otpauth-migration"]

    static func prefill(for urls: [URL]) -> LinkPrefill? {
        let links = urls.compactMap { url -> String? in
            guard let scheme = url.scheme?.lowercased(), schemes.contains(scheme) else { return nil }
            let text = url.absoluteString
            switch InputDetect.classify(text) {
            case .otpauth, .migration: return text
            default: return nil
            }
        }
        return links.isEmpty ? nil : LinkPrefill(text: links.joined(separator: "\n"))
    }
}

extension AppModel {
    @discardableResult
    func receiveLinks(_ urls: [URL]) -> Bool {
        guard let prefill = LinkIntake.prefill(for: urls) else { return false }
        pendingLink = prefill
        if isLocked, !isOpening, !isDemo { unlock() }
        return true
    }
}
