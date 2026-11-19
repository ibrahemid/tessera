//go:build windows

package secretstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	testTargetPrefix = "tessera-test:"
	testVaultPath    = `C:\Users\u\AppData\Roaming\tessera\vault.json`
)

func testCredentialManager() credentialManager {
	return credentialManager{prefix: testTargetPrefix}
}

func swapCred(t *testing.T, write func(*credentialW) error, read func(*uint16, bool) ([]byte, error), del func(*uint16) error) {
	t.Helper()
	prevWrite, prevRead, prevDelete := credWrite, credRead, credDelete
	credWrite, credRead, credDelete = write, read, del
	t.Cleanup(func() { credWrite, credRead, credDelete = prevWrite, prevRead, prevDelete })
}

func forbidCred(t *testing.T) {
	t.Helper()
	swapCred(t,
		func(*credentialW) error { t.Error("CredWriteW ran"); return nil },
		func(*uint16, bool) ([]byte, error) { t.Error("CredReadW ran"); return nil, nil },
		func(*uint16) error { t.Error("CredDeleteW ran"); return nil },
	)
}

func credFailure(call string, errno syscall.Errno) error {
	return &credError{Call: call, Errno: errno}
}

func isZeroed(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

func TestCredentialManagerRegistration(t *testing.T) {
	t.Setenv(backendEnv, "")
	got := Backends()
	if len(got) != 1 || got[0].ID() != "credential-manager" || got[0].Name() != "Windows Credential Manager" {
		t.Fatalf("Backends = %v", got)
	}
	if c, ok := got[0].(credentialManager); !ok || c.prefix != "tessera:" {
		t.Fatalf("Backends()[0] = %#v; want the credential manager with prefix tessera:", got[0])
	}

	t.Setenv(backendEnv, "keychain")
	_, err := ForStore(context.Background())
	if !errors.Is(err, ErrUnknownBackend) || !strings.Contains(err.Error(), "one of: credential-manager") {
		t.Fatalf("ForStore with a backend from another system = %v", err)
	}
}

func TestCredentialProcsLoad(t *testing.T) {
	if err := loadCredProcs(); err != nil {
		t.Fatalf("loadCredProcs = %v", err)
	}
}

func TestCredentialWLayout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layout offsets are for 64-bit Windows")
	}
	var c credentialW
	offsets := []struct {
		field string
		got   uintptr
		want  uintptr
	}{
		{"TargetName", unsafe.Offsetof(c.TargetName), 8},
		{"Comment", unsafe.Offsetof(c.Comment), 16},
		{"LastWritten", unsafe.Offsetof(c.LastWritten), 24},
		{"CredentialBlobSize", unsafe.Offsetof(c.CredentialBlobSize), 32},
		{"CredentialBlob", unsafe.Offsetof(c.CredentialBlob), 40},
		{"Persist", unsafe.Offsetof(c.Persist), 48},
		{"AttributeCount", unsafe.Offsetof(c.AttributeCount), 52},
		{"Attributes", unsafe.Offsetof(c.Attributes), 56},
		{"TargetAlias", unsafe.Offsetof(c.TargetAlias), 64},
		{"UserName", unsafe.Offsetof(c.UserName), 72},
	}
	for _, o := range offsets {
		if o.got != o.want {
			t.Errorf("offset of %s = %d, want %d", o.field, o.got, o.want)
		}
	}
	if size := unsafe.Sizeof(c); size != 80 {
		t.Errorf("sizeof CREDENTIALW = %d, want 80", size)
	}
}

