package main

import (
	"os"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/vault"
)

// Every command test unlocks a vault; the production argon2id cost is covered in internal/vault.
func TestMain(m *testing.M) {
	vault.DefaultArgon2 = vault.Argon2Params{V: vault.DefaultArgon2.V, M: 64, T: 1, P: 1}
	os.Exit(m.Run())
}
