#!/bin/sh
set -eu

SUITES='smoke static secret-service no-secret-service keyctl keyctl-blocked x11 wayland portal screen-wayland package'
DISTROS='debian ubuntu fedora alpine arch'
SUITE_TIMEOUT=600
MOUNT_TIMEOUT=120
IMAGE_TIMEOUT=1200
GO_TEST_TIMEOUT=1800
GO_IMAGE=golang:1.27
VERSION=0.0.0-test
PASSPHRASE=test-passphrase-1
KEYRING_PASSWORD=keyring-test-pw
FIXTURE_SECRET=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ
HOTP_URI="otpauth://hotp/Fixture:hotp@example.com?secret=$FIXTURE_SECRET&issuer=Fixture&counter=0"
TOTP_URI="otpauth://totp/Example:totp@example.com?secret=$FIXTURE_SECRET&issuer=Example&digits=8&period=30"
HOTP_CODE_0=755224
HOTP_CODE_1=287082
SELECTION_SIZE=576

usage() {
  printf 'usage: %s [suite...]\nsuites: %s\nGATE_LINUX_DISTROS selects from: %s\n' "$0" "$SUITES" "$DISTROS" >&2
  exit 2
}

is_word_in() {
  case " $2 " in
    *" $1 "*) return 0 ;;
  esac
  return 1
}

# Stops "$@" after $1 seconds; 124 means it was stopped.
bounded() {
  b_limit=$1
  shift
  "$@" &
  b_pid=$!
  b_waited=0
  while kill -0 "$b_pid" 2>/dev/null; do
    if [ "$b_waited" -ge "$b_limit" ]; then
      kill "$b_pid" 2>/dev/null || :
      wait "$b_pid" 2>/dev/null || :
      return 124
    fi
    sleep 1
    b_waited=$((b_waited + 1))
  done
  wait "$b_pid"
}

# A bind mount the engine cannot serve leaves a run waiting forever, so one bounded run proves the mount first.
check_mount() {
  cm_image=$1 cm_platform=${2:-}
  cm_name=tessera-it-mount-$$
  cm_log=$LOG_DIR/mount.log
  set -- --rm --name "$cm_name" --network none -v "$ROOT:/src:ro"
  if [ -n "$cm_platform" ]; then
    set -- "$@" --platform "$cm_platform"
  fi
  if bounded "$MOUNT_TIMEOUT" docker run "$@" "$cm_image" test -f /src/go/go.mod >"$cm_log" 2>&1; then
    return 0
  else
    cm_rc=$?
  fi
  if [ "$cm_rc" -eq 124 ]; then
    bounded 20 docker rm -f "$cm_name" >/dev/null 2>&1 || :
    printf 'FAIL  mount: container %s did not start with %s mounted within %ss  (%s)\n' \
      "$cm_name" "$ROOT" "$MOUNT_TIMEOUT" "$cm_log"
  else
    printf 'FAIL  mount: %s is not readable inside a container, exit %s  (%s)\n' "$ROOT" "$cm_rc" "$cm_log"
  fi
  return 1
}

build_binaries() {
  for bb_pkg in cmd/tess internal/dbusx internal/secretstore internal/clipboard internal/screenshot; do
    GOOS=linux GOARCH=$1 CGO_ENABLED=0 go -C "$ROOT/go" test -c -trimpath \
      -o "$2/${bb_pkg##*/}.test" "./$bb_pkg" || return 1
  done
  GOOS=linux GOARCH=$1 CGO_ENABLED=0 go -C "$ROOT/go" build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION" -o "$2/tess" ./cmd/tess
}

build_image() {
  bi_log=$LOG_DIR/$1-image.log
  if bounded "$IMAGE_TIMEOUT" docker build --platform "$2" -t "tessera-it-$1" \
    -f "$ROOT/test/linux/$1.Dockerfile" "$ROOT/test/linux" >"$bi_log" 2>&1; then
    return 0
  else
    bi_rc=$?
  fi
  FAILED=1
  case $bi_rc in
    124) printf 'FAIL  %s image: timed out after %ss  (%s)\n' "$1" "$IMAGE_TIMEOUT" "$bi_log" ;;
    *) printf 'FAIL  %s image  (%s)\n' "$1" "$bi_log" ;;
  esac
  return 1
}

