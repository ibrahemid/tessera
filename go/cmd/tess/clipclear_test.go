package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/clipboard"
)

// fakeClipboard is an in-memory clipboard backend.
type fakeClipboard struct {
	text      string
	sensitive bool
	writes    int
	clears    int
	readErr   error
	clearErr  error
}

func (f *fakeClipboard) Name() string { return "fake" }

func (f *fakeClipboard) Write(text string, sensitive bool) error {
	f.text, f.sensitive = text, sensitive
	f.writes++
	return nil
}

func (f *fakeClipboard) Read() (string, error) {
	if f.readErr != nil {
		return "", f.readErr
	}
	return f.text, nil
}

func (f *fakeClipboard) Clear() error {
	if f.clearErr != nil {
		return f.clearErr
	}
	f.text = ""
	f.clears++
	return nil
}

// unreadableClipboard behaves like OSC 52: it takes writes and nothing else.
func unreadableClipboard() *fakeClipboard {
	return &fakeClipboard{readErr: clipboard.ErrReadUnsupported, clearErr: clipboard.ErrClearUnsupported}
}

// detectFake returns a backend picker that always answers fake.
func detectFake(fake *fakeClipboard) func() (clipboard.Clipboard, error) {
	return func() (clipboard.Clipboard, error) { return fake, nil }
}

// stubBackend installs fake as the detected clipboard for one test.
func stubBackend(t *testing.T, fake *fakeClipboard) *fakeClipboard {
	t.Helper()
	old := detectClipboard
	detectClipboard = detectFake(fake)
	t.Cleanup(func() { detectClipboard = old })
	return fake
}

// stubClipboardPair installs a fake clipboard backed by a string the test owns.
func stubClipboardPair(t *testing.T, initial string) *string {
	t.Helper()
	held := initial
	oldRead, oldWrite, oldClear := readClipboard, copyToClipboard, clearClipboard
	readClipboard = func() (string, error) { return held, nil }
	copyToClipboard = func(s string) error { held = s; return nil }
	clearClipboard = func() error { held = ""; return nil }
	t.Cleanup(func() { readClipboard, copyToClipboard, clearClipboard = oldRead, oldWrite, oldClear })
	return &held
}

// recordClipClear captures every scheduled clear in place of the spawn.
func recordClipClear(t *testing.T) *[]clipClearCall {
	t.Helper()
	var calls []clipClearCall
	stubClipClear(t, func(seconds int, c string) error {
		calls = append(calls, clipClearCall{seconds, c})
		return nil
	})
	return &calls
}

type clipClearCall struct {
	seconds int
	code    string
}

// runClipClear runs the detached half the way tess spawns it: the delay in
// argv, the digest on stdin.
func runClipClear(t *testing.T, digest string, args ...string) error {
	t.Helper()
	old := sleepFn
	sleepFn = func(time.Duration) {}
	t.Cleanup(func() { sleepFn = old })

	c := newClipClearCmd()
	c.SetIn(strings.NewReader(digest + "\n"))
	c.SetOut(io.Discard)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(args)
	return c.Execute()
}

func TestClipClearWipesTheCodeItWasSpawnedFor(t *testing.T) {
	held := stubClipboardPair(t, "123456")

	if err := runClipClear(t, digestCode("123456"), "5"); err != nil {
		t.Fatalf("__clip-clear: %v", err)
	}
	if *held != "" {
		t.Errorf("clipboard = %q, want it cleared", *held)
	}
}

// TestClipClearLeavesALaterCopyAlone is the whole point of the digest: the user
// copied something else in the meantime and must not lose it.
func TestClipClearLeavesALaterCopyAlone(t *testing.T) {
	held := stubClipboardPair(t, "a password the user copied since")

	if err := runClipClear(t, digestCode("123456"), "5"); err != nil {
		t.Fatalf("__clip-clear: %v", err)
	}
	if *held != "a password the user copied since" {
		t.Errorf("clipboard = %q, want the user's own copy untouched", *held)
	}
}

func TestClipClearWaitsTheRequestedDelay(t *testing.T) {
	stubClipboardPair(t, "123456")
	var slept time.Duration
	old := sleepFn
	sleepFn = func(d time.Duration) { slept = d }
	t.Cleanup(func() { sleepFn = old })

	c := newClipClearCmd()
	c.SetIn(strings.NewReader(digestCode("123456") + "\n"))
	c.SetOut(io.Discard)
	c.SetArgs([]string{"12"})
	if err := c.Execute(); err != nil {
		t.Fatalf("__clip-clear: %v", err)
	}
	if slept != 12*time.Second {
		t.Errorf("slept %v, want 12s", slept)
	}
}

