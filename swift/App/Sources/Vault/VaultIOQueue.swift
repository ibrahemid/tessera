import Foundation

final class VaultIOQueue: Sendable {
    static let shared = VaultIOQueue()

    private let queue: DispatchQueue

    init(label: String = "com.ibrahemid.tessera.vault-io") {
        queue = DispatchQueue(label: label)
    }

    func run<T: Sendable>(_ work: @escaping @Sendable () throws -> T) async throws -> T {
        try await withCheckedThrowingContinuation { continuation in
            queue.async { continuation.resume(with: Result { try work() }) }
        }
    }
}
