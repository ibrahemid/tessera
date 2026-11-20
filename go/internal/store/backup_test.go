package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/vault"
)

const (
	seWrap      = `{"type":"secure-enclave","se_key":"AAAA","nonce":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","ct":"AAAA"}`
	unknownWrap = `{"type":"fido2","cred":"AAAA"}`
)

func rewrap(t *testing.T, env *vault.Envelope, keepPassphrase bool, extra ...string) *vault.Envelope {
	t.Helper()
	data, err := env.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	var wraps []json.RawMessage
	for _, w := range extra {
		wraps = append(wraps, json.RawMessage(w))
	}
	if keepPassphrase {
		var own []json.RawMessage
		if err := json.Unmarshal(m["wraps"], &own); err != nil {
			t.Fatal(err)
		}
		wraps = append(wraps, own...)
	}
	if m["wraps"], err = json.Marshal(wraps); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	e, err := vault.Unmarshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func openIssuer(t *testing.T, path string) string {
	t.Helper()
	env, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	accts, err := env.Open("pw")
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	return accts[0].Issuer
}

func TestBackupNameIsTheUTCDay(t *testing.T) {
	cases := []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 11, 20, 23, 30, 0, 0, time.FixedZone("west", -5*3600)), "tessera-backup-20261121.json"},
		{time.Date(2026, 11, 21, 8, 0, 0, 0, time.FixedZone("east", 14*3600)), "tessera-backup-20261120.json"},
		{time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), "tessera-backup-20260102.json"},
	}
	for _, c := range cases {
		if got := BackupName(c.at); got != c.want {
			t.Errorf("BackupName(%v) = %s, want %s", c.at, got, c.want)
		}
	}
}

func TestBackupDateReadsOnlyExactNames(t *testing.T) {
	d, ok := BackupDate("tessera-backup-20261120.json")
	if !ok || !d.Equal(time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("BackupDate = %v, %v", d, ok)
	}
	for _, name := range []string{"tessera-backup-2026112.json", "tessera-backup-20261120.json.bak", "x-tessera-backup-20261120.json"} {
		if _, ok := BackupDate(name); ok {
			t.Errorf("BackupDate(%q) matched", name)
		}
	}
}

func TestWriteBackupStartsANewFileAtTheUTCDayBoundary(t *testing.T) {
	dir := t.TempDir()
	env := sealOne(t, "first")
	late := time.Date(2026, 11, 20, 23, 59, 59, 999_000_000, time.UTC)

	if _, err := WriteBackup(dir, "", env, late, false); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteBackup(dir, "", sealOne(t, "second"), late.Add(time.Millisecond).In(time.FixedZone("west", -8*3600)), false); err != nil {
		t.Fatal(err)
	}
	want := []string{"tessera-backup-20261120.json", "tessera-backup-20261121.json"}
	if got := dirNames(t, dir); !slices.Equal(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	if got := openIssuer(t, filepath.Join(dir, want[0])); got != "first" {
		t.Errorf("the earlier day holds %s", got)
	}
	if got := openIssuer(t, filepath.Join(dir, want[1])); got != "second" {
		t.Errorf("the later day holds %s", got)
	}
}

func TestWriteBackupReplacesTheSameDayFile(t *testing.T) {
	dir := t.TempDir()
	morning := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)

	if _, err := WriteBackup(dir, "", sealOne(t, "first"), morning, false); err != nil {
		t.Fatal(err)
	}
	res, err := WriteBackup(dir, "", sealOne(t, "second"), morning.Add(23*time.Hour), false)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirNames(t, dir); !slices.Equal(got, []string{"tessera-backup-20261120.json"}) {
		t.Fatalf("files = %v", got)
	}
	if got := openIssuer(t, res.Path); got != "second" {
		t.Errorf("the day file holds %s, want the later write", got)
	}
	assertOwnerOnly(t, res.Path)
	assertNoTempFiles(t, dir)
}

