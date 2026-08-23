# Site audit, 2026-08-24

Pre-revamp: commit 324c535, renders in `marketer/reviews/2026-08-22/shots/tessera-{fold,full,mobile}.png`.
Revamp: main HEAD 247c4f0 (deployed 2026-08-23), rendered here at 1440/1920/2560/390: `design/renders/head-before/`, measurements in `checks.txt`.

## What the revamp broke

1. **Hero panel overflow.** `HeroVideo` with `bleed` sets `width: calc(100% + 3rem)`, so the media box is 776 px wide in a 728 px cell at 1440 and the band clips it (`hero-media` right edge 1488 on a 1440 viewport; 1968 on 1920; 2608 on 2560). The 48 px that fall off are the countdown column (`24s`) and the end of the key hint (`q quit`). The recording reads as cut because it is.
2. **Recording content.** 1000x560 at 24 px Menlo with 36 px padding: the `tess watch` table uses 7 of 14 rows, so half of every frame is empty ink, and the countdown column sits at the right margin where the crop lands. Six accounts, no GitHub, and the secondary commands (`tess add`, `tess code -c`, `--json`) run as three lines of small output under a mostly empty screen. At 390 the seconds column is clipped inside the recording itself (`21` without `s`).
3. **Empty space.** Hero band is 910 px tall at 1440 with the copy occupying 60% of its cell height and the panel 436 px; at 2560 the band is 1250 px with a 624 px panel and nothing else. The app band keeps `padding-bottom: 7rem` on the copy and a 3.5rem negative margin on the shot; at 2560 the window stops at 1860 px on a 2560 viewport, so the "cut by the viewport edge" idea stops working and the window floats in cream.
4. **Column alignment.** Three different left edges on one page at 2560: header inner (1600 cap, x=480), `content` (1100 cap, x=730), `u-wide` blocks (x=480). The one-vault heading sits at 730 and its three pillars at 480; the compare heading at 730 and its table at 480; the footer at 480. At 1440 header and content differ by 4 px (`--gap` is applied twice in the grid math vs once in the header padding).
5. **Caption placement.** The "Recorded from the real CLI" caption hangs under a panel that is itself clipped, in 12 px mono, outside the panel's own frame; at 1440 it runs to the viewport edge with no right gutter.
6. **Band rhythm.** Ink / sunken / cream / sunken / cream / sunken / cream / ink: strict alternation. The install band and the one-vault band are both sunken with a 1100 px column, so two bands apart they share width and ground. The security rows and the compare table are both `dl`/`table` hairline lists back to back with the same 1.4rem row padding.
7. **Wide-screen drift.** `--wide` is `min(1600px, 100% - gap*2)` but `--content` is `min(1100px, …)`, so above 1180 px the page has two unrelated maximum widths and every section picks one. There is no composition max width: the hero bleeds to the viewport edge at 2560 while the text column stays at 1100, leaving a 1.4:1 split that reads as a layout bug rather than an asymmetric composition.
8. **Panel aspect.** The video is 1000x560 (1.786) but the poster `<img>` and the `<video>` are sized by CSS width with `height: auto` inside a box whose width exceeds the cell, so the rendered aspect is preserved by clipping, not by fitting.
9. **390.** The compare table forces `min-width: 40rem` inside an `overflow-x: auto` wrapper, a horizontal scroller inside a page that otherwise never scrolls sideways. Tarball sha256 lines break mid-hash.

## What the old version did better

- Every column on the page shared one left edge (one `--maxw` container). Nothing overflowed at any width; nothing was clipped.
- The hero had a real composition: a 12-column split with text left and a 2x4 tile grid right that filled its cell with no dead rows. The fold carried the headline, lede, two CTAs and the trust line without scrolling.
- The kicker/heading/lede stack gave each section a consistent entry; section spacing was even.
- The fold at 390 showed headline, lede, both CTAs and the first product image without the hero band running 1.4 screens tall.

## What the old version got wrong (do not restore)

- CSS-drawn tiles and a hand-written terminal instead of the product. No real capture anywhere on the page.
- Eight sections at one width, one heading size, one card style (the template rhythm the standard calls out).
- Cards with drop shadows for features and trust; an icon grid of six claims; "Menu bar" copy.
- No /cli, /security, /vault-format, /vs pages; no install detail; no JSON-LD; no sitemap.

## What must survive from the revamp

Real vhs recording of the real binary (re-recorded), real window captures, the page grid with named lines, two-weight type, hairline rows, the install section (badge, trust + brew lines, install.sh curl line, tarballs with sha256, go install), JSON-LD, sitemap, Umami tag, smart banner meta, privacy disclosure, all eleven pages.

## Fix list (ordered)

1. Re-record: 1280x720 frame, 22 px font, seven accounts, `tess watch` fills the rows, countdown column inside the safe area, poster = frame 0 of the exact encode.
2. Hero panel: `aspect-ratio` from the recording, `width: 100%` of its grid cell, no bleed class; the cell ends at the same right edge the header ends at.
3. One composition width: `--wide` becomes the header width and the footer width; `--content` stays the text column; every band's media uses `wide`, every band's text uses `content`; above 1600 both are capped and centered together.
4. Bands: no two adjacent bands with the same ground, and no two bands anywhere with the same ground and the same column.
5. Caption inside the panel's frame as a status line, or removed.
6. 390: compare as stacked rows; hashes under a `details`.
