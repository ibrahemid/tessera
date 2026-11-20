//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package ui

func isForeground(int) bool { return false }
