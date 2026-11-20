// Package secretstore keeps the vault passphrase in the operating system's
// secret store so the CLI stops prompting on every invocation. Each platform
// backend registers itself from init with a priority; the key is the resolved
// vault path, so several vaults coexist. It holds no crypto.
package secretstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	maxSecretLen = 2048
	backendEnv   = "TESSERA_SECRET_BACKEND"
)

var (
	ErrUnavailable    = errors.New("secretstore: no secret store is available")
	ErrLocked         = errors.New("secretstore: the secret store is locked; unlock it and retry")
	ErrTTLUnsupported = errors.New("secretstore: expiry is not supported")
	ErrInvalidSecret  = errors.New("secretstore: the passphrase must be 1 to 2048 bytes with no line breaks or NUL bytes")
	ErrInvalidKey     = errors.New("secretstore: the vault path must be non-empty with no line breaks or NUL bytes")
	ErrUnknownBackend = errors.New("secretstore: unknown backend")
)

// Backend is one platform secret store.
type Backend interface {
	// ID is the stable identifier accepted by TESSERA_SECRET_BACKEND.
	ID() string
	// Name is the human-readable store name.
	Name() string
	// Available returns nil or an *UnavailableError.
	Available(ctx context.Context) error
	// Store saves secret under key, replacing any entry. ttl 0 means no expiry;
	// a backend without expiry returns an error matching ErrTTLUnsupported.
	Store(ctx context.Context, key, secret string, ttl time.Duration) error
	// Lookup returns the secret for key. It never prompts. A missing entry is
	// "", false, nil; a locked one is "", false, ErrLocked.
	Lookup(ctx context.Context, key string) (secret string, found bool, err error)
	// Has reports whether an entry exists without reading it. It never
	// prompts. A locked entry is false, ErrLocked.
	Has(ctx context.Context, key string) (bool, error)
	// Delete removes the entry for key. A missing entry is not an error. It
	// may prompt to unlock the store.
	Delete(ctx context.Context, key string) error
}

// UnavailableError reports why a backend cannot be used. Backend is the
// backend's Name.
type UnavailableError struct{ Backend, Reason string }

func (e *UnavailableError) Error() string {
	return fmt.Sprintf("secretstore: the %s is unavailable: %s", e.Backend, e.Reason)
}

func (e *UnavailableError) Is(target error) bool { return target == ErrUnavailable }

// NoBackendError reports that no backend can store a secret, with each
// backend's reason.
type NoBackendError struct{ Tried []*UnavailableError }

func (e *NoBackendError) Error() string {
	var b strings.Builder
	b.WriteString("secretstore: no secret store is available")
	var reasons []string
	for _, u := range e.Tried {
		if u != nil {
			reasons = append(reasons, u.Backend+": "+u.Reason)
		}
	}
	if len(reasons) == 0 {
		b.WriteString(" on this system")
	} else {
		b.WriteString(" (")
		b.WriteString(strings.Join(reasons, "; "))
		b.WriteString(")")
	}
	b.WriteString("; set TESSERA_PASSPHRASE to unlock without one")
	return b.String()
}

func (e *NoBackendError) Is(target error) bool { return target == ErrUnavailable }

// Presence is what one available backend holds for a key. Locked means an
// entry may exist but cannot be read until the store is unlocked.
type Presence struct {
	Backend Backend
	Present bool
	Locked  bool
	Err     error
}

type registration struct {
	priority int
	ctor     func() Backend
}

var (
	registryMu sync.RWMutex
	registry   []registration
	swapped    []Backend
	isSwapped  bool
)

func register(priority int, ctor func() Backend) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = append(registry, registration{priority: priority, ctor: ctor})
	slices.SortStableFunc(registry, func(a, b registration) int { return a.priority - b.priority })
}

// Backends returns the backends for this platform by ascending priority.
func Backends() []Backend {
	registryMu.RLock()
	defer registryMu.RUnlock()
	if isSwapped {
		return slices.Clone(swapped)
	}
	out := make([]Backend, 0, len(registry))
	for _, r := range registry {
		out = append(out, r.ctor())
	}
	return out
}

// SwapForTest replaces the registered backends with bs, in the given order,
// until restore is called.
func SwapForTest(bs ...Backend) (restore func()) {
	registryMu.Lock()
	defer registryMu.Unlock()
	prev, prevSwapped := swapped, isSwapped
	swapped, isSwapped = slices.Clone(bs), true
	return func() {
		registryMu.Lock()
		defer registryMu.Unlock()
		swapped, isSwapped = prev, prevSwapped
	}
}

// ValidateKey rejects a key that is empty or contains a line break or NUL.
func ValidateKey(key string) error {
	if key == "" || strings.ContainsAny(key, "\x00\r\n") {
		return ErrInvalidKey
	}
	return nil
}

