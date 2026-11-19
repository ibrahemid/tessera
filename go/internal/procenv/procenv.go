// Package procenv builds the environment handed to child processes.
package procenv

import (
	"os"
	"strings"
)

var passphraseVars = []string{
	"TESSERA_PASSPHRASE",
	"TESSERA_NEW_PASSPHRASE",
	"TESSERA_MERGE_PASSPHRASE",
}

// Child returns the current environment without the variables that carry a
// vault passphrase. Assign it to exec.Cmd.Env for every child process.
func Child() []string {
	return Filter(os.Environ())
}

// Filter drops the passphrase variables from env, which is in os.Environ form.
func Filter(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if carriesPassphrase(kv) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func carriesPassphrase(kv string) bool {
	name, _, _ := strings.Cut(kv, "=")
	for _, v := range passphraseVars {
		if name == v {
			return true
		}
	}
	return false
}
