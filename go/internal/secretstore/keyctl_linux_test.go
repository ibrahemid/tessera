//go:build linux

package secretstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ibrahemid/tessera/go/internal/procenv"
)

const keyctlTestEnv = "TESSERA_KEYCTL_TEST"

type keyctlRecorder struct {
	calls []string
}

func (r *keyctlRecorder) add(format string, args ...any) {
	r.calls = append(r.calls, fmt.Sprintf(format, args...))
}

func useKeyctl(t *testing.T, c keyctlCalls) *keyctlRecorder {
	t.Helper()
	rec := &keyctlRecorder{}
	unexpected := func(name string) error {
		t.Errorf("unexpected keyctl %s", name)
		return unix.EINVAL
	}
	wrap := keyctlCalls{
		addKey: func(keyType, desc string, payload []byte, ring int) (int, error) {
			rec.add("add %s %s %d", keyType, desc, ring)
			if c.addKey == nil {
				return 0, unexpected("add_key")
			}
			return c.addKey(keyType, desc, payload, ring)
		},
		search: func(ring int, keyType, desc string, dest int) (int, error) {
			rec.add("search %d %s %s %d", ring, keyType, desc, dest)
			if c.search == nil {
				return 0, unexpected("search")
			}
			return c.search(ring, keyType, desc, dest)
		},
		read: func(id int, buf []byte) (int, error) {
			rec.add("read %d", id)
			if c.read == nil {
				return 0, unexpected("read")
			}
			return c.read(id, buf)
		},
		setperm: func(id int, perm uint32) error {
			rec.add("setperm %d %#x", id, perm)
			if c.setperm == nil {
				return unexpected("setperm")
			}
			return c.setperm(id, perm)
		},
		keyctlInt: func(cmd, a2, a3, a4, a5 int) (int, error) {
			rec.add("op %d %d %d", cmd, a2, a3)
			if c.keyctlInt == nil {
				return 0, unexpected("op " + strconv.Itoa(cmd))
			}
			return c.keyctlInt(cmd, a2, a3, a4, a5)
		},
	}
	prev := keyctl
	keyctl = wrap
	t.Cleanup(func() { keyctl = prev })
	return rec
}

func forbidKeyctl(t *testing.T) *keyctlRecorder {
	t.Helper()
	return useKeyctl(t, keyctlCalls{})
}

func readReturns(data string, err error) func(int, []byte) (int, error) {
	return func(_ int, buf []byte) (int, error) {
		if err != nil {
			return 0, err
		}
		return copy(buf, data), nil
	}
}

func searchReturns(id int, err error) func(int, string, string, int) (int, error) {
	return func(int, string, string, int) (int, error) { return id, err }
}

func opsSucceed(int, int, int, int, int) (int, error) { return 0, nil }

func TestKernelKeyringIdentity(t *testing.T) {
	k := kernelKeyring{}
	if k.ID() != "kernel-keyring" || k.Name() != "kernel keyring" {
		t.Fatalf("ID, Name = %q, %q", k.ID(), k.Name())
	}
}

func TestKeyctlRejectsBeforeSyscalls(t *testing.T) {
	rec := forbidKeyctl(t)
	k := kernelKeyring{}
	ctx := context.Background()
	for _, key := range []string{"", "a\nb", "a\rb", "a\x00b"} {
		if err := k.Store(ctx, key, "secret", 0); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Store(%q) = %v, want ErrInvalidKey", key, err)
		}
		if _, _, err := k.Lookup(ctx, key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Lookup(%q) = %v, want ErrInvalidKey", key, err)
		}
		if _, err := k.Has(ctx, key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Has(%q) = %v, want ErrInvalidKey", key, err)
		}
		if err := k.Delete(ctx, key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Delete(%q) = %v, want ErrInvalidKey", key, err)
		}
	}
	for _, secret := range []string{"", "a\nb", "a\x00b", strings.Repeat("x", maxSecretLen+1)} {
		if err := k.Store(ctx, "vault", secret, 0); !errors.Is(err, ErrInvalidSecret) {
			t.Errorf("Store(secret len %d) = %v, want ErrInvalidSecret", len(secret), err)
		}
	}
	for _, ttl := range []time.Duration{-time.Second, 200 * 365 * 24 * time.Hour} {
		if err := k.Store(ctx, "vault", "secret", ttl); !errors.Is(err, errKeyringTTL) {
			t.Errorf("Store(ttl %v) = %v, want errKeyringTTL", ttl, err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := k.Store(cancelled, "vault", "secret", 0); !errors.Is(err, context.Canceled) {
		t.Errorf("Store with a cancelled context = %v", err)
	}
	if _, _, err := k.Lookup(cancelled, "vault"); !errors.Is(err, context.Canceled) {
		t.Errorf("Lookup with a cancelled context = %v", err)
	}
	if _, err := k.Has(cancelled, "vault"); !errors.Is(err, context.Canceled) {
		t.Errorf("Has with a cancelled context = %v", err)
	}
	if err := k.Delete(cancelled, "vault"); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete with a cancelled context = %v", err)
	}
	if err := k.Available(cancelled); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Available with a cancelled context = %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("keyctl calls = %q, want none", rec.calls)
	}
}

