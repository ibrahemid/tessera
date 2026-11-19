#!/bin/sh
# shellcheck disable=SC2329
set -u

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
LOG_DIR="${GATE_LOG_DIR:-$ROOT/.status/gate}/release"
GORELEASER_IMAGE="goreleaser/goreleaser:v2.20.2@sha256:5d30bb36091b1a6d275ef450be05ee9c0d8c8b22955efe53dfe0974dd583cdc2"
ALPINE_IMAGE="alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf"
INSPECT_IMAGE="tessera-release-inspect"
GOMOD_VOLUME="tessera-release-gomod"
TREE_A="$LOG_DIR/tree-a"
TREE_B="$LOG_DIR/second-tree-b"
FAILED=0

run_check() {
	check_name="$1"
	shift
	check_log="$LOG_DIR/$check_name.log"
	if "$@" >"$check_log" 2>&1; then
		printf 'ok    %s\n' "$check_name"
	else
		FAILED=1
		printf 'FAIL  %s  (%s)\n' "$check_name" "$check_log"
		tail -n 15 "$check_log"
		return 1
	fi
}

copy_tree() {
	dest="$1"
	rm -rf "$dest"
	git clone --quiet --no-local "$ROOT" "$dest" || return 1
	git -C "$ROOT" ls-files -m -o --exclude-standard | while IFS= read -r path; do
		if [ -e "$ROOT/$path" ]; then
			mkdir -p "$dest/$(dirname -- "$path")" && cp "$ROOT/$path" "$dest/$path" || exit 1
		else
			rm -f "$dest/$path"
		fi
	done
}

goreleaser_in() {
	tree="$1"
	shift
	# The image entrypoint marks only the working directory as a safe git
	# directory; the repository root sits one level up.
	docker run --rm --entrypoint goreleaser \
		-v "$tree:$tree" -w "$tree/go" \
		-v "$GOMOD_VOLUME:/go/pkg/mod" \
		-e GIT_CONFIG_COUNT=1 -e GIT_CONFIG_KEY_0=safe.directory -e GIT_CONFIG_VALUE_0='*' \
		-e GOCACHE=/tmp/gocache \
		-e GORELEASER_CURRENT_TAG="$CURRENT_TAG" \
		"$GORELEASER_IMAGE" "$@"
}

check_config() {
	copy_tree "$TREE_A" && goreleaser_in "$TREE_A" check
}

snapshot() {
	tree="$1"
	if [ "$tree" != "$TREE_A" ]; then
		copy_tree "$tree" || return 1
	fi
	goreleaser_in "$tree" release --snapshot --clean --skip=sign,publish
}

check_reproducible() {
	a="$TREE_A/go/dist/checksums.txt"
	b="$TREE_B/go/dist/checksums.txt"
	cat "$a"
	cmp -s "$a" "$b" && return 0
	printf 'artifacts that differ between %s and %s:\n' "$TREE_A" "$TREE_B"
	diff "$a" "$b" | sed -n 's/^< [0-9a-f]\{64\}[[:space:]]*//p'
	return 1
}

expect_count() {
	printf '%s %s\n' "$2" "$1"
	[ "$2" = "$3" ] || {
		printf 'expected %s %s\n' "$3" "$1"
		status=1
	}
}

check_dist_files() {
	dist="$TREE_A/go/dist"
	status=0
	tgz=0 zip=0 deb=0 rpm=0 apk=0 arch=0 sums=0
	: >"$LOG_DIR/dist-artifacts"
	for f in "$dist"/*; do
		[ -f "$f" ] || continue
		artifact="${f##*/}"
		case "$artifact" in
		tess_*.tar.gz) tgz=$((tgz + 1)) ;;
		tess_*.zip) zip=$((zip + 1)) ;;
		*.deb) deb=$((deb + 1)) ;;
		*.rpm) rpm=$((rpm + 1)) ;;
		*.apk) apk=$((apk + 1)) ;;
		*.pkg.tar.zst) arch=$((arch + 1)) ;;
		checksums.txt)
			sums=$((sums + 1))
			continue
			;;
		artifacts.json | metadata.json | config.yaml) continue ;;
		*)
			printf 'unexpected file in dist: %s\n' "$artifact"
			status=1
			continue
			;;
		esac
		printf '%s\n' "$artifact" >>"$LOG_DIR/dist-artifacts"
	done
	expect_count tar.gz "$tgz" 4
	expect_count zip "$zip" 2
	expect_count deb "$deb" 2
	expect_count rpm "$rpm" 2
	expect_count apk "$apk" 2
	expect_count pkg.tar.zst "$arch" 2
	expect_count checksums.txt "$sums" 1
	listed=$(awk '{print $2}' "$dist/checksums.txt" | sort)
	present=$(sort "$LOG_DIR/dist-artifacts")
	[ "$listed" = "$present" ] || {
		printf 'checksums.txt lists:\n%s\ndist holds:\n%s\n' "$listed" "$present"
		status=1
	}
	return "$status"
}

