//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package ui

import "golang.org/x/sys/unix"

// isForeground reports whether this process group owns the terminal on fd. A
// backgrounded job that reads or configures the terminal is stopped.
func isForeground(fd int) bool {
	pgrp, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	return err == nil && pgrp == unix.Getpgrp()
}
