// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Tessera",
    platforms: [.macOS(.v14)],
    products: [
        .library(name: "TesseraCore", targets: ["TesseraCore"]),
        .library(name: "TesseraArgon2", targets: ["TesseraArgon2"]),
    ],
    dependencies: [
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
        // Vendored PHC reference argon2 (CC0/Apache-2.0). Portable ref.c only,
        // threads disabled — builds clean on Apple Silicon, unlike SIMD opt.c.
        .target(
            name: "CArgon2",
            cSettings: [
                .define("ARGON2_NO_THREADS"),
                .headerSearchPath("."),
                .headerSearchPath("blake2"),
            ]
        ),
        .target(name: "TesseraArgon2", dependencies: ["CArgon2", "TesseraCore"]),
        .testTarget(
            name: "TesseraCoreTests",
            dependencies: ["TesseraCore", "TesseraArgon2"]
        ),
    ]
)