func TestClipClearReportsAnUnreadableClipboard(t *testing.T) {
	old := readClipboard
	readClipboard = func() (string, error) { return "", errors.New("no clipboard") }
	t.Cleanup(func() { readClipboard = old })

	if err := runClipClear(t, digestCode("123456"), "0"); err == nil {
		t.Fatal("an unreadable clipboard must not exit zero")
	}
}

func TestClipClearRejectsABadDelay(t *testing.T) {
	stubClipboardPair(t, "123456")
	if err := runClipClear(t, digestCode("123456"), "soon"); err == nil {
		t.Fatal("a non-numeric delay must be an error")
	}
}

// TestClipClearRefusesAMissingDigest: an empty stdin must not be read as
// permission to wipe whatever the clipboard happens to hold.
func TestClipClearRefusesAMissingDigest(t *testing.T) {
	held := stubClipboardPair(t, "123456")

	c := newClipClearCmd()
	c.SetIn(strings.NewReader(""))
	c.SetOut(io.Discard)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"0"})
	if err := c.Execute(); err == nil {
		t.Fatal("a missing digest must be an error")
	}
	if *held != "123456" {
		t.Errorf("clipboard = %q, want it untouched", *held)
	}
}

// TestClipClearTakesNoDigestArgument pins that the digest never reaches argv,
// where `ps` would hand a 6-digit code to anyone who can brute-force a sha256.
func TestClipClearTakesNoDigestArgument(t *testing.T) {
	stubClipboardPair(t, "123456")
	if err := runClipClear(t, digestCode("123456"), "0", digestCode("123456")); err == nil {
		t.Fatal("__clip-clear must reject a digest passed in argv")
	}
}

// TestClipClearIsHidden: it exists for tess to spawn, not for users to run.
func TestClipClearIsHidden(t *testing.T) {
	if !newClipClearCmd().Hidden {
		t.Error("__clip-clear should not appear in the help")
	}
}

// TestClipClearEmptiesTheBackend runs the detached half against a backend and
// pins that it clears through Clear, not by writing an empty string.
func TestClipClearEmptiesTheBackend(t *testing.T) {
	fake := stubBackend(t, &fakeClipboard{text: "123456"})

	if err := runClipClear(t, digestCode("123456"), "5"); err != nil {
		t.Fatalf("__clip-clear: %v", err)
	}
	if fake.clears != 1 || fake.writes != 0 {
		t.Errorf("clears = %d, writes = %d; want one Clear and no Write", fake.clears, fake.writes)
	}
	if fake.text != "" {
		t.Errorf("clipboard = %q, want it cleared", fake.text)
	}
}

func TestClipClearLeavesTheBackendAloneAfterALaterCopy(t *testing.T) {
	fake := stubBackend(t, &fakeClipboard{text: "a password the user copied since"})

	if err := runClipClear(t, digestCode("123456"), "5"); err != nil {
		t.Fatalf("__clip-clear: %v", err)
	}
	if fake.clears != 0 || fake.text != "a password the user copied since" {
		t.Errorf("clears = %d, clipboard = %q; want the user's copy untouched", fake.clears, fake.text)
	}
}

func TestClipClearReportsAFailedClear(t *testing.T) {
	stubBackend(t, &fakeClipboard{text: "123456", clearErr: errors.New("xsel exited 1")})

	err := runClipClear(t, digestCode("123456"), "0")
	if err == nil || !strings.Contains(err.Error(), "clear clipboard") {
		t.Fatalf("err = %v, want a clear failure", err)
	}
}

func TestCopyToClipboardMarksTheCodeSensitive(t *testing.T) {
	fake := stubBackend(t, &fakeClipboard{})

	if err := copyToClipboard("123456"); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if fake.text != "123456" || !fake.sensitive {
		t.Errorf("clipboard = %q (sensitive %v), want the code marked sensitive", fake.text, fake.sensitive)
	}
}

func TestClipboardSeamsReportAMissingClipboard(t *testing.T) {
	missing := &clipboard.NoClipboardError{Session: "x11", Missing: []string{"xclip", "xsel"}}
	old := detectClipboard
	detectClipboard = func() (clipboard.Clipboard, error) { return nil, missing }
	t.Cleanup(func() { detectClipboard = old })

	var target *clipboard.NoClipboardError
	if err := copyToClipboard("123456"); !errors.As(err, &target) {
		t.Errorf("copy: %v, want the NoClipboardError", err)
	}
	if _, err := readClipboard(); !errors.As(err, &target) {
		t.Errorf("read: %v, want the NoClipboardError", err)
	}
	if err := clearClipboard(); !errors.As(err, &target) {
		t.Errorf("clear: %v, want the NoClipboardError", err)
	}
}

