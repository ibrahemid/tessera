import Foundation

/// Locates go/internal/importers/testdata by walking up from this source file.
/// The Go tree holds the single copy of the shared importer fixtures; adding
/// them as an SPM resource bundle would fork them.
func importerTestdataDir() -> URL {
    var dir = URL(fileURLWithPath: #filePath)
    for _ in 0..<8 {
        dir.deleteLastPathComponent()
        let candidate = dir.appendingPathComponent("go/internal/importers/testdata/expected.json")
        if FileManager.default.fileExists(atPath: candidate.path) {
            return dir.appendingPathComponent("go/internal/importers/testdata")
        }
    }
    fatalError("go/internal/importers/testdata/expected.json not found")
}

/// The parsed rows of expected.json, the fixture table both cores run against.
func importerFixtures() throws -> [[String: Any]] {
    let data = try Data(contentsOf: importerTestdataDir().appendingPathComponent("expected.json"))
    return try JSONSerialization.jsonObject(with: data) as! [[String: Any]]
}