has_dist_packages() {
  for hp_f in "$DIST_DIR"/*.deb "$DIST_DIR"/*.rpm "$DIST_DIR"/*.apk "$DIST_DIR"/*.pkg.tar.zst; do
    if [ -e "$hp_f" ]; then
      return 0
    fi
  done
  return 1
}

run_suite() {
  rs_distro=$1 rs_suite=$2 rs_platform=$3 rs_arch=$4
  rs_log=$LOG_DIR/$rs_distro-$rs_suite.log
  rs_bin_digest=$(sh "$ROOT/test/linux/digest.sh" dir "$BIN_DIR/$rs_arch")
  set -- --rm --init --network none --platform "$rs_platform" \
    -v "$ROOT:/src:ro" -e "TESSERA_IT_BIN=/src/.status/gate/linux/bin/$rs_arch" \
    -e "TESSERA_IT_TREE_DIGEST=$TREE_DIGEST" -e "TESSERA_IT_BIN_DIGEST=$rs_bin_digest"
  if [ -d "$DIST_DIR" ]; then
    set -- "$@" -v "$DIST_DIR:/dist:ro"
  fi
  case $rs_suite in
    package)
      rs_dist_digest=$(sh "$ROOT/test/linux/digest.sh" dir "$DIST_DIR")
      set -- "$@" -u 0:0 -e "TESSERA_IT_DIST_DIGEST=$rs_dist_digest"
      ;;
    *) set -- "$@" -u 1000:1000 ;;
  esac
  case $rs_suite in
    keyctl) set -- "$@" --security-opt seccomp=unconfined ;;
    wayland | screen-wayland) set -- "$@" --cap-add SYS_NICE ;;
  esac
  if bounded "$((SUITE_TIMEOUT + 60))" docker run "$@" "tessera-it-$rs_distro" \
    timeout "$SUITE_TIMEOUT" sh /src/test/linux/run.sh --inside "$rs_suite" >"$rs_log" 2>&1; then
    printf 'ok    %s %s\n' "$rs_distro" "$rs_suite"
    return 0
  else
    rs_rc=$?
  fi
  FAILED=1
  rs_reason=$(sed -n 's/^FAIL: //p' "$rs_log" | tail -n 1)
  if [ -z "$rs_reason" ]; then
    case $rs_rc in
      124) rs_reason="timed out after ${SUITE_TIMEOUT}s" ;;
      *) rs_reason="exit $rs_rc" ;;
    esac
  fi
  printf 'FAIL  %s %s: %s  (%s)\n' "$rs_distro" "$rs_suite" "$rs_reason" "$rs_log"
}

host_main() {
  ROOT=$(cd "$(dirname "$0")/../.." && pwd -P)
  LOG_DIR=${GATE_LOG_DIR:-$ROOT/.status/gate}/linux
  BIN_DIR=$ROOT/.status/gate/linux/bin
  DIST_DIR=${GATE_LINUX_DIST:-$ROOT/go/dist}
  FAILED=0

  selected_suites=${*:-$SUITES}
  for s in $selected_suites; do
    is_word_in "$s" "$SUITES" || usage
  done
  selected_distros=$(printf '%s' "${GATE_LINUX_DISTROS:-$DISTROS}" | tr ',' ' ')
  for d in $selected_distros; do
    is_word_in "$d" "$DISTROS" || usage
  done
  mkdir -p "$LOG_DIR"

  if ! native=$(docker version --format '{{.Server.Arch}}' 2>"$LOG_DIR/docker.log"); then
    printf 'FAIL  docker: the daemon is not reachable  (%s)\n' "$LOG_DIR/docker.log"
    exit 1
  fi
  case $native in
    amd64 | arm64) ;;
    *)
      printf 'FAIL  docker: unsupported server architecture %s\n' "$native"
      exit 1
      ;;
  esac

  arches=
  for d in $selected_distros; do
    if [ "$d" = arch ]; then a=amd64; else a=$native; fi
    is_word_in "$a" "$arches" || arches="$arches $a"
  done
  for a in $arches; do
    mkdir -p "$BIN_DIR/$a"
    if ! build_binaries "$a" "$BIN_DIR/$a" >"$LOG_DIR/build-$a.log" 2>&1; then
      printf 'FAIL  build %s  (%s)\n' "$a" "$LOG_DIR/build-$a.log"
      exit 1
    fi
  done

  TREE_DIGEST=$(sh "$ROOT/test/linux/digest.sh" tree "$ROOT")
  mount_checked=0
  for d in $selected_distros; do
    if [ "$d" = arch ]; then a=amd64; else a=$native; fi
    build_image "$d" "linux/$a" || continue
    if [ "$mount_checked" = 0 ]; then
      check_mount "tessera-it-$d" "linux/$a" || exit 1
      mount_checked=1
    fi
    for s in $selected_suites; do
      if [ "$s" = package ] && ! has_dist_packages; then
        printf 'skip  %s package: no packages in %s\n' "$d" "$DIST_DIR"
        continue
      fi
      run_suite "$d" "$s" "linux/$a" "$a"
    done
  done
  exit "$FAILED"
}

# The module's own tests on Linux, in the toolchain image, against the tree the host has.
go_test_main() {
  ROOT=$(cd "$(dirname "$0")/../.." && pwd -P)
  LOG_DIR=${GATE_LOG_DIR:-$ROOT/.status/gate}/linux
  mkdir -p "$LOG_DIR"
  check_mount "$GO_IMAGE" || exit 1
  gt_digest=$(sh "$ROOT/test/linux/digest.sh" tree "$ROOT")
  if bounded "$GO_TEST_TIMEOUT" docker run --rm -v "$ROOT:/src:ro" -w /src/go -e GOFLAGS=-buildvcs=false \
    -e GOCACHE=/tmp/gocache -e GOMODCACHE=/tmp/gomod -e "TESSERA_IT_TREE_DIGEST=$gt_digest" \
    "$GO_IMAGE" sh /src/test/linux/run.sh --inside-go-test; then
    return 0
  else
    gt_rc=$?
  fi
  if [ "$gt_rc" -eq 124 ]; then
    printf 'FAIL: timed out after %ss\n' "$GO_TEST_TIMEOUT"
  fi
  exit 1
}

inside_go_test_main() {
  SRC=$(cd "$(dirname "$0")/../.." && pwd -P)
  same_as_host tree "$SRC" "${TESSERA_IT_TREE_DIGEST:-}"
  go vet ./... && go test ./...
}

fail() {
  printf 'FAIL: %s\n' "$*"
  exit 1
}

pass() {
  printf 'ok: %s\n' "$*"
}

one_line() {
  printf '%s' "$1" | tr '\n' ' '
}

want_eq() {
  if [ "$2" != "$3" ]; then
    fail "$1: got '$(one_line "$2")', want '$3'"
  fi
  pass "$1"
}

want_has() {
  case $2 in
    *"$3"*) pass "$1" ;;
    *) fail "$1: '$3' not in '$(one_line "$2")'" ;;
  esac
}

want_lacks() {
  case $2 in
    *"$3"*) fail "$1: unexpected '$3' in '$(one_line "$2")'" ;;
    *) pass "$1" ;;
  esac
}

want_file() {
  if [ ! -s "$2" ]; then
    fail "$1: $2 is missing or empty"
  fi
  pass "$1"
}

want_gone() {
  if [ -e "$2" ]; then
    fail "$1: $2 is still present"
  fi
  pass "$1"
}

run_cmd() {
  if "$@" >"$W/stdout" 2>"$W/stderr" </dev/null; then RC=0; else RC=$?; fi
  OUT=$(cat "$W/stdout")
  ERR=$(cat "$W/stderr")
}

must() {
  run_cmd "$@"
  if [ "$RC" -ne 0 ]; then
    fail "$*: exit $RC: $(one_line "$ERR")"
  fi
}

must_fail() {
  run_cmd "$@"
  if [ "$RC" -eq 0 ]; then
    fail "$*: succeeded, want a failure; stdout '$(one_line "$OUT")'"
  fi
}

tess_unlocked() {
  env TESSERA_PASSPHRASE="$PASSPHRASE" "$TESS" "$@"
}

wait_until() {
  wu_n=$1
  shift
  while [ "$wu_n" -gt 0 ]; do
    if "$@"; then
      return 0
    fi
    sleep 0.1
    wu_n=$((wu_n - 1))
  done
  return 1
}

run_gotest() {
  rg_name=$1
  shift
  if (unset TESSERA_VAULT XDG_DATA_HOME && "$@") >"$W/$rg_name.out" 2>&1; then rg_rc=0; else rg_rc=$?; fi
  cat "$W/$rg_name.out"
  if [ "$rg_rc" -ne 0 ] || ! grep -qx PASS "$W/$rg_name.out"; then
    fail "$rg_name tests failed (exit $rg_rc)"
  fi
  pass "$rg_name tests"
}

want_skips_only() {
  ws_extra=$(sed -n 's/^ *--- SKIP: \([^ ]*\).*/\1/p' "$W/$1.out" | grep -Evx "$2" || true)
  if [ -n "$ws_extra" ]; then
    fail "$1: unexpected skips: $(one_line "$ws_extra")"
  fi
  pass "$1 skipped only what is expected"
}

