//go:build unix

package clipboard

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type toolCall struct {
	args  []string
	stdin string
}

// fakeTool writes an executable script that records each call's argv and stdin
// under dir, then runs body.
func fakeTool(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	rec := shQuote(filepath.Join(dir, name+".call."))
	script := "#!/bin/sh\n" +
		"n=1\n" +
		"while [ -e " + rec + "\"$n.argv\" ]; do n=$((n+1)); done\n" +
		"cat > " + rec + "\"$n.stdin\"\n" +
		"printf '%s\\0' \"$#\" \"$@\" > " + rec + "\"$n.argv\"\n" +
		body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func toolCalls(t *testing.T, dir, name string) []toolCall {
	t.Helper()
	var calls []toolCall
	for n := 1; ; n++ {
		base := filepath.Join(dir, name+".call."+strconv.Itoa(n))
		argv, err := os.ReadFile(base + ".argv")
		if errors.Is(err, os.ErrNotExist) {
			return calls
		}
		if err != nil {
			t.Fatal(err)
		}
		stdin, err := os.ReadFile(base + ".stdin")
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Split(strings.TrimSuffix(string(argv), "\x00"), "\x00")
		if count, err := strconv.Atoi(fields[0]); err != nil || count != len(fields)-1 {
			t.Fatalf("%s call %d: malformed argv record %q", name, n, argv)
		}
		calls = append(calls, toolCall{args: fields[1:], stdin: string(stdin)})
	}
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func TestRunPassesArgsAndStdinAndCapturesStdout(t *testing.T) {
	dir := t.TempDir()
	tool := fakeTool(t, dir, "tool", "printf 'out'")
	var out bytes.Buffer
	if err := run("fake", "read", tool, []string{"-a", "b c"}, strings.NewReader("in"), &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "out" {
		t.Fatalf("stdout = %q, want %q", out.String(), "out")
	}
	calls := toolCalls(t, dir, "tool")
	if len(calls) != 1 || strings.Join(calls[0].args, "|") != "-a|b c" || calls[0].stdin != "in" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestRunReportsFailureWithFirstStderrLine(t *testing.T) {
	dir := t.TempDir()
	tool := fakeTool(t, dir, "tool", "printf '\\n  Error: no display\\nsecond\\n' >&2; exit 3")
	err := run("fake", "write", tool, nil, nil, nil)
	var op *OpError
	if !errors.As(err, &op) {
		t.Fatalf("err = %v, want *OpError", err)
	}
	if op.Backend != "fake" || op.Op != "write" || op.Detail != "Error: no display" {
		t.Fatalf("OpError = %+v", op)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("err does not unwrap to exit status 3: %v", err)
	}
	if got, want := err.Error(), "clipboard: fake write failed: Error: no display"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestRunReportsMissingTool(t *testing.T) {
	err := run("fake", "write", filepath.Join(t.TempDir(), "absent"), nil, nil, nil)
	var op *OpError
	if !errors.As(err, &op) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want *OpError wrapping ErrNotExist", err)
	}
}

func TestRunReturnsWhileForkedChildHoldsStderr(t *testing.T) {
	dir := t.TempDir()
	tool := fakeTool(t, dir, "tool", "(sleep 5) &\nexit 0")
	start := time.Now()
	if err := run("fake", "write", tool, nil, strings.NewReader("x"), nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("run took %s; it waited for the forked child", elapsed)
	}
}

func TestRunGivesUpOnHungTool(t *testing.T) {
	old := toolTimeout
	toolTimeout = 200 * time.Millisecond
	t.Cleanup(func() { toolTimeout = old })
	tool := fakeTool(t, t.TempDir(), "tool", "exec sleep 5")
	start := time.Now()
	err := run("fake", "write", tool, nil, nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("run took %s after the timeout", elapsed)
	}
}

func TestRunDropsPassphraseFromChildEnvironment(t *testing.T) {
	t.Setenv("TESSERA_PASSPHRASE", "hunter2")
	t.Setenv("TESSERA_CLIPBOARD_PROBE", "kept")
	tool := fakeTool(t, t.TempDir(), "tool", "env")
	var out bytes.Buffer
	if err := run("fake", "read", tool, nil, nil, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out.String(), "TESSERA_PASSPHRASE=") {
		t.Fatal("child environment carries TESSERA_PASSPHRASE")
	}
	if !strings.Contains(out.String(), "TESSERA_CLIPBOARD_PROBE=kept") {
		t.Fatal("child environment lost unrelated variables")
	}
}
