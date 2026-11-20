//go:build windows

package clipboard

import (
	"errors"
	"os"
	"slices"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPlatformDetectWindowsIgnoresKnownOverrides(t *testing.T) {
	for _, forced := range []string{"", "wayland", "x11", "osc52"} {
		c, err := platformDetect(env{getenv: fixedEnv(map[string]string{OverrideEnv: forced})})
		if err != nil {
			t.Fatalf("override %q: %v", forced, err)
		}
		if c != (winClipboard{}) || c.Name() != "Windows clipboard" {
			t.Fatalf("override %q: got %#v", forced, c)
		}
	}
}

func TestPlatformDetectWindowsRejectsUnknownOverride(t *testing.T) {
	c, err := platformDetect(env{getenv: fixedEnv(map[string]string{OverrideEnv: "win32"})})
	var inv *InvalidOverrideError
	if c != nil || !errors.As(err, &inv) || inv.Value != "win32" {
		t.Fatalf("platformDetect = %v, %v; want *InvalidOverrideError", c, err)
	}
}

func TestLoadProcsFindsEveryCall(t *testing.T) {
	if err := loadProcs(); err != nil {
		t.Fatalf("loadProcs: %v", err)
	}
}

func TestSensitiveFormatsAreTheDocumentedNames(t *testing.T) {
	want := []string{
		"ExcludeClipboardContentFromMonitorProcessing",
		"CanIncludeInClipboardHistory",
		"CanUploadToCloudClipboard",
	}
	if !slices.Equal(sensitiveFormats, want) {
		t.Fatalf("sensitiveFormats = %q, want %q", sensitiveFormats, want)
	}
}

func TestRegisterFormatReturnsTheRegisteredRange(t *testing.T) {
	for _, name := range sensitiveFormats {
		first, err := registerFormat(name)
		if err != nil {
			t.Fatalf("registerFormat(%q): %v", name, err)
		}
		if first < 0xC000 || first > 0xFFFF {
			t.Fatalf("registerFormat(%q) = %#x, outside 0xC000-0xFFFF", name, first)
		}
		again, err := registerFormat(name)
		if err != nil || again != first {
			t.Fatalf("registerFormat(%q) again = %#x, %v; want %#x", name, again, err, first)
		}
	}
}

func TestWriteRejectsNULBeforeTouchingTheClipboard(t *testing.T) {
	err := winClipboard{}.Write("12\x003456", true)
	var op *OpError
	if !errors.As(err, &op) || op.Backend != "Windows clipboard" || op.Op != "write" || !errors.Is(err, errTextHasNUL) {
		t.Fatalf("Write = %v, want an OpError wrapping errTextHasNUL", err)
	}
}

func TestWin32ErrorFormatsAndUnwraps(t *testing.T) {
	err := callError("OpenClipboard", windows.ERROR_ACCESS_DENIED)
	if got, want := err.Error(), "OpenClipboard: "+windows.ERROR_ACCESS_DENIED.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatal("win32Error does not unwrap to its errno")
	}
	bare := callError("GlobalLock", 0)
	if got, want := bare.Error(), "GlobalLock failed"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if errors.Unwrap(bare) != nil {
		t.Fatal("a zero errno unwraps to a non-nil error")
	}
	var errno syscall.Errno
	if errors.As(bare, &errno) {
		t.Fatal("a zero errno is reported as a cause")
	}
}

// realClipboard skips unless TESSERA_WINCLIP_TEST is set; the tests that use
// it overwrite the clipboard of the session they run in and clear it after.
func realClipboard(t *testing.T) Clipboard {
	t.Helper()
	if os.Getenv("TESSERA_WINCLIP_TEST") == "" {
		t.Skip("set TESSERA_WINCLIP_TEST=1 to test against the Windows clipboard")
	}
	c, err := platformDetect(env{getenv: fixedEnv(nil)})
	if err != nil {
		t.Fatalf("platformDetect: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Clear(); err != nil {
			t.Errorf("Clear: %v", err)
		}
	})
	return c
}

const nonASCIIText = "tessera 123 456 ünïcödé ß 日本語 🔐 Ω"

func sensitiveFormatsAvailable(t *testing.T) []bool {
	t.Helper()
	got := make([]bool, len(sensitiveFormats))
	for i, name := range sensitiveFormats {
		id, err := registerFormat(name)
		if err != nil {
			t.Fatalf("registerFormat(%q): %v", name, err)
		}
		got[i] = isFormatAvailable(id)
	}
	return got
}

func TestWindowsClipboardRoundTrip(t *testing.T) {
	c := realClipboard(t)
	if err := c.Write(nonASCIIText, false); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := c.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != nonASCIIText {
		t.Fatalf("Read = %q, want %q", got, nonASCIIText)
	}
}

func TestWindowsClipboardSensitiveWriteAddsTheFormats(t *testing.T) {
	c := realClipboard(t)
	if err := c.Write(nonASCIIText, true); err != nil {
		t.Fatalf("Write: %v", err)
	}
	for i, ok := range sensitiveFormatsAvailable(t) {
		if !ok {
			t.Errorf("%s is not on the clipboard after a sensitive write", sensitiveFormats[i])
		}
	}
	got, err := c.Read()
	if err != nil || got != nonASCIIText {
		t.Fatalf("Read = %q, %v; want %q", got, err, nonASCIIText)
	}
}

func TestWindowsClipboardPlainWriteDropsTheFormats(t *testing.T) {
	c := realClipboard(t)
	if err := c.Write("111111", true); err != nil {
		t.Fatalf("sensitive Write: %v", err)
	}
	if err := c.Write("222222", false); err != nil {
		t.Fatalf("plain Write: %v", err)
	}
	for i, ok := range sensitiveFormatsAvailable(t) {
		if ok {
			t.Errorf("%s is still on the clipboard after a plain write", sensitiveFormats[i])
		}
	}
}

func TestWindowsClipboardClear(t *testing.T) {
	c := realClipboard(t)
	if err := c.Write(nonASCIIText, true); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	got, err := c.Read()
	if err != nil || got != "" {
		t.Fatalf("Read after Clear = %q, %v; want \"\"", got, err)
	}
	if isFormatAvailable(cfUnicodeText) {
		t.Fatal("CF_UNICODETEXT is still on the clipboard after Clear")
	}
	for i, ok := range sensitiveFormatsAvailable(t) {
		if ok {
			t.Errorf("%s is still on the clipboard after Clear", sensitiveFormats[i])
		}
	}
}

func TestWindowsClipboardWritesEmptyText(t *testing.T) {
	c := realClipboard(t)
	if err := c.Write("", false); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := c.Read()
	if err != nil || got != "" {
		t.Fatalf("Read = %q, %v; want \"\"", got, err)
	}
}
