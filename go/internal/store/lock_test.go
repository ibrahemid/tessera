package store

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLockIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	first, err := Acquire(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	// A second acquire must not succeed while the first is held.
	if _, err := TryAcquire(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("second acquire while held: %v, want ErrLocked", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	second, err := Acquire(path)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestLockSerializesWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	const workers = 8
	var (
		mu      sync.Mutex
		inside  int
		maxSeen int
		wg      sync.WaitGroup
	)
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			err := WithLock(path, func() error {
				mu.Lock()
				inside++
				if inside > maxSeen {
					maxSeen = inside
				}
				mu.Unlock()
				time.Sleep(2 * time.Millisecond)
				mu.Lock()
				inside--
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Errorf("WithLock: %v", err)
			}
		}()
	}
	wg.Wait()
	if maxSeen != 1 {
		t.Errorf("%d workers were inside the lock at once, want 1", maxSeen)
	}
}

// TestAcquireGivesUpAfterTheBoundedWait pins that a wedged holder cannot block
// another invocation forever.
func TestAcquireGivesUpAfterTheBoundedWait(t *testing.T) {
	old := lockTimeout
	lockTimeout = 150 * time.Millisecond
	t.Cleanup(func() { lockTimeout = old })

	path := filepath.Join(t.TempDir(), "vault.json")
	held, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	deadline := time.Now().Add(lockTimeout)
	if _, err := Acquire(path); err == nil {
		t.Fatal("acquire succeeded while the lock was held")
	}
	if time.Now().Before(deadline) {
		t.Errorf("acquire gave up before the %v wait elapsed", lockTimeout)
	}
}

func TestReleaseIsNilSafeAndIdempotent(t *testing.T) {
	var l *Lock
	if err := l.Release(); err != nil {
		t.Errorf("nil release: %v", err)
	}
	path := filepath.Join(t.TempDir(), "vault.json")
	held, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	if err := held.Release(); err != nil {
		t.Errorf("second release: %v", err)
	}
}

func TestWithLockPropagatesError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	want := errors.New("boom")
	if got := WithLock(path, func() error { return want }); !errors.Is(got, want) {
		t.Errorf("WithLock error = %v, want %v", got, want)
	}
}

func TestLockCreatesSidecarWithOwnerOnlyPerms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "vault.json")
	l, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	info, err := os.Stat(LockPath(path))
	if err != nil {
		t.Fatalf("stat lock file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("lock perms = %v, want 0600", info.Mode().Perm())
	}
}
