//go:build windows

package main

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDetachedProcAttrsTryBreakawayFirst(t *testing.T) {
	attrs := detachedProcAttrs()
	if len(attrs) != 2 {
		t.Fatalf("got %d attribute sets, want 2", len(attrs))
	}
	for i, a := range attrs {
		if a == nil || !a.HideWindow || a.CreationFlags&detachedFlags != detachedFlags {
			t.Errorf("set %d = %+v, want a hidden, detached process group", i, a)
		}
	}
	if attrs[0].CreationFlags&windows.CREATE_BREAKAWAY_FROM_JOB == 0 {
		t.Error("the first set must break away from the job")
	}
	if attrs[1].CreationFlags&windows.CREATE_BREAKAWAY_FROM_JOB != 0 {
		t.Error("the fallback set must stay in the job")
	}
}

func TestSpawnRetriesOnlyADeniedBreakaway(t *testing.T) {
	denied := &os.PathError{Op: "fork/exec", Path: "tess.exe", Err: windows.ERROR_ACCESS_DENIED}
	if !spawnRetryable(denied) {
		t.Error("access denied must fall back to the set without breakaway")
	}
	notFound := &os.PathError{Op: "fork/exec", Path: "tess.exe", Err: windows.ERROR_FILE_NOT_FOUND}
	if spawnRetryable(notFound) || spawnRetryable(errors.New("fork failed")) {
		t.Error("only access denied is retried")
	}
}
