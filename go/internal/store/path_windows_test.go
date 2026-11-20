package store

import (
	"errors"
	"strings"
	"testing"
)

func TestWindowsDefaultPath(t *testing.T) {
	const (
		profile = `C:\Users\someone`
		legacy  = `C:\Users\someone\.local\share\tessera\vault.json`
		known   = `C:\Users\someone\AppData\Local`
		envApp  = `D:\Local`
	)
	errNoFolder := errors.New("known folder unavailable")
	cases := []struct {
		name         string
		env          map[string]string
		knownFolder  string
		knownErr     error
		legacyExists bool
		want         string
		wantErr      bool
	}{
		{name: "absolute XDG", env: map[string]string{"XDG_DATA_HOME": `E:\xdg`, "USERPROFILE": profile},
			knownFolder: known, legacyExists: true, want: `E:\xdg\tessera\vault.json`},
		{name: "relative XDG is ignored", env: map[string]string{"XDG_DATA_HOME": `rel\xdg`, "USERPROFILE": profile},
			knownFolder: known, want: known + `\Tessera\vault.json`},
		{name: "legacy exists", env: map[string]string{"USERPROFILE": profile},
			knownFolder: known, legacyExists: true, want: legacy},
		{name: "legacy absent", env: map[string]string{"USERPROFILE": profile},
			knownFolder: known, want: known + `\Tessera\vault.json`},
		{name: "known folder fails", env: map[string]string{"USERPROFILE": profile, "LOCALAPPDATA": envApp},
			knownErr: errNoFolder, want: envApp + `\Tessera\vault.json`},
		{name: "known folder fails without LOCALAPPDATA", env: map[string]string{"USERPROFILE": profile},
			knownErr: errNoFolder, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			getenv := func(k string) string { return c.env[k] }
			knownFolder := func() (string, error) { return c.knownFolder, c.knownErr }
			exists := func(p string) bool { return c.legacyExists && p == legacy }
			got, err := windowsDefaultPath(getenv, knownFolder, exists)
			if c.wantErr {
				if err == nil || !errors.Is(err, errNoFolder) || !strings.Contains(err.Error(), "TESSERA_VAULT") {
					t.Fatalf("windowsDefaultPath = %q, %v; want an error wrapping the folder error and naming TESSERA_VAULT", got, err)
				}
				return
			}
			if err != nil || got != c.want {
				t.Errorf("windowsDefaultPath = %q, %v; want %q", got, err, c.want)
			}
		})
	}
}
