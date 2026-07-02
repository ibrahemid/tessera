# Tessera — Design System & Brand

The marketing site for Tessera: a CLI-first, open-source 2FA authenticator for macOS,
free, local-first, App Store-bound. This document is the source of truth for the site's
visual identity. Written before code; the implementation derives every color and type
decision from here.

---

## 1. Brand thesis

**Tessera** (Latin) is a single small tile in a mosaic. The product makes that literal:
every two-factor account is a *tessera* — a colored tile holding an issuer, a six-digit
code, and a countdown ring. Your whole 2FA collection is the **mosaic**: many small,
verified pieces that together form your identity.

Two truths the brand holds in tension, and both are real to the product:

- **Heritage / craft** — mosaics are ancient; Byzantine mosaics famously used *gold*
  tesserae for the most important figures. Tessera's accent is gold. The work is careful,
  auditable, made to last.
- **Modern / precise** — it's a Mac-native, CLI-first security tool. Monospace codes, a
  30-second TOTP heartbeat, real cryptography. Trust through transparency.

The site's job: **make 2FA feel easy and trustworthy at the same time.** Lead with trust
(open-source, local-first, no account, no tracking), teach the newcomer gently, and let
the living mosaic do the convincing.

---

## 2. Why this isn't a generic AI-default look

The three current AI-design defaults, and how Tessera avoids each:

1. **Cream + high-contrast serif + terracotta.** Tessera's canvas is *ink* (or warm
   paper), the accent is *gold* (tied to the name and the app icon, not terracotta), and
   display type is a confident grotesque, not a literary serif.
2. **Near-black + one acid accent.** Tessera's dark theme is warm ink with a *curated
   multi-hue mosaic* (the per-issuer tile colors) anchored on gold — not a single neon.
   And it ships a real, equally-considered light theme.
3. **Broadsheet hairlines, zero radius, dense columns.** Tessera's structural unit is the
   *squircle tile* with soft premium shadows and generous rhythm — the opposite of a
   newspaper grid.

Gold-on-ink with a colored-tile mosaic, monospace carrying the voice, is a palette and
structure none of the three defaults occupy — and it's the one metaphor that *is* the
product and its etymology.

---

## 3. Directions considered

**A — The Living Mosaic (chosen).** Gold-on-ink, warm and crafted. The hero is a live
mosaic of real-looking authenticator tiles that tessellate into place on load, codes
ticking, rings sweeping on the 30s heartbeat. The tile is the structural unit of the
whole page. Trust + approachability in one image. Distinct from the sister site (writ),
which owns the code-editor metaphor.

**B — Terminal-native.** Lean entirely into the CLI: an app-window `tess watch` terminal
as the spine, monospace-dominant, syntax-colored. *Rejected as the identity* — too close
to writ (same house), and too developer-coded for the Gen-Z / non-technical audience the
brief wants to welcome. The best of it survives as **one** section (a real `tess watch`
moment), not the spine.

**C — Calm Apple-HIG minimalism.** Light, restrained, big device mockup, system type.
*Rejected* — lower distinctiveness; it's the safe Mac-app-landing look and drifts toward
generic minimal SaaS.

**Chosen: A.** It's literally the product, dodges all three AI defaults, balances the
security pitch (crafted, premium, trustworthy) with the teaching goal (colorful, friendly,
unintimidating), and differentiates from writ while holding the same craft tier.

---

## 4. Color

Two complete themes. Light is default per house rule (never dark-only); a toggle persists
choice and respects `prefers-color-scheme` on first visit. Gold is the single UI accent in
both; the multi-hue mosaic colors appear only inside live tiles and as small issuer dots.

