import Foundation
import ScreenCaptureKit
import Vision

/// Captures the screen and extracts the first QR payload (an otpauth or
/// otpauth-migration URI). Requires the Screen Recording TCC permission, which
/// macOS prompts for on first use.
enum QRCapture {
    enum CaptureError: Error, CustomStringConvertible {
        case noDisplay, noQRFound
        var description: String {
            switch self {
            case .noDisplay: return "no display available to capture"
            case .noQRFound: return "no QR code found on screen"
            }
        }
    }

    static func scanScreen() async throws -> String {
        let content = try await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: true)
        guard let display = content.displays.first else { throw CaptureError.noDisplay }
        let filter = SCContentFilter(display: display, excludingWindows: [])
        let config = SCStreamConfiguration()
        config.width = display.width
        config.height = display.height
        let image = try await SCScreenshotManager.captureImage(contentFilter: filter, configuration: config)
        return try detectQR(in: image)
    }

    static func detectQR(in cgImage: CGImage) throws -> String {
        let request = VNDetectBarcodesRequest()
        request.symbologies = [.qr]
        let handler = VNImageRequestHandler(cgImage: cgImage, options: [:])
        try handler.perform([request])
        let results = (request.results ?? [])
        for r in results {
            if let payload = r.payloadStringValue,
               payload.hasPrefix("otpauth://") || payload.hasPrefix("otpauth-migration://") {
                return payload
            }
        }
        throw CaptureError.noQRFound
    }
}