func TestResolveClipClear(t *testing.T) {
	cases := []struct {
		name     string
		flag     int
		flagSet  bool
		env      string
		terminal bool
		want     clipClearPlan
		wantErr  bool
	}{
		{"terminal default", 0, false, "", true, clipClearPlan{seconds: 30}, false},
		{"blank env is unset", 0, false, "  ", true, clipClearPlan{seconds: 30}, false},
		{"no terminal, no setting", 0, false, "", false, clipClearPlan{}, false},
		{"flag wins over env", 12, true, "40", true, clipClearPlan{seconds: 12, explicit: true}, false},
		{"flag zero keeps the code", 0, true, "", true, clipClearPlan{explicit: true}, false},
		{"env sets the delay", 0, false, " 15 ", false, clipClearPlan{seconds: 15, explicit: true}, false},
		{"env zero keeps the code", 0, false, "0", true, clipClearPlan{explicit: true}, false},
		{"negative flag", -5, true, "", true, clipClearPlan{}, true},
		{"negative env", 0, false, "-5", true, clipClearPlan{}, true},
		{"words in env", 0, false, "soon", true, clipClearPlan{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(clipClearEnv, tc.env)
			got, err := resolveClipClear(tc.flag, tc.flagSet, tc.terminal)
			if tc.wantErr {
				var delayErr *clipClearDelayError
				if !errors.As(err, &delayErr) {
					t.Fatalf("err = %v, want a clipClearDelayError", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got != tc.want {
				t.Errorf("plan = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestClipClearDelayErrorNamesTheFix(t *testing.T) {
	t.Setenv(clipClearEnv, "soon")
	_, err := resolveClipClear(0, false, true)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{clipClearEnv, `"soon"`, "use 0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestScheduleSpawnsForAReadableClipboard(t *testing.T) {
	stubBackend(t, &fakeClipboard{text: "123456"})
	calls := recordClipClear(t)

	if err := (clipClearPlan{seconds: 30}).schedule("123456"); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0] != (clipClearCall{30, "123456"}) {
		t.Errorf("scheduled %+v, want one clear of the code after 30s", *calls)
	}
}

func TestScheduleZeroDoesNothing(t *testing.T) {
	fake := stubBackend(t, &fakeClipboard{readErr: errors.New("read must not run")})
	calls := recordClipClear(t)

	if err := (clipClearPlan{explicit: true}).schedule("123456"); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if len(*calls) != 0 || fake.clears != 0 {
		t.Errorf("scheduled %+v, want nothing", *calls)
	}
}

// TestScheduleNeverClearsAnUnreadableClipboard: without a read there is no way
// to tell the code from something copied since, so the clear is never spawned.
func TestScheduleNeverClearsAnUnreadableClipboard(t *testing.T) {
	stubBackend(t, unreadableClipboard())
	calls := recordClipClear(t)

	if err := (clipClearPlan{seconds: 30}).schedule("123456"); err != nil {
		t.Errorf("default delay: %v, want the clear dropped without an error", err)
	}
	err := (clipClearPlan{seconds: 30, explicit: true}).schedule("123456")
	if !errors.Is(err, errClipClearUnsupported) {
		t.Errorf("explicit delay: %v, want errClipClearUnsupported", err)
	}
	if len(*calls) != 0 {
		t.Errorf("scheduled %+v, want no spawn", *calls)
	}
}

func TestScheduleReportsAFailedSpawn(t *testing.T) {
	stubBackend(t, &fakeClipboard{text: "123456"})
	stubClipClear(t, func(int, string) error { return errors.New("fork failed") })

	err := (clipClearPlan{seconds: 30}).schedule("123456")
	if err == nil || !strings.Contains(err.Error(), "schedule clipboard clear") {
		t.Fatalf("err = %v, want the spawn failure", err)
	}
}

// stubStartProcess records each child instead of starting it, answering with
// the next error from results.
func stubStartProcess(t *testing.T, results ...error) *[]*exec.Cmd {
	t.Helper()
	var started []*exec.Cmd
	old := startProcess
	startProcess = func(c *exec.Cmd) error {
		started = append(started, c)
		if len(started) <= len(results) {
			return results[len(started)-1]
		}
		return nil
	}
	t.Cleanup(func() { startProcess = old })
	return &started
}

var (
	errBreakawayDenied = errors.New("breakaway denied")
	errOtherStart      = errors.New("no such file")
)

func isBreakawayDenied(err error) bool { return errors.Is(err, errBreakawayDenied) }

func TestStartDetachedFallsBackInOrder(t *testing.T) {
	first, second := &syscall.SysProcAttr{}, &syscall.SysProcAttr{}
	started := stubStartProcess(t, errBreakawayDenied)
	var built []*syscall.SysProcAttr
	build := func(attr *syscall.SysProcAttr) *exec.Cmd {
		built = append(built, attr)
		return &exec.Cmd{SysProcAttr: attr}
	}

	if err := startDetached(build, []*syscall.SysProcAttr{first, second}, isBreakawayDenied); err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(*started) != 2 || len(built) != 2 || built[0] != first || built[1] != second {
		t.Fatalf("tried %d children with %v, want the first then the second attribute set", len(*started), built)
	}
	if (*started)[0] == (*started)[1] {
		t.Error("each attempt must build a fresh child")
	}
}

func TestStartDetachedStopsOnTheFirstSuccess(t *testing.T) {
	started := stubStartProcess(t)
	build := func(attr *syscall.SysProcAttr) *exec.Cmd { return &exec.Cmd{SysProcAttr: attr} }

	if err := startDetached(build, []*syscall.SysProcAttr{{}, {}}, isBreakawayDenied); err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(*started) != 1 {
		t.Errorf("started %d children, want 1", len(*started))
	}
}

func TestStartDetachedFallsBackOnlyOnARetryableError(t *testing.T) {
	started := stubStartProcess(t, errOtherStart)
	build := func(attr *syscall.SysProcAttr) *exec.Cmd { return &exec.Cmd{SysProcAttr: attr} }

	err := startDetached(build, []*syscall.SysProcAttr{{}, {}}, isBreakawayDenied)
	if !errors.Is(err, errOtherStart) {
		t.Errorf("err = %v, want the first failure", err)
	}
	if len(*started) != 1 {
		t.Errorf("started %d children, want no fallback", len(*started))
	}
}

func TestStartDetachedReportsTheLastFailure(t *testing.T) {
	stubStartProcess(t, errBreakawayDenied, errOtherStart)
	build := func(attr *syscall.SysProcAttr) *exec.Cmd { return &exec.Cmd{SysProcAttr: attr} }

	err := startDetached(build, []*syscall.SysProcAttr{{}, {}}, isBreakawayDenied)
	if !errors.Is(err, errOtherStart) {
		t.Errorf("err = %v, want the last attempt's failure", err)
	}
}

func TestStartDetachedWithNoAttributesStartsOnce(t *testing.T) {
	started := stubStartProcess(t)
	build := func(attr *syscall.SysProcAttr) *exec.Cmd { return &exec.Cmd{SysProcAttr: attr} }

	if err := startDetached(build, nil, isBreakawayDenied); err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(*started) != 1 || (*started)[0].SysProcAttr != nil {
		t.Errorf("started %d children, want one with default attributes", len(*started))
	}
}

// TestSpawnClipClearHandsTheDigestOverStdin builds the real child without
// starting it: the delay in argv, the digest on stdin, no passphrase in the
// environment, and the platform's detached attributes.
func TestSpawnClipClearHandsTheDigestOverStdin(t *testing.T) {
	t.Setenv("TESSERA_PASSPHRASE", "testpass123")
	var args, env []string
	var line string
	var attr *syscall.SysProcAttr
	old := startProcess
	startProcess = func(c *exec.Cmd) error {
		args, env, attr = c.Args, c.Env, c.SysProcAttr
		f, ok := c.Stdin.(*os.File)
		if !ok {
			t.Fatalf("stdin is %T, want the digest pipe", c.Stdin)
		}
		var err error
		line, err = bufio.NewReader(f).ReadString('\n')
		if err != nil {
			t.Fatalf("read digest pipe: %v", err)
		}
		return nil
	}
	t.Cleanup(func() { startProcess = old })

	if err := spawnClipClear(30, "123456"); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if len(args) != 3 || args[1] != "__clip-clear" || args[2] != "30" {
		t.Errorf("argv = %q, want [tess __clip-clear 30]", args)
	}
	for _, a := range args {
		if strings.Contains(a, "123456") || strings.Contains(a, digestCode("123456")) {
			t.Errorf("argv carries the code or its digest: %q", args)
		}
	}
	if line != digestCode("123456")+"\n" {
		t.Errorf("stdin = %q, want the digest line", line)
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, "TESSERA_PASSPHRASE=") {
			t.Error("the child inherited the passphrase")
		}
	}
	if want := detachedProcAttrs()[0]; !reflect.DeepEqual(attr, want) {
		t.Errorf("SysProcAttr = %+v, want %+v", attr, want)
	}
}
