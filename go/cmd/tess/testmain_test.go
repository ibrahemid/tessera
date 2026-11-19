package main

import (
	"os"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/vault"
)

// TestMain pins stdin as not a terminal, so no test waits on a prompt when the
// suite runs from an interactive shell, and lowers the argon2id cost: every
// command test unlocks a vault, and the production cost is covered in internal/vault.
// It also swaps in an in-memory clipboard with no detached clearer, so no test
// reaches the real clipboard or spawns a process without its own stub. It clears
// $TESSERA_BACKUP_DIR so no test writes into a real backup folder.
func TestMain(m *testing.M) {
	os.Unsetenv("TESSERA_BACKUP_DIR")
	stdinIsTTY = func() bool { return false }
	vault.DefaultArgon2 = vault.Argon2Params{V: vault.DefaultArgon2.V, M: 64, T: 1, P: 1}
	detectClipboard = detectFake(&fakeClipboard{})
	startClipClear = func(int, string) error { return nil }
	os.Exit(m.Run())
}
