//go:build windows

package clipboard

import (
	"encoding/binary"
	"errors"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
	// hwndMessage is HWND_MESSAGE, (HWND)-3.
	hwndMessage = ^uintptr(2)

	openClipboardTimeout = time.Second
	openClipboardDelay   = 10 * time.Millisecond
)

// sensitiveFormats keep a copy out of clipboard history and cloud clipboard
// sync. Each holds a DWORD 0.
var sensitiveFormats = []string{
	"ExcludeClipboardContentFromMonitorProcessing",
	"CanIncludeInClipboardHistory",
	"CanUploadToCloudClipboard",
}

var errTextHasNUL = errors.New("text contains a NUL character")

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procRegisterClipboardFormatW   = user32.NewProc("RegisterClipboardFormatW")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalFree   = kernel32.NewProc("GlobalFree")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procGlobalSize   = kernel32.NewProc("GlobalSize")
)

var loadProcs = sync.OnceValue(func() error {
	for _, p := range []*windows.LazyProc{
		procCreateWindowExW, procDestroyWindow, procOpenClipboard, procCloseClipboard,
		procEmptyClipboard, procSetClipboardData, procGetClipboardData,
		procIsClipboardFormatAvailable, procRegisterClipboardFormatW,
		procGlobalAlloc, procGlobalFree, procGlobalLock, procGlobalUnlock, procGlobalSize,
	} {
		if err := p.Find(); err != nil {
			return err
		}
	}
	return nil
})

// win32Error reports a failed Win32 call with its GetLastError code; a zero
// code means the call failed without setting one.
type win32Error struct {
	Call  string
	Errno syscall.Errno
}

func (e *win32Error) Error() string {
	if e.Errno == 0 {
		return e.Call + " failed"
	}
	return e.Call + ": " + e.Errno.Error()
}

func (e *win32Error) Unwrap() error {
	if e.Errno == 0 {
		return nil
	}
	return e.Errno
}

func callError(call string, errno syscall.Errno) error {
	return &win32Error{Call: call, Errno: errno}
}

type winClipboard struct{}

func platformDetect(e env) (Clipboard, error) {
	if _, err := override(e); err != nil {
		return nil, err
	}
	if loadProcs() != nil {
		return nil, &NoClipboardError{}
	}
	return winClipboard{}, nil
}

func (winClipboard) Name() string { return "Windows clipboard" }

// Write places text as CF_UNICODETEXT. A sensitive write places the history
// and cloud exclusion formats first, so a failure never leaves the text on
// the clipboard without them.
func (w winClipboard) Write(text string, sensitive bool) error {
	if strings.IndexByte(text, 0) >= 0 {
		return &OpError{Backend: w.Name(), Op: "write", Err: errTextHasNUL}
	}
	units, err := windows.UTF16FromString(text)
	if err != nil {
		return &OpError{Backend: w.Name(), Op: "write", Err: err}
	}
	data := make([]byte, 2*len(units))
	for i, u := range units {
		binary.LittleEndian.PutUint16(data[2*i:], u)
	}
	clear(units)
	defer clear(data)

	var marks []uint32
	if sensitive {
		for _, name := range sensitiveFormats {
			id, err := registerFormat(name)
			if err != nil {
				return &OpError{Backend: w.Name(), Op: "write", Err: err}
			}
			marks = append(marks, id)
		}
	}
	return w.do("write", func() error {
		if err := emptyClipboard(); err != nil {
			return err
		}
		var zero [4]byte
		for _, id := range marks {
			if err := setClipboardData(id, zero[:]); err != nil {
				return errors.Join(err, emptyClipboard())
			}
		}
		if err := setClipboardData(cfUnicodeText, data); err != nil {
			return errors.Join(err, emptyClipboard())
		}
		return nil
	})
}

func (w winClipboard) Read() (string, error) {
	var text string
	err := w.do("read", func() error {
		if !isFormatAvailable(cfUnicodeText) {
			return nil
		}
		var err error
		text, err = readUnicodeText()
		return err
	})
	return text, err
}

func (w winClipboard) Clear() error {
	return w.do("clear", emptyClipboard)
}

// do runs fn with the clipboard open on a fresh OS thread that is never
// unlocked, so the runtime ends the thread with the goroutine and any window
// or clipboard state a failed cleanup left behind goes with it.
func (w winClipboard) do(op string, fn func() error) error {
	if err := loadProcs(); err != nil {
		return &OpError{Backend: w.Name(), Op: op, Err: err}
	}
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		done <- withOpenClipboard(fn)
	}()
	if err := <-done; err != nil {
		return &OpError{Backend: w.Name(), Op: op, Err: err}
	}
	return nil
}

// withOpenClipboard opens the clipboard with a message-only owner window,
// runs fn, then closes the clipboard and destroys the window. The calling
// goroutine must be locked to its OS thread.
func withOpenClipboard(fn func() error) (err error) {
	hwnd, err := createMessageWindow()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, destroyWindow(hwnd)) }()
	if err := openClipboard(hwnd); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeClipboard()) }()
	return fn()
}

