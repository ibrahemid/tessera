import Foundation
import SwiftUI
import TesseraCore

enum ManualOrder {
    /// The order every list shows: the core display order, then pinned
    /// accounts grouped first with their relative order kept, as `tess` sorts.
    static func displayOrder(_ live: [Account]) -> [Account] {
        let sorted = Account.sortedForDisplay(live)
        return sorted.filter(\.pinned) + sorted.filter { !$0.pinned }
    }

    /// Applies a list drag inside one section: `section` is the ids the section
    /// shows, `ids` the dragged rows and `toOffset` the drop offset, as
    /// `onMove` reports them. The section's rows take the arranged order in the
    /// places their group holds in the display order, and every live account
    /// is then numbered 1..n. A row outside the section, a bad offset or a
    /// section whose rows are no longer exactly the pinned or the unpinned live
    /// accounts changes nothing.
    static func move(_ ids: [String], toOffset: Int, section: [String], in accounts: inout [Account]) {
        let members = Set(section)
        guard members.count == section.count, (0...section.count).contains(toOffset) else { return }
        let offsets = IndexSet(ids.compactMap { section.firstIndex(of: $0) })
        guard !offsets.isEmpty, offsets.count == ids.count else { return }
        let groups = liveGroups(accounts)
        guard groups.contains(where: { Set($0) == members }) else { return }
        var order = Array(groups.joined())
        let slots = order.indices.filter { members.contains(order[$0]) }
        var moved = section
        moved.move(fromOffsets: offsets, toOffset: toOffset)
        for (slot, id) in zip(slots, moved) { order[slot] = id }
        number(order, in: &accounts)
    }

    /// Moves the selected live accounts `delta` steps within their group,
    /// pinned or not, gathered into one block in display order. When a block
    /// would pass the edge of its group nothing changes; otherwise every live
    /// account is numbered 1..n.
    static func move(_ ids: Set<String>, by delta: Int, in accounts: inout [Account]) {
        guard delta != 0 else { return }
        let groups = liveGroups(accounts)
        guard groups.joined().contains(where: { ids.contains($0) }) else { return }
        var order: [String] = []
        for group in groups {
            guard let next = shifted(group, selected: ids, by: delta) else { return }
            order += next
        }
        number(order, in: &accounts)
    }

    /// Clears the position of every live account and returns the positions it
    /// cleared by id. Trashed accounts and `updatedAt` are left alone.
    static func clear(in accounts: inout [Account]) -> [String: Int] {
        var previous: [String: Int] = [:]
        for i in accounts.indices where !accounts[i].isTrashed && accounts[i].order != 0 {
            previous[accounts[i].id] = accounts[i].order
            accounts[i].order = 0
        }
        return previous
    }

    /// Puts back positions `clear` returned, on live accounts that still have
    /// none, so a reorder made since then is kept.
    static func reapply(_ previous: [String: Int], in accounts: inout [Account]) {
        for i in accounts.indices where !accounts[i].isTrashed && accounts[i].order == 0 {
            if let order = previous[accounts[i].id], order > 0 { accounts[i].order = order }
        }
    }

    private static func liveGroups(_ accounts: [Account]) -> [[String]] {
        let shown = displayOrder(Account.live(accounts))
        return [shown.filter(\.pinned).map(\.id), shown.filter { !$0.pinned }.map(\.id)]
    }

    private static func shifted(_ group: [String], selected ids: Set<String>, by delta: Int) -> [String]? {
        let picked = group.indices.filter { ids.contains(group[$0]) }
        guard let first = picked.first, let last = picked.last else { return group }
        let start = delta < 0 ? first + delta : last + delta - picked.count + 1
        guard delta < 0 ? start >= 0 : last + delta < group.count else { return nil }
        var out = group.filter { !ids.contains($0) }
        out.insert(contentsOf: picked.map { group[$0] }, at: start)
        return out
    }

    private static func number(_ order: [String], in accounts: inout [Account]) {
        var position: [String: Int] = [:]
        for (n, id) in order.enumerated() { position[id] = n + 1 }
        for i in accounts.indices where !accounts[i].isTrashed {
            if let n = position[accounts[i].id] { accounts[i].order = n }
        }
    }
}
