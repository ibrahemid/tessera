package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/spf13/cobra"
)

var errNoBackupDir = errors.New("no backup folder; set $TESSERA_BACKUP_DIR to a folder or pass --to")

func newVaultBackupCmd() *cobra.Command {
	var to string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Write today's backup file now",
		Long: `Write the vault to tessera-backup-YYYYMMDD.json, named by the UTC date, in
$TESSERA_BACKUP_DIR or the folder given with --to. A backup written earlier the
same day is replaced. The file keeps only the passphrase unlock and opens with
the vault passphrase. The folder keeps the 30 newest backup files; older ones
are deleted, and no other file in it is touched.

With $TESSERA_BACKUP_DIR set, every change to the vault writes this file too.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := backupFolder(to)
			if err != nil {
				return err
			}
			s, err := openSession()
			if err != nil {
				return err
			}
			defer s.close()
			res, err := store.WriteBackup(dir, s.path, s.env, now(), false)
			if res == nil {
				return err
			}
			out(cmd, "Wrote %s", ui.Sanitize(res.Path))
			if len(res.Removed) > 0 {
				out(cmd, "Deleted %s", backupCount(len(res.Removed)))
			}
			if err != nil {
				warn(cmd.ErrOrStderr(), userMessage(err))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "write to this folder instead of $TESSERA_BACKUP_DIR")
	return cmd
}

func backupFolder(to string) (string, error) {
	if to != "" {
		dir, err := filepath.Abs(to)
		if err != nil {
			return "", err
		}
		return dir, store.CheckBackupDir(dir)
	}
	dir := os.Getenv(backupDirEnv)
	if dir == "" {
		return "", errNoBackupDir
	}
	var dirErr *store.BackupDirError
	if err := store.CheckBackupDir(dir); errors.As(err, &dirErr) {
		return "", errors.New(backupDirEnvProblem(dirErr))
	}
	return dir, nil
}
