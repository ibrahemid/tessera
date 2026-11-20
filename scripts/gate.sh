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

# Derived data sits outside the repository: a failing hosted test opens its object files, which never returns under a privacy-protected folder.
derived_data() {
  dd="${GATE_DD_DIR:-$HOME/Library/Caches/tessera-gate/$(printf '%s' "$ROOT" | shasum | cut -c1-12)}"
  mkdir -p "$dd" || return 1
  if [ ! -L "$LOG_DIR/dd" ]; then
    rm -rf "$LOG_DIR/dd"
    ln -s "$dd" "$LOG_DIR/dd"
  fi
  printf '%s' "$dd"
}

AD_HOC_ENTITLEMENTS=App/Resources/Tessera-AdHoc.entitlements
KEYCHAIN_GROUPS="[\"\$(AppIdentifierPrefix)\$(PRODUCT_BUNDLE_IDENTIFIER)\"]"

check_entitlements() {
  dir="$LOG_DIR/entitlements"
  main="$ROOT/swift/App/Resources/Tessera.entitlements"
  mkdir -p "$dir" || return 1
  groups="$(plutil -extract keychain-access-groups json -o - "$main")" || return 1
  [ "$groups" = "$KEYCHAIN_GROUPS" ] || { printf 'keychain-access-groups: %s\n' "$groups"; return 1; }
  cp "$main" "$dir/main-without-groups.plist" &&
    plutil -remove keychain-access-groups "$dir/main-without-groups.plist" &&
    plutil -convert json -r -o "$dir/main.json" "$dir/main-without-groups.plist" &&
    plutil -convert json -r -o "$dir/adhoc.json" "$ROOT/swift/$AD_HOC_ENTITLEMENTS" || return 1
  diff "$dir/main.json" "$dir/adhoc.json"
}

check_app_build() {
  build_log="$LOG_DIR/xcodebuild.log"
  dd="$(derived_data)" || return 1
  cd "$ROOT/swift" && xcodegen generate || return 1
  xcodebuild -project Tessera.xcodeproj -scheme Tessera -configuration Debug \
    -derivedDataPath "$dd" CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY="-" \
    CODE_SIGN_ENTITLEMENTS="$AD_HOC_ENTITLEMENTS" \
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
  dd="$(derived_data)" || return 1
  cd "$ROOT/swift" || return 1
  xcodebuild -project Tessera.xcodeproj -scheme Tessera -configuration Debug \
    -derivedDataPath "$dd" CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY="-" \
    CODE_SIGN_ENTITLEMENTS="$AD_HOC_ENTITLEMENTS" \
    DEVELOPMENT_TEAM= SWIFT_TREAT_WARNINGS_AS_ERRORS=YES \
    -test-timeouts-enabled YES -default-test-execution-time-allowance 60 \
    -maximum-test-execution-time-allowance 120 \
    test -only-testing:TesseraAppTests >"$unit_log" 2>&1 || {
    grep -E "error:|failed|\.swift:[0-9]+: " "$unit_log" | sort -u | head -n 40
    tail -n 5 "$unit_log"
    return 1
  }
  grep -E "Executed [0-9]+ tests" "$unit_log" | tail -n 1
}

gate_app() {
  run_check app-entitlements check_entitlements
  run_check app-build check_app_build
  run_check app-unit check_app_unit
}

gate_shell() {
  run_check shellcheck shellcheck "$ROOT/install.sh" "$ROOT/test/install_test.sh" "$ROOT/scripts/gate.sh" \
    "$ROOT/scripts/release-check.sh" "$ROOT/test/linux/run.sh" "$ROOT/test/linux/fake-slurp" \
    "$ROOT/integrations/alfred/build.sh"
  run_check install-test "$ROOT/test/install_test.sh"
}

gate_site() {
  run_check site-build sh -c "cd '$ROOT/site' && npm run build --silent"
}

check_raycast_deps() (
  cd "$ROOT/integrations/raycast" || exit 1
  [ -d node_modules ] || npm ci --no-audit --no-fund
)

check_integrations_real_tess() (
  dir="$LOG_DIR/integrations-tess"
  rm -rf "$dir"
  mkdir -p "$dir/home" "$dir/xdg" || exit 1
  go -C "$ROOT/go" build -o "$dir/tess" ./cmd/tess || exit 1
  HOME="$dir/home"
  XDG_DATA_HOME="$dir/xdg"
  TESSERA_VAULT="$dir/vault.json"
  TESSERA_PASSPHRASE=test-passphrase-1
  TESS_TEST_BINARY="$dir/tess"
  export HOME XDG_DATA_HOME TESSERA_VAULT TESSERA_PASSPHRASE TESS_TEST_BINARY
  "$dir/tess" vault init </dev/null || exit 1
  printf '%s\n' \
    'otpauth://totp/GitHub:alice@example.com?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=GitHub' \
    'otpauth://hotp/Bank:bob?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=Bank&counter=3' \
    'otpauth://totp/Steam:gamer?secret=JBSWY3DPEHPK3PXP&issuer=Steam&encoder=steam' |
    "$dir/tess" add - || exit 1
  (cd "$ROOT/integrations/raycast" && ./node_modules/.bin/vitest run test/real-tess.test.ts) >"$dir/raycast.out" 2>&1
  raycast_status=$?
  cat "$dir/raycast.out"
  [ "$raycast_status" -eq 0 ] || exit 1
  if grep -q skipped "$dir/raycast.out"; then exit 1; fi
  (cd "$ROOT/integrations/alfred" && node --test --test-reporter=tap test/real-tess.test.mjs) >"$dir/alfred.out" 2>&1
  alfred_status=$?
  cat "$dir/alfred.out"
  [ "$alfred_status" -eq 0 ] || exit 1
  grep -q '^# skipped 0$' "$dir/alfred.out"
)

gate_integrations() {
  raycast="$ROOT/integrations/raycast"
  alfred="$ROOT/integrations/alfred"
  unit_env="env -u TESS_TEST_BINARY -u TESSERA_VAULT -u TESSERA_PASSPHRASE"
  run_check raycast-deps check_raycast_deps
  run_check raycast-build sh -c "cd '$raycast' && ./node_modules/.bin/ray build --output dist"
  run_check raycast-eslint sh -c "cd '$raycast' && ./node_modules/.bin/eslint src test"
  run_check raycast-prettier sh -c "cd '$raycast' && ./node_modules/.bin/prettier --check src test package.json README.md"
  run_check raycast-tsc sh -c "cd '$raycast' && ./node_modules/.bin/tsc --noEmit"
  run_check raycast-vitest sh -c "cd '$raycast' && $unit_env ./node_modules/.bin/vitest run"
  run_check alfred-test sh -c "cd '$alfred' && $unit_env node --test 'test/*.test.mjs'"
  run_check alfred-plist plutil -lint "$alfred/workflow/info.plist"
  run_check alfred-build "$alfred/build.sh"
  run_check integrations-real-tess check_integrations_real_tess
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
    integrations) gate_integrations ;;
    release) gate_release ;;
    linux) gate_linux ;;
    all) gate_go; gate_swift; gate_app; gate_shell; gate_site; gate_integrations; gate_release; gate_linux ;;
    *) printf 'unknown target: %s\n' "$target"; exit 2 ;;
  esac
done
exit "$FAILED"