func TestKeyringTimeout(t *testing.T) {
	cases := []struct {
		ttl  time.Duration
		want int64
	}{
		{0, 0},
		{time.Nanosecond, 1},
		{time.Second, 1},
		{1500 * time.Millisecond, 2},
		{90 * time.Second, 90},
		{maxKeyringTimeout(), 1<<32 - 1},
	}
	for _, c := range cases {
		got, err := keyringTimeout(c.ttl)
		if err != nil || int64(got) != c.want {
			t.Errorf("keyringTimeout(%v) = %d, %v, want %d", c.ttl, got, err, c.want)
		}
	}
	for _, ttl := range []time.Duration{-time.Nanosecond, maxKeyringTimeout() + time.Second} {
		if _, err := keyringTimeout(ttl); !errors.Is(err, errKeyringTTL) {
			t.Errorf("keyringTimeout(%v) = %v, want errKeyringTTL", ttl, err)
		}
	}
}

func maxKeyringTimeout() time.Duration { return (1<<32 - 1) * time.Second }

func TestKeyctlAvailableMapsErrors(t *testing.T) {
	cases := []struct {
		err    error
		reason string
	}{
		{nil, ""},
		{unix.ENOKEY, ""},
		{unix.EKEYEXPIRED, ""},
		{unix.EKEYREVOKED, ""},
		{unix.EPERM, keyringBlocked},
		{unix.ENOSYS, keyringBlocked},
		{unix.EACCES, keyringBlocked},
		{unix.EINVAL, "keyctl failed: " + unix.EINVAL.Error()},
	}
	for _, c := range cases {
		rec := useKeyctl(t, keyctlCalls{search: searchReturns(7, c.err)})
		err := kernelKeyring{}.Available(context.Background())
		if want := []string{fmt.Sprintf("search %d user %s 0", unix.KEY_SPEC_USER_KEYRING, keyringProbe)}; !slices.Equal(rec.calls, want) {
			t.Errorf("calls = %q, want %q", rec.calls, want)
		}
		if c.reason == "" {
			if err != nil {
				t.Errorf("Available with %v = %v, want nil", c.err, err)
			}
			continue
		}
		var u *UnavailableError
		if !errors.As(err, &u) || !errors.Is(err, ErrUnavailable) || u.Backend != "kernel keyring" || u.Reason != c.reason {
			t.Errorf("Available with %v = %#v, want reason %q", c.err, err, c.reason)
		}
	}
}

func TestKeyctlStoreSequence(t *testing.T) {
	cases := []struct {
		ttl     time.Duration
		seconds int
	}{{0, 0}, {1500 * time.Millisecond, 2}, {90 * time.Second, 90}}
	for _, c := range cases {
		const secret = "correct horse"
		var payload string
		rec := useKeyctl(t, keyctlCalls{
			addKey: func(_, _ string, p []byte, _ int) (int, error) {
				payload = string(p)
				return 41, nil
			},
			setperm:   func(int, uint32) error { return nil },
			keyctlInt: opsSucceed,
			search:    searchReturns(41, nil),
			read:      readReturns(secret, nil),
		})
		if err := (kernelKeyring{}).Store(context.Background(), "vault", secret, c.ttl); err != nil {
			t.Fatalf("Store(ttl %v) = %v", c.ttl, err)
		}
		if payload != secret {
			t.Errorf("add_key payload differs from the secret")
		}
		want := []string{
			fmt.Sprintf("op %d %d 1", unix.KEYCTL_GET_KEYRING_ID, unix.KEY_SPEC_THREAD_KEYRING),
			fmt.Sprintf("op %d %d %d", unix.KEYCTL_LINK, unix.KEY_SPEC_USER_KEYRING, unix.KEY_SPEC_THREAD_KEYRING),
			fmt.Sprintf("add user tessera:vault %d", unix.KEY_SPEC_USER_KEYRING),
			"setperm 41 0x3f0b0000",
			fmt.Sprintf("op %d 41 %d", unix.KEYCTL_SET_TIMEOUT, c.seconds),
			fmt.Sprintf("search %d user tessera:vault 0", unix.KEY_SPEC_USER_KEYRING),
			"read 41",
		}
		if !slices.Equal(rec.calls, want) {
			t.Errorf("Store(ttl %v) calls =\n%q\nwant\n%q", c.ttl, rec.calls, want)
		}
	}
}

