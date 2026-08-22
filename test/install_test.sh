#!/bin/sh
# Runs install.sh against a locally built tarball served over file://.
# Covers: install into a chosen dir, checksum verification, and the failure
# modes that must never install a binary.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
INSTALLER="${ROOT}/install.sh"
[ -x "$INSTALLER" ] || {
	printf 'install_test: %s is missing or not executable\n' "$INSTALLER" >&2
	exit 1
}

VERSION="9.9.9"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

fail() {
	printf 'FAIL: %s\n' "$1" >&2
	exit 1
}

sha256_of() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		sha256sum "$1" | cut -d' ' -f1
	fi
}

case "$(uname -s)" in
Darwin) OS="darwin" ;;
Linux) OS="linux" ;;
*) fail "unsupported test platform: $(uname -s)" ;;
esac
case "$(uname -m)" in
arm64 | aarch64) ARCH="arm64" ;;
x86_64 | amd64) ARCH="amd64" ;;
*) fail "unsupported test platform: $(uname -m)" ;;
esac

# A stand-in for the release binary: the installer only unpacks and moves it.
mkdir -p "${WORK}/stage" "${WORK}/serve" "${WORK}/bin"
cat >"${WORK}/stage/tess" <<'STUB'
#!/bin/sh
echo "tess version 9.9.9"
STUB
chmod 755 "${WORK}/stage/tess"

ARCHIVE="tess_${VERSION}_${OS}_${ARCH}.tar.gz"
tar -czf "${WORK}/serve/${ARCHIVE}" -C "${WORK}/stage" tess
printf '%s  %s\n' "$(sha256_of "${WORK}/serve/${ARCHIVE}")" "$ARCHIVE" >"${WORK}/serve/checksums.txt"

# 1. Happy path.
out=$(TESS_VERSION="$VERSION" TESS_BASE_URL="file://${WORK}/serve" \
	TESS_INSTALL_DIR="${WORK}/bin" sh "$INSTALLER" 2>&1) ||
	fail "installer exited non-zero:
${out}"
[ -x "${WORK}/bin/tess" ] || fail "installer did not place an executable at ${WORK}/bin/tess"
[ "$("${WORK}/bin/tess")" = "tess version ${VERSION}" ] || fail "installed binary did not run"
printf '%s\n' "$out" | grep -q "Installed tess ${VERSION}" ||
	fail "installer did not report the install:
${out}"
printf '%s\n' "$out" | grep -q 'tess vault init' ||
	fail "installer did not print the next step:
${out}"

# 2. Checksum mismatch must abort before installing.
rm -f "${WORK}/bin/tess"
printf '%s  %s\n' "0000000000000000000000000000000000000000000000000000000000000000" \
	"$ARCHIVE" >"${WORK}/serve/checksums.txt"
if out=$(TESS_VERSION="$VERSION" TESS_BASE_URL="file://${WORK}/serve" \
	TESS_INSTALL_DIR="${WORK}/bin" sh "$INSTALLER" 2>&1); then
	fail "installer accepted a bad checksum"
fi
printf '%s\n' "$out" | grep -q 'checksum mismatch' ||
	fail "expected a checksum mismatch message, got:
${out}"
[ ! -e "${WORK}/bin/tess" ] || fail "installer wrote a binary despite a bad checksum"

# 3. An archive missing from checksums.txt must abort.
: >"${WORK}/serve/checksums.txt"
if out=$(TESS_VERSION="$VERSION" TESS_BASE_URL="file://${WORK}/serve" \
	TESS_INSTALL_DIR="${WORK}/bin" sh "$INSTALLER" 2>&1); then
	fail "installer accepted an unlisted archive"
fi
printf '%s\n' "$out" | grep -q 'not listed in checksums.txt' ||
	fail "expected an unlisted-archive message, got:
${out}"
[ ! -e "${WORK}/bin/tess" ] || fail "installer wrote a binary for an unlisted archive"

# 4. A missing archive must abort.
rm -f "${WORK}/serve/${ARCHIVE}"
printf '%s  %s\n' "$(sha256_of "$INSTALLER")" "$ARCHIVE" >"${WORK}/serve/checksums.txt"
if out=$(TESS_VERSION="$VERSION" TESS_BASE_URL="file://${WORK}/serve" \
	TESS_INSTALL_DIR="${WORK}/bin" sh "$INSTALLER" 2>&1); then
	fail "installer succeeded without an archive"
fi
printf '%s\n' "$out" | grep -q 'download failed' ||
	fail "expected a download failure message, got:
${out}"

printf 'ok: install.sh (4 cases)\n'
