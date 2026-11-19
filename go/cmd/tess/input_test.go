package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubStdinTTY forces the stdin-is-a-terminal answer for one test.
func stubStdinTTY(t *testing.T, isTTY bool) {
	t.Helper()
	old := stdinIsTTY
	stdinIsTTY = func() bool { return isTTY }
	t.Cleanup(func() { stdinIsTTY = old })
}

// stubPrompts answers hidden and visible prompts from fixed lists and records
// every prompt shown.
func stubPrompts(t *testing.T, hidden, visible []string) *[]string {
	t.Helper()
	var shown []string
	oldHidden, oldLine := promptHidden, promptLine
	next := func(answers *[]string, prompt string) (string, error) {
		shown = append(shown, prompt)
		if len(*answers) == 0 {
			return "", io.EOF
		}
		a := (*answers)[0]
		*answers = (*answers)[1:]
		return a, nil
	}
	promptHidden = func(p string) (string, error) { return next(&hidden, p) }
	promptLine = func(p string) (string, error) { return next(&visible, p) }
	t.Cleanup(func() { promptHidden, promptLine = oldHidden, oldLine })
	return &shown
}

type endlessReader struct{}

func (endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'A'
	}
	return len(p), nil
}

func TestReadCappedRefusesAnOversizedReader(t *testing.T) {
	_, err := readCapped(endlessReader{}, "stdin")
	if !errors.Is(err, errInputTooLarge) {
		t.Fatalf("got %v, want errInputTooLarge", err)
	}
	if !strings.Contains(err.Error(), "64 MiB") {
		t.Errorf("the error should state the limit: %v", err)
	}
}

func TestReadCappedAcceptsInputAtTheLimit(t *testing.T) {
	data, err := readCapped(io.LimitReader(endlessReader{}, maxInputBytes), "stdin")
	if err != nil {
		t.Fatalf("input of exactly the limit must be read: %v", err)
	}
	if len(data) != maxInputBytes {
		t.Fatalf("read %d bytes, want %d", len(data), maxInputBytes)
	}
}

func TestReadFileCappedRefusesAnOversizedFileBeforeReading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxInputBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := readFileCapped(path); !errors.Is(err, errInputTooLarge) {
		t.Fatalf("got %v, want errInputTooLarge", err)
	}
}

func TestImportReportsAnOversizedStdin(t *testing.T) {
	old := stdinReader
	stdinReader = endlessReader{}
	t.Cleanup(func() { stdinReader = old })
	b := collectImport(nil, []string{"-"}, nil, nil, nil)
	if len(b.problems) != 1 || b.problems[0].reason != errInputTooLarge.Error() {
		t.Fatalf("want one size problem, got %+v", b.problems)
	}
}

func TestImportNamesTheOperatingSystemReason(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.txt")
	b := collectImport(nil, []string{missing}, nil, nil, nil)
	if len(b.problems) != 1 || b.problems[0].reason != "no such file or directory" {
		t.Fatalf("want the OS reason, got %+v", b.problems)
	}
	b = collectImport([]string{filepath.Join(t.TempDir(), "missing.png")}, nil, nil, nil, nil)
	if len(b.problems) != 1 || b.problems[0].reason != "no such file or directory" {
		t.Fatalf("want the OS reason for an image, got %+v", b.problems)
	}
}

func TestReadProblemMapsPathErrorsToFixedWording(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"missing", &os.PathError{Op: "open", Path: "x", Err: fs.ErrNotExist}, "no such file or directory"},
		{"denied", &os.PathError{Op: "open", Path: "x", Err: fs.ErrPermission}, "permission denied"},
		{"other", &os.PathError{Op: "open", Path: "x", Err: errors.New("device busy")}, "device busy"},
		{"unknown", errors.New("boom"), "could not read input"},
	}
	for _, c := range cases {
		if got := readProblem(c.err); got != c.want {
			t.Errorf("%s: readProblem = %q, want %q", c.name, got, c.want)
		}
	}
}