func TestCredentialManagerRejectsBeforeCalling(t *testing.T) {
	forbidCred(t)
	c := testCredentialManager()
	ctx := context.Background()
	for _, bad := range []string{"", "\n", "\r", "\x00", `C:\v.json` + "\nx", `C:\v` + "\x00.json"} {
		if err := c.Store(ctx, bad, "secret", 0); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Store(%q) = %v", bad, err)
		}
		if _, _, err := c.Lookup(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Lookup(%q) = %v", bad, err)
		}
		if _, err := c.Has(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Has(%q) = %v", bad, err)
		}
		if err := c.Delete(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Delete(%q) = %v", bad, err)
		}
	}

	err := c.Store(ctx, testVaultPath, "secret", time.Hour)
	if !errors.Is(err, ErrTTLUnsupported) || !strings.Contains(err.Error(), "Windows Credential Manager") {
		t.Fatalf("Store with ttl = %v", err)
	}
	for _, bad := range []string{"", "a\nb", "a\rb", "a\x00b", strings.Repeat("x", 2049), strings.Repeat("x", credMaxBlobSize+1)} {
		if err := c.Store(ctx, testVaultPath, bad, 0); !errors.Is(err, ErrInvalidSecret) {
			t.Fatalf("Store(secret of %d bytes) = %v", len(bad), err)
		}
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	checkCancelled := func(op string, err error) {
		t.Helper()
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "Windows Credential Manager") {
			t.Fatalf("%s with a cancelled context = %v", op, err)
		}
	}
	checkCancelled("Store", c.Store(cancelled, testVaultPath, "secret", 0))
	_, _, err = c.Lookup(cancelled, testVaultPath)
	checkCancelled("Lookup", err)
	_, err = c.Has(cancelled, testVaultPath)
	checkCancelled("Has", err)
	checkCancelled("Delete", c.Delete(cancelled, testVaultPath))
}

func TestStoreWritesAGenericLocalMachineEntry(t *testing.T) {
	const secret = "pässwörd ✓"
	var (
		calls   int
		written []byte
		seen    string
		got     credentialW
		target  string
		user    string
	)
	swapCred(t, func(cred *credentialW) error {
		calls++
		got = *cred
		target = windows.UTF16PtrToString(cred.TargetName)
		user = windows.UTF16PtrToString(cred.UserName)
		written = unsafe.Slice(cred.CredentialBlob, cred.CredentialBlobSize)
		seen = string(written)
		return nil
	}, nil, nil)

	if err := testCredentialManager().Store(context.Background(), testVaultPath, secret, 0); err != nil {
		t.Fatalf("Store = %v", err)
	}
	if calls != 1 {
		t.Fatalf("CredWriteW calls = %d, want 1", calls)
	}
	if target != testTargetPrefix+testVaultPath {
		t.Errorf("TargetName = %q", target)
	}
	if user != "tess" {
		t.Errorf("UserName = %q", user)
	}
	if got.Type != credTypeGeneric || got.Persist != credPersistLocalMachine || got.Flags != 0 {
		t.Errorf("Type, Persist, Flags = %d, %d, %d", got.Type, got.Persist, got.Flags)
	}
	if got.Comment != nil || got.TargetAlias != nil || got.AttributeCount != 0 || got.Attributes != 0 {
		t.Errorf("unexpected optional fields: %+v", got)
	}
	if seen != secret || int(got.CredentialBlobSize) != len(secret) {
		t.Errorf("blob = %q (%d bytes), want the UTF-8 passphrase", seen, got.CredentialBlobSize)
	}
	if !isZeroed(written) {
		t.Error("the blob buffer was not zeroed after the write")
	}
}

func TestStoreReportsWriteFailure(t *testing.T) {
	swapCred(t, func(*credentialW) error {
		return credFailure("CredWriteW", windows.ERROR_NO_SUCH_LOGON_SESSION)
	}, nil, nil)
	err := testCredentialManager().Store(context.Background(), testVaultPath, "secret", 0)
	if !errors.Is(err, errCredWrite) || !errors.Is(err, windows.ERROR_NO_SUCH_LOGON_SESSION) {
		t.Fatalf("Store = %v", err)
	}
}

func TestLookupCopiesAndZeroesTheBlob(t *testing.T) {
	const secret = "pässwörd ✓"
	var (
		returned []byte
		target   string
	)
	swapCred(t, nil, func(p *uint16, withBlob bool) ([]byte, error) {
		if !withBlob {
			t.Error("Lookup read without the blob")
		}
		target = windows.UTF16PtrToString(p)
		returned = []byte(secret)
		return returned, nil
	}, nil)

	got, ok, err := testCredentialManager().Lookup(context.Background(), testVaultPath)
	if err != nil || !ok || got != secret {
		t.Fatalf("Lookup = %q, %v, %v", got, ok, err)
	}
	if target != testTargetPrefix+testVaultPath {
		t.Errorf("TargetName = %q", target)
	}
	if !isZeroed(returned) {
		t.Error("the copied blob was not zeroed")
	}
}

