//go:build unix || windows

package store

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/procenv"
)

const lockHolderEnv = "TESSERA_STORE_LOCK_HOLDER"

func TestMain(m *testing.M) {
	if path := os.Getenv(lockHolderEnv); path != "" {
		holdLockUntilKilled(path)
	}
	os.Exit(m.Run())
}

func holdLockUntilKilled(path string) {
	l, err := TryAcquire(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println("locked")
	time.Sleep(time.Hour)
	runtime.KeepAlive(l)
	os.Exit(1)
}

func TestLockIsReleasedWhenTheHolderIsKilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(procenv.Child(), lockHolderEnv+"="+path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if strings.TrimSpace(line) != "locked" {
		t.Fatalf("holder did not take the lock: %q %v %s", line, err, stderr.String())
	}
	if _, err := TryAcquire(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("TryAcquire while the holder runs = %v, want ErrLocked", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	l, err := TryAcquire(path)
	if err != nil {
		t.Fatalf("TryAcquire after the holder was killed = %v, want the lock", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}