func TestKeyctlStoreErrors(t *testing.T) {
	const secret = "s3cret-value"
	addOK := func(string, string, []byte, int) (int, error) { return 41, nil }
	permOK := func(int, uint32) error { return nil }
	failOp := func(cmd int, err error) func(int, int, int, int, int) (int, error) {
		return func(c, _, _, _, _ int) (int, error) {
			if c == cmd {
				return 0, err
			}
			return 0, nil
		}
	}
	cases := []struct {
		name        string
		calls       keyctlCalls
		want        error
		invalidated bool
	}{
		{"thread keyring", keyctlCalls{keyctlInt: failOp(unix.KEYCTL_GET_KEYRING_ID, unix.ENOMEM)}, errKeyringWrite, false},
		{"link", keyctlCalls{keyctlInt: failOp(unix.KEYCTL_LINK, unix.ENOMEM)}, errKeyringWrite, false},
		{"quota", keyctlCalls{keyctlInt: opsSucceed, addKey: func(string, string, []byte, int) (int, error) { return 0, unix.EDQUOT }}, errKeyringQuota, false},
		{"blocked", keyctlCalls{keyctlInt: failOp(unix.KEYCTL_GET_KEYRING_ID, unix.EPERM)}, ErrUnavailable, false},
		{"setperm", keyctlCalls{keyctlInt: opsSucceed, addKey: addOK, setperm: func(int, uint32) error { return unix.EACCES }}, errKeyringWrite, true},
		{"timeout", keyctlCalls{keyctlInt: failOp(unix.KEYCTL_SET_TIMEOUT, unix.EACCES), addKey: addOK, setperm: permOK}, errKeyringWrite, true},
		{"read back missing", keyctlCalls{keyctlInt: opsSucceed, addKey: addOK, setperm: permOK, search: searchReturns(0, unix.ENOKEY)}, errKeyringPersisted, false},
		{"read back differs", keyctlCalls{keyctlInt: opsSucceed, addKey: addOK, setperm: permOK, search: searchReturns(41, nil), read: readReturns("other", nil)}, errKeyringPersisted, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := useKeyctl(t, c.calls)
			err := kernelKeyring{}.Store(context.Background(), "vault", secret, time.Minute)
			if !errors.Is(err, c.want) {
				t.Fatalf("Store = %v, want %v", err, c.want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error carries the secret")
			}
			invalidate := fmt.Sprintf("op %d 41 0", unix.KEYCTL_INVALIDATE)
			if got := slices.Contains(rec.calls, invalidate); got != c.invalidated {
				t.Errorf("invalidated = %v, want %v; calls %q", got, c.invalidated, rec.calls)
			}
		})
	}
}

