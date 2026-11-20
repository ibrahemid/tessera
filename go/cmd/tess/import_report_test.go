package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/spf13/cobra"
)

const (
	knownSecretB32 = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	freshSecretB32 = "JBSWY3DPEHPK3PXP"
)

// writeImportFile writes one duplicate of the GitHub fixture, one new account
// and one line that is not an account.
func writeImportFile(t *testing.T) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "accounts.txt")
	text := "otpauth://totp/GitHub:me?secret=" + knownSecretB32 + "&issuer=GitHub\n" +
		"otpauth://totp/Dropbox:me?secret=" + freshSecretB32 + "&issuer=Dropbox\n" +
		"not an account\n"
	if err := os.WriteFile(src, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return src
}

func sealGitHubVault(t *testing.T) string {
	t.Helper()
	path := withVault(t)
	sealVault(t, path, []account.Account{handled("a", "GitHub", "me", "gh")})
	return path
}

func assertNoSecret(t *testing.T, streams ...string) {
	t.Helper()
	for _, s := range streams {
		if strings.Contains(s, knownSecretB32) || strings.Contains(s, freshSecretB32) {
			t.Fatalf("the report echoes a secret:\n%s", s)
		}
	}
}

func handleOf(t *testing.T, path, issuer string) string {
	t.Helper()
	for _, a := range openVaultFile(t, path, "testpass123") {
		if a.Issuer == issuer {
			return a.Handle
		}
	}
	t.Fatalf("no %s account in the vault", issuer)
	return ""
}

func TestImportReportsEachItemThenTheTotals(t *testing.T) {
	path := sealGitHubVault(t)
	src := writeImportFile(t)
	stdout, stderr, err := runRootStreams(t, "import", src)
	if err != nil {
		t.Fatal(err)
	}
	want := "Skipped GitHub (me): already in the vault\n" +
		"Added Dropbox (me) as " + handleOf(t, path, "Dropbox") + "\n" +
		"Imported 1, skipped 1, failed 1\n"
	if stdout != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	if !strings.HasPrefix(stderr, "Could not import 1 item:\n  "+src+" line 3: ") {
		t.Fatalf("stderr = %q", stderr)
	}
	assertNoSecret(t, stdout, stderr)
}

func TestImportReportJSON(t *testing.T) {
	path := sealGitHubVault(t)
	src := writeImportFile(t)
	stdout, stderr, err := runRootStreams(t, "import", src, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Fatalf("--json wrote to stderr: %q", stderr)
	}
	assertNoSecret(t, stdout)
	var report struct {
		Items   []map[string]any `json:"items"`
		Added   int              `json:"added"`
		Skipped int              `json:"skipped"`
		Failed  int              `json:"failed"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if report.Added != 1 || report.Skipped != 1 || report.Failed != 1 || len(report.Items) != 3 {
		t.Fatalf("totals = %d/%d/%d with %d items", report.Added, report.Skipped, report.Failed, len(report.Items))
	}
	wantKeys := [][]string{
		{"account", "issuer", "reason", "status", "type"},
		{"account", "handle", "id", "issuer", "status", "type"},
		{"reason", "source", "status"},
	}
	for i, item := range report.Items {
		keys := make([]string, 0, len(item))
		for k := range item {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		if !slices.Equal(keys, wantKeys[i]) {
			t.Errorf("item %d keys = %v, want %v", i, keys, wantKeys[i])
		}
	}
	skipped, added, failed := report.Items[0], report.Items[1], report.Items[2]
	if skipped["status"] != "skipped" || skipped["issuer"] != "GitHub" || skipped["reason"] != reasonAlreadyInVault {
		t.Errorf("skipped item = %v", skipped)
	}
	if added["status"] != "added" || added["issuer"] != "Dropbox" || added["type"] != "totp" ||
		added["handle"] != handleOf(t, path, "Dropbox") {
		t.Errorf("added item = %v", added)
	}
	if failed["status"] != "failed" || failed["source"] != src+" line 3" || failed["reason"] == "" {
		t.Errorf("failed item = %v", failed)
	}
}

func TestImportOnlyDuplicatesSucceeds(t *testing.T) {
	sealGitHubVault(t)
	stdout, _, err := runRootStreams(t, "import", "--otpauth",
		"otpauth://totp/GitHub:me?secret="+knownSecretB32+"&issuer=GitHub")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "Skipped GitHub (me): already in the vault\nImported 0, skipped 1\n" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestImportNothingReadableExitsNonZero(t *testing.T) {
	sealGitHubVault(t)
	missing := filepath.Join(t.TempDir(), "missing.txt")
	if _, _, err := runRootStreams(t, "import", missing); !errors.Is(err, errNothingImported) {
		t.Fatalf("err = %v, want errNothingImported", err)
	}
	stdout, _, err := runRootStreams(t, "import", missing, "--json")
	if !errors.Is(err, errNothingImported) {
		t.Fatalf("--json err = %v, want errNothingImported", err)
	}
	var report importReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if report.Failed != 1 || report.Added != 0 || len(report.Items) != 1 || report.Items[0].Source != missing {
		t.Fatalf("report = %+v", report)
	}
}

func TestImportReportsAnAccountThatFailsValidation(t *testing.T) {
	s := &session{}
	bad := account.Account{Type: account.TOTP, Issuer: "Broken", Account: "who",
		Secret: []byte("12345678901234567890"), Algorithm: "MD5", Digits: 6, Period: 30}
	merged := mergeAccountsDetailed(s, []account.Account{bad})

	cmd := &cobra.Command{}
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	reportImport(cmd, s, merged, nil, false)
	if stdout.String() != "Imported 0, failed 1\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.HasPrefix(stderr.String(), "Could not import 1 item:\n  Broken (who): ") {
		t.Fatalf("stderr = %q", stderr.String())
	}

	stdout.Reset()
	reportImport(cmd, s, merged, nil, true)
	var report importReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 || len(report.Items) != 1 || report.Items[0].Status != "failed" ||
		*report.Items[0].Issuer != "Broken" || report.Items[0].Reason == "" || report.Items[0].ID != "" {
		t.Fatalf("report = %+v", report)
	}
}
