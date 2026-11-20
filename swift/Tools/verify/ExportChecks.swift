import Foundation

/// The `exports` vector set: each format rendered from the vector's accounts
/// gives the Go file names and bytes, transfer links and skip list.
func runExportChecks(_ root: JSONNode, _ check: (Bool, String) -> Void) {
    let e = vnode(root, "exports")
    guard let accounts = try? CanonicalJSON.decode(vbytes(e, "canonical_json")) else {
        check(false, "exports accounts decode")
        return
    }
    let batchID = Int32(truncatingIfNeeded: vint(e, "batch_id"))
    let formats = vnode(e, "formats").items
    check(formats.map { vtext($0, "format") } == Exporters.Format.allCases.map(\.name),
          "export formats in the Go registry order")
    for f in formats {
        let name = vtext(f, "format")
        guard let format = Exporters.Format.lookup(name) else {
            check(false, "export \(name) is registered")
            continue
        }
        do {
            let problems = exportMismatches(try Exporters.render(format, accounts, batchID: batchID), f)
            for p in problems { print("     \(name): \(p)") }
            check(problems.isEmpty, "export \(name) matches Go")
        } catch {
            check(false, "export \(name): \(error)")
        }
    }
    let empty = vnode(e, "migration_empty_secret")
    guard let emptyAccounts = try? CanonicalJSON.decode(vbytes(empty, "canonical_json")) else {
        check(false, "exports migration_empty_secret accounts decode")
        return
    }
    do {
        let r = try Exporters.render(.googleMigration, emptyAccounts, batchID: batchID)
        let problems = exportMismatches(r, empty)
        for p in problems { print("     migration_empty_secret: \(p)") }
        check(problems.isEmpty && r.skipped.contains { $0.kind == .emptySecret },
              "export migration_empty_secret matches Go")
    } catch {
        check(false, "export migration_empty_secret: \(error)")
    }
}

private func exportMismatches(_ r: Exporters.Rendered, _ want: JSONNode) -> [String] {
    var out: [String] = []
    switch r.output {
    case .files(let files):
        let w = want.member("files")?.items ?? []
        if want.member("codes") != nil { out.append("rendered files, want transfer codes") }
        if files.count != w.count { out.append("\(files.count) files, want \(w.count)") }
        for (got, exp) in zip(files, w) {
            if Array(got.name.utf8) != vnode(exp, "name").string { out.append("file name \(got.name)") }
            if [UInt8](got.data) != vnode(exp, "content").string { out.append("\(got.name) content differs") }
        }
    case .transferCodes(let codes):
        let w = want.member("codes")?.items ?? []
        if want.member("files") != nil { out.append("rendered transfer codes, want files") }
        if codes.count != w.count { out.append("\(codes.count) codes, want \(w.count)") }
        for (got, exp) in zip(codes, w) {
            if Array(got.name.utf8) != vnode(exp, "name").string { out.append("code name \(got.name)") }
            if Array(got.uri.utf8) != vnode(exp, "uri").string { out.append("\(got.name) uri differs") }
        }
    }
    let skipped = vnode(want, "skipped").items
    if r.skipped.count != skipped.count { out.append("\(r.skipped.count) skipped, want \(skipped.count)") }
    for (got, exp) in zip(r.skipped, skipped) {
        if Array(got.label.utf8) != vnode(exp, "label").string { out.append("skip label \(got.label)") }
        if got.kind.rawValue != vtext(exp, "kind") { out.append("skip kind \(got.kind.rawValue)") }
        if Array(got.reason.utf8) != vnode(exp, "reason").string { out.append("skip reason \(got.reason)") }
    }
    return out
}
