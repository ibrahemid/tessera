import AppKit
import Foundation
import SwiftUI
import TesseraCore

enum VaultScope: Hashable {
    case all, pinned, folder(String)
}

struct VaultSection: Equatable, Identifiable {
    enum Kind: Hashable { case pinned, others, single }
    let kind: Kind
    let accounts: [Account]
    var id: Kind { kind }
}

struct VaultCounts: Equatable {
    var all = 0
    var pinned = 0
    var folders: [String: Int] = [:]
}

enum VaultList {
    static func matches(_ account: Account, query: String) -> Bool {
        let terms = query.split(whereSeparator: \.isWhitespace).compactMap { raw -> String? in
            let term = raw.drop { $0 == "#" }
            return term.isEmpty ? nil : String(term)
        }
        guard !terms.isEmpty else { return true }
        let fields = [account.issuer, account.account, account.handle, account.folder] + account.tags
        return terms.allSatisfy { term in
            fields.contains { $0.range(of: term, options: [.caseInsensitive, .diacriticInsensitive]) != nil }
        }
    }

    static func inScope(_ account: Account, _ scope: VaultScope) -> Bool {
        switch scope {
        case .all: return true
        case .pinned: return account.pinned
        case .folder(let f): return account.folder == f
        }
    }

    static func sections(_ accounts: [Account], scope: VaultScope, query: String) -> [VaultSection] {
        let shown = accounts.filter { inScope($0, scope) && matches($0, query: query) }
        guard scope == .all else {
            return shown.isEmpty ? [] : [VaultSection(kind: .single, accounts: shown)]
        }
        let pinned = shown.filter(\.pinned)
        let others = shown.filter { !$0.pinned }
        if pinned.isEmpty || others.isEmpty {
            return shown.isEmpty ? [] : [VaultSection(kind: .single, accounts: pinned + others)]
        }
        return [VaultSection(kind: .pinned, accounts: pinned), VaultSection(kind: .others, accounts: others)]
    }

    static func visible(_ accounts: [Account], scope: VaultScope, query: String) -> [Account] {
        sections(accounts, scope: scope, query: query).flatMap(\.accounts)
    }

    static func isReorderable(scope: VaultScope, query: String) -> Bool {
        scope == .all && query.trimmingCharacters(in: .whitespaces).isEmpty
    }

    static func reorder(_ payload: [Account], section ids: [String],
                        fromOffsets: IndexSet, toOffset: Int) -> [Account] {
        guard fromOffsets.allSatisfy({ ids.indices.contains($0) }), (0...ids.count).contains(toOffset) else {
            return payload
        }
        var order = ids
        order.move(fromOffsets: fromOffsets, toOffset: toOffset)
        let members = Set(ids)
        let slots = payload.indices.filter { members.contains(payload[$0].id) }
        guard slots.count == order.count else { return payload }
        var byID: [String: Account] = [:]
        for a in payload where members.contains(a.id) { byID[a.id] = a }
        var out = payload
        for (slot, id) in zip(slots, order) {
            guard let a = byID[id] else { return payload }
            out[slot] = a
        }
        return out
    }

    static func counts(_ accounts: [Account]) -> VaultCounts {
        var c = VaultCounts(all: accounts.count)
        for a in accounts {
            if a.pinned { c.pinned += 1 }
            if !a.folder.isEmpty { c.folders[a.folder, default: 0] += 1 }
        }
        return c
    }

    static func tags(in accounts: [Account]) -> [String] {
        Array(Set(accounts.flatMap(\.tags))).sorted { $0.localizedStandardCompare($1) == .orderedAscending }
    }

    static func dragPayload(for id: String, selection: VaultSelection, order: [String]) -> String {
        selection.isMultiple && selection.contains(id) ? selection.ordered(in: order).joined(separator: "\n") : id
    }

    static func droppedIDs(_ items: [String], known: Set<String>) -> Set<String> {
        Set(items.flatMap { $0.split(whereSeparator: \.isNewline).map(String.init) }.filter(known.contains))
    }
}

enum RowClick: Equatable {
    case activate, toggle, extend

