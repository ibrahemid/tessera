package main

import "golang.org/x/sys/unix"

// hardenProcess keeps the memory of tess, which holds the vault key, out of
// core dumps and away from same-user debuggers. Failures are ignored.
func hardenProcess() {
	_ = unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{})
	_ = unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
}
