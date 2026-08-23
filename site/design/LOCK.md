# Reference lock, 2026-08-24

Source material: the 16-site extraction in `marketer/reviews/2026-08-22/design-standard.md` (§1 patterns, §2 tells, §3b skeleton, §6 grid) and its raw per-site pulls. Refero MCP was not signed in for this session; nothing below comes from model memory, every line traces to that document or to a measured render in `AUDIT.md`.

## Brief

Designing the landing site for Tessera (2FA in the terminal and a native Mac app, one encrypted vault) for developers on macOS, on the web.
Goal: install `tess` or get the app, in under one scroll.
Tone: a security tool written by someone who ships. Facts, code links, no adjectives.
Main objection: "why not Ente / Proton", and "is this safe". Answered by the recording (it works), the Mac App Store badge (reviewed, sandboxed) and the security rows (each one links to its file).
Must remember: the `tess watch` table with live countdown bars, readable at 1440 without zoom.
Constraints: Astro 5 static, Tailwind 4, zero JS beyond theme toggle, copy and play gating; light default with system dark; allowed claims per `marketer/apps/tessera/profile.md`; keep the badge, trust + brew lines, curl line, tarballs, smart banner meta, JSON-LD, sitemap, Umami, privacy disclosure.
Path: visual exploration (three wireframes), then direct build.

## References

**Primary: ghostty.org.** Terminal-first dark hero where the terminal is a real session, not an illustration; Pretendard display + JetBrains Mono; per-component widths from 425 to 1300 px, so text and media have different caps in the same page. Preserve: the terminal is the largest object on the first screen, dark-native, mono only inside the terminal and for commands.

**Secondary: zed.dev.** Hairline columns instead of cards; subhead at `max-w-lg`, content at `max-w-[1100px]`; moderate-weight display type with negative tracking (`.h0` weight 340, `-.02em`); "Clone source" as a first-class button, the source link is the proof. Borrow: the hairline column system for the one-vault pillars and the security rows; the source-as-proof CTA pattern.

**Secondary: culturedcode.com/things and cleanshot.com.** Things: the narrowest container in the study (900 px) with `.panorama` media that breaks out past it; CleanShot: `.column.-left 500px / .column.-right 600px` asymmetric split and one loop per feature. Borrow: the app window cut by the viewport edge with a hairline and no shadow; asymmetric split for the app band; no feature grid, one capture per claim.

## Lock

```
Primary direction: ghostty-style terminal-led dark hero on a light page (the product's own split: dark terminal, light Mac app)
Preserve: real recording as the largest object on the first screen; mono only inside the terminal, commands, codes and paths; two weights (400 body, 700 display) with negative tracking carrying hierarchy; text at a measure, media never; hairlines, no cards, no shadows
Borrow only: zed hairline columns + source-link proof; Things/CleanShot viewport-cut window + asymmetric split
Role rules: gold is the accent for links, the primary button and the wordmark tile only; never a background, never a band; terminal palette (the vhs theme) stays inside the terminal; green `$` prompt stays inside command blocks
Media strategy: hero = vhs recording of the real binary, 1280x720 (16:9), poster = frame 0; app = real window captures light + dark (1440x1044 @2x) cut by the viewport edge; secondary = window crops from the 1.0.3 store frames (lock screen, import sheet) until the owed loops land; no CSS mocks, no placeholders
Reject: centered hero; card grids; icon + three words; gradient blobs; a hero that bleeds past the header's edge; two maximum widths on one page; a mono kicker on every section; decorative serif or italic word swaps; dark-only page
Token commitments: paper #fbfaf7 / ink #17181d (light), #0e0f13 / #ecedf1 (dark); band #0e0f13 in both themes; gold #b07d1a light, #e3b23c dark; hairline rgba(23,24,29,.09); radius 10 px on media, 0.6rem on buttons; Schibsted Grotesk 700 display, Hanken Grotesk 400 body, IBM Plex Mono 400
```

## Composition rules (from AUDIT.md findings 1, 3, 4, 7)

1. One composition width: `--wide` = 1440 px cap, shared by the header, every media block, the footer. `--content` = 1100 px cap for text. Both center together above 1440 + gutters; backgrounds bleed to the viewport.
2. The hero panel is `wide-end` aligned, never past it. Its box is `aspect-ratio: 16 / 9`, the recording's own ratio, so nothing crops.
3. Text gets a measure (60ch). Media never does.
4. No two adjacent bands share ground and column. Sequence on `/`: ink hero (wide media) / paper install strip (content, two columns) / paper app band (bleed-right media) / sunken one-vault (wide hairline columns) / paper security rows (content) / ink close (content, display type) / ink footer (wide).
5. Heading sizes step: display, section, section, section, sub, display.

## Decision ledger

| Decision | Source | Role | Why |
|---|---|---|---|
| Terminal is the hero, 16:9, wide-aligned | ghostty; standard §3b row 2; AUDIT 1-2 | media | the product's proof; the crop is the reported bug |
| Poster = `tess watch` table, 7 rows of 19 used | standard §4.3 "poster carries the message"; AUDIT 2 | media | iPhone and reduced-motion visitors only see frame 0 |
| Light page, dark band, system dark honoured | ibrahem-apps rule (never dark-only); standard §1.6 (match the product: dark terminal, light Mac app) | canvas | the app is light by default, the terminal is dark |
| Hairline columns, no cards | zed; anti-slop #2 | surfaces | nothing on the page is interactive enough to be a card |
| Window cut by the viewport edge, hairline only | Things panorama, CleanShot columns; standard §2.9 | media | "a screenshot cropped by the viewport reads as the app" |
| One width cap for header, media and footer | AUDIT 4, 7 | layout | three left edges at 2560 was the alignment complaint |
| Security rows each link to a file | zed "Clone source"; profile tone rules | proof | "point at the repo, don't adjective" |
| Compare table stacked at 390 | AUDIT 9 | layout | no horizontal scroller inside a vertical page |
| Badge + brew line in the hero, full install below | standard §1.7 (CTA carries operational detail), critique item 4 | CTA | version, OS floor and licence under the button |