    init(modifiers: NSEvent.ModifierFlags) {
        if modifiers.contains(.command) {
            self = .toggle
        } else if modifiers.contains(.shift) {
            self = .extend
        } else {
            self = .activate
        }
    }
}

enum TypeToSearch {
    static func text(from characters: String, modifiers: EventModifiers) -> String? {
        guard modifiers.isDisjoint(with: [.command, .control, .option]), !characters.isEmpty else { return nil }
        let printable = characters.unicodeScalars.allSatisfy { scalar in
            !CharacterSet.controlCharacters.contains(scalar) && !(0xF700...0xF8FF).contains(scalar.value)
        }
        return printable ? characters : nil
    }
}

enum VaultKeyRoute: Equatable {
    case move(by: Int, extending: Bool)
    case type(String)

    static func route(key: KeyEquivalent, characters: String, modifiers: EventModifiers,
                      searchFocused: Bool) -> VaultKeyRoute? {
        guard searchFocused else {
            return TypeToSearch.text(from: characters, modifiers: modifiers).map(VaultKeyRoute.type)
        }
        guard modifiers.isDisjoint(with: [.command, .control, .option]) else { return nil }
        let extending = modifiers.contains(.shift)
        switch key {
        case .downArrow: return .move(by: 1, extending: extending)
        case .upArrow: return .move(by: -1, extending: extending)
        default: return nil
        }
    }
}

struct VaultSelection: Equatable {
    private(set) var ids: Set<String> = []
    private(set) var cursor: String?
    private(set) var anchor: String?

    var count: Int { ids.count }
    var isEmpty: Bool { ids.isEmpty }
    var isMultiple: Bool { ids.count > 1 }
    var single: String? { ids.count == 1 ? ids.first : nil }

    func contains(_ id: String) -> Bool { ids.contains(id) }

    func ordered(in order: [String]) -> [String] { order.filter { ids.contains($0) } }

    mutating func select(_ id: String) {
        ids = [id]
        cursor = id
        anchor = id
    }

    mutating func toggle(_ id: String, in order: [String]) {
        if ids.contains(id) {
            ids.remove(id)
            if cursor == id || cursor.map({ !ids.contains($0) }) == true {
                cursor = order.first { ids.contains($0) }
            }
            anchor = cursor
        } else {
            ids.insert(id)
            cursor = id
            anchor = id
        }
    }

    mutating func extend(to id: String, in order: [String]) {
        guard let end = order.firstIndex(of: id) else { return }
        let start = anchor.flatMap { order.firstIndex(of: $0) } ?? cursor.flatMap { order.firstIndex(of: $0) } ?? end
        ids = Set(order[min(start, end)...max(start, end)])
        anchor = order[start]
        cursor = id
    }

    mutating func move(by delta: Int, in order: [String], extending: Bool) {
        guard !order.isEmpty, delta != 0 else { return }
        let target: String
        if let c = cursor, let i = order.firstIndex(of: c) {
            target = order[max(0, min(order.count - 1, i + delta))]
        } else {
            target = delta > 0 ? order[0] : order[order.count - 1]
        }
        if extending { extend(to: target, in: order) } else { select(target) }
    }

    mutating func prune(to order: [String]) {
        let shown = Set(order)
        ids.formIntersection(shown)
        if let c = cursor, !shown.contains(c) { cursor = order.first { ids.contains($0) } }
        if let a = anchor, !shown.contains(a) { anchor = cursor }
    }

    mutating func clear() {
        ids = []
        cursor = nil
        anchor = nil
    }

    mutating func click(_ id: String, _ kind: RowClick, in order: [String]) {
        switch kind {
        case .activate: select(id)
        case .toggle: toggle(id, in: order)
        case .extend: extend(to: id, in: order)
        }
    }

    func targets(clicked id: String?, in order: [String]) -> [String] {
        guard let id else { return ordered(in: order) }
        return isMultiple && ids.contains(id) ? ordered(in: order) : [id]
    }

    func activationTarget(in order: [String]) -> String? {
        if let cursor, order.contains(cursor) { return cursor }
        return order.first
    }
}

enum TickSchedule {
    static let lead: TimeInterval = 0.02

    static func nextFire(after date: Date) -> Date {
        Date(timeIntervalSince1970: date.timeIntervalSince1970.rounded(.down) + 1 + lead)
    }
}