want_passed() {
  ws_name=$1
  shift
  for ws_t in "$@"; do
    if ! grep -q -- "--- PASS: $ws_t " "$W/$ws_name.out"; then
      fail "$ws_name: $ws_t did not pass"
    fi
  done
  pass "$ws_name: $*"
}

make_vault() {
  must tess_unlocked vault init
  must tess_unlocked add "$HOTP_URI"
  want_has "add the HOTP fixture" "$OUT" "Added Fixture (hotp@example.com) as "
  H=${OUT##* as }
}

start_runtime_dir() {
  export XDG_RUNTIME_DIR="$W/run"
  mkdir -p "$XDG_RUNTIME_DIR"
  chmod 700 "$XDG_RUNTIME_DIR"
}

has_bus_name() {
  dbus-send --session --print-reply --dest=org.freedesktop.DBus / \
    org.freedesktop.DBus.NameHasOwner "string:$1" 2>/dev/null | grep -q 'boolean true'
}

has_sway_sockets() {
  SWAY_WAYLAND=
  SWAY_IPC=
  for ss_f in "$XDG_RUNTIME_DIR"/wayland-[0-9]; do
    if [ -S "$ss_f" ]; then SWAY_WAYLAND=${ss_f##*/}; fi
  done
  for ss_f in "$XDG_RUNTIME_DIR"/sway-ipc.*.sock; do
    if [ -S "$ss_f" ]; then SWAY_IPC=$ss_f; fi
  done
  [ -n "$SWAY_WAYLAND" ] && [ -n "$SWAY_IPC" ]
}

start_sway() {
  start_runtime_dir
  WLR_BACKENDS=headless WLR_RENDERER=pixman WLR_LIBINPUT_NO_DEVICES=1 \
    sway -c "$SRC/test/linux/sway.conf" >"$W/sway.log" 2>&1 &
  if ! wait_until 200 has_sway_sockets; then
    fail "sway did not start: $(one_line "$(tail -n 5 "$W/sway.log")")"
  fi
  export WAYLAND_DISPLAY="$SWAY_WAYLAND" SWAYSOCK="$SWAY_IPC"
  pass "sway on $WAYLAND_DISPLAY"
}

has_x_socket() {
  [ -S /tmp/.X11-unix/X99 ]
}

start_xvfb() {
  Xvfb :99 -nolisten tcp >"$W/xvfb.log" 2>&1 &
  if ! wait_until 200 has_x_socket; then
    fail "Xvfb did not start: $(one_line "$(tail -n 5 "$W/xvfb.log")")"
  fi
  export DISPLAY=:99
  pass "Xvfb on $DISPLAY"
}

path_without() {
  pw_dir=$W/path-without-$1
  mkdir -p "$pw_dir"
  pw_ifs=$IFS
  IFS=:
  for pw_p in $PATH; do
    for pw_f in "$pw_p"/*; do
      pw_n=${pw_f##*/}
      if [ "$pw_n" != "$1" ] && [ -x "$pw_f" ] && [ ! -e "$pw_dir/$pw_n" ]; then
        ln -s "$pw_f" "$pw_dir/$pw_n"
      fi
    done
  done
  IFS=$pw_ifs
  printf '%s\n' "$pw_dir"
}

is_clip_clear_running() {
  for cc_f in /proc/[0-9]*/cmdline; do
    if cat "$cc_f" 2>/dev/null | tr '\0' ' ' | grep -q '[_]_clip-clear'; then
      return 0
    fi
  done
  return 1
}

is_clip_clear_done() {
  if is_clip_clear_running; then
    return 1
  fi
  return 0
}

read_clipboard() {
  case $1 in
    xclip) xclip -selection clipboard -o 2>/dev/null || true ;;
    xsel) xsel --clipboard --output 2>/dev/null || true ;;
    wayland) wl-paste --no-newline 2>/dev/null || true ;;
  esac
}