func TestLookupRejectsAMalformedBlob(t *testing.T) {
	for _, blob := range [][]byte{
		{},
		[]byte("a\x00b\x00"),
		[]byte("line\nbreak"),
		[]byte(strings.Repeat("x", 2049)),
	} {
		swapCred(t, nil, func(*uint16, bool) ([]byte, error) { return blob, nil }, nil)
		got, ok, err := testCredentialManager().Lookup(context.Background(), testVaultPath)
		if !errors.Is(err, errCredMalformed) || ok || got != "" {
			t.Fatalf("Lookup of a %d-byte blob = %q, %v, %v", len(blob), got, ok, err)
		}
	}
}

func TestCredentialManagerResults(t *testing.T) {
	c := testCredentialManager()
	ctx := context.Background()

	swapCred(t, nil, func(*uint16, bool) ([]byte, error) {
		return nil, credFailure("CredReadW", windows.ERROR_NOT_FOUND)
	}, func(*uint16) error {
		return credFailure("CredDeleteW", windows.ERROR_NOT_FOUND)
	})
	if got, ok, err := c.Lookup(ctx, testVaultPath); err != nil || ok || got != "" {
		t.Fatalf("Lookup of a missing entry = %q, %v, %v", got, ok, err)
	}
	if has, err := c.Has(ctx, testVaultPath); err != nil || has {
		t.Fatalf("Has of a missing entry = %v, %v", has, err)
	}
	if err := c.Delete(ctx, testVaultPath); err != nil {
		t.Fatalf("Delete of a missing entry = %v", err)
	}

	swapCred(t, nil, func(_ *uint16, withBlob bool) ([]byte, error) {
		if withBlob {
			t.Error("Has read the blob")
		}
		return nil, nil
	}, func(*uint16) error { return nil })
	if has, err := c.Has(ctx, testVaultPath); err != nil || !has {
		t.Fatalf("Has of a present entry = %v, %v", has, err)
	}
	if err := c.Delete(ctx, testVaultPath); err != nil {
		t.Fatalf("Delete of a present entry = %v", err)
	}

	swapCred(t, nil, func(*uint16, bool) ([]byte, error) {
		return nil, credFailure("CredReadW", windows.ERROR_NO_SUCH_LOGON_SESSION)
	}, func(*uint16) error {
		return credFailure("CredDeleteW", windows.ERROR_NO_SUCH_LOGON_SESSION)
	})
	if _, _, err := c.Lookup(ctx, testVaultPath); !errors.Is(err, errCredRead) || !errors.Is(err, windows.ERROR_NO_SUCH_LOGON_SESSION) {
		t.Fatalf("Lookup failure = %v", err)
	}
	if _, err := c.Has(ctx, testVaultPath); !errors.Is(err, errCredQuery) || !errors.Is(err, windows.ERROR_NO_SUCH_LOGON_SESSION) {
		t.Fatalf("Has failure = %v", err)
	}
	if err := c.Delete(ctx, testVaultPath); !errors.Is(err, errCredDelete) || !errors.Is(err, windows.ERROR_NO_SUCH_LOGON_SESSION) {
		t.Fatalf("Delete failure = %v", err)
	}
}

func TestAvailableProbesTheCredentialSet(t *testing.T) {
	c := testCredentialManager()
	for _, tc := range []struct {
		name      string
		err       error
		available bool
		reason    string
	}{
		{"missing probe entry", credFailure("CredReadW", windows.ERROR_NOT_FOUND), true, ""},
		{"present probe entry", nil, true, ""},
		{"network logon", credFailure("CredReadW", windows.ERROR_NO_SUCH_LOGON_SESSION), false, "no credential set"},
		{"other failure", credFailure("CredReadW", windows.ERROR_INVALID_FLAGS), false, "CredReadW"},
	} {
		var target string
		swapCred(t, nil, func(p *uint16, withBlob bool) ([]byte, error) {
			if withBlob {
				t.Errorf("%s: Available read the blob", tc.name)
			}
			target = windows.UTF16PtrToString(p)
			return nil, tc.err
		}, nil)
		err := c.Available(context.Background())
		if target != testTargetPrefix {
			t.Errorf("%s: probe target = %q", tc.name, target)
		}
		if tc.available {
			if err != nil {
				t.Errorf("%s: Available = %v", tc.name, err)
			}
			continue
		}
		var u *UnavailableError
		if !errors.As(err, &u) || u.Backend != "Windows Credential Manager" || !strings.Contains(u.Reason, tc.reason) || !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: Available = %v", tc.name, err)
		}
	}
}

