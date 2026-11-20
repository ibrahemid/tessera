package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/secretstore"
	"github.com/ibrahemid/tessera/go/internal/secretstore/secretstoretest"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/vault"
)

func TestUserMessageNamesTheFix(t *testing.T) {
	path := withVault(t)
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"missing vault", fmt.Errorf("load: %w", store.ErrNotFound),
			"no vault at " + path + ". Create one with `tess vault init`, or pass --vault to use another file."},
		{"bad setup key", fmt.Errorf("otpauth: secret: %w", base32x.ErrInvalid),
			"invalid setup key: base32 uses A-Z and 2-7"},
		{"env passphrase", &passphraseSourceError{source: "TESSERA_PASSPHRASE", path: path, err: vault.ErrWrongPassphrase},
			"the passphrase in $TESSERA_PASSPHRASE doesn't open " + path},
		{"keychain passphrase", &passphraseSourceError{store: "keychain", path: path, err: vault.ErrWrongPassphrase},
			"the keychain passphrase doesn't open " + path + "; run `tess vault forget`"},
		{"secret service passphrase", &passphraseSourceError{store: "Secret Service", path: path, err: vault.ErrWrongPassphrase},
			"the Secret Service passphrase doesn't open " + path + "; run `tess vault forget`"},
		{"package prefix", errors.New("store: vault: something broke"), "something broke"},
		{"secret store prefix", fmt.Errorf("remember: %w", secretstore.ErrLocked), "remember: the secret store is locked; unlock it and retry"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := userMessage(c.err); got != c.want {
				t.Errorf("userMessage = %q, want %q", got, c.want)
			}
		})
	}
}

func TestReportErrorUsesTheExitCode(t *testing.T) {
	var buf bytes.Buffer
	if code := reportError(&buf, errors.New("plain")); code != 1 || buf.String() != "error: plain\n" {
		t.Errorf("code = %d output = %q", code, buf.String())
	}
	buf.Reset()
	wrapped := fmt.Errorf("code: %w", errAmbiguous(2, "acme"))
	if code := reportError(&buf, wrapped); code != 2 {
		t.Errorf("an ambiguous query exits %d, want 2", code)
	}
}

func TestMissingVaultSaysHowToCreateOne(t *testing.T) {
	path := withVault(t)
	_, stderr, err := runRootStreams(t, "list")
	if err == nil {
		t.Fatal("list without a vault must fail")
	}
	want := "error: no vault at " + path + ". Create one with `tess vault init`, or pass --vault to use another file.\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestWrongEnvPassphraseNamesTheVariable(t *testing.T) {
	path := withVault(t)
	sealAt(t, path, "a-different-pass", []account.Account{totp("a", "ACME", "x", 1)})
	_, stderr, err := runRootStreams(t, "list")
	if err == nil {
		t.Fatal("a wrong passphrase must fail")
	}
	if stderr != "error: the passphrase in $TESSERA_PASSPHRASE doesn't open "+path+"\n" {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestStaleStoredPassphraseSaysToForgetIt(t *testing.T) {
	cases := []struct {
		id, name, want string
	}{
		{"keychain", "login keychain", "the keychain passphrase doesn't open /v/vault.json; run `tess vault forget`"},
		{"secret-service", "Secret Service", "the Secret Service passphrase doesn't open /v/vault.json; run `tess vault forget`"},
		{"credential-manager", "Windows Credential Manager", "the Windows Credential Manager passphrase doesn't open /v/vault.json; run `tess vault forget`"},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			from := &secretstoretest.Fake{BackendID: c.id, BackendName: c.name}
			p := unlockProvider{
				path: "/v/vault.json",
				lookup: func() (string, secretstore.Backend, bool, error) {
					return "old", from, true, nil
				},
				prompt: func(string) (string, error) { return "", errors.New("no terminal") },
				tryOpen: func(string) ([]account.Account, error) {
					return nil, vault.ErrWrongPassphrase
				},
			}
			_, _, err := resolvePassphrase(p)
			if got := userMessage(err); got != c.want {
				t.Errorf("message = %q, want %q", got, c.want)
			}
		})
	}
}

func TestVaultInitRefusesAShortEnvPassphrase(t *testing.T) {
	path := withVault(t)
	t.Setenv("TESSERA_PASSPHRASE", "short")
	_, stderr, err := runRootStreams(t, "vault", "init")
	if err == nil {
		t.Fatal("init with a short $TESSERA_PASSPHRASE must fail")
	}
	if stderr != fmt.Sprintf("error: $TESSERA_PASSPHRASE must be at least %d characters\n", minPassphraseLen) {
		t.Errorf("stderr = %q", stderr)
	}
	if store.Exists(path) {
		t.Fatal("a vault was created with a short passphrase")
	}
}