write_clipboard() {
  case $1 in
    xclip) printf '%s' "$2" | xclip -selection clipboard >/dev/null 2>&1 ;;
    xsel) printf '%s' "$2" | xsel --clipboard --input >/dev/null 2>&1 ;;
    wayland) printf '%s' "$2" | wl-copy >/dev/null 2>&1 ;;
  esac
}

check_timed_clear() {
  must env PATH="$2" TESSERA_PASSPHRASE="$PASSPHRASE" "$TESS" code "$H" -c --clear 1
  want_eq "$1: code printed" "$OUT" "$HOTP_CODE_0"
  want_eq "$1: clipboard holds the code" "$(read_clipboard "$1")" "$HOTP_CODE_0"
  if [ "$1" = wayland ]; then
    cl_types=$(wl-paste --list-types 2>/dev/null || true)
    if [ "$WL_SENSITIVE" = true ]; then
      want_has "wayland: password manager hint offered" "$cl_types" x-kde-passwordManagerHint
    else
      want_lacks "wayland: no password manager hint before wl-copy 2.3.0" "$cl_types" x-kde-passwordManagerHint
    fi
  fi
  if ! wait_until 150 is_clip_clear_done; then
    fail "$1: the clear did not finish"
  fi
  want_eq "$1: clipboard empty after the deadline" "$(read_clipboard "$1")" ""

  must env PATH="$2" TESSERA_PASSPHRASE="$PASSPHRASE" "$TESS" code "$H" -c --clear 2
  if ! is_clip_clear_running; then
    fail "$1: no clear process is waiting after -c --clear 2"
  fi
  write_clipboard "$1" manual-copy
  if ! wait_until 150 is_clip_clear_done; then
    fail "$1: the clear did not finish"
  fi
  want_eq "$1: a later manual copy survives the clear" "$(read_clipboard "$1")" manual-copy
}

is_version_at_least() {
  printf '%s\n' "$1" | awk -v a="$2" -v b="$3" -v c="$4" '
    NR == 1 {
      for (i = 1; i <= NF; i++) {
        if ($i !~ /^[0-9]/) continue
        split($i, v, ".")
        x = v[1] + 0; y = v[2] + 0; z = v[3] + 0
        if (x != a) exit !(x > a)
        if (y != b) exit !(y > b)
        exit !(z >= c)
      }
      exit 1
    }'
}

check_no_screen_leftovers() {
  for nl_d in "$XDG_RUNTIME_DIR"/tessera-screen-* /tmp/tessera-screen-*; do
    if [ -e "$nl_d" ]; then
      fail "leftover scratch directory $nl_d"
    fi
  done
  pass "no scratch directories left"
}

check_static() {
  must "$1" --version
  want_has "$1 runs" "$OUT" "tess version "
  cs_out=$(ldd "$1" 2>&1 || true)
  case $cs_out in
    *"not a dynamic executable"* | *"Not a valid dynamic program"* | *"statically linked"*) pass "$1 is static" ;;
    *) fail "$1 is not static: ldd: $(one_line "$cs_out")" ;;
  esac
}

