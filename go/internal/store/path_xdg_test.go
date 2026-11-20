//go:build !darwin && !windows

package store

import "testing"

func TestDefaultPathHonoursOnlyAbsoluteXDGDataHome(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	cases := []struct {
		name, xdg, want string
	}{
		{"unset", "", "/home/someone/.local/share/tessera/vault.json"},
		{"absolute", "/data/xdg", "/data/xdg/tessera/vault.json"},
		{"relative is ignored", "rel/xdg", "/home/someone/.local/share/tessera/vault.json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", c.xdg)
			got, err := DefaultPath()
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("DefaultPath() = %q, want %q", got, c.want)
			}
		})
	}
}