func TestSecretLookingQueryIsNotEchoed(t *testing.T) {
	withVault(t)
	sealVault(t, vaultPath, []account.Account{totp("a", "ACME", "x", 1)})
	for _, q := range []string{
		"JBSWY3DPEHPK3PXP",
		"otpauth://totp/ACME:x?secret=JBSWY3DPEHPK3PXP",
	} {
		stdout, stderr, err := runRootStreams(t, "code", q)
		if err == nil {
			t.Fatalf("%q matched an account", q)
		}
		if strings.Contains(stdout+stderr, "JBSWY3DPEHPK3PXP") {
			t.Errorf("the secret was echoed: stdout %q stderr %q", stdout, stderr)
		}
		if !strings.Contains(stderr, "`tess add`") {
			t.Errorf("the error should point at tess add, got %q", stderr)
		}
	}
}

func TestMalformedSecretQueryIsNotEchoed(t *testing.T) {
	withVault(t)
	sealVault(t, vaultPath, []account.Account{totp("a", "ACME", "x", 1)})
	for _, q := range []string{
		"otpauth//totp/X:y?secret=GEZDGNBVGY3TQOJQ",
		"secret=GEZDGNBVGY3TQOJQ",
		"issuer=X&SECRET=GEZDGNBVGY3TQOJQ",
		"otpauth-migration//offline?data=GEZDGNBVGY3TQOJQ",
		"Data=GEZDGNBVGY3TQOJQ",
	} {
		stdout, stderr, err := runRootStreams(t, "code", q)
		if err == nil {
			t.Fatalf("%q matched an account", q)
		}
		if strings.Contains(stdout+stderr, "GEZDGNBVGY3TQOJQ") {
			t.Errorf("the secret was echoed: stdout %q stderr %q", stdout, stderr)
		}
		if !strings.Contains(stderr, "`tess add`") {
			t.Errorf("the error should point at tess add, got %q", stderr)
		}
	}
}

func TestAmbiguousQueryOffATerminalExitsTwo(t *testing.T) {
	for _, args := range [][]string{{"code", "acme"}, {"acme"}, {"show", "acme"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			withVault(t)
			sealVault(t, vaultPath, []account.Account{handled("a", "ACME", "one", "ac"), handled("b", "ACME", "two", "ac2")})
			stubTTY(t, false)
			stdout, stderr, err := runRootStreams(t, args...)
			var exit *exitError
			if !errors.As(err, &exit) || exit.code != 2 {
				t.Fatalf("err = %v, want exit status 2", err)
			}
			if stdout != "" {
				t.Errorf("the match table belongs on stderr, stdout = %q", stdout)
			}
			if !strings.Contains(stderr, "ac2") || !strings.HasSuffix(stderr, "error: 2 accounts match \"acme\"; pass an alias\n") {
				t.Errorf("stderr = %q", stderr)
			}
		})
	}
}

func TestAmbiguousQueryJSONStaysOnStdout(t *testing.T) {
	withVault(t)
	sealVault(t, vaultPath, []account.Account{handled("a", "ACME", "one", "ac"), handled("b", "ACME", "two", "ac2")})
	stubTTY(t, false)
	stdout, _, err := runRootStreams(t, "code", "acme", "--json")
	if err != nil {
		t.Fatalf("code --json: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(stdout), "[") {
		t.Errorf("want a JSON array on stdout, got %q", stdout)
	}
}

func TestAmbiguousQueryWithCopyExitsTwo(t *testing.T) {
	cases := []struct {
		args []string
		tty  bool
	}{
		{[]string{"code", "acme", "-c", "--json"}, false},
		{[]string{"code", "acme", "-c", "--json"}, true},
		{[]string{"code", "acme", "-c"}, true},
		{[]string{"acme", "-c"}, true},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s tty=%v", strings.Join(c.args, " "), c.tty), func(t *testing.T) {
			withVault(t)
			sealVault(t, vaultPath, []account.Account{handled("a", "ACME", "one", "ac"), handled("b", "ACME", "two", "ac2")})
			stubTTY(t, c.tty)
			stubClipboard(t, func(string) error {
				t.Error("an ambiguous query must not copy anything")
				return nil
			})
			stdout, stderr, err := runRootStreams(t, c.args...)
			var exit *exitError
			if !errors.As(err, &exit) || exit.code != 2 {
				t.Fatalf("err = %v, want exit status 2", err)
			}
			if stdout != "" {
				t.Errorf("the match table belongs on stderr, stdout = %q", stdout)
			}
			if !strings.Contains(stderr, "ac2") || !strings.HasSuffix(stderr, "error: 2 accounts match \"acme\"; pass an alias\n") {
				t.Errorf("stderr = %q", stderr)
			}
		})
	}
}

func TestImportProblemsGoToStderrWithTheReason(t *testing.T) {
	withVault(t)
	sealVault(t, vaultPath, []account.Account{})
	dir := t.TempDir()
	one := filepath.Join(dir, "missing.txt")
	stdout, stderr, _ := runRootStreams(t, "import", one)
	if stdout != "" {
		t.Errorf("problems belong on stderr, stdout = %q", stdout)
	}
	if !strings.HasPrefix(stderr, "Could not import 1 item:\n") || !strings.Contains(stderr, "no such file or directory") {
		t.Errorf("stderr = %q", stderr)
	}

	bad := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(bad, []byte("junk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, _ = runRootStreams(t, "import", one, bad)
	if !strings.HasPrefix(stderr, "Could not import 2 items:\n") {
		t.Errorf("stderr = %q", stderr)
	}
}