func createMessageWindow() (uintptr, error) {
	class, err := windows.UTF16PtrFromString("STATIC")
	if err != nil {
		return 0, err
	}
	hwnd, _, errno := syscall.SyscallN(procCreateWindowExW.Addr(),
		0,                              // dwExStyle
		uintptr(unsafe.Pointer(class)), // lpClassName
		0,                              // lpWindowName
		0,                              // dwStyle
		0, 0, 0, 0,                     // X, Y, nWidth, nHeight
		hwndMessage, // hWndParent
		0,           // hMenu
		0,           // hInstance
		0,           // lpParam
	)
	if hwnd == 0 {
		return 0, callError("CreateWindowExW", errno)
	}
	return hwnd, nil
}

func destroyWindow(hwnd uintptr) error {
	if r, _, errno := syscall.SyscallN(procDestroyWindow.Addr(), hwnd); r == 0 {
		return callError("DestroyWindow", errno)
	}
	return nil
}

// openClipboard retries while another window holds the clipboard open.
func openClipboard(hwnd uintptr) error {
	deadline := time.Now().Add(openClipboardTimeout)
	for {
		r, _, errno := syscall.SyscallN(procOpenClipboard.Addr(), hwnd)
		if r != 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return callError("OpenClipboard", errno)
		}
		time.Sleep(openClipboardDelay)
	}
}

func closeClipboard() error {
	if r, _, errno := syscall.SyscallN(procCloseClipboard.Addr()); r == 0 {
		return callError("CloseClipboard", errno)
	}
	return nil
}

func emptyClipboard() error {
	if r, _, errno := syscall.SyscallN(procEmptyClipboard.Addr()); r == 0 {
		return callError("EmptyClipboard", errno)
	}
	return nil
}

func registerFormat(name string) (uint32, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	id, _, errno := syscall.SyscallN(procRegisterClipboardFormatW.Addr(), uintptr(unsafe.Pointer(p)))
	if id == 0 {
		return 0, callError("RegisterClipboardFormatW", errno)
	}
	return uint32(id), nil
}

// isFormatAvailable treats every zero return as absent: the documentation
// gives no way to tell a failure from a missing format.
func isFormatAvailable(format uint32) bool {
	r, _, _ := syscall.SyscallN(procIsClipboardFormatAvailable.Addr(), uintptr(format))
	return r != 0
}

// setClipboardData copies data into a movable global block and hands it to
// the clipboard, which owns the block once the call succeeds.
func setClipboardData(format uint32, data []byte) error {
	h, _, errno := syscall.SyscallN(procGlobalAlloc.Addr(), gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return callError("GlobalAlloc", errno)
	}
	if err := fillGlobal(h, data); err != nil {
		return errors.Join(err, globalFree(h))
	}
	if r, _, errno := syscall.SyscallN(procSetClipboardData.Addr(), uintptr(format), h); r == 0 {
		return errors.Join(callError("SetClipboardData", errno), globalFree(h))
	}
	return nil
}

func fillGlobal(h uintptr, data []byte) error {
	p, err := globalLock(h)
	if err != nil {
		return err
	}
	copy(unsafe.Slice((*byte)(p), len(data)), data)
	return globalUnlock(h)
}

func readUnicodeText() (string, error) {
	h, _, errno := syscall.SyscallN(procGetClipboardData.Addr(), cfUnicodeText)
	if h == 0 {
		return "", callError("GetClipboardData", errno)
	}
	p, err := globalLock(h)
	if err != nil {
		return "", err
	}
	size, _, errno := syscall.SyscallN(procGlobalSize.Addr(), h)
	if size == 0 && errno != 0 {
		return "", errors.Join(callError("GlobalSize", errno), globalUnlock(h))
	}
	text := windows.UTF16ToString(unsafe.Slice((*uint16)(p), size/2))
	return text, globalUnlock(h)
}

func globalLock(h uintptr) (unsafe.Pointer, error) {
	r, _, errno := syscall.SyscallN(procGlobalLock.Addr(), h)
	if r == 0 {
		return nil, callError("GlobalLock", errno)
	}
	// r addresses memory the system allocated outside the Go heap.
	return *(*unsafe.Pointer)(unsafe.Pointer(&r)), nil
}

// globalUnlock follows the documented convention: zero with NO_ERROR means
// the block is now unlocked.
func globalUnlock(h uintptr) error {
	r, _, errno := syscall.SyscallN(procGlobalUnlock.Addr(), h)
	if r == 0 && errno != 0 {
		return callError("GlobalUnlock", errno)
	}
	return nil
}

func globalFree(h uintptr) error {
	if r, _, errno := syscall.SyscallN(procGlobalFree.Addr(), h); r != 0 {
		return callError("GlobalFree", errno)
	}
	return nil
}