build_inspector() {
	printf 'FROM %s\nRUN apk add --no-cache dpkg rpm tar zstd unzip\n' "$ALPINE_IMAGE" |
		docker build --quiet --tag "$INSPECT_IMAGE" -
}

inspector_script() {
	cat <<'INSPECT'
set -eu
fmt="$1"
commit="$2"
version="$3"
man_pages="$4"
cd /dist
work=$(mktemp -d)
status=0

fail() {
	printf 'FAIL %s\n' "$1"
	status=1
}

listing() {
	prefix="${1:-}"
	while read -r perms _owner _size day clock path _rest; do
		path="${path#./}"
		printf '%s %s %s/%s\n' "$(date -u -d "$day $clock" +%s)" "$perms" "$prefix" "${path%/}"
	done
}

expected() {
	printf '%s\n' /usr/bin/tess /usr/share/bash-completion/completions/tess /usr/share/fish/vendor_completions.d/tess.fish
	for page in $man_pages; do
		printf '/usr/share/man/man1/%s\n' "$page"
	done
	if [ "$1" = deb ]; then
		printf '%s\n' /usr/share/zsh/vendor-completions/_tess /usr/share/doc/tessera/copyright
	else
		printf '%s\n' /usr/share/zsh/site-functions/_tess /usr/share/licenses/tessera/LICENSE
	fi
}

check_newer() {
	awk -v c="$commit" '$1 + 0 > c + 0' "$2" >"$work/newer"
	[ ! -s "$work/newer" ] || {
		fail "$1: entries newer than the commit"
		cat "$work/newer"
	}
}

check_listing() {
	pkg="$1"
	list="$2"
	printf '== %s\n' "$pkg"
	cat "$list"
	expected "$fmt" | sort >"$work/want"
	awk '$2 ~ /^-/ && $3 !~ /^\/\./ {print $3}' "$list" | sort >"$work/got"
	diff "$work/want" "$work/got" >"$work/diff" || {
		fail "$pkg: files differ from the planned layout"
		cat "$work/diff"
	}
	[ "$(awk '$3 == "/usr/bin/tess" {print $2}' "$list")" = "-rwxr-xr-x" ] ||
		fail "$pkg: /usr/bin/tess is not mode 0755"
	awk '$2 ~ /^-/ && $3 != "/usr/bin/tess" && $3 !~ /^\/\./ && $2 != "-rw-r--r--"' "$list" >"$work/modes"
	[ ! -s "$work/modes" ] || {
		fail "$pkg: data files not mode 0644"
		cat "$work/modes"
	}
	check_newer "$pkg" "$list"
}

check_suggests() {
	for tool in wl-clipboard xclip xsel grim slurp; do
		grep -qw -- "$tool" "$2" || fail "$1: $tool is not suggested"
	done
}

check_pkginfo() {
	grep -qx 'pkgname = tessera' "$2" || fail "$1: pkgname is not tessera"
	built=$(sed -n 's/^builddate = //p' "$2")
	if [ -n "$built" ] && [ "$built" -gt "$commit" ]; then
		fail "$1: builddate $built is after the commit"
	fi
}

case "$fmt" in
archive)
	for pkg in tess_*.tar.gz; do
		tar -tvz --full-time -f "$pkg" | listing >"$work/list"
		printf '== %s\n' "$pkg"
		cat "$work/list"
		[ "$(awk '{print $3}' "$work/list")" = /tess ] || fail "$pkg: holds more than the tess binary"
		check_newer "$pkg" "$work/list"
	done
	for pkg in tess_*.zip; do
		names=$(unzip -Z1 "$pkg")
		printf '== %s\n%s\n' "$pkg" "$names"
		[ "$names" = tess.exe ] || fail "$pkg: holds more than tess.exe"
	done
	case "$(uname -m)" in
	aarch64 | arm64) arch=arm64 ;;
	*) arch=amd64 ;;
	esac
	mkdir -p "$work/home"
	tar -xzO -f "tess_${version}_linux_${arch}.tar.gz" tess >"$work/tess"
	chmod 755 "$work/tess"
	reported=$(HOME="$work/home" XDG_DATA_HOME="$work/home/data" TESSERA_VAULT="$work/home/vault.json" "$work/tess" --version)
	printf '%s\n' "$reported"
	case "$reported" in
	*"$version"*) ;;
	*) fail "linux_${arch} binary does not report $version" ;;
	esac
	sha256sum -c checksums.txt || fail "checksums.txt does not match dist"
	;;
