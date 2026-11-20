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

func saveVault(w io.Writer, path string, env *vault.Envelope, rotated bool) (*store.BackupResult, error) {
	if err := store.Save(path, env); err != nil {
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
