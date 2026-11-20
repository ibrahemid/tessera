//go:build darwin

package secretstore

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const testService = "tessera-test"

func swapSecurity(t *testing.T, fn func(ctx context.Context, args ...string) *exec.Cmd) {
	t.Helper()
	prev := securityCommand
	securityCommand = fn
	t.Cleanup(func() { securityCommand = prev })
}

func forbidSecurity(t *testing.T) {
	t.Helper()
	swapSecurity(t, func(ctx context.Context, args ...string) *exec.Cmd {
		t.Errorf("security ran with %q", args)
		return exec.CommandContext(ctx, "/usr/bin/false")
	})
}

func exitWith(code string) func(ctx context.Context, args ...string) *exec.Cmd {
	return func(ctx context.Context, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/bin/sh", "-c", "exit "+code)
	}
}

func TestKeychainRoundTrip(t *testing.T) {
	if os.Getenv("TESSERA_KEYCHAIN_TEST") == "" {
		t.Skip("set TESSERA_KEYCHAIN_TEST=1 to test against the login keychain")
	}
	k := keychainBackend{service: testService}
	ctx := context.Background()
	const account = "/tmp/tessera-test-vault.json"
	const secret = `p@ss "with" \slashes and $pecial`
	t.Cleanup(func() { _ = k.Delete(context.Background(), account) })

	if err := k.Available(ctx); err != nil {
		t.Fatalf("Available = %v", err)
	}
	if err := k.Store(ctx, account, secret, 0); err != nil {
		t.Fatalf("Store = %v", err)
	}
	if got, ok, err := k.Lookup(ctx, account); err != nil || !ok || got != secret {
		t.Fatalf("Lookup = %q, %v, %v", got, ok, err)
	}
	if has, err := k.Has(ctx, account); err != nil || !has {
		t.Fatalf("Has = %v, %v", has, err)
	}
	if err := k.Store(ctx, account, "replaced", 0); err != nil {
		t.Fatalf("Store over an existing entry = %v", err)
	}
	if got, ok, err := k.Lookup(ctx, account); err != nil || !ok || got != "replaced" {
		t.Fatalf("Lookup after replace = %q, %v, %v", got, ok, err)
	}
	if err := k.Delete(ctx, account); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	if has, err := k.Has(ctx, account); err != nil || has {
		t.Fatalf("Has after Delete = %v, %v", has, err)
	}
	if got, ok, err := k.Lookup(ctx, account); err != nil || ok || got != "" {
		t.Fatalf("Lookup after Delete = %q, %v, %v", got, ok, err)
	}
	if err := k.Delete(ctx, account); err != nil {
		t.Fatalf("Delete of a missing entry = %v", err)
	}
}

func TestKeychainRegistration(t *testing.T) {
	t.Setenv(backendEnv, "")
	if got := Backends(); len(got) != 1 || got[0].ID() != "keychain" || got[0].Name() != "login keychain" {
		t.Fatalf("Backends = %v", got)
	}
	b, err := ForStore(context.Background())
	if err != nil {
		t.Fatalf("ForStore = %v", err)
	}
	if k, ok := b.(keychainBackend); !ok || k.service != "tessera" {
		t.Fatalf("ForStore = %#v; want the keychain backend with service tessera", b)
	}

	t.Setenv(backendEnv, "keychain")
	if b, err := ForStore(context.Background()); err != nil || b.ID() != "keychain" {
		t.Fatalf("ForStore forced keychain = %v, %v", b, err)
	}

	t.Setenv(backendEnv, "secret-service")
	_, err = ForStore(context.Background())
	if !errors.Is(err, ErrUnknownBackend) || !strings.Contains(err.Error(), "one of: keychain") {
		t.Fatalf("ForStore with a backend from another system = %v", err)
	}
}

type stubBackend struct{ id string }

func (s stubBackend) ID() string                                               { return s.id }
func (s stubBackend) Name() string                                             { return s.id }
func (stubBackend) Available(context.Context) error                            { return nil }
func (stubBackend) Store(context.Context, string, string, time.Duration) error { return nil }
func (stubBackend) Lookup(context.Context, string) (string, bool, error)       { return "", false, nil }
func (stubBackend) Has(context.Context, string) (bool, error)                  { return false, nil }
func (stubBackend) Delete(context.Context, string) error                       { return nil }