deb)
	for pkg in *.deb; do
		{
			dpkg-deb --fsys-tarfile "$pkg" | tar -tv --full-time | listing
			dpkg-deb --ctrl-tarfile "$pkg" | tar -tv --full-time | listing /.control
		} >"$work/list"
		check_listing "$pkg" "$work/list"
		dpkg-deb -I "$pkg"
		[ "$(dpkg-deb -f "$pkg" Package)" = tessera ] || fail "$pkg: Package is not tessera"
		dpkg-deb -f "$pkg" Suggests >"$work/suggests"
		check_suggests "$pkg" "$work/suggests"
		dpkg-deb --fsys-tarfile "$pkg" | tar -xO -f - ./usr/share/man/man1/tess.1.gz | gzip -dc |
			sed 's/\\-/-/g' | grep -qF "tess $version" || fail "$pkg: tess.1 footer does not carry $version"
	done
	;;
rpm)
	for pkg in *.rpm; do
		rpm -qp --nosignature --qf '[%{FILEMTIMES} %{FILEMODES:perms} %{FILENAMES}\n]' "$pkg" >"$work/list"
		check_listing "$pkg" "$work/list"
		rpm -qip --nosignature "$pkg"
		[ "$(rpm -qp --nosignature --qf '%{NAME}' "$pkg")" = tessera ] || fail "$pkg: NAME is not tessera"
		built=$(rpm -qp --nosignature --qf '%{BUILDTIME}' "$pkg")
		[ "$built" -le "$commit" ] || fail "$pkg: BUILDTIME $built is after the commit"
		rpm -qp --nosignature --suggests "$pkg" >"$work/suggests"
		check_suggests "$pkg" "$work/suggests"
	done
	;;
apk)
	for pkg in *.apk; do
		tar -tvz -i --full-time -f "$pkg" | listing >"$work/list"
		check_listing "$pkg" "$work/list"
		tar -xz -i -O -f "$pkg" .PKGINFO >"$work/pkginfo"
		cat "$work/pkginfo"
		check_pkginfo "$pkg" "$work/pkginfo"
	done
	;;
archlinux)
	for pkg in *.pkg.tar.zst; do
		tar --zstd -tv --full-time -f "$pkg" | listing >"$work/list"
		check_listing "$pkg" "$work/list"
		tar --zstd -xO -f "$pkg" .PKGINFO >"$work/pkginfo"
		cat "$work/pkginfo"
		check_pkginfo "$pkg" "$work/pkginfo"
		check_suggests "$pkg" "$work/pkginfo"
		tar --zstd -xO -f "$pkg" .MTREE | gzip -dc | sed -n 's/.* time=\([0-9]*\).*/\1/p' |
			awk -v c="$commit" '$1 + 0 > c + 0' >"$work/newer"
		[ ! -s "$work/newer" ] || fail "$pkg: .MTREE records times after the commit"
	done
	;;
*)
	fail "unknown format $fmt"
	;;
esac
exit "$status"
INSPECT
}

inspect() {
	inspector_script | docker run --rm -i -e TZ=UTC -v "$TREE_A/go/dist:/dist:ro" "$INSPECT_IMAGE" \
		sh -s -- "$1" "$COMMIT_EPOCH" "$SNAPSHOT_VERSION" "$MAN_PAGES"
}

command -v docker >/dev/null 2>&1 || {
	printf 'FAIL  docker is required on PATH\n'
	exit 1
}
mkdir -p "$LOG_DIR"
CURRENT_TAG=$(git -C "$ROOT" describe --tags --abbrev=0 --match 'v[0-9]*') || {
	printf 'FAIL  no vX.Y.Z tag reachable from HEAD\n'
	exit 1
}
COMMIT_EPOCH=$(git -C "$ROOT" log -1 --format=%ct)

run_check goreleaser-check check_config || exit 1
run_check snapshot-a snapshot "$TREE_A" || exit 1
run_check snapshot-b snapshot "$TREE_B" || exit 1
run_check reproducible check_reproducible
run_check dist-files check_dist_files

SNAPSHOT_VERSION=$(sed -n 's/.*"version":"\([^"]*\)".*/\1/p' "$TREE_A/go/dist/metadata.json")
MAN_PAGES=$(cd "$TREE_A/go/build/man" && ls)
run_check inspect-image build_inspector || exit 1
for fmt in archive deb rpm apk archlinux; do
	run_check "contents-$fmt" inspect "$fmt"
done
exit "$FAILED"
