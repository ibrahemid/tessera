package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/code"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// copyToClipboard is the clipboard writer, replaceable in tests.
var copyToClipboard = clipboard.WriteAll

// readClipboard is the clipboard reader, replaceable in tests.
var readClipboard = clipboard.ReadAll

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
	clearAfter int
}

// shouldCopy reports whether this run puts the code on the clipboard.
// --no-copy always wins and -c always copies; left to itself, only an
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

func newCodeCmd() *cobra.Command {
	opts := &codeOptions{}
	cmd := &cobra.Command{
		Use:   "code [query]",
		Short: "Print the current code for a matching account (or all if omitted)",
		Long: `Print one account's code and copy it to the clipboard:

  tess code acme            # print and copy
  tess code acme --no-copy  # print only
  tess code acme --json     # machine-readable, never copies
  tess code                 # every account's code

The code is copied on a terminal. Piped or with --json, tess prints the raw
digits and leaves the clipboard alone unless you pass -c.`,
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
	f.BoolVar(&opts.next, "next", false, "for HOTP, advance and persist the counter")
	f.BoolVar(&opts.noCopy, "no-copy", false, "print the code without touching the clipboard")
	f.BoolVar(&opts.asJSON, "json", false, "output JSON (never copies)")
	f.BoolVar(&opts.nowOnly, "now", false, "print the current code instead of waiting for the next one")
	f.IntVar(&opts.clearAfter, "clear", defaultClipClear(),
		"clear the clipboard after this many seconds, 0 to keep (default $TESSERA_CLIP_CLEAR)")
	f.BoolVarP(&opts.forceCopy, "copy", "c", false,
		"copy the code to the clipboard (the default on a terminal; -c copies even when piped)")
	_ = f.MarkHidden("copy")
}

// defaultClipClear reads the --clear default from $TESSERA_CLIP_CLEAR. Anything
// that is not a positive integer leaves the clipboard untouched.
func defaultClipClear() int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("TESSERA_CLIP_CLEAR")))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func runCode(cmd *cobra.Command, args []string, opts *codeOptions) error {
	if len(args) == 0 {
		return runCodeAll(cmd, opts.asJSON)
	}
	s, err := openSession()
	if err != nil {
		return err
	}
	defer s.close()
	idxs, err := s.resolveMatches(args[0])
	if err != nil {
		return err
	}
	if len(idxs) > 1 {
		return printAmbiguousCodes(cmd, s.accounts, idxs, opts.asJSON)
	}
	return printOneCode(cmd, s, idxs[0], opts)
}

// printAmbiguousCodes reports every match rather than guessing which account
// the user meant. There is no single code to copy, so this path leaves the
// clipboard alone and exits zero: the listed codes answer the request.
func printAmbiguousCodes(cmd *cobra.Command, accts []account.Account, idxs []int, asJSON bool) error {
	if asJSON {
		matched := make([]account.Account, 0, len(idxs))
		for _, i := range idxs {
			matched = append(matched, accts[i])
		}
		out(cmd, "%s", mustJSON(codeRows(matched, now())))
		return nil
	}
	printMatchTable(cmd, accts, idxs, true)
	return nil
}

// printOneCode generates, prints and (by default) copies a single account's code.
func printOneCode(cmd *cobra.Command, s *session, idx int, opts *codeOptions) error {
	a := &s.accounts[idx]
	if a.Type == account.HOTP && opts.next {
		a.Counter++
		a.UpdatedAt = now().Unix()
	}
	t := now()
	if !opts.asJSON && !opts.nowOnly {
		t = waitForFreshCode(cmd, *a, t)
	}
	c, err := genCode(*a, t)
	if err != nil {
		return err
	}
	willCopy := opts.shouldCopy()
	if opts.asJSON {
		fields := map[string]any{
			"issuer": a.Issuer, "account": a.Account, "code": c, "type": string(a.Type),
		}
		if a.Type != account.HOTP {
			fields["expires_in"] = code.Remaining(*a, t)
		}
		out(cmd, "%s", mustJSON(fields))
	} else {
		printCodeLine(cmd, c, code.Remaining(*a, t), a.Type, willCopy)
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
	if opts.clearAfter > 0 {
		if err := startClipClear(opts.clearAfter, c); err != nil {
			return fmt.Errorf("schedule clipboard clear: %w", err)
		}
	}
	return nil
}

// printCodeLine renders the code for a human on a terminal, and the bare digits
// for anything else so `tess code x | pbcopy` and `$(tess code x)` stay usable.
func printCodeLine(cmd *cobra.Command, c string, remaining int, typ account.Type, copied bool) {
	if !stdoutIsTTY() {
		out(cmd, "%s", c)
		return
	}
	var meta []string
	if copied {
		meta = append(meta, "copied")
	}
	if typ != account.HOTP {
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

// codeRow is the JSON shape of one account's current code.
type codeRow struct {
	Issuer  string `json:"issuer"`
	Account string `json:"account"`
	Type    string `json:"type"`
	Code    string `json:"code"`
	Expires int    `json:"expires_in,omitempty"`
}

func codeRows(accts []account.Account, t time.Time) []codeRow {
	rows := make([]codeRow, 0, len(accts))
	for _, a := range accts {
		c, _ := genCode(a, t)
		rows = append(rows, codeRow{a.Issuer, a.Account, string(a.Type), c, code.Remaining(a, t)})
	}
	return rows
}

func runCodeAll(cmd *cobra.Command, asJSON bool) error {
	s, err := openSession()
	if err != nil {
		return err
	}
	defer s.close()
	accts := filterAccounts(s.accounts, "", "")
	if asJSON {
		out(cmd, "%s", mustJSON(codeRows(accts, now())))
		return nil
	}
	if len(accts) == 0 {
		out(cmd, "%s", ui.SubtleStyle.Render("No accounts. Add one with `tess add`."))
		return nil
	}
	t := now()
	hw := handleWidth(accts)
	for _, a := range accts {
		tag := ui.Handle(a.Issuer+a.Account, padRight(a.Handle, hw))
		name := ui.IssuerStyle.Render(padRight(labelText(a), 26))
		if a.Type == account.HOTP {
			meta := ui.SubtleStyle.Render(fmt.Sprintf("hotp #%d  (tess code %s --next)", a.Counter, a.Handle))
			out(cmd, "%s  %s  %s  %s", tag, name, ui.CodeStyle.Render(padRight("------", 9)), meta)
			continue
		}
		c, err := genCode(a, t)
		if err != nil {
			out(cmd, "%s  %s  %s", tag, name, ui.WarnStyle.Render(err.Error()))
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