func TestRegisterSortsByPriority(t *testing.T) {
	registryMu.Lock()
	saved := slices.Clone(registry)
	registryMu.Unlock()
	t.Cleanup(func() {
		registryMu.Lock()
		registry = saved
		registryMu.Unlock()
	})

	register(20, func() Backend { return stubBackend{"late"} })
	register(5, func() Backend { return stubBackend{"early"} })
	register(10, func() Backend { return stubBackend{"tie"} })

	var got []string
	for _, b := range Backends() {
		got = append(got, b.ID())
	}
	if want := []string{"early", "keychain", "tie", "late"}; !slices.Equal(got, want) {
		t.Fatalf("Backends = %v, want %v", got, want)
	}
}

func TestKeychainAvailable(t *testing.T) {
	if err := (keychainBackend{service: testService}).Available(context.Background()); err != nil {
		t.Fatalf("Available = %v", err)
	}
}

func TestInvalidKeyNeverReachesSecurity(t *testing.T) {
	forbidSecurity(t)
	t.Cleanup(SwapForTest(keychainBackend{service: testService}))
	k := keychainBackend{service: testService}
	ctx := context.Background()
	for _, bad := range []string{
		"",
		"\n",
		"\r",
		"\x00",
		"/tmp/v.json\ndelete-generic-password -s tessera",
		"/tmp/v.json\rdelete-generic-password -s tessera",
		"/tmp/v\x00.json",
	} {
		if err := k.Store(ctx, bad, "secret", 0); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Store(%q) = %v", bad, err)
		}
		if _, _, err := k.Lookup(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Lookup(%q) = %v", bad, err)
		}
		if _, err := k.Has(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Has(%q) = %v", bad, err)
		}
		if err := k.Delete(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Delete(%q) = %v", bad, err)
		}
		if _, _, _, err := Lookup(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("package Lookup(%q) = %v", bad, err)
		}
		if _, err := Forget(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Forget(%q) = %v", bad, err)
		}
		if ps := Locate(ctx, bad); len(ps) != 1 || !errors.Is(ps[0].Err, ErrInvalidKey) {
			t.Fatalf("Locate(%q) = %+v", bad, ps)
		}
	}
}

func TestStoreRejectsBeforeSecurity(t *testing.T) {
	forbidSecurity(t)
	k := keychainBackend{service: testService}
	ctx := context.Background()
	err := k.Store(ctx, "/tmp/v.json", "secret", time.Hour)
	if !errors.Is(err, ErrTTLUnsupported) || !strings.Contains(err.Error(), "login keychain") {
		t.Fatalf("Store with ttl = %v", err)
	}
	for _, bad := range []string{"", "a\nb", "a\rb", "a\x00b", strings.Repeat("x", 2049)} {
		if err := k.Store(ctx, "/tmp/v.json", bad, 0); !errors.Is(err, ErrInvalidSecret) {
			t.Fatalf("Store(secret %q) = %v", bad, err)
		}
	}
}

func readEnv(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read child env: %v", err)
	}
	return strings.Split(string(b), "\n")
}

func TestStoreFeedsSecurityOneQuotedLineWithFilteredEnv(t *testing.T) {
	dir := t.TempDir()
	stdinFile := filepath.Join(dir, "stdin")
	addEnv := filepath.Join(dir, "add-env")
	findEnv := filepath.Join(dir, "find-env")
	t.Setenv("TESSERA_PASSPHRASE", "parent-only-1")
	t.Setenv("TESSERA_NEW_PASSPHRASE", "parent-only-2")
	t.Setenv("TESSERA_MERGE_PASSPHRASE", "parent-only-3")
	t.Setenv("TESSERA_CHILD_MARKER", "kept")

	const key = `/tmp/a "b" \c.json`
	const secret = `p@ss "with" \slashes and $pecial`
	var calls [][]string
	swapSecurity(t, func(ctx context.Context, args ...string) *exec.Cmd {
		calls = append(calls, args)
		if slices.Equal(args, []string{"-i"}) {
			return exec.CommandContext(ctx, "/bin/sh", "-c", `cat >"$1"; env >"$2"`, "sh", stdinFile, addEnv)
		}
		return exec.CommandContext(ctx, "/bin/sh", "-c", `env >"$1"; printf '%s\n' "$2"`, "sh", findEnv, secret)
	})

	k := keychainBackend{service: testService}
	if err := k.Store(context.Background(), key, secret, 0); err != nil {
		t.Fatalf("Store = %v", err)
	}

	wantCalls := [][]string{
		{"-i"},
		{"find-generic-password", "-s", testService, "-a", key, "-w"},
	}
	if !slices.EqualFunc(calls, wantCalls, slices.Equal) {
		t.Fatalf("security calls = %q, want %q", calls, wantCalls)
	}
	for _, args := range calls {
		if slices.Contains(args, secret) {
			t.Fatal("the secret appeared in security's argv")
		}
	}

	stdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatal(err)
	}
	wantLine := `add-generic-password -U -s "tessera-test" -a "/tmp/a \"b\" \\c.json" -w "p@ss \"with\" \\slashes and $pecial"` + "\n"
	if string(stdin) != wantLine {
		t.Fatalf("stdin = %q, want %q", stdin, wantLine)
	}

	for _, path := range []string{addEnv, findEnv} {
		env := readEnv(t, path)
		if !slices.Contains(env, "TESSERA_CHILD_MARKER=kept") {
			t.Fatalf("%s: child env lost an ordinary variable", filepath.Base(path))
		}
		for _, line := range env {
			for _, name := range []string{"TESSERA_PASSPHRASE=", "TESSERA_NEW_PASSPHRASE=", "TESSERA_MERGE_PASSPHRASE="} {
				if strings.HasPrefix(line, name) {
					t.Fatalf("%s: child env carries %s", filepath.Base(path), strings.TrimSuffix(name, "="))
				}
			}
		}
	}
}

