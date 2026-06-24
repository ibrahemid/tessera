// Package spectest loads the shared interop vectors from /spec/testvectors.json.
package spectest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Load reads /spec/testvectors.json by walking up from the working directory
// until it finds the spec file, then unmarshals it into v.
func Load(t *testing.T, v any) {
	t.Helper()
	path := find(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decode vectors: %v", err)
	}
}

// Path returns the resolved path to testvectors.json.
func Path(t *testing.T) string { t.Helper(); return find(t) }

func find(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "spec", "testvectors.json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("spec/testvectors.json not found walking up from working dir")
		}
		dir = parent
	}
}
