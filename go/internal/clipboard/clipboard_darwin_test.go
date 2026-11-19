//go:build darwin

package clipboard

import (
	"errors"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fakePasteboard struct {
	dir string
	pb  pasteboard
}

// newFakePasteboard points every tool at a recording fake, so no test touches
// the real pasteboard.
func newFakePasteboard(t *testing.T, osascriptBody, pbcopyBody, pbpasteBody string) fakePasteboard {
	t.Helper()
	dir := t.TempDir()
	return fakePasteboard{dir: dir, pb: pasteboard{
		osascript: fakeTool(t, dir, "osascript", osascriptBody),
		pbcopy:    fakeTool(t, dir, "pbcopy", pbcopyBody),
		pbpaste:   fakeTool(t, dir, "pbpaste", pbpasteBody),
	}}
}

func (f fakePasteboard) calls(t *testing.T, name string) []toolCall {
	return toolCalls(t, f.dir, name)
}

func TestPasteboardSensitiveWriteMarksTheItem(t *testing.T) {
	f := newFakePasteboard(t, "", "", "")
	if err := f.pb.Write("123456", true); err != nil {
		t.Fatalf("Write: %v", err)
	}
	calls := f.calls(t, "osascript")
	if len(calls) != 1 {
		t.Fatalf("osascript calls = %d, want 1", len(calls))
	}
	want := []string{"-l", "JavaScript", "-e", sensitiveWriteScript}
	if !slices.Equal(calls[0].args, want) {
		t.Fatalf("osascript argv = %q, want %q", calls[0].args, want)
	}
	if calls[0].stdin != "123456" {
		t.Fatalf("osascript stdin = %q, want the text", calls[0].stdin)
	}
	if n := len(f.calls(t, "pbcopy")); n != 0 {
		t.Fatalf("pbcopy ran %d times after osascript succeeded", n)
	}
}

func TestSensitiveWriteScriptDeclaresTheHints(t *testing.T) {
	for _, want := range []string{
		"fileHandleWithStandardInput",
		"'public.utf8-plain-text'",
		"'org.nspasteboard.ConcealedType'",
		"'org.nspasteboard.TransientType'",
		"prepareForNewContentsWithOptions($.NSPasteboardContentsCurrentHostOnly)",
		"$.NSArray.arrayWithObject(item)",
	} {
		if !strings.Contains(sensitiveWriteScript, want) {
			t.Errorf("sensitive write script lacks %s", want)
		}
	}
	if n := strings.Count(sensitiveWriteScript, "NSPasteboardItem.alloc"); n != 1 {
		t.Errorf("script builds %d pasteboard items, want 1", n)
	}
}

func TestPasteboardScriptsCompile(t *testing.T) {
	if _, err := exec.LookPath("/usr/bin/osacompile"); err != nil {
		t.Skip("osacompile not available")
	}
	for name, src := range map[string]string{"write": sensitiveWriteScript, "clear": clearScript} {
		out := filepath.Join(t.TempDir(), name+".scpt")
		cmd := exec.Command("/usr/bin/osacompile", "-l", "JavaScript", "-e", src, "-o", out)
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s script does not compile: %v: %s", name, err, msg)
		}
	}
}

func TestPasteboardSensitiveWriteFallsBackToPbcopy(t *testing.T) {
	f := newFakePasteboard(t, "echo 'execution error' >&2; exit 1", "", "")
	if err := f.pb.Write("123456", true); err != nil {
		t.Fatalf("Write: %v", err)
	}
	calls := f.calls(t, "pbcopy")
	if len(calls) != 1 || len(calls[0].args) != 0 || calls[0].stdin != "123456" {
		t.Fatalf("pbcopy calls = %+v, want one argless call with the text on stdin", calls)
	}
}

func TestPasteboardSensitiveWriteWithoutOsascriptUsesPbcopy(t *testing.T) {
	f := newFakePasteboard(t, "", "", "")
	f.pb.osascript = filepath.Join(f.dir, "absent")
	if err := f.pb.Write("123456", true); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if calls := f.calls(t, "pbcopy"); len(calls) != 1 || calls[0].stdin != "123456" {
		t.Fatalf("pbcopy calls = %+v", calls)
	}
}

func TestPasteboardPlainWriteUsesPbcopyOnly(t *testing.T) {
	f := newFakePasteboard(t, "", "", "")
	if err := f.pb.Write("hello", false); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n := len(f.calls(t, "osascript")); n != 0 {
		t.Fatalf("osascript ran %d times for a plain write", n)
	}
	if calls := f.calls(t, "pbcopy"); len(calls) != 1 || calls[0].stdin != "hello" {
		t.Fatalf("pbcopy calls = %+v", calls)
	}
}

func TestPasteboardWriteReportsPbcopyFailure(t *testing.T) {
	f := newFakePasteboard(t, "exit 1", "echo 'pasteboard unavailable' >&2; exit 1", "")
	err := f.pb.Write("123456", true)
	var op *OpError
	if !errors.As(err, &op) || op.Backend != "pbcopy" || op.Op != "write" || op.Detail != "pasteboard unavailable" {
		t.Fatalf("err = %v, want pbcopy write *OpError", err)
	}
}

func TestPasteboardRead(t *testing.T) {
	f := newFakePasteboard(t, "", "", "printf 'abc\\n'")
	got, err := f.pb.Read()
	if err != nil || got != "abc\n" {
		t.Fatalf("Read() = %q, %v; want %q", got, err, "abc\n")
	}
	if calls := f.calls(t, "pbpaste"); len(calls) != 1 || len(calls[0].args) != 0 {
		t.Fatalf("pbpaste calls = %+v", calls)
	}
}

func TestPasteboardReadEmpty(t *testing.T) {
	f := newFakePasteboard(t, "", "", "")
	if got, err := f.pb.Read(); err != nil || got != "" {
		t.Fatalf("Read() = %q, %v; want empty", got, err)
	}
}

func TestPasteboardClearEmptiesThroughOsascript(t *testing.T) {
	f := newFakePasteboard(t, "", "", "")
	if err := f.pb.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	calls := f.calls(t, "osascript")
	if len(calls) != 1 || !slices.Equal(calls[0].args, []string{"-l", "JavaScript", "-e", clearScript}) {
		t.Fatalf("osascript calls = %+v", calls)
	}
	if !strings.Contains(clearScript, "clearContents") {
		t.Fatal("clear script does not call clearContents")
	}
	if n := len(f.calls(t, "pbcopy")); n != 0 {
		t.Fatalf("pbcopy ran %d times after osascript succeeded", n)
	}
}

func TestPasteboardClearFallsBackToPbcopy(t *testing.T) {
	f := newFakePasteboard(t, "exit 1", "", "")
	if err := f.pb.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if calls := f.calls(t, "pbcopy"); len(calls) != 1 || calls[0].stdin != "" {
		t.Fatalf("pbcopy calls = %+v, want one call with empty stdin", calls)
	}
}

func TestPlatformDetectDarwinUsesSystemTools(t *testing.T) {
	for _, forced := range []string{"", "x11", "osc52"} {
		c, err := platformDetect(env{getenv: fixedEnv(map[string]string{OverrideEnv: forced})})
		if err != nil {
			t.Fatalf("override %q: %v", forced, err)
		}
		want := pasteboard{osascript: "/usr/bin/osascript", pbcopy: "/usr/bin/pbcopy", pbpaste: "/usr/bin/pbpaste"}
		if c != want || c.Name() != "pbcopy" {
			t.Fatalf("override %q: got %#v", forced, c)
		}
	}
}
