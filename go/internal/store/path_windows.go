package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// DefaultPath returns the canonical vault path: $XDG_DATA_HOME\tessera\vault.json
// when XDG_DATA_HOME is absolute, else %USERPROFILE%\.local\share\tessera\vault.json
// when that file exists, else <Local AppData>\Tessera\vault.json.
func DefaultPath() (string, error) {
	return windowsDefaultPath(os.Getenv, localAppDataFolder, fileExists)
}

// DefaultPathHint returns the default vault path in the form shown to users.
func DefaultPathHint() string { return `%LOCALAPPDATA%\Tessera\vault.json` }

func localAppDataFolder() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func windowsDefaultPath(getenv func(string) string, knownFolder func() (string, error), exists func(string) bool) (string, error) {
	if x := getenv("XDG_DATA_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "tessera", "vault.json"), nil
	}
	if home := getenv("USERPROFILE"); home != "" {
		legacy := filepath.Join(home, ".local", "share", "tessera", "vault.json")
		if exists(legacy) {
			return legacy, nil
		}
	}
	base, err := knownFolder()
	if err != nil || base == "" {
		base = getenv("LOCALAPPDATA")
	}
	if base == "" {
		if err == nil {
			err = errors.New("empty path")
		}
		return "", fmt.Errorf("store: find the Local AppData folder: %w; set TESSERA_VAULT or pass --vault", err)
	}
	return filepath.Join(base, "Tessera", "vault.json"), nil
}
