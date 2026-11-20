//go:build unix && !linux

package main

import "golang.org/x/sys/unix"

// hardenProcess keeps the memory of tess, which holds the vault key, out of
// core dumps. Failures are ignored.
func hardenProcess() {
	_ = unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{})
}
