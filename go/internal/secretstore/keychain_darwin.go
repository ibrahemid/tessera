//go:build darwin

package secretstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ibrahemid/tessera/go/internal/procenv"
)

const (
	securityPath    = "/usr/bin/security"
	keychainService = "tessera"
)

var (
	errKeychainWrite     = errors.New("secretstore: could not write the login keychain entry; unlock the login keychain and retry")
	errKeychainPersisted = errors.New("secretstore: the login keychain entry did not persist; unlock the login keychain and retry")
	errKeychainRead      = errors.New("secretstore: could not read the login keychain entry; unlock the login keychain and retry")
	errKeychainQuery     = errors.New("secretstore: could not query the login keychain; unlock the login keychain and retry")
	errKeychainDelete    = errors.New("secretstore: could not delete the login keychain entry; unlock the login keychain and retry")
)

var securityCommand = func(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, securityPath, args...)
}

func init() {
	register(10, func() Backend { return keychainBackend{service: keychainService} })
}

type keychainBackend struct{ service string }

func (keychainBackend) ID() string   { return "keychain" }
func (keychainBackend) Name() string { return "login keychain" }

func (k keychainBackend) Available(context.Context) error {
	if _, err := os.Stat(securityPath); err != nil {
		return &UnavailableError{Backend: k.Name(), Reason: securityPath + " not found"}
	}
	return nil
}

func (k keychainBackend) Store(ctx context.Context, key, secret string, ttl time.Duration) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if err := ValidateSecret(secret); err != nil {
		return err
	}
	if ttl != 0 {
		return errTTLUnsupported(k)
	}
	// Batch mode over stdin keeps the secret out of argv.
	line := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n",
		quote(k.service), quote(key), quote(secret))
	if err := k.run(ctx, strings.NewReader(line), nil, "-i"); err != nil {
		return k.fail(ctx, errKeychainWrite)
	}
	// security -i exits 0 even when the add fails, so read the entry back.
	got, ok, err := k.Lookup(ctx, key)
	if err != nil {
		return err
	}
	if !ok || got != secret {
		return errKeychainPersisted
	}
	return nil
}

func (k keychainBackend) Lookup(ctx context.Context, key string) (string, bool, error) {
	if err := ValidateKey(key); err != nil {
		return "", false, err
	}
	var out bytes.Buffer
	err := k.run(ctx, nil, &out, "find-generic-password", "-s", k.service, "-a", key, "-w")
	if err == nil {
		return strings.TrimSuffix(out.String(), "\n"), true, nil
	}
	if isNotFound(err) {
		return "", false, nil
	}
	return "", false, k.fail(ctx, errKeychainRead)
}

func (k keychainBackend) Has(ctx context.Context, key string) (bool, error) {
	if err := ValidateKey(key); err != nil {
		return false, err
	}
	err := k.run(ctx, nil, nil, "find-generic-password", "-s", k.service, "-a", key)
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}
	return false, k.fail(ctx, errKeychainQuery)
}

func (k keychainBackend) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	err := k.run(ctx, nil, nil, "delete-generic-password", "-s", k.service, "-a", key)
	if err == nil || isNotFound(err) {
		return nil
	}
	return k.fail(ctx, errKeychainDelete)
}

func (k keychainBackend) run(ctx context.Context, stdin io.Reader, stdout io.Writer, args ...string) error {
	cmd := securityCommand(ctx, args...)
	cmd.Env = procenv.Child()
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	return cmd.Run()
}

func (k keychainBackend) fail(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("secretstore: %s: %w", k.Name(), ctxErr)
	}
	return err
}

// isNotFound reports whether err is security's errSecItemNotFound (exit 44).
func isNotFound(err error) bool {
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ExitCode() == 44
}

func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}
