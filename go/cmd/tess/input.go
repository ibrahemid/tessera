package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/detect"
	"golang.org/x/term"
)

// stdinIsTTY reports whether stdin is a terminal, replaceable in tests.
var stdinIsTTY = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// promptHidden reads one line from the terminal without echo, replaceable in tests.
var promptHidden = readHiddenLine

// promptLine reads one visible line from stdin, replaceable in tests.
var promptLine = readVisibleLine

func readHiddenLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

func readVisibleLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// maxInputBytes caps every text, JSON, CSV or archive input read from a file or
// stdin, so a runaway pipe or a huge file cannot exhaust memory.
const maxInputBytes = 64 << 20

// errInputTooLarge reports an input refused by maxInputBytes.
var errInputTooLarge = errors.New("input is larger than 64 MiB")

// readCapped reads all of r, refusing more than maxInputBytes.
func readCapped(r io.Reader, source string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", source, err)
	}
	if len(data) > maxInputBytes {
		return nil, fmt.Errorf("%s: %w", source, errInputTooLarge)
	}
	return data, nil
}

// readFileCapped reads the file at path, refusing one over maxInputBytes before
// reading it.
func readFileCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info, serr := f.Stat(); serr == nil && info.Size() > maxInputBytes {
		return nil, fmt.Errorf("%s: %w", path, errInputTooLarge)
	}
	return readCapped(f, path)
}

// readProblem is the reason a file or stdin could not be read, as listed in an
// import report: the operating system's reason, or the size limit.
func readProblem(err error) string {
	var pathErr *os.PathError
	switch {
	case errors.Is(err, errInputTooLarge):
		return errInputTooLarge.Error()
	case errors.As(err, &pathErr):
		return pathErr.Err.Error()
	}
	return "could not read input"
}

// looksLikeSecret reports whether text is an otpauth or migration URI or a bare
// setup key: input that carries a secret and must never be echoed back.
func looksLikeSecret(text string) bool {
	switch detect.Classify(strings.TrimSpace(text)) {
	case detect.OTPAuth, detect.Migration, detect.SetupKey:
		return true
	}
	return false
}

// historyWarning is printed after a secret arrives as a command-line argument
// on an interactive shell.
const historyWarning = "The secret argument is now in your shell history; `tess add -` reads it from stdin instead."

// argumentNote is the help paragraph for commands that take secrets as arguments.
const argumentNote = `Command-line arguments are visible to other processes and saved in shell
history. Run tess add with no argument to type a secret without echo, or pipe
it to tess add -.`
