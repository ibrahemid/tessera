//go:build windows

package main

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

const detachedFlags = windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP

// detachedProcAttrs starts the child with no console, outside the console's
// process group, and out of the job that holds tess, so closing the terminal
// does not end the wait. A job that forbids breakaway gets the second set.
func detachedProcAttrs() []*syscall.SysProcAttr {
	return []*syscall.SysProcAttr{
		{CreationFlags: detachedFlags | windows.CREATE_BREAKAWAY_FROM_JOB, HideWindow: true},
		{CreationFlags: detachedFlags, HideWindow: true},
	}
}

// spawnRetryable accepts the refusal CreateProcess returns when the job tess
// runs in does not allow breakaway.
func spawnRetryable(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
