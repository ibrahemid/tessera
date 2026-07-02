import Foundation
import Vision
import ScreenCaptureKit

/// Captures the screen(s) and extracts every otpauth / otpauth-migration QR
/// payload found. Requires the Screen Recording TCC permission, which macOS
/// prompts for on first use.
enum QRCapture {
    enum CaptureError: Error, CustomStringConvertible {
        case noDisplay, noQRFound
        var description: String {
            switch self {
            case .noDisplay: return "no display available to capture"
            case .noQRFound: return "no 2FA QR code found on screen"
            }
        }
    }

    /// Scan all displays and return every distinct OTP QR payload. Throws
    /// `noQRFound` only when nothing relevant is on any screen.
    static func scanScreen() async throws -> [String] {
        let content = try await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: true)
        guard !content.displays.isEmpty else { throw CaptureError.noDisplay }
        var seen = Set<String>()
        var payloads: [String] = []
        var captured = 0
        var lastError: Error?
        for display in content.displays {
            let filter = SCContentFilter(display: display, excludingWindows: [])
            let config = SCStreamConfiguration()
            config.width = display.width
            config.height = display.height
            do {
                let image = try await SCScreenshotManager.captureImage(contentFilter: filter, configuration: config)
                captured += 1
                for payload in try detectAll(in: image) where seen.insert(payload).inserted {
                    payloads.append(payload)
                }
            } catch { lastError = error }
        }
        // Don't mask a permission/capture/detection failure as "no QR found".
        if payloads.isEmpty, let lastError { throw lastError }
        if payloads.isEmpty { throw captured == 0 ? CaptureError.noDisplay : CaptureError.noQRFound }
        return payloads
    }

    /// Every OTP QR payload in an image (one image can hold many codes).
    static func detectAll(in cgImage: CGImage) throws -> [String] {
        let request = VNDetectBarcodesRequest()
        request.symbologies = [.qr]
        let handler = VNImageRequestHandler(cgImage: cgImage, options: [:])
        try handler.perform([request])
        var seen = Set<String>()
        var out: [String] = []
        for r in (request.results ?? []) {
            guard let payload = r.payloadStringValue,
                  payload.hasPrefix("otpauth://") || payload.hasPrefix("otpauth-migration://") else { continue }
            if seen.insert(payload).inserted { out.append(payload) }
        }
        return out
    }
}
