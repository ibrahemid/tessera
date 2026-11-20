// Package secretstoretest provides an in-memory secretstore.Backend for tests.
package secretstoretest

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/ibrahemid/tessera/go/internal/secretstore"
)

// Call is one recorded Backend method call. Key and TTL are zero where the
// method has no such argument.
type Call struct {
	Method string
	Key    string
	TTL    time.Duration
}

// Fake is an in-memory secretstore.Backend that records every method call. Set
// the exported fields before first use. A non-nil AvailableErr is returned by
// Available and by every other method. Locked keys fail Lookup and Has with
// secretstore.ErrLocked; Store and Delete unlock them.
type Fake struct {
	BackendID    string
	BackendName  string
	AvailableErr error
	TTLSupported bool

	mu      sync.Mutex
	secrets map[string]string
	ttls    map[string]time.Duration
	locked  map[string]bool
	calls   []Call
}

var _ secretstore.Backend = (*Fake)(nil)

// ID returns BackendID.
func (f *Fake) ID() string { return f.BackendID }

// Name returns BackendName.
func (f *Fake) Name() string { return f.BackendName }

// Available returns AvailableErr.
func (f *Fake) Available(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(Call{Method: "Available"})
	return f.AvailableErr
}

// Store saves secret under key after the same validation real backends apply.
func (f *Fake) Store(_ context.Context, key, secret string, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(Call{Method: "Store", Key: key, TTL: ttl})
	if f.AvailableErr != nil {
		return f.AvailableErr
	}
	if err := secretstore.ValidateKey(key); err != nil {
		return err
	}
	if err := secretstore.ValidateSecret(secret); err != nil {
		return err
	}
	if ttl != 0 && !f.TTLSupported {
		return fmt.Errorf("%w by the %s; store without --for", secretstore.ErrTTLUnsupported, f.BackendName)
	}
	f.init()
	f.secrets[key] = secret
	f.ttls[key] = ttl
	delete(f.locked, key)
	return nil
}

// Lookup returns the secret for key, or secretstore.ErrLocked for a locked key.
func (f *Fake) Lookup(_ context.Context, key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(Call{Method: "Lookup", Key: key})
	if f.AvailableErr != nil {
		return "", false, f.AvailableErr
	}
	if err := secretstore.ValidateKey(key); err != nil {
		return "", false, err
	}
	if f.locked[key] {
		return "", false, secretstore.ErrLocked
	}
	s, ok := f.secrets[key]
	return s, ok, nil
}

// Has reports whether key has an entry, or secretstore.ErrLocked for a locked
// key.
func (f *Fake) Has(_ context.Context, key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(Call{Method: "Has", Key: key})
	if f.AvailableErr != nil {
		return false, f.AvailableErr
	}
	if err := secretstore.ValidateKey(key); err != nil {
		return false, err
	}
	if f.locked[key] {
		return false, secretstore.ErrLocked
	}
	_, ok := f.secrets[key]
	return ok, nil
}

// Delete removes key. A missing key is not an error.
func (f *Fake) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(Call{Method: "Delete", Key: key})
	if f.AvailableErr != nil {
		return f.AvailableErr
	}
	if err := secretstore.ValidateKey(key); err != nil {
		return err
	}
	delete(f.secrets, key)
	delete(f.ttls, key)
	delete(f.locked, key)
	return nil
}

// Put seeds an entry without recording a call.
func (f *Fake) Put(key, secret string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	f.secrets[key] = secret
}

// Lock makes Lookup and Has on key fail with secretstore.ErrLocked.
func (f *Fake) Lock(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	f.locked[key] = true
}

// Unlock reverses Lock.
func (f *Fake) Unlock(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.locked, key)
}

// Secret returns the stored secret for key without recording a call.
func (f *Fake) Secret(key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.secrets[key]
	return s, ok
}

// TTL returns the ttl the entry for key was stored with.
func (f *Fake) TTL(key string) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ttls[key]
}

// Calls returns the recorded calls in order.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *Fake) record(c Call) { f.calls = append(f.calls, c) }

func (f *Fake) init() {
	if f.secrets == nil {
		f.secrets = map[string]string{}
		f.ttls = map[string]time.Duration{}
		f.locked = map[string]bool{}
	}
}