suite_smoke() {
  must "$TESS" --version
  want_eq "version" "$OUT" "tess version $VERSION"

  sm_default=$HOME/.local/share/tessera/vault.json
  sm_vault=$TESSERA_VAULT
  unset TESSERA_VAULT
  XDG_DATA_HOME=relative/data
  must tess_unlocked vault init
  want_eq "init at the default path" "$OUT" "Created vault at $sm_default"
  want_file "vault file" "$sm_default"
  want_eq "vault file mode" "$(stat -c %a "$sm_default")" 600
  want_gone "relative XDG_DATA_HOME ignored" "$W/relative"

  must tess_unlocked add "$HOTP_URI"
  want_has "add HOTP" "$OUT" "Added Fixture (hotp@example.com) as "
  sm_hotp=${OUT##* as }
  must tess_unlocked add "$TOTP_URI"
  want_has "add TOTP" "$OUT" "Added Example (totp@example.com) as "
  sm_totp=${OUT##* as }

  must tess_unlocked code "$sm_hotp" --no-copy
  want_eq "HOTP counter 0" "$OUT" "$HOTP_CODE_0"
  must tess_unlocked code "$sm_hotp" --no-copy --next
  want_eq "HOTP --next" "$OUT" "$HOTP_CODE_1"
  must tess_unlocked code "$sm_hotp" --no-copy
  want_eq "HOTP counter persisted" "$OUT" "$HOTP_CODE_1"

  must tess_unlocked code "$sm_totp" --no-copy --now
  case $OUT in
    [0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]) pass "TOTP has 8 digits" ;;
    *) fail "TOTP code: got '$OUT', want 8 digits" ;;
  esac
  must tess_unlocked code "$sm_totp" --json --now
  want_has "TOTP JSON type" "$OUT" '"type": "totp"'
  want_has "TOTP JSON expiry" "$OUT" '"expires_in": '

  must tess_unlocked list --json
  want_has "list HOTP" "$OUT" '"issuer": "Fixture"'
  want_has "list TOTP" "$OUT" '"issuer": "Example"'
  want_has "list HOTP type" "$OUT" '"type": "hotp"'

  must "$TESS" vault status
  want_has "status path" "$OUT" "Path:     $sm_default"
  want_has "status wraps" "$OUT" "Wraps:    passphrase"

  must_fail env TESSERA_PASSPHRASE=wrong-passphrase "$TESS" code "$sm_hotp" --no-copy
  pass "a wrong passphrase is rejected"

  XDG_DATA_HOME="$W/xdg"
  must "$TESS" vault status
  want_has "absolute XDG_DATA_HOME" "$OUT" "Path:     $W/xdg/tessera/vault.json"
  want_has "missing vault" "$OUT" "File:     not found"
  TESSERA_VAULT=$sm_vault
  export TESSERA_VAULT
  must "$TESS" vault status
  want_has "TESSERA_VAULT" "$OUT" "Path:     $TESSERA_VAULT"
}

suite_static() {
  check_static "$TESS"
  case $(uname -m) in
    aarch64) st_arch=arm64 ;;
    x86_64) st_arch=amd64 ;;
    *) fail "unsupported machine $(uname -m)" ;;
  esac
  for st_f in /dist/*_linux_"$st_arch"*/tess; do
    if [ -f "$st_f" ]; then
      check_static "$st_f"
    fi
  done
  if [ -e /etc/alpine-release ]; then
    for st_f in /lib/ld-linux-* /lib64/ld-linux-* /lib/libc.so.6 /usr/lib/libc.so.6; do
      if [ -e "$st_f" ]; then
        fail "glibc is present: $st_f"
      fi
    done
    pass "no glibc on this system"
  fi
}

suite_secret_service() {
  start_runtime_dir
  dbus-run-session -- sh "$SELF" --inside secret-service-bus
}

bus_secret_service() {
  printf '%s' "$KEYRING_PASSWORD" | gnome-keyring-daemon --unlock --components=secrets >/dev/null
  if ! wait_until 100 has_bus_name org.freedesktop.secrets; then
    fail "gnome-keyring did not take org.freedesktop.secrets"
  fi
  run_gotest secretstore env TESSERA_DBUS_TEST=1 TESSERA_SECRET_SERVICE_TEST=1 \
    TESSERA_SECRET_SERVICE_TEST_PASSWORD="$KEYRING_PASSWORD" \
    "$BIN/secretstore.test" -test.v -test.count=1 -test.run 'SecretService|LinuxBackendOrder'
  want_skips_only secretstore ''
  want_passed secretstore TestSecretServiceGnomeKeyring TestSecretServiceGnomeKeyringLocked

  make_vault
  must tess_unlocked vault remember
  want_has "remember names the Secret Service" "$OUT" "in the Secret Service"
  must "$TESS" code "$H" --no-copy
  want_eq "code unlocks from the Secret Service" "$OUT" "$HOTP_CODE_0"
  must "$TESS" __complete code ""
  if ! printf '%s\n' "$OUT" | grep -q "^$H	"; then
    fail "completion does not list $H: $(one_line "$OUT")"
  fi
  pass "completion lists $H"
  must "$TESS" vault status
  want_has "status shows the entry" "$OUT" "Secrets:  entry present (Secret Service)"
  must "$TESS" vault forget
  want_has "forget" "$OUT" "Removed the stored passphrase for $TESSERA_VAULT from the Secret Service"
  must_fail "$TESS" code "$H" --no-copy
  want_has "no unlock after forget" "$ERR" "TESSERA_PASSPHRASE"
}

