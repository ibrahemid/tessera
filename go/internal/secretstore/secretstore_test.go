package secretstore_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/secretstore"
	"github.com/ibrahemid/tessera/go/internal/secretstore/secretstoretest"
)

const key = "/home/u/.local/share/tessera/vault.json"

func newFake(id, name string) *secretstoretest.Fake {
	return &secretstoretest.Fake{BackendID: id, BackendName: name}
}

func unavailable(f *secretstoretest.Fake, reason string) *secretstoretest.Fake {
	f.AvailableErr = &secretstore.UnavailableError{Backend: f.BackendName, Reason: reason}
	return f
}

func swap(t *testing.T, bs ...secretstore.Backend) {
	t.Helper()
	t.Setenv("TESSERA_SECRET_BACKEND", "")
	t.Cleanup(secretstore.SwapForTest(bs...))
}

func ids(bs []secretstore.Backend) []string {
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		out = append(out, b.ID())
	}
	return out
}

func methods(f *secretstoretest.Fake) []string {
	var out []string
	for _, c := range f.Calls() {
		out = append(out, c.Method)
	}
	return out
}

func TestBackendsKeepPriorityOrder(t *testing.T) {
	a, b, c := newFake("a", "A"), newFake("b", "B"), newFake("c", "C")
	swap(t, a, b, c)
	if got := ids(secretstore.Backends()); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("Backends = %v", got)
	}

	got, err := secretstore.ForStore(context.Background())
	if err != nil || got != a {
		t.Fatalf("ForStore = %v, %v; want a", got, err)
	}
	unavailable(a, "down")
	got, err = secretstore.ForStore(context.Background())
	if err != nil || got != b {
		t.Fatalf("ForStore with a unavailable = %v, %v; want b", got, err)
	}

	b.Put(key, "from-b")
	c.Put(key, "from-c")
	secret, from, found, err := secretstore.Lookup(context.Background(), key)
	if err != nil || !found || secret != "from-b" || from != b {
		t.Fatalf("Lookup = %q, %v, %v, %v; want from-b from b", secret, from, found, err)
	}
	if calls := methods(c); len(calls) != 0 {
		t.Fatalf("Lookup consulted c after a hit in b: %v", calls)
	}
}

func TestSwapForTestRestores(t *testing.T) {
	before := ids(secretstore.Backends())
	restore := secretstore.SwapForTest(newFake("x", "X"))
	if got := ids(secretstore.Backends()); !slices.Equal(got, []string{"x"}) {
		t.Fatalf("Backends while swapped = %v", got)
	}
	restore()
	if got := ids(secretstore.Backends()); !slices.Equal(got, before) {
		t.Fatalf("Backends after restore = %v, want %v", got, before)
	}
}

func TestForStoreForcedBackend(t *testing.T) {
	a, b := newFake("a", "A"), newFake("b", "B")
	swap(t, a, b)
	t.Setenv("TESSERA_SECRET_BACKEND", "b")
	got, err := secretstore.ForStore(context.Background())
	if err != nil || got != b {
		t.Fatalf("ForStore forced b = %v, %v", got, err)
	}
	if calls := methods(a); len(calls) != 0 {
		t.Fatalf("forced ForStore consulted a: %v", calls)
	}
}

func TestForStoreForcedBackendUnavailable(t *testing.T) {
	a, b := newFake("a", "A"), unavailable(newFake("b", "B"), "bus is down")
	swap(t, a, b)
	t.Setenv("TESSERA_SECRET_BACKEND", "b")
	got, err := secretstore.ForStore(context.Background())
	if got != nil {
		t.Fatalf("ForStore returned %v for an unavailable forced backend", got.ID())
	}
	var nb *secretstore.NoBackendError
	if !errors.As(err, &nb) || !errors.Is(err, secretstore.ErrUnavailable) {
		t.Fatalf("ForStore = %v; want *NoBackendError", err)
	}
	if len(nb.Tried) != 1 || nb.Tried[0].Backend != "B" || nb.Tried[0].Reason != "bus is down" {
		t.Fatalf("Tried = %+v", nb.Tried)
	}
}

func TestForStoreForcedBackendUnknown(t *testing.T) {
	swap(t, newFake("a", "A"), newFake("b", "B"))
	t.Setenv("TESSERA_SECRET_BACKEND", "nope")
	_, err := secretstore.ForStore(context.Background())
	if !errors.Is(err, secretstore.ErrUnknownBackend) {
		t.Fatalf("ForStore = %v; want ErrUnknownBackend", err)
	}
	for _, want := range []string{`"nope"`, "TESSERA_SECRET_BACKEND", "a, b"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %s", err, want)
		}
	}

	swap(t)
	t.Setenv("TESSERA_SECRET_BACKEND", "nope")
	_, err = secretstore.ForStore(context.Background())
	if !errors.Is(err, secretstore.ErrUnknownBackend) || !strings.Contains(err.Error(), "unset TESSERA_SECRET_BACKEND") {
		t.Fatalf("ForStore with no backends = %v", err)
	}
}