// ValidateSecret rejects a secret that is empty, longer than 2048 bytes, or
// contains a line break or NUL.
func ValidateSecret(secret string) error {
	if secret == "" || len(secret) > maxSecretLen || strings.ContainsAny(secret, "\x00\r\n") {
		return ErrInvalidSecret
	}
	return nil
}

// ForStore returns the backend that Store should use: the one named by
// TESSERA_SECRET_BACKEND, else the first available one.
func ForStore(ctx context.Context) (Backend, error) {
	backends := Backends()
	if id := os.Getenv(backendEnv); id != "" {
		for _, b := range backends {
			if b.ID() != id {
				continue
			}
			if err := b.Available(ctx); err != nil {
				return nil, &NoBackendError{Tried: []*UnavailableError{asUnavailable(b, err)}}
			}
			return b, nil
		}
		return nil, unknownBackend(id, backends)
	}
	var tried []*UnavailableError
	for _, b := range backends {
		err := b.Available(ctx)
		if err == nil {
			return b, nil
		}
		tried = append(tried, asUnavailable(b, err))
	}
	return nil, &NoBackendError{Tried: tried}
}

// Lookup returns the first secret found for key across the available
// backends. A locked backend does not stop the scan. With no hit it returns
// the first error that is not ErrLocked, else the ErrLocked error.
func Lookup(ctx context.Context, key string) (secret string, from Backend, found bool, err error) {
	if err := ValidateKey(key); err != nil {
		return "", nil, false, err
	}
	var lockedErr, otherErr error
	var lockedFrom, otherFrom Backend
	for _, b := range Backends() {
		if err := ctx.Err(); err != nil {
			return "", nil, false, err
		}
		if b.Available(ctx) != nil {
			continue
		}
		s, ok, err := b.Lookup(ctx, key)
		switch {
		case err == nil && ok:
			return s, b, true, nil
		case err == nil:
		case errors.Is(err, ErrLocked):
			if lockedErr == nil {
				lockedErr, lockedFrom = err, b
			}
		default:
			if otherErr == nil {
				otherErr, otherFrom = err, b
			}
		}
	}
	if otherErr != nil {
		return "", otherFrom, false, otherErr
	}
	if lockedErr != nil {
		return "", lockedFrom, false, lockedErr
	}
	return "", nil, false, nil
}

// Locate reports what each available backend holds for key. An invalid key
// yields one Presence per backend carrying ErrInvalidKey, and no backend runs.
func Locate(ctx context.Context, key string) []Presence {
	backends := Backends()
	if err := ValidateKey(key); err != nil {
		out := make([]Presence, 0, len(backends))
		for _, b := range backends {
			out = append(out, Presence{Backend: b, Err: err})
		}
		return out
	}
	var out []Presence
	for _, b := range backends {
		if b.Available(ctx) != nil {
			continue
		}
		has, err := b.Has(ctx, key)
		switch {
		case errors.Is(err, ErrLocked):
			out = append(out, Presence{Backend: b, Locked: true})
		case err != nil:
			out = append(out, Presence{Backend: b, Err: err})
		default:
			out = append(out, Presence{Backend: b, Present: has})
		}
	}
	return out
}

// Forget deletes key from every available backend that holds it and returns
// the Names of the backends it was removed from.
func Forget(ctx context.Context, key string) (removedFrom []string, err error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	var errs []error
	for _, b := range Backends() {
		if b.Available(ctx) != nil {
			continue
		}
		has, err := b.Has(ctx, key)
		if err != nil && !errors.Is(err, ErrLocked) {
			errs = append(errs, err)
			continue
		}
		if !has && err == nil {
			continue
		}
		if err := b.Delete(ctx, key); err != nil {
			errs = append(errs, err)
			continue
		}
		removedFrom = append(removedFrom, b.Name())
	}
	return removedFrom, errors.Join(errs...)
}

func asUnavailable(b Backend, err error) *UnavailableError {
	var u *UnavailableError
	if errors.As(err, &u) {
		return u
	}
	return &UnavailableError{Backend: b.Name(), Reason: err.Error()}
}

func unknownBackend(id string, backends []Backend) error {
	ids := make([]string, 0, len(backends))
	for _, b := range backends {
		ids = append(ids, b.ID())
	}
	if len(ids) == 0 {
		return fmt.Errorf("%w %q: this system has no secret store backends; unset %s", ErrUnknownBackend, id, backendEnv)
	}
	return fmt.Errorf("%w %q: set %s to one of: %s", ErrUnknownBackend, id, backendEnv, strings.Join(ids, ", "))
}

func errTTLUnsupported(b Backend) error {
	return fmt.Errorf("%w by the %s; store without --for", ErrTTLUnsupported, b.Name())
}
