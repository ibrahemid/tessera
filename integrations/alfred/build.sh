#!/bin/sh
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
OUT_DIR="$ROOT/.status/dist"
OUT="$OUT_DIR/Tessera.alfredworkflow"
ICON="$ROOT/swift/App/Resources/Assets.xcassets/AppIcon.appiconset/icon_256.png"

plutil -lint -s "$HERE/workflow/info.plist"
[ -f "$ICON" ] || { printf 'error: icon not found at %s\n' "$ICON" >&2; exit 1; }

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT INT TERM

cp "$HERE/workflow/info.plist" "$HERE/workflow/tess.js" "$STAGE/"
cp "$ICON" "$STAGE/icon.png"

mkdir -p "$OUT_DIR"
rm -f "$OUT"
(cd "$STAGE" && /usr/bin/zip -q -X -r "$OUT" .)
printf '%s\n' "$OUT"
