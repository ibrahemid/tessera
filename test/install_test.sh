#!/bin/sh
# Runs install.sh against locally built releases served over file://, with
# stand-ins for curl (to log flags and answer the latest-release lookup),
# cosign, and uname. Covers the checksum and signature checks and the failure
# modes that must never install a binary. Nothing here reaches the network.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
INSTALLER="${ROOT}/install.sh"
[ -x "$INSTALLER" ] || {
	printf 'install_test: %s is missing or not executable\n' "$INSTALLER" >&2
	exit 1
}

VERSION="9.9.9"
OLD_VERSION="1.1.0"
IDENTITY="https://github.com/ibrahemid/tessera/.github/workflows/release.yml@refs/tags/v${VERSION}"
ISSUER="https://token.actions.githubusercontent.com"
GOOD_BUNDLE="signed-by-release-workflow"
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

REAL_CURL=$(command -v curl) || fail "curl is required"
mkdir -p "${WORK}/bin" "${WORK}/tools" "${WORK}/cosign" "${WORK}/uname"

# make_release writes a release for version $2 into directory $1: a stand-in
# binary in the platform archive, checksums.txt, and a bundle unless $3 is "unsigned".
make_release() {
	dir="$1"
	version="$2"
	mkdir -p "${dir}/stage"
	printf '#!/bin/sh\necho "tess version %s"\n' "$version" >"${dir}/stage/tess"
	chmod 755 "${dir}/stage/tess"
	archive="tess_${version}_${OS}_${ARCH}.tar.gz"
	tar -czf "${dir}/${archive}" -C "${dir}/stage" tess
	printf '%s  %s\n' "$(sha256_of "${dir}/${archive}")" "$archive" >"${dir}/checksums.txt"
	if [ "${3:-}" != unsigned ]; then
		printf '%s\n' "$GOOD_BUNDLE" >"${dir}/checksums.txt.sigstore.json"
	fi
}

cat >"${WORK}/tools/curl" <<EOF
#!/bin/sh
printf '%s\n' "\$*" >>"${WORK}/curl.log"
for arg; do
	if [ "\$arg" = "https://api.github.com/repos/ibrahemid/tessera/releases/latest" ]; then
		cat "${WORK}/latest.json"
		exit 0
	fi
done
exec "${REAL_CURL}" "\$@"
EOF

cat >"${WORK}/cosign/cosign" <<EOF
#!/bin/sh
printf '%s\n' "\$*" >>"${WORK}/cosign.log"
bundle=""
prev=""
for arg; do
	if [ "\$prev" = --bundle ]; then
		bundle="\$arg"
	fi
	prev="\$arg"
done
[ "\$(cat "\$bundle")" = "${GOOD_BUNDLE}" ] || {
	echo "Error: none of the expected identities matched what was in the certificate" >&2
	exit 1
}
EOF
chmod 755 "${WORK}/tools/curl" "${WORK}/cosign/cosign"

# A PATH without any real cosign, so the test controls which branch runs.
BASE_PATH="${WORK}/tools"
old_ifs=$IFS
IFS=:
for dir in $PATH; do
	[ -n "$dir" ] && [ ! -x "${dir}/cosign" ] && BASE_PATH="${BASE_PATH}:${dir}"
done
IFS=$old_ifs
COSIGN_PATH="${WORK}/cosign:${BASE_PATH}"

# run_installer PATH BASE_URL VERSION runs install.sh; an empty VERSION resolves
# the latest release through the curl stand-in.
run_installer() {
	rm -f "${WORK}/bin/tess" "${WORK}/cosign.log"
	printf '.' >>"${WORK}/cases"
	env PATH="$1" TESS_BASE_URL="$2" TESS_VERSION="$3" TESS_INSTALL_DIR="${WORK}/bin" \
		sh "$INSTALLER" 2>&1
}

expect_installed() {
	[ -x "${WORK}/bin/tess" ] || fail "$1: no executable at ${WORK}/bin/tess"
	[ "$("${WORK}/bin/tess")" = "tess version $2" ] || fail "$1: installed binary did not run"
}

expect_output() {
	printf '%s\n' "$2" | grep -qF -- "$3" || fail "$1: expected output containing '$3', got:
$2"
}

expect_refused() {
	[ ! -e "${WORK}/bin/tess" ] || fail "$1: installer wrote a binary"
}

SIGNED="${WORK}/signed"
OLD="${WORK}/old"
make_release "$SIGNED" "$VERSION"
make_release "$OLD" "$OLD_VERSION" unsigned

out=$(run_installer "$BASE_PATH" "file://${SIGNED}" "$VERSION") || fail "install without cosign exited non-zero:
${out}"
expect_installed "install without cosign" "$VERSION"
expect_output "install without cosign" "$out" "Installed tess ${VERSION}"
expect_output "install without cosign" "$out" "tess vault init"
expect_output "install without cosign" "$out" "Signature not checked (cosign not found)"
expect_output "install without cosign" "$out" "cosign verify-blob --bundle checksums.txt.sigstore.json --certificate-identity ${IDENTITY} --certificate-oidc-issuer ${ISSUER} checksums.txt"
[ "$(printf '%s\n' "$out" | grep -c 'Signature not checked')" -eq 1 ] ||
	fail "install without cosign: expected one line about the unchecked signature"

