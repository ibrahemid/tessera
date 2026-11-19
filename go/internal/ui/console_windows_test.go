package ui

import (
	"errors"
	"os"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"golang.org/x/sys/windows"
)

func fileHandle(t *testing.T) windows.Handle {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stream")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return windows.Handle(f.Fd())
}

// consoleOutput opens the console screen buffer, attaching a new console when
// the test process has none, as under a CI service.
func consoleOutput(t *testing.T) windows.Handle {
	t.Helper()
	open := func() (windows.Handle, error) {
		name, err := windows.UTF16PtrFromString("CONOUT$")
		if err != nil {
			return windows.InvalidHandle, err
		}
		return windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	}
	h, err := open()
	if err != nil {
		kernel32 := windows.NewLazySystemDLL("kernel32.dll")
		if r, _, allocErr := kernel32.NewProc("AllocConsole").Call(); r == 0 {
			t.Skipf("no console available: %v; AllocConsole: %v", err, allocErr)
		}
		t.Cleanup(func() { _, _, _ = kernel32.NewProc("FreeConsole").Call() })
		if h, err = open(); err != nil {
			t.Fatalf("open CONOUT$ after AllocConsole: %v", err)
		}
	}
	t.Cleanup(func() { windows.CloseHandle(h) })
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		t.Fatalf("GetConsoleMode(CONOUT$): %v", err)
	}
	t.Cleanup(func() { _ = windows.SetConsoleMode(h, mode) })
	return h
}

func consoleMode(t *testing.T, h windows.Handle) uint32 {
	t.Helper()
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		t.Fatalf("GetConsoleMode: %v", err)
	}
	return mode
}

func TestEnableConsoleOnNonConsoleTurnsColorsOff(t *testing.T) {
	pinColorProfile(t, colorprofile.TrueColor)
	out, errOut := fileHandle(t), fileHandle(t)

	restore, err := enableConsole(out, errOut)
	if restore == nil {
		t.Fatal("restore = nil")
	}
	restore()
	var ce *ConsoleError
	if !errors.As(err, &ce) || ce.Stream != "stdout" {
		t.Fatalf("error = %v, want a *ConsoleError for stdout", err)
	}
	if got := outputProfile(); got != colorprofile.NoTTY {
		t.Errorf("color profile = %v, want %v", got, colorprofile.NoTTY)
	}
}

func TestEnableConsoleTurnsOnVirtualTerminalAndRestores(t *testing.T) {
	pinColorProfile(t, colorprofile.TrueColor)
	h := consoleOutput(t)
	base := consoleMode(t, h) &^ windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	if err := windows.SetConsoleMode(h, base); err != nil {
		t.Fatalf("SetConsoleMode: %v", err)
	}

	restore, err := enableConsole(h, h)
	if err != nil {
		t.Fatalf("enableConsole: %v", err)
	}
	if consoleMode(t, h)&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING == 0 {
		t.Error("virtual terminal processing is off after enableConsole")
	}
	if got := outputProfile(); got != colorprofile.TrueColor {
		t.Errorf("color profile = %v, want %v", got, colorprofile.TrueColor)
	}
	restore()
	if got := consoleMode(t, h); got != base {
		t.Errorf("mode after restore = %#x, want %#x", got, base)
	}
}

func TestEnableConsoleKeepsColorsWhenOnlyStderrIsRedirected(t *testing.T) {
	pinColorProfile(t, colorprofile.TrueColor)
	h := consoleOutput(t)

	restore, err := enableConsole(h, fileHandle(t))
	defer restore()
	var ce *ConsoleError
	if !errors.As(err, &ce) || ce.Stream != "stderr" {
		t.Fatalf("error = %v, want a *ConsoleError for stderr", err)
	}
	if got := outputProfile(); got != colorprofile.TrueColor {
		t.Errorf("color profile = %v, want %v", got, colorprofile.TrueColor)
	}
}
