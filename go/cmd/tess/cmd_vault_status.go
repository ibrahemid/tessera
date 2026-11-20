package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/ibrahemid/tessera/go/internal/vault"
)

type argon2Cost struct {
	M            uint32 `json:"m"`
	T            uint32 `json:"t"`
	P            uint8  `json:"p"`
	BelowDefault bool   `json:"below_default"`
}

type vaultStatus struct {
	Path           string       `json:"path"`
	Exists         bool         `json:"exists"`
	Size           int64        `json:"size,omitempty"`
	Modified       string       `json:"modified,omitempty"`
	Unreadable     string       `json:"unreadable,omitempty"`
	Version        int          `json:"version,omitempty"`
	Wraps          []string     `json:"wraps"`
	Argon2         []argon2Cost `json:"argon2"`
	Previous       string       `json:"previous,omitempty"`
	BackupDir      string       `json:"backup_dir,omitempty"`
	BackupDirError string       `json:"backup_dir_error,omitempty"`
	NewestBackup   string       `json:"newest_backup,omitempty"`
	RememberedIn   []string     `json:"remembered_in"`
	LockedIn       []string     `json:"locked_in"`

	modified time.Time
	loaded   bool
}

func readVaultStatus(path string) *vaultStatus {
	st := &vaultStatus{Path: path, Wraps: []string{}, Argon2: []argon2Cost{}}
	var env *vault.Envelope
	if info, err := os.Stat(path); err == nil {
		st.Exists = true
		st.Size = info.Size()
		st.modified = info.ModTime()
		st.Modified = info.ModTime().UTC().Format(time.RFC3339)
		loaded, lerr := store.Load(path)
		var withBak *store.BackupAvailableError
		if errors.As(lerr, &withBak) {
			lerr = withBak.Err
		}
		if lerr != nil {
			st.Unreadable = userMessage(lerr)
		} else {
			env, st.loaded = loaded, true
			st.Version = env.Version
			st.Wraps = env.WrapTypes()
			for _, w := range env.Wraps {
				if w.Type != "passphrase" || w.Params == nil {
					continue
				}
				p := *w.Params
				d := vault.DefaultArgon2
				st.Argon2 = append(st.Argon2, argon2Cost{M: p.M, T: p.T, P: p.P, BelowDefault: p.M < d.M || p.T < d.T || p.P < d.P})
			}
		}
	}
	if store.HasBackup(path) {
		st.Previous = store.BackupPath(path)
	}
	st.BackupDir = os.Getenv(backupDirEnv)
	if st.BackupDir == "" {
		return st
	}
	var dirErr *store.BackupDirError
	if err := store.CheckBackupDir(st.BackupDir); errors.As(err, &dirErr) {
		st.BackupDirError = backupDirEnvProblem(dirErr)
		return st
	}
	if env != nil && !store.HasPassphraseWrap(env) {
		st.BackupDirError = "the vault has no passphrase unlock, so no backup is written"
	}
	names, err := store.ListBackups(st.BackupDir)
	if err != nil {
		st.BackupDirError = fmt.Sprintf("cannot list %s (%s)", st.BackupDir, userMessage(err))
		return st
	}
	if len(names) > 0 {
		if d, ok := store.BackupDate(names[0]); ok {
			st.NewestBackup = d.Format("2006-01-02")
		}
	}
	return st
}

func (st *vaultStatus) lines() []string {
	lines := []string{"Path:     " + st.Path}
	if !st.Exists {
		lines = append(lines, "File:     not found")
	} else {
		lines = append(lines, fmt.Sprintf("File:     %d bytes, modified %s", st.Size, st.modified.Format("2006-01-02 15:04")))
		if !st.loaded {
			lines = append(lines, "Wraps:    unreadable ("+st.Unreadable+")")
		} else {
			lines = append(lines,
				fmt.Sprintf("Version:  %d", st.Version),
				"Wraps:    "+strings.Join(st.Wraps, ", "))
			if len(st.Argon2) > 0 {
				costs := make([]string, len(st.Argon2))
				for i, c := range st.Argon2 {
					costs[i] = fmt.Sprintf("m=%d KiB, t=%d, p=%d", c.M, c.T, c.P)
					if c.BelowDefault {
						costs[i] += " (below the default; raised at the next unlock)"
					}
				}
				lines = append(lines, "Argon2:   "+strings.Join(costs, "; "))
			}
		}
	}
	switch {
	case st.Previous != "":
		lines = append(lines, "Previous: "+st.Previous)
	case st.Exists:
		lines = append(lines, "Previous: none")
	}
	switch {
	case st.BackupDirError != "":
		lines = append(lines, "Backups:  not written: "+st.BackupDirError)
	case st.BackupDir != "" && st.NewestBackup != "":
		lines = append(lines, "Backups:  "+st.BackupDir+", newest "+st.NewestBackup)
	case st.BackupDir != "":
		lines = append(lines, "Backups:  "+st.BackupDir+", none yet")
	case st.Exists:
		lines = append(lines, "Backups:  off; set $"+backupDirEnv+" to a folder to keep a daily copy")
	}
	for i := range lines {
		lines[i] = ui.Sanitize(lines[i])
	}
	return lines
}
