import AppKit
import SwiftUI
import XCTest
@testable import Tessera

@MainActor
final class VaultLocationViewsTests: XCTestCase {
    private func makeInfo(in dir: URL) -> DamagedVaultInfo {
        let vault = dir.appendingPathComponent("vault.json")
        return DamagedVaultInfo(vault: vault, backup: VaultSidecar.backupURL(for: vault),
                                rollback: dir.appendingPathComponent("vault.json.rollback"))
    }

    func testDamagedVaultOptionsFollowTheAvailableCopies() throws {
        let dir = try makeScratchDirectory()
        let info = makeInfo(in: dir)

        XCTAssertEqual(DamagedVaultView.options(for: info).map(\.title),
                       ["Restore previous version", "Restore copy from before the key change"])
        XCTAssertEqual(DamagedVaultView.options(for: info).map(\.source), [info.backup, info.rollback].compactMap { $0 })
        XCTAssertEqual(DamagedVaultView.options(for: DamagedVaultInfo(vault: info.vault, backup: nil, rollback: info.rollback))
                        .map(\.source), [info.rollback].compactMap { $0 })
        XCTAssertEqual(DamagedVaultView.options(for: DamagedVaultInfo(vault: info.vault, backup: nil, rollback: nil)), [])
    }

    func testTheAccessLineNamesWhatSavesUse() throws {
        let vault = try makeScratchDirectory().appendingPathComponent("vault.json")

        XCTAssertEqual(VaultLocationSection.accessLine(for: VaultLocation(fileURL: vault, access: .container)),
                       "Built-in vault.")
        XCTAssertEqual(VaultLocationSection.accessLine(for: VaultLocation(fileURL: vault, access: .folder)),
                       "Shared with the tess CLI. Saves use its lock file and keep vault.json.bak.")
        XCTAssertEqual(VaultLocationSection.accessLine(for: VaultLocation(fileURL: vault, access: .fileOnly)),
                       "Shared with the tess CLI. Without folder access, saves skip the CLI’s lock file and backup.")
    }

    func testOnlyFileOnlyAccessOffersTheFolderGrant() throws {
        let dir = try makeScratchDirectory()
        let real = dir.appendingPathComponent("real", isDirectory: true)
        try FileManager.default.createDirectory(at: real, withIntermediateDirectories: false)
        let target = real.appendingPathComponent("vault.json")
        try VaultFiles.write(Data("{}".utf8), to: target, mode: 0o600)
        let link = dir.appendingPathComponent("vault.json")
        try VaultFiles.symlink(link, to: target)

        XCTAssertEqual(VaultLocationSection.grantFolder(for: VaultLocation(fileURL: link, access: .fileOnly))?.path,
                       real.path)
        XCTAssertNil(VaultLocationSection.grantFolder(for: VaultLocation(fileURL: link, access: .folder)))
        XCTAssertNil(VaultLocationSection.grantFolder(for: VaultLocation(fileURL: link, access: .container)))
    }

    func testTheViewsRenderInBothAppearances() throws {
        let dir = try makeScratchDirectory()
        let info = makeInfo(in: dir)
        let path: (URL) -> String = { "~/.local/share/tessera/" + $0.lastPathComponent }
        var corners: [Bool: NSColor] = [:]
        for dark in [false, true] {
            let suffix = dark ? "dark" : "light"
            let damaged = try render(DamagedVaultView(info: info, displayPath: path, restore: { _ in nil },
                                                      openAnother: { nil })
                                        .background(Palette.background),
                                     size: CGSize(width: 520, height: 440), dark: dark, name: "damaged-\(suffix)")
            corners[dark] = damaged.colorAt(x: damaged.pixelsWide / 2, y: 4)
            for access in [VaultLocation.Access.fileOnly, .folder] {
                let location = VaultLocation(fileURL: info.vault, access: access)
                _ = try render(Form {
                    VaultLocationSection(location: location, displayPath: path, grantFolderAccess: { nil })
                }
                .formStyle(.grouped),
                size: CGSize(width: 520, height: 200), dark: dark, name: "location-\(access)-\(suffix)")
            }
        }
        let light = try XCTUnwrap(corners[false]?.usingColorSpace(.sRGB))
        let dark = try XCTUnwrap(corners[true]?.usingColorSpace(.sRGB))
        XCTAssertGreaterThan(light.brightnessComponent, 0.9)
        XCTAssertLessThan(dark.brightnessComponent, 0.2)
    }

    private func render<V: View>(_ view: V, size: CGSize, dark: Bool, name: String) throws -> NSBitmapImageRep {
        let host = NSHostingView(rootView: view.environment(\.colorScheme, dark ? .dark : .light))
        let window = NSWindow(contentRect: NSRect(origin: .zero, size: size), styleMask: [.borderless],
                              backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
        window.contentView = host
        defer { window.close() }
        host.frame = NSRect(origin: .zero, size: size)
        host.layoutSubtreeIfNeeded()
        let rep = try XCTUnwrap(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: rep)
        let png = try XCTUnwrap(rep.representation(using: .png, properties: [:]))
        let attachment = XCTAttachment(data: png, uniformTypeIdentifier: "public.png")
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        return rep
    }
}
