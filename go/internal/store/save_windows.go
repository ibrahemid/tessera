package store

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	retryAttempts = 10
	retryDelay    = 20 * time.Millisecond
)

func isTransient(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// Scanners and indexers hold handles without FILE_SHARE_DELETE for a moment, failing a rename or read.
func retry(op, path string, fn func() error) error {
	var err error
	for attempt := 1; attempt <= retryAttempts; attempt++ {
		if err = fn(); err == nil || !isTransient(err) {
			return err
		}
		if attempt < retryAttempts {
			time.Sleep(time.Duration(attempt) * retryDelay)
		}
	}
	return fmt.Errorf("%s %s: still in use by another process after %d attempts: %w", op, path, retryAttempts, err)
}

func readFile(path string) ([]byte, error) {
	var data []byte
	err := retry("read", path, func() error {
		var rerr error
		data, rerr = readCappedFile(path)
		return rerr
	})
	return data, err
}

func removeFile(path string) error {
	return retry("remove", path, func() error { return os.Remove(path) })
}

func syncFile(f *os.File) error { return f.Sync() }

// MOVEFILE_WRITE_THROUGH returns only once the rename is on disk, so the directory needs no flush.
func syncDir(string) error { return nil }

func renameReplace(from, to string) error {
	return moveFile(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func renameNoReplace(from, to string) error {
	err := moveFile(from, to, windows.MOVEFILE_WRITE_THROUGH)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return &ExistsError{Path: to}
	}
	return err
}

func moveFile(from, to string, flags uint32) error {
	src, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return retry("rename", to, func() error {
		if err := windows.MoveFileEx(src, dst, flags); err != nil {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
		}
		return nil
	})
}

func privateSecurityDescriptor(dir bool) (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read the current user's SID: %w", err)
	}
	inherit := ""
	if dir {
		inherit = "OICI"
	}
	sddl := fmt.Sprintf("D:P(A;%[1]s;FA;;;%[2]s)(A;%[1]s;FA;;;SY)", inherit, user.User.Sid.String())
	return windows.SecurityDescriptorFromString(sddl)
}

func securityAttributes(sd *windows.SECURITY_DESCRIPTOR) *windows.SecurityAttributes {
	sa := &windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	return sa
}

func applyPrivateDACL(h windows.Handle, sd *windows.SECURITY_DESCRIPTOR) error {
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func createTemp(dir string) (*os.File, error) {
	sd, err := privateSecurityDescriptor(false)
	if err != nil {
		return nil, err
	}
	name := filepath.Join(dir, ".tess-"+rand.Text()+".tmp")
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.WRITE_DAC, 0,
		securityAttributes(sd), windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "create", Path: name, Err: err}
	}
	if err := applyPrivateDACL(h, sd); err != nil {
		windows.CloseHandle(h)
		os.Remove(name)
		return nil, &os.PathError{Op: "set access control", Path: name, Err: err}
	}
	return os.NewFile(uintptr(h), name), nil
}

func mkdirPrivate(dir string) error {
	dir = filepath.Clean(dir)
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return &os.PathError{Op: "mkdir", Path: dir, Err: syscall.ENOTDIR}
		}
		return nil
	}
	if parent := filepath.Dir(dir); parent != dir {
		if err := mkdirPrivate(parent); err != nil {
			return err
		}
	}
	sd, err := privateSecurityDescriptor(true)
	if err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	if err := windows.CreateDirectory(p, securityAttributes(sd)); err != nil {
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			return nil
		}
		return &os.PathError{Op: "mkdir", Path: dir, Err: err}
	}
	h, err := windows.CreateFile(p, windows.WRITE_DAC|windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return &os.PathError{Op: "open", Path: dir, Err: err}
	}
	defer windows.CloseHandle(h)
	if err := applyPrivateDACL(h, sd); err != nil {
		return &os.PathError{Op: "set access control", Path: dir, Err: err}
	}
	return nil
}