func TestKeyctlLookupMapsErrors(t *testing.T) {
	cases := []struct {
		name  string
		calls keyctlCalls
		value string
		found bool
		want  error
	}{
		{"missing", keyctlCalls{search: searchReturns(0, unix.ENOKEY)}, "", false, nil},
		{"expired", keyctlCalls{search: searchReturns(0, unix.EKEYEXPIRED)}, "", false, nil},
		{"revoked", keyctlCalls{search: searchReturns(0, unix.EKEYREVOKED)}, "", false, nil},
		{"search fails", keyctlCalls{search: searchReturns(0, unix.EIO)}, "", false, errKeyringQuery},
		{"blocked", keyctlCalls{search: searchReturns(0, unix.EPERM)}, "", false, ErrUnavailable},
		{"expired before read", keyctlCalls{search: searchReturns(9, nil), read: readReturns("", unix.EKEYEXPIRED)}, "", false, nil},
		{"read denied", keyctlCalls{search: searchReturns(9, nil), read: readReturns("", unix.EACCES)}, "", false, errKeyringRead},
		{"oversize", keyctlCalls{search: searchReturns(9, nil), read: readReturns(strings.Repeat("x", maxSecretLen+1), nil)}, "", false, errKeyringRead},
		{"found", keyctlCalls{search: searchReturns(9, nil), read: readReturns("hunter2", nil)}, "hunter2", true, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useKeyctl(t, c.calls)
			got, found, err := kernelKeyring{}.Lookup(context.Background(), "vault")
			if got != c.value || found != c.found || !errors.Is(err, c.want) || (c.want == nil && err != nil) {
				t.Fatalf("Lookup = %q, %v, %v; want %q, %v, %v", got, found, err, c.value, c.found, c.want)
			}
		})
	}
}

func TestKeyctlHas(t *testing.T) {
	cases := []struct {
		err   error
		found bool
		want  error
	}{
		{nil, true, nil},
		{unix.ENOKEY, false, nil},
		{unix.EKEYEXPIRED, false, nil},
		{unix.EIO, false, errKeyringQuery},
	}
	for _, c := range cases {
		rec := useKeyctl(t, keyctlCalls{search: searchReturns(3, c.err)})
		found, err := kernelKeyring{}.Has(context.Background(), "vault")
		if found != c.found || !errors.Is(err, c.want) || (c.want == nil && err != nil) {
			t.Errorf("Has with %v = %v, %v", c.err, found, err)
		}
		if len(rec.calls) != 1 {
			t.Errorf("Has with %v made calls %q, want one search", c.err, rec.calls)
		}
	}
}

func TestKeyctlDelete(t *testing.T) {
	invalidate := fmt.Sprintf("op %d 5 0", unix.KEYCTL_INVALIDATE)
	t.Run("missing", func(t *testing.T) {
		rec := useKeyctl(t, keyctlCalls{search: searchReturns(0, unix.ENOKEY)})
		if err := (kernelKeyring{}).Delete(context.Background(), "vault"); err != nil {
			t.Fatalf("Delete = %v", err)
		}
		if slices.Contains(rec.calls, invalidate) {
			t.Fatalf("Delete invalidated a missing key: %q", rec.calls)
		}
	})
	t.Run("present", func(t *testing.T) {
		gone := false
		rec := useKeyctl(t, keyctlCalls{
			search: func(int, string, string, int) (int, error) {
				if gone {
					return 0, unix.EKEYREVOKED
				}
				return 5, nil
			},
			keyctlInt: func(cmd, id, _, _, _ int) (int, error) {
				gone = cmd == unix.KEYCTL_INVALIDATE && id == 5
				return 0, nil
			},
		})
		if err := (kernelKeyring{}).Delete(context.Background(), "vault"); err != nil {
			t.Fatalf("Delete = %v", err)
		}
		if n := countCalls(rec.calls, invalidate); n != 1 {
			t.Fatalf("invalidations = %d, want 1; calls %q", n, rec.calls)
		}
	})
	t.Run("never gone", func(t *testing.T) {
		rec := useKeyctl(t, keyctlCalls{search: searchReturns(5, nil), keyctlInt: opsSucceed})
		if err := (kernelKeyring{}).Delete(context.Background(), "vault"); !errors.Is(err, errKeyringDelete) {
			t.Fatalf("Delete = %v, want errKeyringDelete", err)
		}
		if n := countCalls(rec.calls, invalidate); n != keyringMaxDuplicates {
			t.Fatalf("invalidations = %d, want %d", n, keyringMaxDuplicates)
		}
	})
	t.Run("denied", func(t *testing.T) {
		useKeyctl(t, keyctlCalls{search: searchReturns(5, nil), keyctlInt: func(int, int, int, int, int) (int, error) { return 0, unix.EACCES }})
		if err := (kernelKeyring{}).Delete(context.Background(), "vault"); !errors.Is(err, errKeyringDelete) {
			t.Fatalf("Delete = %v, want errKeyringDelete", err)
		}
	})
}

func countCalls(calls []string, call string) int {
	n := 0
	for _, c := range calls {
		if c == call {
			n++
		}
	}
	return n
}

