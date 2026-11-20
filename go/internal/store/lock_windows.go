package store

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func tryAcquire(lockPath string) (*Lock, error) {
	f, err := openLockFile(lockPath)
	if err != nil {
		return nil, err
	}
	err = windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped))
	if err != nil {
		f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return &Lock{f: f}, nil
}

type fileAttributeTagInfo struct {
	FileAttributes uint32
	ReparseTag     uint32
}

func openLockFile(lockPath string) (*os.File, error) {
	sd, err := privateSecurityDescriptor(false)
	if err != nil {
		return nil, err
	}
	p, err := windows.UTF16PtrFromString(lockPath)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, securityAttributes(sd), windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, lockOpenError(lockPath, &os.PathError{Op: "open", Path: lockPath, Err: err})
	}
	var info fileAttributeTagInfo
	if err := windows.GetFileInformationByHandleEx(h, windows.FileAttributeTagInfo,
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(h)
		return nil, &os.PathError{Op: "stat", Path: lockPath, Err: err}
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 &&
		(info.ReparseTag == windows.IO_REPARSE_TAG_SYMLINK || info.ReparseTag == windows.IO_REPARSE_TAG_MOUNT_POINT) {
		windows.CloseHandle(h)
		return nil, &LockFileError{Path: lockPath, Reason: "is a symbolic link"}
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		windows.CloseHandle(h)
		return nil, &LockFileError{Path: lockPath, Reason: "is not a regular file"}
	}
	return os.NewFile(uintptr(h), lockPath), nil
}

// Release drops the lock. The sidecar file is never unlinked, as on unix.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	f := l.f
	l.f = nil
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
	return f.Close()
}
