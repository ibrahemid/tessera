# Tessera — usability rebuild plan

## What's wrong (root cause)
The app is `MenuBarExtra(.window)` only, `LSUIElement = YES` (no Dock app, no real window).
A menu-bar popover auto-dismisses on focus loss, so:
- opening the Add-account sheet → focus leaves → popover closes
- Settings opens a separate window → popover vanishes
- there is no actual app to return to

Fix is architectural: make Tessera a **real windowed Mac app**, with the menu bar as an
optional quick-access layer on top — not the only surface.

## Decision
- Primary: a normal macOS app — Dock icon, menu bar menu, a resizable main window.
  `LSUIElement = NO`.
- Secondary: keep `MenuBarExtra` for glanceable codes + quick copy. Optional, default on.
- One shared `AppModel` drives both surfaces.
- Daily unlock = **Touch ID** (Secure Enclave). Never a passphrase in normal use.

## Phase 0 — groundwork
- Store name → "Tessera: 2FA Authenticator" (bundle display name stays "Tessera").
- Make `AppModel` a single instance injected into both the window and the menu bar.

## Phase 1 — windowed app shell
- `WindowGroup` main window with the vault: resizable, sensible min size, toolbar
  (search, add, lock), standard menus (About, Settings ⌘,, Quit, Copy).
- Adaptive layout via `NavigationSplitView`: sidebar = All / Pinned / Folders; detail =
  the code list. Scales with the window instead of a fixed 380×500.
- `LSUIElement = NO`; app activates and shows the window on launch.

## Phase 2 — fix the broken flows
- Add account → a **sheet on the main window** (no dismiss). From the menu bar, activate
  the app + open the window, then present.
- Settings → lives correctly now that a persistent main window exists (in-window pane,
  plus the standard ⌘, Settings). Nothing orphans or disappears.
- Verify: clicking anywhere inside the app never closes it.

## Phase 3 — auth: biometrics-first, no daily password
- Daily unlock = Touch ID via the Secure Enclave wrap. No passphrase prompt in normal use.
- Onboarding: enable Touch ID + set a **one-time recovery passphrase/key** (only used if
  biometrics is unavailable or on a new Mac). Nudge an encrypted backup.
- Auto-lock: on sleep / after N minutes / on quit; re-unlock with Touch ID.
- PIN note: a bare PIN cannot protect the on-disk vault (offline brute-force). On macOS,
  secure usability = Touch ID + recovery. Recommend NO standalone PIN for v1; a PIN could
  later be layered on top of the SE wrap as convenience only.

## Phase 4 — real verification (the step I skipped)
- Build, **launch the actual app, click every flow** (add, copy, search, settings, lock,
  unlock, menu bar), screenshot the real windows, fix what breaks.
- Then collect the rest of your feedback and iterate before any App Store resubmit.

## Out of scope this round
Sync, remaining feedback list, resubmission. Get it usable first.
