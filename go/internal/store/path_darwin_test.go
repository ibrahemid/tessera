package store

import "testing"

func TestDefaultPathDarwinIsUnchanged(t *testing.T) {
	t.Setenv("HOME", "/Users/someone")
	cases := []struct {
		name, xdg, want string
	}{
		{"unset", "", "/Users/someone/.local/share/tessera/vault.json"},
		{"absolute", "/data/xdg", "/data/xdg/tessera/vault.json"},
		{"relative", "rel/xdg", "rel/xdg/tessera/vault.json"},
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
