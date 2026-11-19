//go:build unix

package main

import (
	"errors"
	"syscall"
	"testing"
)

func TestDetachedProcAttrsStartANewSession(t *testing.T) {
	attrs := detachedProcAttrs()
	if len(attrs) != 1 || attrs[0] == nil || !attrs[0].Setsid {
		t.Errorf("attrs = %+v, want one set with Setsid", attrs)
	}
}

func TestSpawnIsNeverRetriedOnUnix(t *testing.T) {
	if spawnRetryable(syscall.EACCES) || spawnRetryable(errors.New("fork failed")) {
		t.Error("a unix spawn failure has no fallback")
	}
}
