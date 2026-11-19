//go:build !unix && !windows

package main

import "syscall"

func detachedProcAttrs() []*syscall.SysProcAttr { return []*syscall.SysProcAttr{nil} }

func spawnRetryable(error) bool { return false }