suite_no_secret_service() {
  start_runtime_dir
  cat >"$W/session.conf" <<'EOF'
<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>session</type>
  <listen>unix:tmpdir=/tmp</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <allow send_destination="*" eavesdrop="true"/>
    <allow eavesdrop="true"/>
    <allow own="*"/>
  </policy>
</busconfig>
EOF
  dbus-run-session --config-file="$W/session.conf" -- sh "$SELF" --inside no-secret-service-bus
}

bus_no_secret_service() {
  if [ -z "${DBUS_SESSION_BUS_ADDRESS:-}" ]; then
    fail "no session bus"
  fi
  make_vault
  must_fail tess_unlocked vault remember
  want_has "Secret Service reason" "$ERR" "Secret Service: nothing provides org.freedesktop.secrets"
  want_has "kernel keyring reason" "$ERR" "kernel keyring: keyctl blocked (seccomp or kernel config)"
  want_has "passphrase hint" "$ERR" "TESSERA_PASSPHRASE"
  must "$TESS" vault status
  want_has "status reasons" "$OUT" "Secrets:  unavailable: Secret Service: nothing provides org.freedesktop.secrets"
  must_fail "$TESS" code "$H" --no-copy
  want_has "unlock without a store" "$ERR" "TESSERA_PASSPHRASE"
  must tess_unlocked code "$H" --no-copy
  want_eq "TESSERA_PASSPHRASE still unlocks" "$OUT" "$HOTP_CODE_0"
}

suite_keyctl() {
  run_gotest secretstore env TESSERA_KEYCTL_TEST=1 \
    "$BIN/secretstore.test" -test.v -test.count=1 -test.run 'Keyctl|Keyring'
  want_skips_only secretstore 'TestKeyctlChildProcess|TestKeyctlBlocked|TestSecretServiceGnomeKeyring(Locked)?'
  want_passed secretstore TestKeyctlRoundTrip TestKeyctlExpiry TestKeyctlPermissions TestKeyctlSecondProcess

  make_vault
  must env TESSERA_SECRET_BACKEND=kernel-keyring TESSERA_PASSPHRASE="$PASSPHRASE" "$TESS" vault remember --for 30s
  want_has "remember with --for" "$OUT" "in the kernel keyring until "
  must keyctl session - "$TESS" code "$H" --no-copy
  want_eq "read from a fresh session keyring" "$(printf '%s\n' "$OUT" | tail -n 1)" "$HOTP_CODE_0"
  must "$TESS" vault status
  want_has "status shows the entry" "$OUT" "Secrets:  entry present (kernel keyring)"
  must "$TESS" vault forget
  want_has "forget" "$OUT" "Removed the stored passphrase for $TESSERA_VAULT from the kernel keyring"
  must_fail "$TESS" code "$H" --no-copy
  pass "no unlock after forget"

  must env TESSERA_SECRET_BACKEND=kernel-keyring TESSERA_PASSPHRASE="$PASSPHRASE" "$TESS" vault remember --for 2s
  must "$TESS" code "$H" --no-copy
  want_eq "unlock before expiry" "$OUT" "$HOTP_CODE_0"
  if ! wait_until 150 is_vault_locked; then
    fail "the kernel keyring entry did not expire"
  fi
  pass "the entry expires"
  must "$TESS" vault status
  want_has "status after expiry" "$OUT" "Secrets:  no entry"
}

is_vault_locked() {
  if "$TESS" code "$H" --no-copy </dev/null >/dev/null 2>&1; then
    return 1
  fi
  return 0
}

suite_keyctl_blocked() {
  run_gotest secretstore env TESSERA_KEYCTL_TEST=blocked \
    "$BIN/secretstore.test" -test.v -test.count=1 -test.run TestKeyctlBlocked
  want_passed secretstore TestKeyctlBlocked
  make_vault
  must_fail env TESSERA_SECRET_BACKEND=kernel-keyring TESSERA_PASSPHRASE="$PASSPHRASE" "$TESS" vault remember
  want_has "remember reports blocked" "$ERR" "kernel keyring: keyctl blocked (seccomp or kernel config)"
  must "$TESS" vault status
  want_has "status reports blocked" "$OUT" "kernel keyring: keyctl blocked (seccomp or kernel config)"
}

suite_x11() {
  start_xvfb
  x_no_xsel=$(path_without xsel)
  x_no_xclip=$(path_without xclip)
  run_gotest clipboard-xclip env PATH="$x_no_xsel" TESSERA_CLIPBOARD_TEST=x11 \
    "$BIN/clipboard.test" -test.v -test.count=1 -test.run TestRealClipboardRoundTrip
  want_passed clipboard-xclip TestRealClipboardRoundTrip
  want_has "xclip backend" "$(cat "$W/clipboard-xclip.out")" "backend: xclip"
  run_gotest clipboard-xsel env PATH="$x_no_xclip" TESSERA_CLIPBOARD_TEST=x11 \
    "$BIN/clipboard.test" -test.v -test.count=1 -test.run TestRealClipboardRoundTrip
  want_passed clipboard-xsel TestRealClipboardRoundTrip
  want_has "xsel backend" "$(cat "$W/clipboard-xsel.out")" "backend: xsel"

  make_vault
  check_timed_clear xclip "$x_no_xsel"
  check_timed_clear xsel "$x_no_xclip"
}

