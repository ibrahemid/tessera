# Owed assets

Two Screen Studio loops. The home page has a two-up band under the app window that renders only when all three files of a loop exist in `public/media/`; until then the band is absent (no placeholder). Slot code: `src/pages/index.astro`, `loops`.

## Common spec

- Record the real app at its default window size, 2x, both light and dark appearance are not needed: record in the appearance the app looks best in (light, matching the app capture already on the page).
- Frame: 1440x1044 px (the same box as `src/assets/app-vault-light.png`), window fills the frame, no desktop, no shadow, no cursor zoom; cursor smoothing on, click zoom off.
- Length 6 to 10 s, seamless loop (end state equals start state or cuts on a still frame).
- Export "Custom", 1440 px wide, no audio. Then:

```sh
ffmpeg -i raw.mov -an -c:v libx264 -profile:v high -pix_fmt yuv420p -crf 24 -preset slow -movflags +faststart NAME.mp4
ffmpeg -i raw.mov -an -c:v libvpx-vp9 -crf 34 -b:v 0 -row-mt 1 NAME.webm
ffmpeg -i NAME.mp4 -frames:v 1 poster.png && cwebp -q 88 poster.png -o NAME-poster.webp
```

- Budgets: mp4 under 1.2 MB, webm under 0.8 MB, poster under 120 KB. Frame 0 must carry the message on its own (reduced motion and Low Power Mode visitors only see it).
- Drop the three files in `site/public/media/`, rebuild, render (`node scripts/render-widths.mjs dist --out design/renders/loops`).

## 1. `touchid`

Files: `touchid.mp4`, `touchid.webm`, `touchid-poster.webp`.
Content: the locked window ("Tessera is locked", Unlock with Touch ID) → the system Touch ID prompt → the vault with live codes. Frame 0 = the locked window. Demo vault only; the accounts on screen must match the throwaway set (AWS, Cloudflare, Fastmail, GitHub, Steam, Tailscale) so the page stays consistent.

## 2. `qr-scan`

Files: `qr-scan.mp4`, `qr-scan.webm`, `qr-scan-poster.webp`.
Content: a browser window showing a 2FA setup QR (use `tess export --qr` output for a demo account, never a real one) beside the app → the Scan command → the account appears in the list. Frame 0 = QR visible and the app beside it. Because the frame is the app window only, stage the QR inside the app's own capture area (the on-screen scan picker) rather than a second window.

## Not used

`docs/appstore-assets/1.0.3/*.png` are composites (window without title bar on a store background with a baked shadow). They stay out of the site; the page uses live window captures only.
