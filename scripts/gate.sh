#!/bin/sh
# shellcheck disable=SC2329
set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PATH="$PATH:$(go env GOPATH 2>/dev/null)/bin"
export PATH
LOG_DIR="${GATE_LOG_DIR:-$ROOT/.status/gate}"
mkdir -p "$LOG_DIR"
FAILED=0

run_check() {
  name="$1"
  shift
  log="$LOG_DIR/$name.log"
  if "$@" >"$log" 2>&1; then
    printf 'ok    %s\n' "$name"
  else
    FAILED=1
    printf 'FAIL  %s  (%s)\n' "$name" "$log"
    tail -n 15 "$log"
  fi
}

check_gofmt() {
  out="$(gofmt -l "$ROOT/go")"
  [ -z "$out" ] || { printf '%s\n' "$out"; return 1; }
}

check_swift_verify() {
  bin="$LOG_DIR/tessera_verify"
  swiftc -O "$ROOT"/swift/Sources/TesseraCore/*.swift "$ROOT/swift/Tools/verify/main.swift" -o "$bin" &&
    "$bin" "$ROOT/spec" "$ROOT/go/internal/importers/testdata"
}

check_govuln() {
  dir="$LOG_DIR/vuln"
  mkdir -p "$dir"
  for os in darwin linux windows; do
    GOOS="$os" GOARCH=arm64 CGO_ENABLED=0 go -C "$ROOT/go" build -o "$dir/tess-$os" ./cmd/tess || return 1
    govulncheck -mode=binary "$dir/tess-$os" || return 1
  done
}

check_app_build() {
  build_log="$LOG_DIR/xcodebuild.log"
  cd "$ROOT/swift" && xcodegen generate || return 1
  xcodebuild -project Tessera.xcodeproj -scheme Tessera -configuration Debug \
    -derivedDataPath "$LOG_DIR/dd" CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY="-" \
    DEVELOPMENT_TEAM= SWIFT_TREAT_WARNINGS_AS_ERRORS=YES \
    build-for-testing >"$build_log" 2>&1 || {
    grep -E "\.swift:[0-9]+:[0-9]+: (error|warning)" "$build_log" | sort -u | head -n 40
    tail -n 5 "$build_log"
    return 1
  }
}

check_linux() {
  docker run --rm -v "$ROOT":/src:ro -w /src/go -e GOFLAGS=-buildvcs=false \
    -e GOCACHE=/tmp/gocache -e GOMODCACHE=/tmp/gomod golang:1.27 \
    sh -c 'go vet ./... && go test ./...'
}

gate_go() {
  run_check go-build go -C "$ROOT/go" build ./...
  run_check go-vet go -C "$ROOT/go" vet ./...
  run_check go-fmt check_gofmt
  run_check go-vet-gendocs go -C "$ROOT/go" vet -tags gendocs ./cmd/tess
  run_check go-test-gendocs env XDG_DATA_HOME="$LOG_DIR/xdg" TESSERA_VAULT= go -C "$ROOT/go" test -race -tags gendocs ./cmd/tess
  run_check go-test env XDG_DATA_HOME="$LOG_DIR/xdg" TESSERA_VAULT= go -C "$ROOT/go" test -race ./...
  if command -v govulncheck >/dev/null 2>&1; then
    run_check go-vuln check_govuln
  fi
  if command -v staticcheck >/dev/null 2>&1; then
    run_check go-staticcheck sh -c "cd '$ROOT/go' && staticcheck ./..."
  fi
  for target_os in linux windows; do
    run_check "go-vet-$target_os" env GOOS="$target_os" GOARCH=amd64 go -C "$ROOT/go" vet ./...
    run_check "go-staticcheck-$target_os" sh -c "cd '$ROOT/go' && GOOS=$target_os GOARCH=amd64 staticcheck ./..."
  done
  for target in linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/amd64; do
    run_check "go-cross-$(printf '%s' "$target" | tr / -)" \
      env GOOS="${target%/*}" GOARCH="${target#*/}" CGO_ENABLED=0 go -C "$ROOT/go" build -o /dev/null ./cmd/tess
  done
}

gate_swift() {
  run_check swift-verify check_swift_verify
  run_check swift-test sh -c "cd '$ROOT/swift' && swift test"
}

check_app_unit() {
  unit_log="$LOG_DIR/xcodebuild-unit.log"
  cd "$ROOT/swift" || return 1
  xcodebuild -project Tessera.xcodeproj -scheme Tessera -configuration Debug \
    -derivedDataPath "$LOG_DIR/dd" CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY="-" \
    DEVELOPMENT_TEAM= SWIFT_TREAT_WARNINGS_AS_ERRORS=YES \
    test -only-testing:TesseraAppTests >"$unit_log" 2>&1 || {
    grep -E "error:|failed|\.swift:[0-9]+: " "$unit_log" | sort -u | head -n 40
    tail -n 5 "$unit_log"
    return 1
  }
  grep -E "Executed [0-9]+ tests" "$unit_log" | tail -n 1
}

gate_app() {
  run_check app-build check_app_build
  run_check app-unit check_app_unit
}

gate_shell() {
  run_check shellcheck shellcheck "$ROOT/install.sh" "$ROOT/test/install_test.sh" "$ROOT/scripts/gate.sh" \
    "$ROOT/scripts/release-check.sh" "$ROOT/test/linux/run.sh" "$ROOT/test/linux/fake-slurp"
  run_check install-test "$ROOT/test/install_test.sh"
}

gate_site() {
  run_check site-build sh -c "cd '$ROOT/site' && npm run build --silent"
}

gate_release() {
  "$ROOT/scripts/release-check.sh" || FAILED=1
}

gate_linux() {
  run_check linux-test check_linux
  dist="$LOG_DIR/release/tree-a/go/dist"
  if [ -d "$dist" ]; then
    GATE_LINUX_DIST="$dist" "$ROOT/test/linux/run.sh" || FAILED=1
  else
    "$ROOT/test/linux/run.sh" || FAILED=1
  fi
}

[ "$#" -gt 0 ] || set -- go swift app shell
for target in "$@"; do
  case "$target" in
    go) gate_go ;;
    swift) gate_swift ;;
    app) gate_app ;;
    shell) gate_shell ;;
    site) gate_site ;;
    release) gate_release ;;
    linux) gate_linux ;;
    all) gate_go; gate_swift; gate_app; gate_shell; gate_site; gate_release; gate_linux ;;
    *) printf 'unknown target: %s\n' "$target"; exit 2 ;;
  esac
done
exit "$FAILED"
