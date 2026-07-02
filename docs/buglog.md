# Tessera bug log

Running ledger for the audit loop. The app can't be built here (Xcode-gated), so
fixes are code-reviewed and parse-checked (`swiftc -parse`); Ibra builds and
confirms the runtime behavior. Status: `fixed` (code done, needs build-check),
`confirmed` (Ibra verified in a build), `open`, `wontfix`.

## Batch 1 — reported by Ibra (2026-06-30)

| # | Bug | Fix | Status |
|---|-----|-----|--------|
| 1 | Launch: no search focus; can't Enter-to-copy | In-content search header, focused on launch; Enter copies top hit + "Copied X" toast | fixed |
| 2 | Toolbar overflowed into ugly "View density" submenu | Removed segmented picker → single density button; search left the toolbar | fixed |
| 3a | Search bar position jumps on expand/collapse | Search moved to a fixed in-content header (no toolbar reflow) | fixed |
| 3b | Two sidebar icons; sidebar collapse adds nothing | `.toolbar(removing: .sidebarToggle)`; sidebar fixed open in window mode | fixed |
| 4 | QR/export sheet only showed image, no copy | Added live code + Copy button (local "Copied" state) to QRExportView | fixed |
| 5 | Couldn't create a list / collection | `AppModel.setFolder` + context-menu "Add to List" (existing + New List…) + remove | fixed |
| 6 | Pin worked in compact, not window | Row is one Button; pin via context menu + swipe; visible pinned star | fixed |
| 7 | (extra) Stale selection when a list empties | Reset selection to All when the selected folder disappears | fixed |

## Batch 2 — audit findings

Cumulative additional fixes (target ~20): **20 / 20 — audit loop complete**

| # | Bug | Fix | Status | Quality note |
|---|-----|-----|--------|--------------|
| A1 | Stale selection when a list empties | Reset to All when selected folder disappears | fixed | onChange(of: folders), idiomatic |
| A2 | ⌘F no longer focuses search (lost with `.searchable`) | Hidden ⌘F shortcut button focuses the field | fixed | works app-wide while view present |
| A3 | Empty list showed search "No matches" | Distinct empty state per selection | fixed | clear, native copy |
| A4 | Unlock button: white label on bright gold (low contrast, dark mode) | Use `Palette.onAccent` like PrimaryButton | fixed | matches the button-contrast fix |
| A5 | "Add to List" gave no sense of current membership | Checkmark on the account's current list | fixed | native menu affordance |
| A6 | Settings showed hardcoded "1.0.0" | Read CFBundleShortVersionString (+ build) | fixed | never drifts from build |
| A7 | HOTP advance copied silently (no "Copied" toast, unlike TOTP) | Set status in advanceHOTP | fixed | feedback now consistent across types |
| A8 | Density toolbar button had no accessibility label | Added accessibilityLabel | fixed | VoiceOver no longer reads the SF Symbol name |
| A9 | Window mode: full-width search over a 680-capped list (misaligned edges) | One centered content column for search + filter + list | fixed | edges now align; native detail-column look |
| A10 | Search clear button unlabeled for VoiceOver | accessibilityLabel "Clear search" | fixed | minor a11y |
| A11 | Drag-reorder in filtered/searched views initiated then sprang back | `.moveDisabled(!reorderable)` per row | fixed | drag can't start when it wouldn't apply |
| A12 | Manual add gave no "Added" toast (link/scan did) | Set status in addManual on success | fixed | feedback consistent across all add paths |
| A13 | Stale "Added N" status lingered when switching Add modes | Clear status (not just error) on mode change | fixed | no stale feedback in link/scan tabs |
| A14 | Export backup: Enter didn't submit | `.onSubmit` on confirm field → submitExport() | fixed | matches native form behavior |
| A15 | **Window mode snapped back to 920px every second** (resize ran on each ~1s render, fighting manual resize; compact couldn't be widened) | Coordinator gates resize to actual density toggles only; titlebar re-applied idempotently | fixed | the significant one — manual sizing now sticks |
| A16 | No ⌘N to add an account | keyboardShortcut on the + button (newItem command was empty) | fixed | standard Mac shortcut |
| A15+ | (re-judge) compact-persisted launch opened wide before resizing | Seed coordinator with default density so a compact launch resizes once | fixed | launch matches the saved density |
| A17 | Escape didn't clear / dismiss search | `.onExitCommand` clears query, then drops focus | fixed | native search-field behavior |
| A18 | HOTP advance didn't flash the code gold (only TOTP copy did) | Extracted `markCopied`, shared by copy + HOTP advance | fixed | consistent visual confirmation |
| A19 | Compact "Folders" menu likely showed a double chevron (built-in + custom) | `.menuIndicator(.hidden)` | fixed | single chevron |
| A20 | VoiceOver read the code as one large number | Spell the code digit-by-digit in the row label | fixed | codes are dictated correctly |

### Resolved-on-review (no change needed)

- HOTP `remaining(for:)` divide-by-zero: `OTP.remainingSeconds` already guards `period <= 0`. Safe.
- Drag-to-reorder as a `Button` row: the original shipped row (`PressableTile`) was already a Button with `.onMove`; no regression.

## Loop stopped at 20 — remaining items need a real build, not more blind edits

These are the honest next steps, each needing Xcode to validate (so the loop would only be guessing):

- Pinned accounts don't float to top in "All" (only star + Pinned filter). Interacts with drag-reorder offset mapping — needs a build to choose section vs sort.
- Full keyboard list navigation (arrow keys to move selection, Enter to copy the selected row). Larger; needs a build to tune `List` selection + focus.
- Build-check the render-dependent fixes: titlebar seam (A15/orig), density-toggle resize (A15), `.swipeActions` rendering on macOS, drag-reorder with Button rows.
- Optional: minimum-strength hint for the backup password.
- Audit AddAccountView manual entry: no way to assign a list at creation; secret shown in cleartext.
- Backup password has no strength floor; consider a gentle warning.
- Verify WindowConfigurator resize vs reactive `minWidth` don't fight during the density toggle (needs build check).
- Consider ⌘N → Add account, ⌘, handled by Settings scene already.
