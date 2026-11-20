package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/clipboard"
	"github.com/ibrahemid/tessera/go/internal/code"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// detectClipboard picks the clipboard backend, replaceable in tests.
var detectClipboard = clipboard.Detect

// copyToClipboard is the clipboard writer, replaceable in tests. Codes are
// written as sensitive, which keeps them out of clipboard history where the
// platform allows.
var copyToClipboard = func(text string) error {
	b, err := detectClipboard()
	if err != nil {
		return err
	}
	return b.Write(text, true)
}

// readClipboard is the clipboard reader, replaceable in tests.
var readClipboard = func() (string, error) {
	b, err := detectClipboard()
	if err != nil {
		return "", err
	}
	return b.Read()
}

// stdoutIsTTY reports whether stdout is a terminal, replaceable in tests.
var stdoutIsTTY = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

// sleepFn is the clock wait used by the freshness guard and the clipboard
// clear, replaceable in tests.
var sleepFn = time.Sleep

// freshnessFloor is the seconds-remaining threshold under which a code is too
// close to rolling over: pasting it usually loses the race with the server.
const freshnessFloor = 4

// codeOptions holds the flags shared by `tess code` and the bare `tess <query>`
// shortcut, which run the same code path.
type codeOptions struct {
	next       bool
	noCopy     bool
	forceCopy  bool
	asJSON     bool
	nowOnly    bool
	all        bool
	at         string
	clearAfter int
}

var (
	errNoCopyAndCopy = errors.New("--no-copy and -c ask for opposite things; drop one")
	errAllWithQuery  = errors.New("--all prints every account; drop the query or drop --all")
	errAllWithNext   = errors.New("--all never advances a counter; advance one with `tess code <query> --next`")
	errAllWithCopy   = errors.New("--all never copies; copy one code with `tess code <query> -c`")
	errAtNoQuery     = errors.New("--at needs an account: `tess code <query> --at <time>`")
	errAtWithNext    = errors.New("--at already picks the time; drop --next")
	errAtWithCopy    = errors.New("--at never copies; drop -c")
	errAtTime        = errors.New("--at takes an RFC 3339 time such as 2026-01-02T15:04:05Z, or unix seconds")
	errAtCounter     = errors.New("--at works only for time-based accounts, and this one counts codes (HOTP)")
)

// validate rejects flag combinations that ask for something the run would not do.
func (o codeOptions) validate(hasQuery bool) error {
	switch {
	case o.noCopy && o.forceCopy:
		return errNoCopyAndCopy
	case o.all && hasQuery:
		return errAllWithQuery
	case o.all && o.next:
		return errAllWithNext
	case o.all && o.forceCopy:
		return errAllWithCopy
	case o.at != "" && !hasQuery:
		return errAtNoQuery
	case o.at != "" && o.next:
		return errAtWithNext
	case o.at != "" && o.forceCopy:
		return errAtWithCopy
	}
	return nil
}

// shouldCopy reports whether this run puts the code on the clipboard.
// --no-copy never copies and -c always copies; left to itself, only an
// interactive run that did not ask for JSON copies. A piped run is a script's,
// and a script that wants the clipboard says so with -c.
func (o codeOptions) shouldCopy() bool {
	if o.noCopy {
		return false
	}
	if o.forceCopy {
		return true
	}
	return !o.asJSON && stdoutIsTTY()
}

// clipClear resolves when this run's copy leaves the clipboard. The terminal
// default covers a run that prints for a person, which --json does not.
func (o codeOptions) clipClear(cmd *cobra.Command) (clipClearPlan, error) {
	return resolveClipClear(o.clearAfter, cmd.Flags().Changed("clear"), !o.asJSON && stdoutIsTTY())
}

