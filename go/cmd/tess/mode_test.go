package main

import (
	"io/fs"
	"runtime"
	"testing"
)

func assertMode(t *testing.T, name string, info fs.FileInfo, want fs.FileMode) {
	t.Helper()
	got := info.Mode().Perm()
	if runtime.GOOS == "windows" {
		if want&0o200 != 0 && got&0o200 == 0 {
			t.Errorf("%s mode = %v, want it writable", name, got)
		}
		return
	}
	if got != want {
		t.Errorf("%s mode = %v, want %04o", name, got, uint32(want))
	}
}
