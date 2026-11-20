import SwiftUI

struct AutoLockSection: View {
    @AppStorage private var minutes: Int

    init(defaults: UserDefaults) {
        _minutes = AppStorage(wrappedValue: AutoLock.defaultMinutes, AutoLock.minutesPrefKey, store: defaults)
    }

    var body: some View {
        Section {
            VStack(alignment: .leading, spacing: 6) {
                Picker("Lock when idle for", selection: $minutes) {
                    ForEach(AutoLock.choices, id: \.self) { Text(AutoLock.label(for: $0)).tag($0) }
                }
                Text("After an idle lock, opening the vault asks for Touch ID or your Mac password.")
                    .font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }
}
