package procenv

import (
	"slices"
	"testing"
)

func TestFilterDropsPassphraseVariables(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"TESSERA_PASSPHRASE=hunter2",
		"TESSERA_NEW_PASSPHRASE=next",
		"TESSERA_MERGE_PASSPHRASE=other",
		"TESSERA_VAULT=/tmp/v.json",
		"TESSERA_PASSPHRASE_HINT=kept",
		"EMPTY=",
	}
	want := []string{
		"PATH=/usr/bin",
		"TESSERA_VAULT=/tmp/v.json",
		"TESSERA_PASSPHRASE_HINT=kept",
		"EMPTY=",
	}
	if got := Filter(in); !slices.Equal(got, want) {
		t.Fatalf("Filter = %q, want %q", got, want)
	}
}

func TestChildOmitsPassphraseFromProcessEnvironment(t *testing.T) {
	t.Setenv("TESSERA_PASSPHRASE", "hunter2")
	t.Setenv("TESSERA_VAULT", "/tmp/v.json")
	env := Child()
	if slices.Contains(env, "TESSERA_PASSPHRASE=hunter2") {
		t.Fatal("Child kept TESSERA_PASSPHRASE")
	}
	if !slices.Contains(env, "TESSERA_VAULT=/tmp/v.json") {
		t.Fatal("Child dropped TESSERA_VAULT")
	}
}