func TestForStoreWrapsPlainAvailableError(t *testing.T) {
	a := newFake("a", "A")
	a.AvailableErr = errors.New("probe failed")
	swap(t, a)
	_, err := secretstore.ForStore(context.Background())
	var nb *secretstore.NoBackendError
	if !errors.As(err, &nb) || len(nb.Tried) != 1 || nb.Tried[0].Backend != "A" || nb.Tried[0].Reason != "probe failed" {
		t.Fatalf("ForStore = %v", err)
	}
}

func TestNoBackendErrorListsEveryReason(t *testing.T) {
	swap(t, unavailable(newFake("a", "Secret Service"), "no D-Bus session bus"),
		unavailable(newFake("b", "kernel keyring"), "keyctl blocked (seccomp or kernel config)"))
	_, err := secretstore.ForStore(context.Background())
	var nb *secretstore.NoBackendError
	if !errors.As(err, &nb) || !errors.Is(err, secretstore.ErrUnavailable) {
		t.Fatalf("ForStore = %v; want *NoBackendError", err)
	}
	msg := err.Error()
	for _, want := range []string{
		"Secret Service: no D-Bus session bus",
		"kernel keyring: keyctl blocked (seccomp or kernel config)",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q does not contain %q", msg, want)
		}
	}
	if !strings.HasSuffix(msg, "set TESSERA_PASSPHRASE to unlock without one") {
		t.Fatalf("message %q does not end with the TESSERA_PASSPHRASE hint", msg)
	}

	empty := (&secretstore.NoBackendError{}).Error()
	if !strings.Contains(empty, "on this system") || !strings.HasSuffix(empty, "set TESSERA_PASSPHRASE to unlock without one") {
		t.Fatalf("empty NoBackendError message = %q", empty)
	}
}

