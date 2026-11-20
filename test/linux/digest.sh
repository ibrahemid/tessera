#!/bin/sh
# Prints one SHA-256 over the names and contents of files, so a host and a container can prove they read the same bytes.
set -eu

usage() {
  printf 'usage: %s tree <repository> | dir <directory>\n' "$0" >&2
  exit 2
}

[ $# -eq 2 ] || usage
cd "$2"
case $1 in
  tree) set -- go spec test/linux ;;
  dir) set -- . ;;
  *) usage ;;
esac

if command -v sha256sum >/dev/null 2>&1; then
  sum() { sha256sum; }
else
  sum() { shasum -a 256; }
fi

find "$@" -type f ! -path 'go/dist/*' | LC_ALL=C sort | while IFS= read -r f; do
  printf '%s\n' "$f"
  cat "$f"
done | sum | cut -d ' ' -f 1
