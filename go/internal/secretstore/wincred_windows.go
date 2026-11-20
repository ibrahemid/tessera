//go:build windows

package secretstore

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	credMaxBlobSize         = 2560
	credUserName            = "tess"
	credTargetPrefix        = "tessera:"
)

var (
	errCredWrite     = errors.New("secretstore: could not write the Windows Credential Manager entry")
	errCredRead      = errors.New("secretstore: could not read the Windows Credential Manager entry")
	errCredQuery     = errors.New("secretstore: could not query the Windows Credential Manager")
	errCredDelete    = errors.New("secretstore: could not delete the Windows Credential Manager entry")
	errCredMalformed = errors.New("secretstore: the Windows Credential Manager entry does not hold a valid passphrase; forget it and remember the passphrase again")
)

// credentialW mirrors CREDENTIALW from wincred.h.
type credentialW struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

var (
	modadvapi32     = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW  = modadvapi32.NewProc("CredWriteW")
	procCredReadW   = modadvapi32.NewProc("CredReadW")
	procCredDeleteW = modadvapi32.NewProc("CredDeleteW")
	procCredFree    = modadvapi32.NewProc("CredFree")

	loadCredProcs = sync.OnceValue(func() error {
		for _, p := range []*windows.LazyProc{procCredWriteW, procCredReadW, procCredDeleteW, procCredFree} {
			if err := p.Find(); err != nil {
				return err
			}
		}
		return nil
	})
)

var (
	credWrite  = credWriteW
	credRead   = credReadW
	credDelete = credDeleteW
)

// credError is a failed Credential Manager call and its GetLastError code.
type credError struct {
	Call  string
	Errno syscall.Errno
}

func (e *credError) Error() string {
	if e.Errno == 0 {
		return e.Call + " failed"
	}
	return e.Call + ": " + e.Errno.Error()
}

func (e *credError) Unwrap() error {
	if e.Errno == 0 {
		return nil
	}
	return e.Errno
}

func init() {
	register(10, func() Backend { return credentialManager{prefix: credTargetPrefix} })
}

type credentialManager struct{ prefix string }

func (credentialManager) ID() string   { return "credential-manager" }
func (credentialManager) Name() string { return "Windows Credential Manager" }

func (c credentialManager) Available(context.Context) error {
	if err := loadCredProcs(); err != nil {
		return &UnavailableError{Backend: c.Name(), Reason: err.Error()}
	}
	target, err := windows.UTF16PtrFromString(c.prefix)
	if err != nil {
		return &UnavailableError{Backend: c.Name(), Reason: err.Error()}
	}
	_, err = credRead(target, false)
	switch {
	case err == nil, isCredMiss(err):
		return nil
	case errors.Is(err, windows.ERROR_NO_SUCH_LOGON_SESSION):
		return &UnavailableError{Backend: c.Name(), Reason: "this logon session has no credential set"}
	default:
		return &UnavailableError{Backend: c.Name(), Reason: err.Error()}
	}
}

func (c credentialManager) Store(ctx context.Context, key, secret string, ttl time.Duration) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if err := ValidateSecret(secret); err != nil {
		return err
	}
	if len(secret) > credMaxBlobSize {
		return ErrInvalidSecret
	}
	if ttl != 0 {
		return errTTLUnsupported(c)
	}
	target, err := c.prepare(ctx, key)
	if err != nil {
		return err
	}
	user, err := windows.UTF16PtrFromString(credUserName)
	if err != nil {
		return fmt.Errorf("%w: %w", errCredWrite, err)
	}
	blob := []byte(secret)
	defer clear(blob)
	cred := credentialW{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(blob)),
		CredentialBlob:     &blob[0],
		Persist:            credPersistLocalMachine,
		UserName:           user,
	}
	if err := credWrite(&cred); err != nil {
		return fmt.Errorf("%w: %w", errCredWrite, err)
	}
	return nil
}

func (c credentialManager) Lookup(ctx context.Context, key string) (string, bool, error) {
	if err := ValidateKey(key); err != nil {
		return "", false, err
	}
	target, err := c.prepare(ctx, key)
	if err != nil {
		return "", false, err
	}
	blob, err := credRead(target, true)
	if isCredMiss(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("%w: %w", errCredRead, err)
	}
	defer clear(blob)
	secret := string(blob)
	if ValidateSecret(secret) != nil {
		return "", false, errCredMalformed
	}
	return secret, true, nil
}

func (c credentialManager) Has(ctx context.Context, key string) (bool, error) {
	if err := ValidateKey(key); err != nil {
		return false, err
	}
	target, err := c.prepare(ctx, key)
	if err != nil {
		return false, err
	}
	_, err = credRead(target, false)
	if isCredMiss(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: %w", errCredQuery, err)
	}
	return true, nil
}

func (c credentialManager) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	target, err := c.prepare(ctx, key)
	if err != nil {
		return err
	}
	err = credDelete(target)
	if err == nil || isCredMiss(err) {
		return nil
	}
	return fmt.Errorf("%w: %w", errCredDelete, err)
}

func (c credentialManager) prepare(ctx context.Context, key string) (*uint16, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("secretstore: %s: %w", c.Name(), err)
	}
	if err := loadCredProcs(); err != nil {
		return nil, &UnavailableError{Backend: c.Name(), Reason: err.Error()}
	}
	target, err := windows.UTF16PtrFromString(c.prefix + key)
	if err != nil {
		return nil, ErrInvalidKey
	}
	return target, nil
}

func isCredMiss(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_FOUND)
}

func credWriteW(cred *credentialW) error {
	r, _, errno := syscall.SyscallN(procCredWriteW.Addr(), uintptr(unsafe.Pointer(cred)), 0)
	runtime.KeepAlive(cred)
	if r == 0 {
		return &credError{Call: "CredWriteW", Errno: errno}
	}
	return nil
}

// credReadW returns a copy of the blob when withBlob is set, and zeroes the
// system's buffer before freeing it.
func credReadW(target *uint16, withBlob bool) ([]byte, error) {
	var cred *credentialW
	r, _, errno := syscall.SyscallN(procCredReadW.Addr(),
		uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&cred)))
	runtime.KeepAlive(target)
	if r == 0 {
		return nil, &credError{Call: "CredReadW", Errno: errno}
	}
	if cred == nil {
		return nil, &credError{Call: "CredReadW"}
	}
	defer credFree(cred)
	if !withBlob {
		return nil, nil
	}
	if cred.CredentialBlob == nil || cred.CredentialBlobSize == 0 {
		return []byte{}, nil
	}
	src := unsafe.Slice(cred.CredentialBlob, cred.CredentialBlobSize)
	blob := make([]byte, len(src))
	copy(blob, src)
	clear(src)
	return blob, nil
}

func credDeleteW(target *uint16) error {
	r, _, errno := syscall.SyscallN(procCredDeleteW.Addr(), uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	runtime.KeepAlive(target)
	if r == 0 {
		return &credError{Call: "CredDeleteW", Errno: errno}
	}
	return nil
}

func credFree(cred *credentialW) {
	syscall.SyscallN(procCredFree.Addr(), uintptr(unsafe.Pointer(cred)))
}