func TestWriteBackupKeepsOnlyPassphraseWraps(t *testing.T) {
	dir := t.TempDir()
	env := rewrap(t, sealOne(t, "x"), true, seWrap, unknownWrap)

	res, err := WriteBackup(dir, "", env, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := Load(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got := backup.WrapTypes(); !slices.Equal(got, []string{"passphrase"}) {
		t.Errorf("backup wraps = %v, want only the passphrase wrap", got)
	}
	if got := env.WrapTypes(); !slices.Equal(got, []string{"secure-enclave", "fido2", "passphrase"}) {
		t.Errorf("the written envelope changed: %v", got)
	}
	if got := openIssuer(t, res.Path); got != "x" {
		t.Errorf("backup holds %s", got)
	}
}

func TestWriteBackupRefusesAVaultWithoutAPassphraseWrap(t *testing.T) {
	dir := t.TempDir()
	env := rewrap(t, sealOne(t, "x"), false, seWrap)

	if _, err := WriteBackup(dir, "", env, time.Now(), false); !errors.Is(err, ErrBackupNeedsPassphrase) {
		t.Fatalf("WriteBackup = %v, want ErrBackupNeedsPassphrase", err)
	}
	if got := dirNames(t, dir); len(got) != 0 {
		t.Errorf("files written: %v", got)
	}
}

func TestWriteBackupKeepsTheThirtyNewestExactMatches(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var dated []string
	for i := range 35 {
		name := BackupName(start.AddDate(0, 0, i))
		dated = append(dated, name)
		touch(t, filepath.Join(dir, name))
	}
	others := []string{
		"tessera-backup-2025010.json",
		"tessera-backup-20250101.json.bak",
		"Tessera-backup-20250101.json",
		"tessera-backup-20250101.json~",
		"tessera-backup-2025010a.json",
		"notes.txt",
	}
	for _, name := range others {
		touch(t, filepath.Join(dir, name))
	}
	if err := os.Mkdir(filepath.Join(dir, "tessera-backup-20240101.json"), 0o700); err != nil {
		t.Fatal(err)
	}

	res, err := WriteBackup(dir, "", sealOne(t, "x"), time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), false)
	if err != nil {
		t.Fatal(err)
	}
	var wantRemoved []string
	for _, name := range dated[:6] {
		wantRemoved = append(wantRemoved, filepath.Join(dir, name))
	}
	slices.Reverse(wantRemoved)
	if !slices.Equal(res.Removed, wantRemoved) {
		t.Errorf("removed = %v, want %v", res.Removed, wantRemoved)
	}
	names, err := ListBackups(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != BackupRetention || names[0] != "tessera-backup-20260301.json" || names[len(names)-1] != dated[6] {
		t.Errorf("kept = %v", names)
	}
	for _, name := range append(others, "tessera-backup-20240101.json") {
		if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was touched: %v", name, err)
		}
	}
}

func TestWriteBackupAfterARotationDeletesEveryOtherBackup(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"tessera-backup-20261101.json", "tessera-backup-20261119.json", "tessera-backup-20991231.json", "keep-me.json"} {
		touch(t, filepath.Join(dir, name))
	}
	at := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	if _, err := WriteBackup(dir, "", sealOne(t, "old"), at, false); err != nil {
		t.Fatal(err)
	}

	res, err := WriteBackup(dir, "", sealOne(t, "rotated"), at.Add(time.Hour), true)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirNames(t, dir); !slices.Equal(got, []string{"keep-me.json", "tessera-backup-20261120.json"}) {
		t.Errorf("files = %v", got)
	}
	if len(res.Removed) != 3 {
		t.Errorf("removed = %v", res.Removed)
	}
	if got := openIssuer(t, res.Path); got != "rotated" {
		t.Errorf("the day file holds %s", got)
	}
}

func TestCheckBackupDirNamesTheProblem(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	touch(t, file)
	cases := []struct {
		dir  string
		want BackupDirProblem
	}{
		{"backups", BackupDirRelative},
		{filepath.Join(dir, "missing"), BackupDirMissing},
		{file, BackupDirNotFolder},
	}
	for _, c := range cases {
		err := CheckBackupDir(c.dir)
		var dirErr *BackupDirError
		if !errors.As(err, &dirErr) || dirErr.Problem != c.want || dirErr.Dir != c.dir {
			t.Errorf("CheckBackupDir(%s) = %v, want problem %d", c.dir, err, c.want)
		}
	}
	if err := CheckBackupDir(dir); err != nil {
		t.Errorf("CheckBackupDir(%s) = %v", dir, err)
	}
}

func TestWriteBackupToAnUnusableFolderWritesNothing(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	_, err := WriteBackup(missing, "", sealOne(t, "x"), time.Now(), false)
	var dirErr *BackupDirError
	if !errors.As(err, &dirErr) || dirErr.Problem != BackupDirMissing {
		t.Fatalf("WriteBackup = %v", err)
	}
	if _, err := os.Lstat(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the missing folder was created: %v", err)
	}
}

func TestBackupPruneErrorNamesEachFile(t *testing.T) {
	one := &BackupPruneError{Paths: []string{"/b/tessera-backup-20260101.json"}, Err: fs.ErrPermission}
	if got, want := one.Error(), fmt.Sprintf("store: could not delete the older backup /b/tessera-backup-20260101.json (%v); delete it by hand", fs.ErrPermission); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	two := &BackupPruneError{Paths: []string{"/b/a", "/b/c"}, Err: fs.ErrPermission}
	if got, want := two.Error(), fmt.Sprintf("store: could not delete the older backups /b/a, /b/c (%v); delete them by hand", fs.ErrPermission); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
