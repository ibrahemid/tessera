//go:build unix

package store

import (
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"
)

const staleTempAge = 24 * time.Hour

var tempNamePattern = regexp.MustCompile(`^\.tess-[0-9]+\.tmp$`)

func removeStaleTemps(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := now.Add(-staleTempAge)
	for _, e := range entries {
		if !tempNamePattern.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !info.ModTime().Before(cutoff) {
			continue
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int64(st.Uid) != int64(os.Getuid()) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}
