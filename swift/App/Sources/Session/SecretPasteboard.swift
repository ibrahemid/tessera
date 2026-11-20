import AppKit

@MainActor
protocol SecretPasteboard: AnyObject {
    var changeCount: Int { get }
    func prepareForNewContents(with options: NSPasteboard.ContentsOptions) -> Int
    func writeObjects(_ objects: [any NSPasteboardWriting]) -> Bool
    @discardableResult func clearContents() -> Int
}

extension NSPasteboard: SecretPasteboard {}