out=$(run_installer "$COSIGN_PATH" "file://${SIGNED}" "$VERSION") || fail "install with a good signature exited non-zero:
${out}"
expect_installed "good signature" "$VERSION"
expect_output "good signature" "$out" "Verified the checksums.txt signature from ${IDENTITY}"
expect_output "good signature cosign args" "$(cat "${WORK}/cosign.log")" "verify-blob --bundle /"
expect_output "good signature cosign args" "$(cat "${WORK}/cosign.log")" "/checksums.txt.sigstore.json --certificate-identity ${IDENTITY} --certificate-oidc-issuer ${ISSUER} /"
case "$(cat "${WORK}/cosign.log")" in
*/checksums.txt) ;;
*) fail "good signature: cosign did not verify checksums.txt" ;;
esac

printf '{"tag_name": "v%s"}\n' "$VERSION" >"${WORK}/latest.json"
out=$(run_installer "$COSIGN_PATH" "file://${SIGNED}" "") || fail "install of the latest signed release exited non-zero:
${out}"
expect_installed "latest signed release" "$VERSION"
expect_output "latest signed release" "$out" "Verified the checksums.txt signature"

BAD="${WORK}/bad-signature"
make_release "$BAD" "$VERSION"
printf 'forged\n' >"${BAD}/checksums.txt.sigstore.json"
if out=$(run_installer "$COSIGN_PATH" "file://${BAD}" "$VERSION"); then
	fail "installer accepted a bad signature"
fi
expect_output "bad signature" "$out" "signature check failed for checksums.txt of v${VERSION}"
expect_refused "bad signature"

NOBUNDLE="${WORK}/no-bundle"
make_release "$NOBUNDLE" "$VERSION" unsigned
if out=$(run_installer "$COSIGN_PATH" "file://${NOBUNDLE}" "$VERSION"); then
	fail "installer accepted a signed-era release without a bundle"
fi
expect_output "missing bundle" "$out" "refusing to install without checking the signature"
expect_refused "missing bundle"

out=$(run_installer "$COSIGN_PATH" "file://${OLD}" "$OLD_VERSION") || fail "install of a pinned release before signing exited non-zero:
${out}"
expect_installed "pinned release before signing" "$OLD_VERSION"
expect_output "pinned release before signing" "$out" "v${OLD_VERSION} predates release signing; checked the checksum only"
[ ! -e "${WORK}/cosign.log" ] || fail "pinned release before signing: cosign ran without a bundle"

printf '{"tag_name": "v%s"}\n' "$OLD_VERSION" >"${WORK}/latest.json"
if out=$(run_installer "$COSIGN_PATH" "file://${OLD}" ""); then
	fail "installer accepted an unsigned release resolved as latest"
fi
expect_output "unsigned latest" "$out" "set TESS_VERSION=${OLD_VERSION}"
expect_refused "unsigned latest"

MISMATCH="${WORK}/mismatch"
make_release "$MISMATCH" "$VERSION"
printf '%s  %s\n' "0000000000000000000000000000000000000000000000000000000000000000" \
	"tess_${VERSION}_${OS}_${ARCH}.tar.gz" >"${MISMATCH}/checksums.txt"
if out=$(run_installer "$COSIGN_PATH" "file://${MISMATCH}" "$VERSION"); then
	fail "installer accepted a bad checksum"
fi
expect_output "checksum mismatch" "$out" "checksum mismatch"
expect_refused "checksum mismatch"

UNLISTED="${WORK}/unlisted"
make_release "$UNLISTED" "$VERSION"
: >"${UNLISTED}/checksums.txt"
if out=$(run_installer "$BASE_PATH" "file://${UNLISTED}" "$VERSION"); then
	fail "installer accepted an unlisted archive"
fi
expect_output "unlisted archive" "$out" "not listed in checksums.txt"
expect_refused "unlisted archive"

MISSING="${WORK}/missing"
make_release "$MISSING" "$VERSION"
rm -f "${MISSING}/tess_${VERSION}_${OS}_${ARCH}.tar.gz"
if out=$(run_installer "$BASE_PATH" "file://${MISSING}" "$VERSION"); then
	fail "installer succeeded without an archive"
fi
expect_output "missing archive" "$out" "download failed"
expect_refused "missing archive"

if out=$(run_installer "$BASE_PATH" "http://127.0.0.1:9/download" "$VERSION"); then
	fail "installer downloaded over plain http"
fi
expect_output "plain http" "$out" "download failed: http://127.0.0.1:9/download"
expect_refused "plain http"
tail -n 1 "${WORK}/curl.log" | grep -qF -- "--proto =https --tlsv1.2" ||
	fail "plain http: curl was not limited to https"

for system in MINGW64_NT-10.0-26100 MSYS_NT-10.0-26100 CYGWIN_NT-10.0-26100; do
	cat >"${WORK}/uname/uname" <<EOF
#!/bin/sh
case "\$1" in
-m) echo x86_64 ;;
*) echo ${system} ;;
esac
EOF
	chmod 755 "${WORK}/uname/uname"
	if out=$(run_installer "${WORK}/uname:${BASE_PATH}" "file://${SIGNED}" "$VERSION"); then
		fail "installer ran on ${system}"
	fi
	expect_output "$system" "$out" "on Windows, download tess_<version>_windows_amd64.zip or tess_<version>_windows_arm64.zip"
	expect_refused "$system"
done

if grep -v -e '--proto =https --tlsv1.2' -e '--proto =file --tlsv1.2' "${WORK}/curl.log"; then
	fail "a curl call ran without a protocol and TLS floor"
fi

printf 'ok: install.sh (%s cases)\n' "$(wc -c <"${WORK}/cases" | tr -d ' ')"
