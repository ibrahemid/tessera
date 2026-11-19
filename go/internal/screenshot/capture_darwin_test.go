//go:build darwin

package screenshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeScreencapture puts a screencapture script first on PATH. It records its
// argv and whether TESSERA_PASSPHRASE reached it, then runs body.
func fakeScreencapture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"" + dir + "/argv\"\n" +
		"printf '%s' \"${TESSERA_PASSPHRASE-unset}\" > \"" + dir + "/passphrase\"\n" +
		body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "screencapture"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/bin:/usr/bin")
	t.Setenv("TESSERA_PASSPHRASE", "test-passphrase-1")
	return dir
}

func TestCaptureRunsScreencapture(t *testing.T) {
	rec := fakeScreencapture(t, `printf 'png' > "$3"`)
	scratch := t.TempDir()
	path, err := Capture(context.Background(), scratch)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if want := filepath.Join(scratch, captureName); path != want {
		t.Fatalf("Capture = %s, want %s", path, want)
	}
	argv, err := os.ReadFile(filepath.Join(rec, "argv"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(argv), "-i\n-x\n"+path+"\n"; got != want {
		t.Fatalf("screencapture argv = %q, want %q", got, want)
	}
	pass, err := os.ReadFile(filepath.Join(rec, "passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	if string(pass) != "unset" {
		t.Fatal("screencapture inherited TESSERA_PASSPHRASE")
	}
}

func TestCaptureWithoutFileIsCancel(t *testing.T) {
	fakeScreencapture(t, "exit 1")
	if _, err := Capture(context.Background(), t.TempDir()); !errors.Is(err, ErrCancelled) {
		t.Fatalf("Capture = %v, want ErrCancelled", err)
	}
}

func TestCaptureFailureAfterWriting(t *testing.T) {
	fakeScreencapture(t, `printf 'png' > "$3"; exit 2`)
	_, err := Capture(context.Background(), t.TempDir())
	var ce *CaptureError
	if !errors.As(err, &ce) || ce.Tool != "screencapture" || !strings.Contains(err.Error(), "exit status 2") {
		t.Fatalf("Capture = %v, want a screencapture CaptureError", err)
	}
}

func TestCaptureHonoursContext(t *testing.T) {
	fakeScreencapture(t, "exec sleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Capture(ctx, t.TempDir())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Capture = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Capture returned after %v", elapsed)
	}
}