func TestUnavailableError(t *testing.T) {
	err := &secretstore.UnavailableError{Backend: "Secret Service", Reason: "no default keyring collection"}
	if !errors.Is(err, secretstore.ErrUnavailable) {
		t.Fatal("UnavailableError does not match ErrUnavailable")
	}
	if got, want := err.Error(), "secretstore: the Secret Service is unavailable: no default keyring collection"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestLookupSkipsLockedBackend(t *testing.T) {
	a, b := newFake("a", "A"), newFake("b", "B")
	a.Put(key, "from-a")
	a.Lock(key)
	b.Put(key, "from-b")
	swap(t, a, b)
	secret, from, found, err := secretstore.Lookup(context.Background(), key)
	if err != nil || !found || secret != "from-b" || from != b {
		t.Fatalf("Lookup = %q, %v, %v, %v; want from-b from b", secret, from, found, err)
	}
}

func TestLookupLockedOnly(t *testing.T) {
	a, b := newFake("a", "A"), newFake("b", "B")
	a.Lock(key)
	swap(t, a, b)
	secret, from, found, err := secretstore.Lookup(context.Background(), key)
	if !errors.Is(err, secretstore.ErrLocked) || found || secret != "" || from != a {
		t.Fatalf("Lookup = %q, %v, %v, %v; want ErrLocked from a", secret, from, found, err)
	}
}

type broken struct {
	*secretstoretest.Fake
	err error
}

func (b broken) Lookup(context.Context, string) (string, bool, error) { return "", false, b.err }
func (b broken) Has(context.Context, string) (bool, error)            { return false, b.err }

func TestLookupPrefersOtherErrorOverLocked(t *testing.T) {
	a, c := newFake("a", "A"), newFake("c", "C")
	a.Lock(key)
	failure := errors.New("call failed")
	b := broken{newFake("b", "B"), failure}
	swap(t, a, b, c)
	_, from, found, err := secretstore.Lookup(context.Background(), key)
	if !errors.Is(err, failure) || found || from == nil || from.ID() != "b" {
		t.Fatalf("Lookup = %v, %v, %v; want the failure from b", from, found, err)
	}

	c.Put(key, "from-c")
	secret, from, found, err := secretstore.Lookup(context.Background(), key)
	if err != nil || !found || secret != "from-c" || from != c {
		t.Fatalf("Lookup = %q, %v, %v, %v; want from-c past the failure", secret, from, found, err)
	}
}

func TestLocateAndForgetReportBackendErrors(t *testing.T) {
	failure := errors.New("call failed")
	b := broken{newFake("b", "B"), failure}
	c := newFake("c", "C")
	c.Put(key, "s")
	swap(t, b, c)

	ps := secretstore.Locate(context.Background(), key)
	if len(ps) != 2 || !errors.Is(ps[0].Err, failure) || ps[0].Present || !ps[1].Present {
		t.Fatalf("Locate = %+v", ps)
	}

	removed, err := secretstore.Forget(context.Background(), key)
	if !errors.Is(err, failure) || !slices.Equal(removed, []string{"C"}) {
		t.Fatalf("Forget = %v, %v; want [C] and the failure", removed, err)
	}
	if slices.Contains(methods(b.Fake), "Delete") {
		t.Fatal("Forget deleted from a backend whose Has failed")
	}
}

func TestLookupSkipsUnavailableBackend(t *testing.T) {
	a, b := unavailable(newFake("a", "A"), "down"), newFake("b", "B")
	a.Put(key, "from-a")
	swap(t, a, b)
	secret, from, found, err := secretstore.Lookup(context.Background(), key)
	if err != nil || found || secret != "" || from != nil {
		t.Fatalf("Lookup = %q, %v, %v, %v; want a clean miss", secret, from, found, err)
	}
	if got := methods(a); !slices.Equal(got, []string{"Available"}) {
		t.Fatalf("calls on unavailable backend = %v; want only Available", got)
	}
}

func TestLookupStopsOnCancelledContext(t *testing.T) {
	a := newFake("a", "A")
	a.Put(key, "from-a")
	swap(t, a)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, found, err := secretstore.Lookup(ctx, key); !errors.Is(err, context.Canceled) || found {
		t.Fatalf("Lookup = %v, %v; want context.Canceled", found, err)
	}
	if calls := a.Calls(); len(calls) != 0 {
		t.Fatalf("Lookup called the backend after cancel: %v", calls)
	}
}

func TestLocate(t *testing.T) {
	present, locked, absent := newFake("p", "P"), newFake("l", "L"), newFake("a", "A")
	down := unavailable(newFake("d", "D"), "down")
	present.Put(key, "s")
	locked.Put(key, "s")
	locked.Lock(key)
	swap(t, present, locked, down, absent)

	got := secretstore.Locate(context.Background(), key)
	want := []secretstore.Presence{
		{Backend: present, Present: true},
		{Backend: locked, Locked: true},
		{Backend: absent},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Locate = %+v, want %+v", got, want)
	}
	for _, f := range []*secretstoretest.Fake{present, locked, absent} {
		if slices.Contains(methods(f), "Lookup") {
			t.Fatalf("Locate read the secret from %s", f.ID())
		}
	}
}

func TestForgetReportsRemovals(t *testing.T) {
	a, b, c := newFake("a", "A"), newFake("b", "B"), newFake("c", "C")
	a.Put(key, "s")
	b.Put(key, "s")
	b.Lock(key)
	swap(t, a, b, c)

	removed, err := secretstore.Forget(context.Background(), key)
	if err != nil || !slices.Equal(removed, []string{"A", "B"}) {
		t.Fatalf("Forget = %v, %v; want [A B]", removed, err)
	}
	for _, f := range []*secretstoretest.Fake{a, b} {
		if _, ok := f.Secret(key); ok {
			t.Fatalf("%s still holds the key", f.ID())
		}
	}
	if slices.Contains(methods(c), "Delete") {
		t.Fatal("Forget deleted from a backend without an entry")
	}

	removed, err = secretstore.Forget(context.Background(), key)
	if err != nil || len(removed) != 0 {
		t.Fatalf("second Forget = %v, %v; want nothing removed", removed, err)
	}
}

func TestForgetSkipsUnavailable(t *testing.T) {
	a := unavailable(newFake("a", "A"), "down")
	a.Put(key, "s")
	swap(t, a)
	removed, err := secretstore.Forget(context.Background(), key)
	if err != nil || len(removed) != 0 {
		t.Fatalf("Forget = %v, %v", removed, err)
	}
	if _, ok := a.Secret(key); !ok {
		t.Fatal("Forget removed from an unavailable backend")
	}
}

func TestInvalidKeyRejectedBeforeAnyBackend(t *testing.T) {
	for _, bad := range []string{"", "/tmp/a\nb.json", "/tmp/a\rb.json", "/tmp/a\x00b.json", "/tmp/v.json\ndelete-generic-password -s tessera"} {
		if err := secretstore.ValidateKey(bad); !errors.Is(err, secretstore.ErrInvalidKey) {
			t.Fatalf("ValidateKey(%q) = %v", bad, err)
		}
		a := newFake("a", "A")
		a.Put(bad, "s")
		swap(t, a)
		if _, _, _, err := secretstore.Lookup(context.Background(), bad); !errors.Is(err, secretstore.ErrInvalidKey) {
			t.Fatalf("Lookup(%q) = %v", bad, err)
		}
		if _, err := secretstore.Forget(context.Background(), bad); !errors.Is(err, secretstore.ErrInvalidKey) {
			t.Fatalf("Forget(%q) = %v", bad, err)
		}
		ps := secretstore.Locate(context.Background(), bad)
		if len(ps) != 1 || ps[0].Backend != a || !errors.Is(ps[0].Err, secretstore.ErrInvalidKey) || ps[0].Present || ps[0].Locked {
			t.Fatalf("Locate(%q) = %+v", bad, ps)
		}
		if calls := a.Calls(); len(calls) != 0 {
			t.Fatalf("backend ran for invalid key %q: %v", bad, calls)
		}
	}
	if err := secretstore.ValidateKey(key); err != nil {
		t.Fatalf("ValidateKey(%q) = %v", key, err)
	}
}

func TestInvalidSecret(t *testing.T) {
	bad := map[string]string{
		"empty":     "",
		"newline":   "pass\nword",
		"return":    "pass\rword",
		"nul":       "pass\x00word",
		"too long":  strings.Repeat("x", 2049),
		"multibyte": strings.Repeat("é", 1025),
	}
	f := newFake("a", "A")
	for name, s := range bad {
		if err := secretstore.ValidateSecret(s); !errors.Is(err, secretstore.ErrInvalidSecret) {
			t.Fatalf("ValidateSecret(%s) = %v", name, err)
		}
		if err := f.Store(context.Background(), key, s, 0); !errors.Is(err, secretstore.ErrInvalidSecret) {
			t.Fatalf("Store(%s) = %v", name, err)
		}
	}
	if _, ok := f.Secret(key); ok {
		t.Fatal("an invalid secret was stored")
	}
	for _, s := range []string{"x", strings.Repeat("x", 2048), `p@ss "with" \slashes and $pecial`, "pässwörd"} {
		if err := secretstore.ValidateSecret(s); err != nil {
			t.Fatalf("ValidateSecret(%q) = %v", s, err)
		}
	}
}

func TestFakeTTL(t *testing.T) {
	f := newFake("a", "A")
	err := f.Store(context.Background(), key, "s", time.Hour)
	if !errors.Is(err, secretstore.ErrTTLUnsupported) || !strings.Contains(err.Error(), "A") {
		t.Fatalf("Store with ttl on a backend without expiry = %v", err)
	}
	f.TTLSupported = true
	if err := f.Store(context.Background(), key, "s", time.Hour); err != nil {
		t.Fatalf("Store with ttl = %v", err)
	}
	if got := f.TTL(key); got != time.Hour {
		t.Fatalf("TTL = %v", got)
	}
	want := []secretstoretest.Call{
		{Method: "Store", Key: key, TTL: time.Hour},
		{Method: "Store", Key: key, TTL: time.Hour},
	}
	if got := f.Calls(); !slices.Equal(got, want) {
		t.Fatalf("Calls = %+v", got)
	}
}

func TestFakeRoundTrip(t *testing.T) {
	f := newFake("a", "A")
	ctx := context.Background()
	if has, err := f.Has(ctx, key); err != nil || has {
		t.Fatalf("Has before Store = %v, %v", has, err)
	}
	if err := f.Store(ctx, key, "s1", 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Store(ctx, key, "s2", 0); err != nil {
		t.Fatal(err)
	}
	if s, ok, err := f.Lookup(ctx, key); err != nil || !ok || s != "s2" {
		t.Fatalf("Lookup = %q, %v, %v", s, ok, err)
	}
	f.Lock(key)
	if has, err := f.Has(ctx, key); !errors.Is(err, secretstore.ErrLocked) || has {
		t.Fatalf("Has on locked = %v, %v", has, err)
	}
	f.Unlock(key)
	if has, err := f.Has(ctx, key); err != nil || !has {
		t.Fatalf("Has after Unlock = %v, %v", has, err)
	}
	if err := f.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := f.Delete(ctx, key); err != nil {
		t.Fatalf("Delete of a missing entry = %v", err)
	}
	if s, ok, err := f.Lookup(ctx, key); err != nil || ok || s != "" {
		t.Fatalf("Lookup after Delete = %q, %v, %v", s, ok, err)
	}

	down := unavailable(newFake("d", "D"), "down")
	if err := down.Store(ctx, key, "s", 0); !errors.Is(err, secretstore.ErrUnavailable) {
		t.Fatalf("Store on unavailable fake = %v", err)
	}
}
