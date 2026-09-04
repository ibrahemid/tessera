//go:build unix

package main

import "syscall"

// detachedProcAttr puts the child in its own session so it outlives the shell
// and the terminal that spawned tess.
func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