### Light — "Atrium" (the lit gallery wall)
| Token | Hex | Use |
|---|---|---|
| `--paper` | `#FBFAF7` | page canvas, warm near-white |
| `--surface` | `#FFFFFF` | raised tiles / cards |
| `--surface-sunken` | `#F3F1EC` | wells, code blocks bg |
| `--ink` | `#17181D` | primary text |
| `--ink-soft` | `#595E6B` | secondary text |
| `--ink-faint` | `#8A8F9C` | captions, meta |
| `--line` | `rgba(23,24,29,0.09)` | hairlines, tile borders |

### Dark — "Tessellate" (ink + gold, Byzantine)
| Token | Hex | Use |
|---|---|---|
| `--paper` | `#0E0F13` | page canvas, deep ink |
| `--surface` | `#16181F` | raised tiles / cards |
| `--surface-sunken` | `#0A0B0E` | wells, code blocks bg |
| `--ink` | `#ECEDF1` | primary text |
| `--ink-soft` | `#9398A6` | secondary text |
| `--ink-faint` | `#6A6F7C` | captions, meta |
| `--line` | `rgba(255,255,255,0.09)` | hairlines, tile borders |

### Gold (brand accent — both themes)
| Token | Light | Dark | Use |
|---|---|---|---|
| `--gold` | `#B07D1A` | `#E3B23C` | accent text/links (AA on canvas) |
| `--gold-solid` | `#C8901E` | `#E3B23C` | filled buttons / the gold tessera |
| `--gold-bright` | `#E3B23C` | `#F0C45A` | highlights, ring leading edge |
| `--on-gold` | `#1A1206` | `#1A1206` | text on a gold fill |

### Mosaic — per-issuer tile colors (live tiles + issuer dots only)
`#2BB3A3` teal · `#E5643E` coral · `#7C6CF0` violet · `#3B6FE0` blue · `#2E9E5B` green ·
`#E3B23C` gold. Each rendered as a soft tile with a tinted background (12% alpha) and a
saturated dot/ring.

### Time semantics (countdown ring)
Full window reads gold/green; as the 30s window depletes past ~25s the leading edge shifts
toward `--coral` to signal "about to roll." Never alarm-red in the resting state.

---

## 5. Typography