func newCodeCmd() *cobra.Command {
	opts := &codeOptions{}
	cmd := &cobra.Command{
		Use:   "code [query]",
		Short: "Print the current code for a matching account (or all if omitted)",
		Long: `Print one account's code and copy it to the clipboard:

  tess code acme            # print and copy
  tess code acme --no-copy  # print only
  tess code acme --json     # machine-readable, never copies
  tess code --all           # every account's code; same as tess code
  tess code acme --at 2026-01-02T15:04:05Z
                            # the code at another time, never copies

The code is copied on a terminal. Piped or with --json, tess prints the raw
digits and leaves the clipboard alone unless you pass -c.

On a terminal, tess clears the copied code after 30 seconds if the clipboard
still holds it. --clear sets the delay; --clear 0 or TESSERA_CLIP_CLEAR=0
keeps the code.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeFirstArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCode(cmd, args, opts)
		},
	}
	bindCodeFlags(cmd, opts)
	return cmd
}

// bindCodeFlags registers the code flags on cmd. Both `tess code` and the root
// command carry the set, so `tess acme` and `tess code acme` accept the same
// options.
func bindCodeFlags(cmd *cobra.Command, opts *codeOptions) {
	f := cmd.Flags()
	f.BoolVar(&opts.next, "next", false,
		"for TOTP and Steam, the code of the following time step; for HOTP, advance and persist the counter")
	f.BoolVar(&opts.noCopy, "no-copy", false, "print the code without touching the clipboard")
	f.BoolVar(&opts.asJSON, "json", false, "output JSON (never copies)")
	f.BoolVar(&opts.nowOnly, "now", false, "print the current code instead of waiting for the next one")
	f.BoolVar(&opts.all, "all", false, "print the code of every account; never copies or advances a counter")
	f.StringVar(&opts.at, "at", "",
		"print the code at this time, RFC 3339 or unix seconds (time-based accounts; never copies)")
	f.IntVar(&opts.clearAfter, "clear", 0,
		"clear the clipboard after this many seconds, 0 to keep (default $TESSERA_CLIP_CLEAR, else 30 on a terminal)")
	f.BoolVarP(&opts.forceCopy, "copy", "c", false,
		"copy the code to the clipboard (the default on a terminal; -c copies even when piped)")
}

func runCode(cmd *cobra.Command, args []string, opts *codeOptions) error {
	if err := opts.validate(len(args) > 0); err != nil {
		return err
	}
	if len(args) == 0 {
		return runCodeAll(cmd, opts.asJSON)
	}
	var at time.Time
	if opts.at != "" {
		var err error
		if at, err = parseCodeTime(opts.at); err != nil {
			return err
		}
	}
	s, err := openSession()
	if err != nil {
		return err
	}
	defer s.close()
	if opts.at != "" {
		return printCodeAt(cmd, s, args[0], at, opts.asJSON)
	}
	idxs, err := s.resolveMatches(args[0])
	if err != nil {
		return err
	}
	if len(idxs) > 1 {
		return printAmbiguousCodes(cmd, s.accounts, idxs, args[0], opts)
	}
	return printOneCode(cmd, s, idxs[0], opts)
}

// printAmbiguousCodes reports every match rather than guessing which account
// the user meant. There is no single code to copy, so this path leaves the
// clipboard alone. With -c the copy that was asked for cannot happen, so the
// table goes to stderr and the command exits 2. Otherwise, on a terminal or
// with --json it exits zero: the listed codes answer the request. Off a
// terminal a script expected one code on stdout, so the table goes to stderr
// and the command exits 2.
func printAmbiguousCodes(cmd *cobra.Command, accts []account.Account, idxs []int, query string, opts *codeOptions) error {
	if opts.forceCopy {
		printMatchTableTo(cmd.ErrOrStderr(), accts, idxs, true)
		return errAmbiguous(len(idxs), query)
	}
	if opts.asJSON {
		matched := make([]account.Account, 0, len(idxs))
		for _, i := range idxs {
			matched = append(matched, accts[i])
		}
		rows, err := codeRows(matched, now())
		if err != nil {
			return err
		}
		out(cmd, "%s", mustJSON(rows))
		return nil
	}
	if !stdoutIsTTY() {
		printMatchTableTo(cmd.ErrOrStderr(), accts, idxs, true)
		return errAmbiguous(len(idxs), query)
	}
	printMatchTable(cmd, accts, idxs, true)
	return nil
}

// printOneCode generates, prints and (by default) copies a single account's code.
// With --next a time-based account's printed and copied code is the following
// step's, without the freshness wait; the JSON row stays the current one.
func printOneCode(cmd *cobra.Command, s *session, idx int, opts *codeOptions) error {
	willCopy := opts.shouldCopy()
	var wipe clipClearPlan
	if willCopy {
		var err error
		if wipe, err = opts.clipClear(cmd); err != nil {
			return err
		}
	}
	a := &s.accounts[idx]
	upcoming := opts.next && a.Type != account.HOTP
	if a.Type == account.HOTP && opts.next {
		next, err := account.NextCounter(a.Counter)
		if err != nil {
			return fmt.Errorf("cannot advance the counter of %s (%w); `tess edit %s --counter N` sets a new one",
				label(*a), err, displayHandle(*a))
		}
		a.Counter = next
		a.UpdatedAt = now().Unix()
	}
	t := now()
	if !opts.asJSON && !opts.nowOnly && !upcoming {
		t = waitForFreshCode(cmd, *a, t)
	}
	at := t
	if upcoming {
		next, ok := stepStart(t, a.Period, 1)
		if !ok {
			return fmt.Errorf("%s has no next code: its period runs past the last time Tessera can count to", label(*a))
		}
		at = next
	}
	c, err := genCode(*a, at)
	if err != nil {
		return err
	}
	if opts.asJSON {
		row, err := newCodeRow(*a, t)
		if err != nil {
			return err
		}
		out(cmd, "%s", mustJSON(row))
	} else {
		printCodeLine(cmd, c, code.Remaining(*a, t), a.Type, willCopy, upcoming)
	}
	// Persist the advanced counter before reporting the copy: a failed
	// clipboard must not roll back a counter the user already saw.
	if a.Type == account.HOTP && opts.next {
		if err := s.save(); err != nil {
			return err
		}
	}
	if !willCopy {
		return nil
	}
	if err := copyToClipboard(c); err != nil {
		return fmt.Errorf("copy to clipboard: %w", err)
	}
	if err := wipe.schedule(c); err != nil {
		if errors.Is(err, errClipClearUnsupported) {
			errOut(cmd, "warning: %s", err)
			return nil
		}
		return err
	}
	return nil
}

// parseCodeTime reads an --at value: unix seconds, or an RFC 3339 time.
func parseCodeTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n < 0 {
			return time.Time{}, errAtTime
		}
		return time.Unix(n, 0), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil || t.Unix() < 0 {
		return time.Time{}, errAtTime
	}
	return t, nil
}

// printCodeAt prints one time-based account's code at t. It never copies and
// never waits for a fresh code: t is the time asked for, not now.
func printCodeAt(cmd *cobra.Command, s *session, query string, t time.Time, asJSON bool) error {
	idx, err := s.single(cmd, query)
	if err != nil {
		return err
	}
	a := s.accounts[idx]
	if a.Type == account.HOTP {
		return fmt.Errorf("%s: %w", label(a), errAtCounter)
	}
	c, err := genCode(a, t)
	if err != nil {
		return err
	}
	switch {
	case asJSON:
		row, err := newCodeRow(a, t)
		if err != nil {
			return err
		}
		out(cmd, "%s", mustJSON(row))
	case stdoutIsTTY():
		out(cmd, "%s", ui.CodeStyle.Render(ui.GroupCode(c)))
	default:
		out(cmd, "%s", c)
	}
	return nil
}

// printCodeLine renders the code for a human on a terminal, and the bare digits
// for anything else so `tess code x | pbcopy` and `$(tess code x)` stay usable.
// An upcoming code is the following time step's, valid once remaining runs out.
func printCodeLine(cmd *cobra.Command, c string, remaining int, typ account.Type, copied, upcoming bool) {
	if !stdoutIsTTY() {
		out(cmd, "%s", c)
		return
	}
	var meta []string
	if copied {
		meta = append(meta, "copied")
	}
	switch {
	case typ == account.HOTP:
	case upcoming:
		meta = append(meta, fmt.Sprintf("valid in %ds", remaining))
	default:
		meta = append(meta, fmt.Sprintf("%ds", remaining))
	}
	if len(meta) == 0 {
		out(cmd, "%s", ui.CodeStyle.Render(ui.GroupCode(c)))
		return
	}
	out(cmd, "%s   %s", ui.CodeStyle.Render(ui.GroupCode(c)),
		ui.SubtleStyle.Render(strings.Join(meta, " · ")))
}

// waitForFreshCode holds back a code that is about to roll over and returns the
// time to generate at. A code handed out with a second left is one the user
// pastes after the server has already retired it.
func waitForFreshCode(cmd *cobra.Command, a account.Account, t time.Time) time.Time {
	if a.Type == account.HOTP || a.Period <= 0 {
		return t
	}
	rem := code.Remaining(a, t)
	if rem >= freshnessFloor {
		return t
	}
	errOut(cmd, "waiting %ds for a fresh code", rem)
	sleepFn(time.Duration(rem) * time.Second)
	return now()
}

// codeRow is the JSON shape of one account's code; docs/CLI-JSON.md is its
// contract. NextCode and Expires apply to time-based accounts only.
type codeRow struct {
	ID       string `json:"id"`
	Handle   string `json:"handle"`
	Issuer   string `json:"issuer"`
	Account  string `json:"account"`
	Type     string `json:"type"`
	Code     string `json:"code"`
	NextCode string `json:"next_code,omitempty"`
	Expires  int    `json:"expires_in,omitempty"`
}

// newCodeRow is a's code at t; an HOTP account's is the code at its stored
// counter. A time-based row carries the following step's code when that step
// starts within int64 seconds.
func newCodeRow(a account.Account, t time.Time) (codeRow, error) {
	c, err := genCode(a, t)
	if err != nil {
		return codeRow{}, fmt.Errorf("%s: %w", label(a), err)
	}
	row := codeRow{ID: a.ID, Handle: a.Handle, Issuer: a.Issuer, Account: a.Account, Type: string(a.Type), Code: c}
	if a.Type != account.HOTP {
		if next, ok := stepStart(t, a.Period, 1); ok {
			if row.NextCode, err = genCode(a, next); err != nil {
				return codeRow{}, fmt.Errorf("%s: %w", label(a), err)
			}
		}
		row.Expires = code.Remaining(a, t)
	}
	return row, nil
}

func codeRows(accts []account.Account, t time.Time) ([]codeRow, error) {
	rows := make([]codeRow, 0, len(accts))
	for _, a := range accts {
		row, err := newCodeRow(a, t)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// stepStart is the first second of the time step offset steps from the one t
// falls in, computed in whole seconds; false when that step starts before the
// epoch or after the largest int64 second.
func stepStart(t time.Time, period int, offset int64) (time.Time, bool) {
	p := int64(period)
	if p <= 0 {
		return time.Time{}, false
	}
	cur := t.Unix() / p
	if (offset > 0 && cur > math.MaxInt64-offset) || (offset < 0 && cur < math.MinInt64-offset) {
		return time.Time{}, false
	}
	n := cur + offset
	if n < 0 || n > math.MaxInt64/p {
		return time.Time{}, false
	}
	return time.Unix(n*p, 0), true
}

func runCodeAll(cmd *cobra.Command, asJSON bool) error {
	s, err := openSession()
	if err != nil {
		return err
	}
	defer s.close()
	accts := filterAccounts(s.accounts, "", "")
	if asJSON {
		rows, err := codeRows(accts, now())
		if err != nil {
			return err
		}
		out(cmd, "%s", mustJSON(rows))
		return nil
	}
	if len(accts) == 0 {
		out(cmd, "%s", ui.SubtleStyle.Render("No accounts. Add one with `tess add`."))
		return nil
	}
	t := now()
	hw := handleWidth(accts)
	for _, a := range accts {
		tag := ui.Handle(a.Issuer+a.Account, padRight(displayHandle(a), hw))
		name := ui.IssuerStyle.Render(padRight(labelText(a), 26))
		if a.Type == account.HOTP {
			meta := ui.SubtleStyle.Render(fmt.Sprintf("hotp #%d  (tess code %s --next)", a.Counter, displayHandle(a)))
			out(cmd, "%s  %s  %s  %s", tag, name, ui.CodeStyle.Render(padRight("------", 9)), meta)
			continue
		}
		c, err := genCode(a, t)
		if err != nil {
			out(cmd, "%s  %s  %s", tag, name, ui.WarnStyle.Render(ui.Sanitize(userMessage(err))))
			continue
		}
		rem := code.Remaining(a, t)
		bar := ui.Bar(rem, a.Period, 10)
		out(cmd, "%s  %s  %s  %s %s", tag, name,
			ui.CodeStyle.Render(padRight(ui.GroupCode(c), 9)), bar,
			ui.SubtleStyle.Render(fmt.Sprintf("%2ds", rem)))
	}
	return nil
}

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}