suite_wayland() {
  start_sway
  if is_version_at_least "$(wl-copy --version 2>&1 | head -n 1)" 2 3 0; then
    WL_SENSITIVE=true
  else
    WL_SENSITIVE=false
  fi
  run_gotest clipboard env TESSERA_CLIPBOARD_TEST=wayland "$BIN/clipboard.test" -test.v -test.count=1 -test.run TestRealClipboardRoundTrip
  want_passed clipboard TestRealClipboardRoundTrip
  want_has "wl-clipboard backend" "$(cat "$W/clipboard.out")" "backend: wl-clipboard"
  want_has "sensitive hint matches wl-copy" "$(cat "$W/clipboard.out")" "sensitive hint offered: $WL_SENSITIVE"

  make_vault
  check_timed_clear wayland "$PATH"
}

suite_portal() {
  run_gotest screenshot dbus-run-session -- env TESSERA_PORTAL_TEST=1 \
    "$BIN/screenshot.test" -test.v -test.count=1 -test.run TestFakePortal
  want_skips_only screenshot ''
  want_passed screenshot TestFakePortalSuccess
  run_gotest dbusx dbus-run-session -- env TESSERA_DBUS_TEST=1 \
    "$BIN/dbusx.test" -test.v -test.count=1
  want_skips_only dbusx ''
}

suite_screen_wayland() {
  start_sway
  make_vault
  mkdir -p "$W/qr" "$W/fakebin"
  must tess_unlocked export --qr "$W/qr" "$H"
  set -- "$W"/qr/*.png
  if [ $# -ne 1 ] || [ ! -s "$1" ]; then
    fail "export --qr wrote $# files"
  fi
  must swaymsg output '*' bg "$1" center '#ffffff'
  sw_mode=$(swaymsg -p -t get_outputs | sed -n 's/^ *Current mode: \([0-9]*\)x\([0-9]*\).*/\1 \2/p' | head -n 1)
  [ -n "$sw_mode" ] || fail "no output mode from swaymsg"
  sw_w=${sw_mode% *}
  sw_h=${sw_mode#* }
  sw_geometry="$(((sw_w - SELECTION_SIZE) / 2)),$(((sw_h - SELECTION_SIZE) / 2)) ${SELECTION_SIZE}x$SELECTION_SIZE"
  ln -s "$SRC/test/linux/fake-slurp" "$W/fakebin/slurp"
  sw_path="$W/fakebin:$PATH"

  export TESSERA_VAULT="$W/screen-vault.json"
  must tess_unlocked vault init
  must_fail env PATH="$sw_path" FAKE_SLURP_CANCEL=1 TESSERA_PASSPHRASE="$PASSPHRASE" "$TESS" add --screen
  want_has "a cancelled selection" "$ERR" "cancelled"
  must tess_unlocked list --json
  want_lacks "nothing added after cancel" "$OUT" "Fixture"

  sw_tries=0
  while :; do
    sw_tries=$((sw_tries + 1))
    run_cmd env PATH="$sw_path" FAKE_SLURP_GEOMETRY="$sw_geometry" FAKE_SLURP_MARKER="$W/slurp-ran" \
      TESSERA_PASSPHRASE="$PASSPHRASE" "$TESS" add --screen
    if [ "$RC" -eq 0 ] || [ "$sw_tries" -ge 20 ]; then
      break
    fi
    case $ERR in
      *"decode: "* | *"no QR code"*) printf 'retry: the background is not drawn yet: %s\n' "$(one_line "$ERR")" ;;
      *) break ;;
    esac
    sleep 1
  done
  if [ "$RC" -ne 0 ]; then
    fail "add --screen after $sw_tries attempts: $(one_line "$ERR")"
  fi
  want_has "add --screen at $sw_geometry after $sw_tries attempt(s)" "$OUT" "Added Fixture (hotp@example.com) as "
  if [ ! -e "$W/slurp-ran" ]; then
    fail "fake slurp did not run"
  fi
  sw_h=${OUT##* as }
  must tess_unlocked code "$sw_h" --no-copy
  want_eq "the captured account has the fixture secret" "$OUT" "$HOTP_CODE_0"
  check_no_screen_leftovers

  run_gotest screenshot env TESSERA_SCREEN_TEST=wayland "$BIN/screenshot.test" -test.v -test.count=1 -test.run TestRealWaylandCapture
  want_passed screenshot TestRealWaylandCapture
  run_gotest tess env TESSERA_SCREEN_TEST=wayland "$BIN/tess.test" -test.v -test.count=1 -test.run TestAddScreenWaylandEndToEnd
  want_passed tess TestAddScreenWaylandEndToEnd
  check_no_screen_leftovers
}

