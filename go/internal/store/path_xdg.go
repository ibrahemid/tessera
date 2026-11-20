//go:build !darwin && !windows

package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultPath returns the canonical vault path:
// $XDG_DATA_HOME/tessera/vault.json when XDG_DATA_HOME is absolute, falling
// back to ~/.local/share/tessera/vault.json.
func DefaultPath() (string, error) {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "tessera", "vault.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("store: resolve home: %w", err)
	}
	return filepath.Join(home, ".local", "share", "tessera", "vault.json"), nil
}

// DefaultPathHint returns the default vault path in the form shown to users.
func DefaultPathHint() string { return "~/.local/share/tessera/vault.json" }
