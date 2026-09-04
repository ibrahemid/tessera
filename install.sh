#!/bin/sh
# Install the tess CLI from a GitHub release: verified checksum, no toolchain.
#   curl -fsSL https://raw.githubusercontent.com/ibrahemid/tessera/main/install.sh | sh
# Environment overrides: TESS_VERSION, TESS_INSTALL_DIR, TESS_BASE_URL.
set -eu

REPO="ibrahemid/tessera"
RELEASES_URL="https://github.com/${REPO}/releases"
API_LATEST_URL="https://api.github.com/repos/${REPO}/releases/latest"

TMPDIR_TESS=""

cleanup() {
	[ -n "$TMPDIR_TESS" ] && rm -rf "$TMPDIR_TESS"
	return 0
}
trap cleanup EXIT INT TERM

die() {
	printf 'install.sh: %s\n' "$1" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || die "$1 is required but was not found on PATH"
}

detect_platform() {
	os=$(uname -s)
	case "$os" in
	Darwin) os="darwin" ;;
	Linux) os="linux" ;;
	*) die "unsupported operating system: $os (darwin and linux only)" ;;
	esac

	arch=$(uname -m)
	case "$arch" in
	arm64 | aarch64) arch="arm64" ;;
	x86_64 | amd64) arch="amd64" ;;
	*) die "unsupported architecture: $arch (arm64 and amd64 only)" ;;
	esac

	printf '%s_%s' "$os" "$arch"
}

# resolve_version prints the release version without its leading "v".
resolve_version() {
	if [ -n "${TESS_VERSION:-}" ]; then
		printf '%s' "${TESS_VERSION#v}"
		return 0
	fi

	tag=$(curl -fsSL "$API_LATEST_URL" 2>/dev/null |
		sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
		head -n 1) || tag=""
	[ -n "$tag" ] || die "could not resolve the latest release from ${API_LATEST_URL}; set TESS_VERSION=1.1.0 and retry"
	printf '%s' "${tag#v}"
}

# resolve_install_dir prints a writable directory on which to place the binary.
resolve_install_dir() {
	if [ -n "${TESS_INSTALL_DIR:-}" ]; then
		mkdir -p "$TESS_INSTALL_DIR" || die "cannot create TESS_INSTALL_DIR=${TESS_INSTALL_DIR}"
		[ -w "$TESS_INSTALL_DIR" ] || die "TESS_INSTALL_DIR=${TESS_INSTALL_DIR} is not writable"
		printf '%s' "$TESS_INSTALL_DIR"
		return 0
	fi

	if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
		printf '%s' /usr/local/bin
		return 0
	fi

	mkdir -p "$HOME/.local/bin" || die "cannot create ${HOME}/.local/bin"
	[ -w "$HOME/.local/bin" ] || die "${HOME}/.local/bin is not writable"
	printf '%s' "$HOME/.local/bin"
}

verify_checksum() {
	file="$1"
	want="$2"

	if command -v shasum >/dev/null 2>&1; then
		got=$(shasum -a 256 "$file" | cut -d' ' -f1)
	elif command -v sha256sum >/dev/null 2>&1; then
		got=$(sha256sum "$file" | cut -d' ' -f1)
	else
		die "no sha256 tool found (need shasum or sha256sum); refusing to install unverified binary"
	fi

	[ "$got" = "$want" ] || die "checksum mismatch for $(basename "$file"): expected ${want}, got ${got}"
}

main() {
	need curl
	need tar

	platform=$(detect_platform)
	version=$(resolve_version)
	base_url="${TESS_BASE_URL:-${RELEASES_URL}/download/v${version}}"
	archive="tess_${version}_${platform}.tar.gz"

	TMPDIR_TESS=$(mktemp -d) || die "cannot create a temporary directory"

	printf 'Downloading %s ...\n' "$archive"
	curl -fsSL -o "${TMPDIR_TESS}/${archive}" "${base_url}/${archive}" ||
		die "download failed: ${base_url}/${archive}"
	curl -fsSL -o "${TMPDIR_TESS}/checksums.txt" "${base_url}/checksums.txt" ||
		die "download failed: ${base_url}/checksums.txt"

	want=$(sed -n "s/^\([0-9a-f]\{64\}\)[[:space:]]\{1,\}\*\{0,1\}${archive}\$/\1/p" \
		"${TMPDIR_TESS}/checksums.txt" | head -n 1)
	[ -n "$want" ] || die "${archive} is not listed in checksums.txt for v${version}"
	verify_checksum "${TMPDIR_TESS}/${archive}" "$want"

	tar -xzf "${TMPDIR_TESS}/${archive}" -C "$TMPDIR_TESS" tess ||
		die "the archive does not contain a tess binary"
	chmod 755 "${TMPDIR_TESS}/tess"

	install_dir=$(resolve_install_dir)
	mv -f "${TMPDIR_TESS}/tess" "${install_dir}/tess" ||
		die "cannot write ${install_dir}/tess"

	printf 'Installed tess %s to %s/tess\n' "$version" "$install_dir"

	case ":${PATH}:" in
	*":${install_dir}:"*) ;;
	*) printf "Add it to your PATH: export PATH=\"%s:\$PATH\"\n" "$install_dir" ;;
	esac

	printf 'Next: tess vault init\n'
}

main "$@"