suite_package() {
  if [ "$(id -u)" -ne 0 ]; then
    fail "the package suite runs as root"
  fi
  pk_m=$(uname -m)
  case $pk_m in
    aarch64) pk_goarch=arm64 ;;
    x86_64) pk_goarch=amd64 ;;
    *) fail "unsupported machine $pk_m" ;;
  esac
  if command -v apk >/dev/null 2>&1; then
    pk_fmt=apk pk_ext=apk
  elif command -v pacman >/dev/null 2>&1; then
    pk_fmt=archlinux pk_ext=pkg.tar.zst
  elif command -v dpkg >/dev/null 2>&1; then
    pk_fmt=deb pk_ext=deb
  elif command -v rpm >/dev/null 2>&1; then
    pk_fmt=rpm pk_ext=rpm
  else
    fail "no package manager found"
  fi
  set --
  for pk_f in /dist/*"$pk_m"*."$pk_ext" /dist/*"$pk_goarch"*."$pk_ext"; do
    if [ -f "$pk_f" ]; then
      set -- "$@" "$pk_f"
    fi
  done
  if [ $# -ne 1 ]; then
    fail "want one $pk_fmt package for $pk_m in the dist directory, found $#: $*"
  fi
  pk_file=$1

  pk_zsh=/usr/share/zsh/site-functions/_tess
  if [ "$pk_fmt" = deb ]; then
    pk_zsh=/usr/share/zsh/vendor-completions/_tess
  fi
  pk_license=/usr/share/licenses/tessera/LICENSE
  if [ "$pk_fmt" = deb ]; then
    pk_license=/usr/share/doc/tessera/copyright
  fi
  set -- /usr/bin/tess /usr/share/bash-completion/completions/tess "$pk_zsh" \
    /usr/share/fish/vendor_completions.d/tess.fish /usr/share/man/man1/tess.1.gz \
    "$pk_license"

  case $pk_fmt in
    apk) must apk add --no-network --allow-untrusted "$pk_file" ;;
    archlinux) must pacman -U --noconfirm "$pk_file" ;;
    deb) must dpkg -i "$pk_file" ;;
    rpm)
      if command -v dnf >/dev/null 2>&1; then
        must dnf install -y --nogpgcheck --disablerepo='*' "$pk_file"
      else
        must rpm -i --nosignature "$pk_file"
      fi
      ;;
  esac
  pass "installed ${pk_file##*/}"

  must /usr/bin/tess --version
  want_has "installed binary runs" "$OUT" "tess version "
  for pk_p in "$@"; do
    want_file "installed $pk_p" "$pk_p"
  done
  must gzip -t /usr/share/man/man1/tess.1.gz

  case $pk_fmt in
    apk) must apk del --no-network tessera ;;
    archlinux) must pacman -R --noconfirm tessera ;;
    deb) must dpkg --purge tessera ;;
    rpm) must rpm -e tessera ;;
  esac
  pass "removed tessera"
  for pk_p in "$@"; do
    want_gone "removed $pk_p" "$pk_p"
  done
  for pk_p in /usr/share/man/man1/tess*.1.gz; do
    want_gone "removed man pages" "$pk_p"
  done
}

# A container engine can serve files that no longer match the host; a run against them proves nothing.
same_as_host() {
  sh_got=$(sh "$SRC/test/linux/digest.sh" "$1" "$2" 2>/dev/null) || sh_got=
  if [ -z "$3" ] || [ "$sh_got" != "$3" ]; then
    fail "$2 in the container differs from the host: the files changed during the run, or the container engine serves stale files"
  fi
}

inside_main() {
  SELF=$0
  SRC=$(cd "$(dirname "$0")/../.." && pwd -P)
  BIN=${TESSERA_IT_BIN:-}
  if [ -z "$BIN" ]; then
    fail "TESSERA_IT_BIN is not set; run test/linux/run.sh from the host"
  fi
  TESS=$BIN/tess
  if [ -z "${TESSERA_IT_VERIFIED:-}" ]; then
    same_as_host tree "$SRC" "${TESSERA_IT_TREE_DIGEST:-}"
    same_as_host dir "$BIN" "${TESSERA_IT_BIN_DIGEST:-}"
    if [ "$1" = package ]; then
      same_as_host dir /dist "${TESSERA_IT_DIST_DIGEST:-}"
    fi
    export TESSERA_IT_VERIFIED=1
  fi
  W=${TESSERA_IT_WORK:-}
  if [ -z "$W" ]; then
    W=$(mktemp -d)
    export TESSERA_IT_WORK="$W"
  fi
  export HOME="$W/home" XDG_DATA_HOME="$W/data" TESSERA_VAULT="$W/vault.json"
  unset TESSERA_PASSPHRASE TESSERA_SECRET_BACKEND TESSERA_CLIPBOARD TESSERA_CLIP_CLEAR
  mkdir -p "$HOME"
  cd "$W"

  case $1 in
    smoke) suite_smoke ;;
    static) suite_static ;;
    secret-service) suite_secret_service ;;
    secret-service-bus) bus_secret_service ;;
    no-secret-service) suite_no_secret_service ;;
    no-secret-service-bus) bus_no_secret_service ;;
    keyctl) suite_keyctl ;;
    keyctl-blocked) suite_keyctl_blocked ;;
    x11) suite_x11 ;;
    wayland) suite_wayland ;;
    portal) suite_portal ;;
    screen-wayland) suite_screen_wayland ;;
    package) suite_package ;;
    *) fail "unknown suite $1" ;;
  esac
  pass "suite $1"
}

if [ "${1:-}" = --inside ]; then
  [ $# -eq 2 ] || usage
  inside_main "$2"
elif [ "${1:-}" = --go-test ]; then
  [ $# -eq 1 ] || usage
  go_test_main
elif [ "${1:-}" = --inside-go-test ]; then
  [ $# -eq 1 ] || usage
  inside_go_test_main
else
  host_main "$@"
fi