enum CodeTiming {
    static let copyNextBelow: TimeInterval = 3
    static let showNextWithin = 5

    static func isTimeBased(_ a: Account) -> Bool { a.type != .hotp && period(of: a) > 0 }

    static func period(of a: Account) -> Int { a.type == .steam ? 30 : a.period }

    static func remaining(_ a: Account, at time: Date) -> Int {
        OTP.remainingSeconds(time: time, period: period(of: a))
    }

    static func windowEnd(_ a: Account, at time: Date) -> Date {
        let p = Int64(period(of: a))
        guard p > 0 else { return time }
        let t = Int64(time.timeIntervalSince1970.rounded(.down))
        return Date(timeIntervalSince1970: TimeInterval((t / p + 1) * p))
    }

    static func shouldCopyNext(_ a: Account, at time: Date) -> Bool {
        isTimeBased(a) && windowEnd(a, at: time).timeIntervalSince(time) < copyNextBelow
    }

    static func showsNext(_ a: Account, at time: Date, selected: Bool) -> Bool {
        isTimeBased(a) && (selected || remaining(a, at: time) <= showNextWithin)
    }
}

enum RingMotion {
    static func animates(from old: Double?, to new: Double, reduceMotion: Bool) -> Bool {
        guard !reduceMotion, let old else { return false }
        return new < old
    }
}

enum VaultEdit {
    static func normalizedTag(_ raw: String) -> String? {
        let t = raw.trimmingCharacters(in: .whitespacesAndNewlines).drop { $0 == "#" }
        let tag = String(t).trimmingCharacters(in: .whitespacesAndNewlines)
        return tag.isEmpty ? nil : tag
    }

    @discardableResult
    static func setPinned(_ pinned: Bool, ids: Set<String>, in accounts: inout [Account], at stamp: Int64) -> Int {
        apply(to: &accounts, at: stamp) { a in
            guard ids.contains(a.id), a.pinned != pinned else { return false }
            a.pinned = pinned
            return true
        }
    }

    @discardableResult
    static func setFolder(_ folder: String, ids: Set<String>, in accounts: inout [Account], at stamp: Int64) -> Int {
        let name = folder.trimmingCharacters(in: .whitespaces)
        return apply(to: &accounts, at: stamp) { a in
            guard ids.contains(a.id), a.folder != name else { return false }
            a.folder = name
            return true
        }
    }

    @discardableResult
    static func addTag(_ raw: String, ids: Set<String>, in accounts: inout [Account], at stamp: Int64) -> Int {
        guard let tag = normalizedTag(raw) else { return 0 }
        return apply(to: &accounts, at: stamp) { a in
            guard ids.contains(a.id), !a.tags.contains(tag) else { return false }
            a.tags.append(tag)
            return true
        }
    }

    @discardableResult
    static func removeTag(_ tag: String, ids: Set<String>, in accounts: inout [Account], at stamp: Int64) -> Int {
        apply(to: &accounts, at: stamp) { a in
            guard ids.contains(a.id), a.tags.contains(tag) else { return false }
            a.tags.removeAll { $0 == tag }
            return true
        }
    }

    @discardableResult
    static func renameFolder(_ old: String, to new: String, in accounts: inout [Account], at stamp: Int64) -> Int {
        let name = new.trimmingCharacters(in: .whitespaces)
        guard !old.isEmpty, !name.isEmpty, name != old else { return 0 }
        return apply(to: &accounts, at: stamp) { a in
            guard a.folder == old else { return false }
            a.folder = name
            return true
        }
    }

    @discardableResult
    static func removeFolder(_ name: String, in accounts: inout [Account], at stamp: Int64) -> Int {
        guard !name.isEmpty else { return 0 }
        return apply(to: &accounts, at: stamp) { a in
            guard a.folder == name else { return false }
            a.folder = ""
            return true
        }
    }

    private static func apply(to accounts: inout [Account], at stamp: Int64,
                              _ change: (inout Account) -> Bool) -> Int {
        var changed = 0
        for i in accounts.indices where change(&accounts[i]) {
            accounts[i].updatedAt = stamp
            changed += 1
        }
        return changed
    }
}