func requireKeyctl(t *testing.T, mode string) {
	t.Helper()
	if os.Getenv(keyctlTestEnv) != mode {
		t.Skipf("set %s=%s to test against the kernel keyring", keyctlTestEnv, mode)
	}
}

func randomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func scratchKey(t *testing.T) string {
	t.Helper()
	key := "/tmp/tessera-keyctl-test-" + randomHex(t) + "/vault.json"
	t.Cleanup(func() { _ = kernelKeyring{}.Delete(context.Background(), key) })
	return key
}

func keyID(t *testing.T, key string) int {
	t.Helper()
	id, err := unix.KeyctlSearch(unix.KEY_SPEC_USER_KEYRING, keyringKeyType, keyringDescPrefix+key, 0)
	if err != nil {
		t.Fatalf("search %s: %v", key, err)
	}
	return id
}

func TestKeyctlRoundTrip(t *testing.T) {
	requireKeyctl(t, "1")
	k := kernelKeyring{}
	ctx := context.Background()
	key := scratchKey(t)
	first, second := "first "+randomHex(t), "second "+randomHex(t)

	if err := k.Available(ctx); err != nil {
		t.Fatalf("Available = %v", err)
	}
	if has, err := k.Has(ctx, key); err != nil || has {
		t.Fatalf("Has before Store = %v, %v", has, err)
	}
	if err := k.Store(ctx, key, first, 0); err != nil {
		t.Fatalf("Store = %v", err)
	}
	if got, ok, err := k.Lookup(ctx, key); err != nil || !ok || got != first {
		t.Fatalf("Lookup = %v, %v, want the stored value", ok, err)
	}
	if has, err := k.Has(ctx, key); err != nil || !has {
		t.Fatalf("Has = %v, %v", has, err)
	}
	id := keyID(t, key)
	if err := k.Store(ctx, key, second, 0); err != nil {
		t.Fatalf("Store over an existing key = %v", err)
	}
	if got, ok, err := k.Lookup(ctx, key); err != nil || !ok || got != second {
		t.Fatalf("Lookup after replace = %v, %v, want the new value", ok, err)
	}
	if again := keyID(t, key); again != id {
		t.Fatalf("replace created key %d next to %d", again, id)
	}
	if err := k.Delete(ctx, key); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	if got, ok, err := k.Lookup(ctx, key); err != nil || ok || got != "" {
		t.Fatalf("Lookup after Delete = %v, %v", ok, err)
	}
	if has, err := k.Has(ctx, key); err != nil || has {
		t.Fatalf("Has after Delete = %v, %v", has, err)
	}
	if err := k.Delete(ctx, key); err != nil {
		t.Fatalf("Delete of a missing key = %v", err)
	}
}

