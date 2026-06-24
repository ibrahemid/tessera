// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Tessera",
    platforms: [.macOS(.v14)],
    products: [
        .library(name: "TesseraCore", targets: ["TesseraCore"]),
    ],
    dependencies: [
        // argon2id is the one primitive CryptoKit lacks. Argon2Swift wraps the
        // reference C implementation; used only to verify the passphrase wrap in
        // tests/CI and by the app target (kept out of TesseraCore so the core
        // stays dependency-free and swiftc-verifiable).
        .package(url: "https://github.com/tmthecoder/Argon2Swift.git", from: "1.0.0"),
        // swift-crypto provides CryptoKit-compatible APIs for Linux CI; on macOS
        // the system CryptoKit is used via the canImport guard in the sources.
        .package(url: "https://github.com/apple/swift-crypto.git", from: "3.0.0"),
    ],
    targets: [
        .target(
            name: "TesseraCore",
            dependencies: [
                .product(name: "Crypto", package: "swift-crypto", condition: .when(platforms: [.linux])),
            ]
        ),
        .testTarget(
            name: "TesseraCoreTests",
            dependencies: [
                "TesseraCore",
                .product(name: "Argon2Swift", package: "Argon2Swift"),
            ]
        ),
    ]
)
