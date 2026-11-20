//go:build unix

package main

import "syscall"

// detachedProcAttrs puts the child in its own session so it outlives the shell
// and the terminal that spawned tess.
func detachedProcAttrs() []*syscall.SysProcAttr {
	return []*syscall.SysProcAttr{{Setsid: true}}
}

func spawnRetryable(error) bool { return false }