func TestKeyctlExpiry(t *testing.T) {
	requireKeyctl(t, "1")
	k := kernelKeyring{}
	ctx := context.Background()
	key := scratchKey(t)

	if err := k.Store(ctx, key, "expiring", time.Second); err != nil {
		t.Fatalf("Store with a TTL = %v", err)
	}
	if _, ok, err := k.Lookup(ctx, key); err != nil || !ok {
		t.Fatalf("Lookup right after Store = %v, %v", ok, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, ok, err := k.Lookup(ctx, key)
		if err != nil {
			t.Fatalf("Lookup while expiring = %v", err)
		}
		if !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("key still readable 5s after a 1s TTL")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if has, err := k.Has(ctx, key); err != nil || has {
		t.Fatalf("Has after expiry = %v, %v", has, err)
	}

	if err := k.Store(ctx, key, "expiring", time.Second); err != nil {
		t.Fatalf("Store with a TTL after expiry = %v", err)
	}
	if err := k.Store(ctx, key, "kept", 0); err != nil {
		t.Fatalf("Store without a TTL = %v", err)
	}
	time.Sleep(2500 * time.Millisecond)
	if got, ok, err := k.Lookup(ctx, key); err != nil || !ok || got != "kept" {
		t.Fatalf("Lookup after a TTL was cleared = %v, %v", ok, err)
	}
}

func TestKeyctlPermissions(t *testing.T) {
	requireKeyctl(t, "1")
	k := kernelKeyring{}
	key := scratchKey(t)
	if err := k.Store(context.Background(), key, "perm-check", 0); err != nil {
		t.Fatalf("Store = %v", err)
	}
	desc, err := unix.KeyctlString(unix.KEYCTL_DESCRIBE, keyID(t, key))
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	fields := strings.SplitN(desc, ";", 5)
	if len(fields) != 5 {
		t.Fatalf("describe = %q", desc)
	}
	perm, err := strconv.ParseUint(fields[3], 16, 32)
	if err != nil {
		t.Fatalf("describe perm %q: %v", fields[3], err)
	}
	if fields[0] != "user" || fields[1] != strconv.Itoa(os.Getuid()) || perm != keyringPerm || fields[4] != keyringDescPrefix+key {
		t.Fatalf("describe = %q, want user;%d;*;%x;%s", desc, os.Getuid(), keyringPerm, keyringDescPrefix+key)
	}
}

func TestKeyctlSecondProcess(t *testing.T) {
	requireKeyctl(t, "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, mode, key, want string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		sum := sha256.Sum256([]byte(want))
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestKeyctlChildProcess$", "-test.count=1", "-test.v")
		cmd.Env = append(procenv.Child(),
			keyctlTestEnv+"=child",
			"TESSERA_KEYCTL_CHILD_MODE="+mode,
			"TESSERA_KEYCTL_CHILD_KEY="+key,
			"TESSERA_KEYCTL_CHILD_SHA256="+hex.EncodeToString(sum[:]),
		)
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "--- PASS: TestKeyctlChildProcess") {
			t.Fatalf("child (%s) = %v\n%s", mode, err, out)
		}
	}

	t.Run("stored key is readable", func(t *testing.T) {
		key := scratchKey(t)
		secret := "shared " + randomHex(t)
		if err := (kernelKeyring{}).Store(context.Background(), key, secret, 0); err != nil {
			t.Fatalf("Store = %v", err)
		}
		run(t, "read", key, secret)
	})
	t.Run("default permissions are not", func(t *testing.T) {
		key := "/tmp/tessera-keyctl-test-" + randomHex(t) + "/default.json"
		id, err := unix.AddKey(keyringKeyType, keyringDescPrefix+key, []byte("unreadable"), unix.KEY_SPEC_USER_KEYRING)
		if err != nil {
			t.Fatalf("add_key: %v", err)
		}
		t.Cleanup(func() { _, _ = unix.KeyctlInt(unix.KEYCTL_UNLINK, id, unix.KEY_SPEC_USER_KEYRING, 0, 0) })
		run(t, "denied", key, "")
	})
}

func TestKeyctlChildProcess(t *testing.T) {
	requireKeyctl(t, "child")
	runtime.LockOSThread()
	if _, err := unix.KeyctlInt(unix.KEYCTL_JOIN_SESSION_KEYRING, 0, 0, 0, 0); err != nil {
		t.Fatalf("join an anonymous session keyring: %v", err)
	}
	key := os.Getenv("TESSERA_KEYCTL_CHILD_KEY")
	got, ok, err := kernelKeyring{}.Lookup(context.Background(), key)
	switch os.Getenv("TESSERA_KEYCTL_CHILD_MODE") {
	case "read":
		sum := sha256.Sum256([]byte(got))
		if err != nil || !ok || hex.EncodeToString(sum[:]) != os.Getenv("TESSERA_KEYCTL_CHILD_SHA256") {
			t.Fatalf("Lookup = found %v, %v, value matches %v", ok, err, hex.EncodeToString(sum[:]) == os.Getenv("TESSERA_KEYCTL_CHILD_SHA256"))
		}
	case "denied":
		if !errors.Is(err, errKeyringRead) || ok {
			t.Fatalf("Lookup of a key without user read = found %v, %v, want errKeyringRead", ok, err)
		}
	default:
		t.Fatal("unknown child mode")
	}
}

func TestKeyctlBlocked(t *testing.T) {
	requireKeyctl(t, "blocked")
	k := kernelKeyring{}
	ctx := context.Background()
	err := k.Available(ctx)
	var u *UnavailableError
	if !errors.As(err, &u) || u.Reason != keyringBlocked {
		t.Fatalf("Available = %v, want the blocked reason", err)
	}
	if err := k.Store(ctx, "vault", "secret", 0); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Store = %v, want ErrUnavailable", err)
	}
	if _, _, err := k.Lookup(ctx, "vault"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Lookup = %v, want ErrUnavailable", err)
	}
}