func TestSecurityExitCodes(t *testing.T) {
	k := keychainBackend{service: testService}
	ctx := context.Background()
	const key = "/tmp/v.json"

	swapSecurity(t, exitWith("44"))
	if got, ok, err := k.Lookup(ctx, key); err != nil || ok || got != "" {
		t.Fatalf("Lookup on missing = %q, %v, %v", got, ok, err)
	}
	if has, err := k.Has(ctx, key); err != nil || has {
		t.Fatalf("Has on missing = %v, %v", has, err)
	}
	if err := k.Delete(ctx, key); err != nil {
		t.Fatalf("Delete on missing = %v", err)
	}

	swapSecurity(t, exitWith("0"))
	if has, err := k.Has(ctx, key); err != nil || !has {
		t.Fatalf("Has on present = %v, %v", has, err)
	}
	if err := k.Delete(ctx, key); err != nil {
		t.Fatalf("Delete on present = %v", err)
	}

	swapSecurity(t, exitWith("1"))
	if _, _, err := k.Lookup(ctx, key); !errors.Is(err, errKeychainRead) {
		t.Fatalf("Lookup on failure = %v", err)
	}
	if _, err := k.Has(ctx, key); !errors.Is(err, errKeychainQuery) {
		t.Fatalf("Has on failure = %v", err)
	}
	if err := k.Delete(ctx, key); !errors.Is(err, errKeychainDelete) {
		t.Fatalf("Delete on failure = %v", err)
	}
	if err := k.Store(ctx, key, "secret", 0); !errors.Is(err, errKeychainWrite) {
		t.Fatalf("Store on failure = %v", err)
	}
}

func TestStoreVerifiesTheEntryPersisted(t *testing.T) {
	k := keychainBackend{service: testService}
	ctx := context.Background()
	for name, find := range map[string]string{
		"missing":   "exit 44",
		"different": `printf '%s\n' other`,
	} {
		swapSecurity(t, func(ctx context.Context, args ...string) *exec.Cmd {
			if slices.Equal(args, []string{"-i"}) {
				return exec.CommandContext(ctx, "/bin/sh", "-c", "cat >/dev/null")
			}
			return exec.CommandContext(ctx, "/bin/sh", "-c", find)
		})
		if err := k.Store(ctx, "/tmp/v.json", "secret", 0); !errors.Is(err, errKeychainPersisted) {
			t.Fatalf("%s: Store = %v", name, err)
		}
	}
}

func TestCancelledContextIsReported(t *testing.T) {
	swapSecurity(t, exitWith("0"))
	k := keychainBackend{service: testService}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := k.Lookup(ctx, "/tmp/v.json"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Lookup = %v", err)
	}
	if _, err := k.Has(ctx, "/tmp/v.json"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Has = %v", err)
	}
	if err := k.Delete(ctx, "/tmp/v.json"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete = %v", err)
	}
	if err := k.Store(ctx, "/tmp/v.json", "secret", 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("Store = %v", err)
	}
}
