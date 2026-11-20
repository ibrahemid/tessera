package main

import (
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/spf13/cobra"
)

const githubURI = "otpauth://totp/GitHub:ibrahem?secret=JBSWY3DPEHPK3PXP&issuer=GitHub"

func emptyVault(t *testing.T) string {
	t.Helper()
	path := withVault(t)
	sealVault(t, path, []account.Account{})
	return path
}

func TestAddNamesTheAliasAndSkipsADuplicate(t *testing.T) {
	path := emptyVault(t)
	stdout, stderr, err := runRootStreams(t, "add", githubURI)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if stdout != "Added GitHub (ibrahem) as gi\n" {
		t.Errorf("stdout = %q", stdout)
	}
	if stderr != "" {
		t.Errorf("an argument off a terminal needs no warning, got %q", stderr)
	}
	stdout, _, err = runRootStreams(t, "add", githubURI)
	if err != nil {
		t.Fatalf("a duplicate is not a failure: %v", err)
	}
	if stdout != "Skipped GitHub (ibrahem): already in the vault\n" {
		t.Errorf("stdout = %q", stdout)
	}
	if got := openVaultFile(t, path, "testpass123"); len(got) != 1 {
		t.Fatalf("want one account, got %d", len(got))
	}
}

func TestAddBareKeyExplainsHowToNameIt(t *testing.T) {
	emptyVault(t)
	stdout, _, err := runRootStreams(t, "add", "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if !strings.HasPrefix(stdout, "Added an account with no name as ") || !strings.Contains(stdout, "--issuer NAME`.") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestAddBareKeyAsksForAnIssuerOnATerminal(t *testing.T) {
	path := emptyVault(t)
	stubStdinTTY(t, true)
	shown := stubPrompts(t, nil, []string{"GitHub"})
	stdout, stderr, err := runRootStreams(t, "add", "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(*shown) != 1 || (*shown)[0] != "Issuer: " {
		t.Errorf("prompts = %q", *shown)
	}
	if got := openVaultFile(t, path, "testpass123"); len(got) != 1 || got[0].Issuer != "GitHub" {
		t.Fatalf("issuer not stored: %+v", got)
	}
	if !strings.HasPrefix(stdout, "Added GitHub") {
		t.Errorf("stdout = %q", stdout)
	}
	if stderr != historyWarning+"\n" {
		t.Errorf("want one history line on stderr, got %q", stderr)
	}
}

func TestAddWithNoArgumentPromptsWithoutEcho(t *testing.T) {
	path := emptyVault(t)
	stubStdinTTY(t, true)
	shown := stubPrompts(t, []string{githubURI}, nil)
	stdout, stderr, err := runRootStreams(t, "add")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(*shown) != 1 || (*shown)[0] != "otpauth URI or setup key: " {
		t.Errorf("prompts = %q", *shown)
	}
	if !strings.HasPrefix(stdout, "Added GitHub (ibrahem)") || stderr != "" {
		t.Errorf("stdout = %q stderr = %q", stdout, stderr)
	}
	if got := openVaultFile(t, path, "testpass123"); len(got) != 1 {
		t.Fatalf("want one account, got %d", len(got))
	}
}

func TestAddWithNoArgumentOffATerminalExplainsTheInputs(t *testing.T) {
	emptyVault(t)
	_, stderr, err := runRootStreams(t, "add")
	if err == nil || !strings.Contains(stderr, "provide an otpauth URI") {
		t.Fatalf("err = %v stderr = %q", err, stderr)
	}
}

func TestAddSecretFlagReadsStdin(t *testing.T) {
	path := emptyVault(t)
	stubStdin(t, "JBSWY3DPEHPK3PXP\n")
	stdout, stderr, err := runRootStreams(t, "add", "--secret", "-", "--issuer", "GitHub", "--account", "me")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if !strings.HasPrefix(stdout, "Added GitHub (me)") || stderr != "" {
		t.Errorf("stdout = %q stderr = %q", stdout, stderr)
	}
	got := openVaultFile(t, path, "testpass123")
	if len(got) != 1 || string(got[0].Secret) != "Hello!\xde\xad\xbe\xef" {
		t.Fatalf("secret not read from stdin: %+v", got)
	}
}

func TestAddSecretFlagPromptsOnATerminal(t *testing.T) {
	emptyVault(t)
	stubStdinTTY(t, true)
	shown := stubPrompts(t, []string{"JBSWY3DPEHPK3PXP"}, nil)
	_, stderr, err := runRootStreams(t, "add", "--secret", "-", "--issuer", "GitHub")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(*shown) != 1 || (*shown)[0] != "Secret: " {
		t.Errorf("prompts = %q", *shown)
	}
	if stderr != "" {
		t.Errorf("a prompted secret needs no warning, got %q", stderr)
	}
}

func TestAddSecretFlagRefusesAnEmptyStdin(t *testing.T) {
	emptyVault(t)
	stubStdin(t, "\n")
	if _, stderr, err := runRootStreams(t, "add", "--secret", "-", "--issuer", "GitHub"); err == nil || !strings.Contains(stderr, "no secret on stdin") {
		t.Fatalf("err = %v stderr = %q", err, stderr)
	}
}

func TestSecretArgumentOnATerminalWarnsAboutHistory(t *testing.T) {
	cases := [][]string{
		{"add", githubURI},
		{"add", "--secret", "JBSWY3DPEHPK3PXP", "--issuer", "GitHub"},
		{"import", "--otpauth", githubURI},
	}
	for _, args := range cases {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			emptyVault(t)
			stubStdinTTY(t, true)
			stubPrompts(t, nil, nil)
			_, stderr, err := runRootStreams(t, args...)
			if err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			if stderr != historyWarning+"\n" {
				t.Errorf("want exactly the history line, got %q", stderr)
			}
		})
	}
}

func TestAddIssuerFlagSettlesAnIssuerMismatch(t *testing.T) {
	path := emptyVault(t)
	const uri = "otpauth://totp/GitHub:me?secret=JBSWY3DPEHPK3PXP&issuer=GitHub%20Inc"
	_, stderr, err := runRootStreams(t, "add", uri)
	if err == nil {
		t.Fatal("a mismatched issuer must not be added silently")
	}
	want := "error: issuer differs between the label (GitHub) and the issuer parameter (GitHub Inc); add with --issuer to choose\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if _, _, err := runRootStreams(t, "add", uri, "--issuer", "GitHub"); err != nil {
		t.Fatalf("add --issuer: %v", err)
	}
	if got := openVaultFile(t, path, "testpass123"); len(got) != 1 || got[0].Issuer != "GitHub" || got[0].Account != "me" {
		t.Fatalf("got %+v", got)
	}
}

func TestAddAndImportHelpSayArgumentsAreVisible(t *testing.T) {
	for _, c := range []*cobra.Command{newAddCmd(), newImportCmd()} {
		if !strings.Contains(c.Long, "visible to other processes") {
			t.Errorf("%s help does not say arguments are visible to other processes", c.Name())
		}
	}
}
