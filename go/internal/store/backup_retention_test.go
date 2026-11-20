package store

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/vault"
)

func lowerArgon2(t *testing.T) {
	t.Helper()
	old := vault.DefaultArgon2
	vault.DefaultArgon2 = vault.Argon2Params{V: old.V, M: 64, T: 1, P: 1}
	t.Cleanup(func() { vault.DefaultArgon2 = old })
}

func TestRetentionCountsOnlyRealDates(t *testing.T) {
	lowerArgon2(t)
	dir := t.TempDir()
	day := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	var real []string
	for i := 1; i <= 10; i++ {
		name := BackupName(day.AddDate(0, 0, -i))
		real = append(real, name)
		touch(t, filepath.Join(dir, name))
	}
	for i := 0; i < BackupRetention-1; i++ {
		touch(t, filepath.Join(dir, fmt.Sprintf("tessera-backup-9999%04d.json", i)))
	}
	if _, err := WriteBackup(dir, "", sealOne(t, "Now"), day, false); err != nil {
		t.Fatal(err)
	}
	var gone []string
	for _, name := range real {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			gone = append(gone, name)
		}
	}
	if len(gone) > 0 {
		t.Fatalf("%d dated backups deleted while %d names that are not dates were kept: %v", len(gone), BackupRetention-1, gone)
	}
}

func TestRetentionNeitherCountsNorPrunesUnrankedNames(t *testing.T) {
	lowerArgon2(t)
	dir := t.TempDir()
	day := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	unranked := []string{
		"tessera-backup-20261121.json",
		"tessera-backup-20991231.json",
		"tessera-backup-20260230.json",
		"tessera-backup-20261300.json",
		"tessera-backup-00000000.json",
	}
	for _, name := range unranked {
		touch(t, filepath.Join(dir, name))
	}
	for i := 1; i <= BackupRetention+2; i++ {
		touch(t, filepath.Join(dir, BackupName(day.AddDate(0, 0, -i))))
	}
	res, err := WriteBackup(dir, "", sealOne(t, "Now"), day, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, BackupName(day.AddDate(0, 0, -(BackupRetention+2)))),
		filepath.Join(dir, BackupName(day.AddDate(0, 0, -(BackupRetention+1)))),
		filepath.Join(dir, BackupName(day.AddDate(0, 0, -BackupRetention))),
	}
	got := slices.Clone(res.Removed)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("removed %v, want %v", got, want)
	}
	for _, name := range unranked {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was pruned by retention: %v", name, err)
		}
	}
}

func TestRotationCleanupRemovesUnrankedNames(t *testing.T) {
	lowerArgon2(t)
	dir := t.TempDir()
	day := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"tessera-backup-20991231.json", "tessera-backup-20261300.json", BackupName(day.AddDate(0, 0, -1))} {
		touch(t, filepath.Join(dir, name))
	}
	touch(t, filepath.Join(dir, "notes.json"))
	if _, err := WriteBackup(dir, "", sealOne(t, "Now"), day, true); err != nil {
		t.Fatal(err)
	}
	if got, want := dirNames(t, dir), []string{"notes.json", BackupName(day)}; !slices.Equal(got, want) {
		t.Fatalf("folder after rotation = %v, want %v", got, want)
	}
}

func TestBackupWriteSparesTheVaultItBacksUp(t *testing.T) {
	for _, rotated := range []bool{false, true} {
		t.Run(fmt.Sprintf("rotated=%v", rotated), func(t *testing.T) {
			lowerArgon2(t)
			dir := t.TempDir()
			day := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
			vaultPath := filepath.Join(dir, BackupName(day.AddDate(0, -3, 0)))
			env := sealOne(t, "Restored")
			if err := Save(vaultPath, env); err != nil {
				t.Fatal(err)
			}
			for i := 1; i <= BackupRetention; i++ {
				touch(t, filepath.Join(dir, BackupName(day.AddDate(0, 0, -i))))
			}
			if _, err := WriteBackup(dir, vaultPath, env, day, rotated); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(vaultPath); err != nil {
				t.Fatalf("the backup write deleted the vault it backs up (%s): %v", filepath.Base(vaultPath), err)
			}
		})
	}
}

func TestBackupWriteSparesTheVaultThroughAnotherName(t *testing.T) {
	lowerArgon2(t)
	dir := t.TempDir()
	day := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	vaultPath := filepath.Join(t.TempDir(), "vault.json")
	env := sealOne(t, "Linked")
	if err := Save(vaultPath, env); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, BackupName(day.AddDate(0, 0, -2)))
	if err := os.Link(vaultPath, alias); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if _, err := WriteBackup(dir, vaultPath, env, day, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(alias); err != nil {
		t.Fatalf("the rotation cleanup deleted a name of the vault file: %v", err)
	}
}
