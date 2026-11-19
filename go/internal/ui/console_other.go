//go:build !windows

package ui

// EnableConsole prepares stdout and stderr for styled output. Terminals off
// Windows interpret escape sequences already, so it changes nothing and the
// returned restore does nothing.
func EnableConsole() (restore func(), err error) {
	return func() {}, nil
}
