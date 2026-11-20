import Foundation

/// The `merge` vector set: a two-way merge of each case gives both sides,
/// their written flags and their counts as Go does.
func runMergeChecks(_ root: JSONNode, _ check: (Bool, String) -> Void) {
    let cases = vnode(vnode(root, "merge"), "cases").items
    var allOK = true
    for c in cases {
        let name = vtext(c, "name")
        do {
            let problems = try mergeMismatches(c)
            for p in problems { print("     \(name): \(p)") }
            if !problems.isEmpty { allOK = false }
        } catch {
            print("     \(name): \(error)")
            allOK = false
        }
    }
    check(allOK && !cases.isEmpty, "merge (\(cases.count) cases)")
}

private func mergeMismatches(_ c: JSONNode) throws -> [String] {
    let localIn = vbytes(c, "local")
    let sourceIn = vbytes(c, "source")
    var local = try CanonicalJSON.decode(localIn)
    let source = try CanonicalJSON.decode(sourceIn)
    var out: [String] = []
    if Handles.assign(&local) { out.append("a local account has no handle") }
    let r = Merge.twoWay(local: local, source: source)
    out += sideMismatches("local", r.local, r.localCounts, localIn, c)
    out += sideMismatches("source", r.source, r.sourceCounts, sourceIn, c)
    return out
}

private func sideMismatches(_ side: String, _ accounts: [Account], _ counts: Merge.Counts,
                            _ input: Data, _ c: JSONNode) -> [String] {
    var out: [String] = []
    let w = vnode(c, "\(side)_counts")
    let want = Merge.Counts(added: Int(vint(w, "added")), updated: Int(vint(w, "updated")),
                            trashed: Int(vint(w, "trashed")), unchanged: Int(vint(w, "unchanged")),
                            duplicate: Int(vint(w, "duplicate")), invalid: Int(vint(w, "invalid")))
    if counts != want { out.append("\(side) counts \(counts)") }
    let written = vraw(vnode(c, "\(side)_written")) == Array("true".utf8)
    if counts.changed != written { out.append("\(side) written \(counts.changed)") }
    var after = input
    if counts.changed {
        var merged = accounts
        Merge.dedupeHandles(&merged)
        Handles.assign(&merged)
        after = CanonicalJSON.encode(merged)
    }
    if after != vbytes(c, "\(side)_after") { out.append("\(side) after differs") }
    return out
}