func TestCredError(t *testing.T) {
	bare := &credError{Call: "CredReadW"}
	if bare.Error() != "CredReadW failed" || bare.Unwrap() != nil {
		t.Fatalf("bare credError = %q, %v", bare.Error(), bare.Unwrap())
	}
	full := credFailure("CredDeleteW", windows.ERROR_NOT_FOUND)
	if !strings.HasPrefix(full.Error(), "CredDeleteW: ") || !errors.Is(full, windows.ERROR_NOT_FOUND) {
		t.Fatalf("credError = %q", full.Error())
	}
}

func readStoredCredential(t *testing.T, target string) (typ, persist uint32, user string) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(target)
	if err != nil {
		t.Fatalf("UTF16PtrFromString(%q) = %v", target, err)
	}
	var cred *credentialW
	r, _, errno := syscall.SyscallN(procCredReadW.Addr(),
		uintptr(unsafe.Pointer(p)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&cred)))
	runtime.KeepAlive(p)
	if r == 0 || cred == nil {
		t.Fatalf("CredReadW(%q) = %v", target, errno)
	}
	defer credFree(cred)
	return cred.Type, cred.Persist, windows.UTF16PtrToString(cred.UserName)
}

func TestCredentialManagerRoundTrip(t *testing.T) {
	if os.Getenv("TESSERA_WINCRED_TEST") == "" {
		t.Skip("set TESSERA_WINCRED_TEST=1 to test against Windows Credential Manager")
	}
	c := testCredentialManager()
	ctx := context.Background()
	key := filepath.Join(t.TempDir(), "vault.json")
	const secret = `pässwörd ✓ 鍵 "quoted" \slash`
	t.Cleanup(func() { _ = c.Delete(context.Background(), key) })

	if err := c.Available(ctx); err != nil {
		t.Fatalf("Available = %v", err)
	}
	if got, ok, err := c.Lookup(ctx, key); err != nil || ok || got != "" {
		t.Fatalf("Lookup before Store = %q, %v, %v", got, ok, err)
	}
	if has, err := c.Has(ctx, key); err != nil || has {
		t.Fatalf("Has before Store = %v, %v", has, err)
	}
	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete of a missing entry = %v", err)
	}

	if err := c.Store(ctx, key, secret, 0); err != nil {
		t.Fatalf("Store = %v", err)
	}
	if got, ok, err := c.Lookup(ctx, key); err != nil || !ok || got != secret {
		t.Fatalf("Lookup = %q, %v, %v", got, ok, err)
	}
	if has, err := c.Has(ctx, key); err != nil || !has {
		t.Fatalf("Has = %v, %v", has, err)
	}
	if typ, persist, user := readStoredCredential(t, testTargetPrefix+key); typ != credTypeGeneric || persist != credPersistLocalMachine || user != "tess" {
		t.Fatalf("stored Type, Persist, UserName = %d, %d, %q", typ, persist, user)
	}
	production, err := windows.UTF16PtrFromString(credTargetPrefix + key)
	if err != nil {
		t.Fatalf("UTF16PtrFromString = %v", err)
	}
	if _, err := credReadW(production, false); !isCredMiss(err) {
		t.Fatalf("the entry leaked under the tessera: prefix: %v", err)
	}

	replaced := strings.Repeat("é", maxSecretLen/2)
	if err := c.Store(ctx, key, replaced, 0); err != nil {
		t.Fatalf("Store over an existing entry = %v", err)
	}
	if got, ok, err := c.Lookup(ctx, key); err != nil || !ok || got != replaced {
		t.Fatalf("Lookup after replace = %d bytes, %v, %v", len(got), ok, err)
	}

	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	if has, err := c.Has(ctx, key); err != nil || has {
		t.Fatalf("Has after Delete = %v, %v", has, err)
	}
	if got, ok, err := c.Lookup(ctx, key); err != nil || ok || got != "" {
		t.Fatalf("Lookup after Delete = %q, %v, %v", got, ok, err)
	}
	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete of a deleted entry = %v", err)
	}
}
