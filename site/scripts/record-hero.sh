#!/bin/sh
# Re-records the hero loop and the four stills from the real tess binary against a
# throwaway vault, then encodes them into public/media and src/assets.
# Needs go, vhs, ffmpeg and cwebp on PATH. Run from anywhere:
#   sh site/scripts/record-hero.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
site=$(cd "$here/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/bin"
go -C "$site/../go" build -o "$work/bin/tess" ./cmd/tess
cd "$work"
export PATH="$work/bin:$PATH"
export TESSERA_PASSPHRASE='correct horse battery staple'

# the QR that gets added on camera comes from a second vault, so the demo vault starts without GitHub
export TESSERA_VAULT=seed.json
tess vault init >/dev/null
tess add "otpauth://totp/GitHub:ibra?secret=JBSWY3DPEHPK3PXP&issuer=GitHub" >/dev/null
mkdir qr
tess export --qr qr github >/dev/null
mv qr/*.png github-2fa.png
rm -rf qr seed.json seed.json.lock

export TESSERA_VAULT=vault.json
tess vault init >/dev/null
tess add "otpauth://hotp/AWS:root?secret=JBSWY3DPEHPK3PXP&issuer=AWS&counter=4" >/dev/null
tess add "otpauth://totp/Cloudflare:ibra%40example.com?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=Cloudflare" >/dev/null
tess add "otpauth://totp/Fastmail:ibra%40fastmail.com?secret=KRSXG5DFOIQHI2DJONQSA2LTEBUW4ZJAMVXGG4TZOB2GSZLE&issuer=Fastmail" >/dev/null
tess add "otpauth://totp/Tailscale:ibra?secret=ORSXG5DFOIQHI2DJONQSAZLYMFWXA3DF&issuer=Tailscale" >/dev/null
tess add "otpauth://totp/npm:ibra?secret=NZYW2IDUMVZXIIDTMVRXEZLU&issuer=npm" >/dev/null
tess add "otpauth://totp/Steam:ibra?secret=JBSWY3DPEHPK3PXP&issuer=Steam" --type steam >/dev/null

# start at the top of a 30 s TOTP window so the bars are full in the poster
sleep $((30 - $(date +%s) % 30))
vhs "$here/hero.tape" >/dev/null
vhs "$here/stills.tape" >/dev/null

mkdir -p "$site/public/media"
ffmpeg -v error -y -i hero.mp4 -an -c:v libx264 -profile:v high -pix_fmt yuv420p -crf 24 -preset slow -movflags +faststart "$site/public/media/hero.mp4"
ffmpeg -v error -y -i hero.mp4 -an -c:v libvpx-vp9 -crf 34 -b:v 0 -row-mt 1 "$site/public/media/hero.webm"
ffmpeg -v error -y -i "$site/public/media/hero.mp4" -frames:v 1 poster.png
cwebp -quiet -q 88 poster.png -o "$site/public/media/hero-poster.webp"

node "$here/crop-stills.mjs" still-vault-status.png still-not-found.png still-show.png still-codes.png
cp still-vault-status.png still-not-found.png still-show.png still-codes.png "$site/src/assets/"

ffprobe -v error -select_streams v:0 -show_entries stream=width,height -show_entries format=duration -of default=nw=1 "$site/public/media/hero.mp4"
ls -l "$site/public/media" "$site"/src/assets/still-*.png
