package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/ibrahemid/tessera/go/internal/vault"
)

const backupDirEnv = "TESSERA_BACKUP_DIR"

var warnOut io.Writer = os.Stderr

func warn(w io.Writer, msg string) {
	fmt.Fprintln(w, "warning:", ui.Sanitize(msg))
}

// printNote returns a notify function that prints each notice to w.
func printNote(w io.Writer) func(string) {
	return func(msg string) { fmt.Fprintln(w, "note:", ui.Sanitize(msg)) }
}

// versionNotice is the notice for a write that took the stored vault at path
// from format version 1 to 2.
func versionNotice(path string) string {
	return path + " is now format version 2, which Tessera 1.1.0 and earlier (the Mac app and tess) cannot open. " +
		"It returns to version 1 once the Trash is empty (`tess trash empty`) and no account has a note " +
		"(`tess note --clear <account>`) or a manual position (`tess order --clear <account>`)."
}

// writeVault saves env to path. When the stored vault was format version 1 and
// env is version 2, it passes versionNotice to notify once the save succeeds. A
// vault this write creates, or a file that does not parse, has no version to
// rise from.
func writeVault(notify func(string), path string, env *vault.Envelope) error {
	before := 0
	if stored, err := store.Load(path); err == nil {
		before = stored.Version
	}
	if err := store.Save(path, env); err != nil {
		return err
	}
	if before == 1 && env.Version == 2 {
		notify(versionNotice(path))
	}
	return nil
}

// saveVault writes the vault through writeVault, then the automatic backup.
// Warnings go to w; the version notice goes to notify.
func saveVault(w io.Writer, notify func(string), path string, env *vault.Envelope, rotated bool) (*store.BackupResult, error) {
	if err := writeVault(notify, path, env); err != nil {
		return nil, err
	}
	if rotated {
		if err := store.RemoveBackup(path); err != nil {
			warn(w, fmt.Sprintf("%s still opens with the old key and could not be deleted (%s); delete it", store.BackupPath(path), userMessage(err)))
		}
	}
	return writeAutoBackup(w, path, env, rotated), nil
}

func writeAutoBackup(w io.Writer, path string, env *vault.Envelope, rotated bool) *store.BackupResult {
	dir := os.Getenv(backupDirEnv)
	if dir == "" {
		return nil
	}
	res, err := store.WriteBackup(dir, path, env, now(), rotated)
	if err != nil {
		warn(w, backupFailure(err))
	}
	return res
}

func backupFailure(err error) string {
	var dirErr *store.BackupDirError
	switch {
	case errors.As(err, &dirErr):
		return "no backup written: " + backupDirEnvProblem(dirErr)
	case errors.Is(err, store.ErrBackupNeedsPassphrase):
		return "no backup written: the vault has no passphrase unlock"
	}
	return userMessage(err)
}

func backupDirEnvProblem(e *store.BackupDirError) string {
	switch e.Problem {
	case store.BackupDirRelative:
		return fmt.Sprintf("$%s is a relative path (%s); set it to an absolute path", backupDirEnv, e.Dir)
	case store.BackupDirMissing:
		return fmt.Sprintf("the folder %s in $%s does not exist; create it or change the variable", e.Dir, backupDirEnv)
	case store.BackupDirNotFolder:
		return fmt.Sprintf("%s in $%s is not a folder; set the variable to a folder", e.Dir, backupDirEnv)
	}
	return fmt.Sprintf("cannot open the folder %s in $%s (%v)", e.Dir, backupDirEnv, e.Err)
}
