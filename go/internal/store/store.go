// Package store handles vault file location and atomic, owner-only-readable
// persistence of the encrypted envelope. The path is shared with the macOS app.
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ibrahemid/tessera/go/internal/vault"
)

// ErrNotFound is returned when no vault exists at the resolved path.
var ErrNotFound = errors.New("store: no vault found (run `tess vault init`)")

// DefaultPath returns the canonical vault path:
// $XDG_DATA_HOME/tessera/vault.json, falling back to ~/.local/share/tessera/vault.json.
func DefaultPath() (string, error) {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "tessera", "vault.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("store: resolve home: %w", err)
	}
	return filepath.Join(home, ".local", "share", "tessera", "vault.json"), nil
}

// Resolve returns override if non-empty, else $TESSERA_VAULT, else DefaultPath.
func Resolve(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if e := os.Getenv("TESSERA_VAULT"); e != "" {
		return e, nil
	}
	return DefaultPath()
}

// Exists reports whether a vault file exists at path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Load reads and parses the envelope at path.
func Load(path string) (*vault.Envelope, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: read vault: %w", err)
	}
	return vault.Unmarshal(data)
}

// Save writes the envelope atomically with 0600 perms (dir created 0700).
func Save(path string, env *vault.Envelope) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("store: create vault dir: %w", err)
	}
	data, err := env.Marshal()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".vault-*.tmp")
	if err != nil {
		return fmt.Errorf("store: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("store: chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("store: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("store: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("store: rename: %w", err)
	}
	return nil
}