Three self-hosted families (no Google CDN — a no-tracking app's site tracks nothing).
Personality is spent on the **display face** and the **monospace**, which is elevated to a
co-lead because codes and the CLI *are* the product.

- **Display — Schibsted Grotesk** (700 / 800). Headlines and the wordmark. Confident,
  geometric-humanist, modern-trust register. Set tight: `-0.035em`, large optical sizes.
  Distinct from writ's Bricolage Grotesque.
- **Body — Hanken Grotesk** (400 / 500 / 600). Warm, friendly, highly readable — carries
  the gentle teaching copy without feeling clinical.
- **Mono — IBM Plex Mono** (400 / 500 / 600). Codes (tabular, grouped `123 456`), CLI
  blocks, eyebrows, tile labels, section markers. This is the brand's *voice*: every
  structural label is a small monospace cue, echoing the terminal.

Type scale (fluid, clamp): display `clamp(2.4rem, 6vw, 4.5rem)`; h2 `clamp(1.6rem, 3vw,
2.4rem)`; lede `clamp(1.05rem, 1.6vw, 1.3rem)`; body `1rem/1.6`; mono-eyebrow `0.78rem`,
uppercase, `0.12em` tracking, gold.

Numerals use `font-variant-numeric: tabular-nums` everywhere a code or countdown appears.

---

## 6. Layout & spacing

- 4px base unit. Section vertical rhythm `clamp(5rem, 10vw, 8rem)`.
- Content max-width `72rem`; prose max-width `38rem`.
- **Tile geometry is the system.** Cards are squircles: radius `18px` (small), `22px`
  (large), `1px` `--line` border, soft layered shadow. Feature blocks, the live mosaic,
  the get-it cards — all read as tesserae.
- Background texture (subtle): a faint mosaic-grout grid, `--line` at very low alpha, only
  behind the hero, masked out toward content. Never noisy.

---

## 7. Motion — "tessellation"

- **Signature load:** hero tiles settle into their grid positions with a soft spring
  (`cubic-bezier(.22,1,.36,1)`), staggered ~40ms, slight scale-from-0.92 + fade. The
  mosaic *assembles*.
- **Recurring micro-motion:** the **countdown ring** sweeps linearly over the live 30s
  TOTP window; on rollover the code does a quick gold "verify" pulse and the digits
  cross-fade. This is the heartbeat — calm, not busy.
- **Scroll reveals:** sections fade-rise 12px once, via IntersectionObserver, staggered.
- **Hover:** tiles lift 2px with a tightened shadow; buttons get a gold glow.
- **Easing tokens:** `--ease cubic-bezier(.33,1,.68,1)`, `--spring cubic-bezier(.22,1,.36,1)`.
- **`prefers-reduced-motion`:** no assembly, no ticking, no reveals — the mosaic renders
  fully formed and static; countdown rings show a fixed resting state. Everything legible
  and complete without motion.

Implementation: Astro static + **one** small vanilla-TS island for the mosaic + theme
toggle. No React. Keeps the LCP headline unblocked and the CF static deploy lean.

---

## 8. Signature element — the tessera tile

A rounded-square authenticator tile:

```
┌─────────────────────────┐
│ ●  GitHub          ▦     │   ● issuer dot (mosaic color)   ▦ ring
│                          │
│   824 159                │   6 digits, mono, tabular, grouped 3·3
│   ◜‾‾‾‾‾◝  18s           │   countdown ring + seconds remaining
└─────────────────────────┘
```

It appears (1) en masse as the living hero mosaic, (2) singly to teach "what a code is,"
(3) abstracted as section markers and the wordmark glyph (a 2×2 of tiles, one gold). One
element, many scales — the page is remembered by it.

---

## 9. Voice & copy

Plain, confident, never corporate. Teach without lecturing; show, don't sell. Lead every
trust claim with the concrete mechanism ("argon2id + XChaCha20-Poly1305," "no server to
breach"), not adjectives. No filler, hype, decoration, repeated claims, or hollow status
labels — see `~/.claude/rules/ui-copy.md` and the copy-gate scanner. Honesty rules:

- **Both the CLI and the macOS app ship at release** (owner's call). The get-it section
  presents both as available; the App Store button needs the real listing ID before
  deploy (placeholder `appStore` const in index.astro), and the GitHub repo must be public
  so `go install …` and every "read the source / auditable" claim resolve.
- **No paid tier, no sync tease.** v1 is free and open source; no pricing table, no
  "planned paid sync." Monetization is a later decision, kept off the site.
- **Don't overclaim against competitors.** Free/OSS authenticators exist (Proton, 2FAS,
  Ente); Touch ID is free elsewhere. The defensible edge is the CLI-first authenticator
  sharing one locally-encrypted vault with a menu-bar app — lead with that, not a paywall jab.
- Don't imply affiliation with Google / Microsoft / Steam (App Store 5.2): "works with any
  TOTP service."

Contact: `support@ibrahemid.com`. Source: `github.com/ibrahemid/tessera`.

---

## 10. Sections (home)

1. **Hero** — wordmark, thesis headline, lede, CTAs, the living mosaic.
2. **What is 2FA / why it matters** — gentle explainer for newcomers; one tile dissected.
3. **Features** — TOTP/HOTP/Steam, bulk import, QR, CLI, Touch ID, local-first crypto.
4. **The CLI moment** — a real `tess watch` terminal (the best of Direction B).
5. **Trust / open-source** — local-first crypto explained, auditable, no account/tracking.
6. **Platforms & roadmap** — macOS + CLI now; iPhone, Android planned, without overpromising.
7. **Free** — one panel: everything free and open source, no tier, no account.
8. **Get it** — App Store + CLI (both at release).
9. **Footer** — privacy, support, source, license.

Plus full `/privacy` and `/support` pages (Apple requires the URLs) and a `/404`.
