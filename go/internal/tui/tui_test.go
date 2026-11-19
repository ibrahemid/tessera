package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/ui"
)

func checkFrame(t *testing.T, h *harness, width, height int, step string) {
	t.Helper()
	lines := strings.Split(h.m.View().Content, "\n")
	if len(lines) != height {
		t.Fatalf("%s: %d lines, want %d", step, len(lines), height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > width {
			t.Fatalf("%s: line %d is %d cells wide, over %d: %q", step, i, w, width, ansi.Strip(l))
		}
	}
	if !strings.HasPrefix(ansi.Strip(lines[0]), "Tessera") {
		t.Fatalf("%s: header is off screen: %q", step, ansi.Strip(lines[0]))
	}
}

func selectedLine(h *harness) string {
	for _, l := range h.lines() {
		if strings.HasPrefix(l, "▸") {
			return l
		}
	}
	return ""
}

func TestOrderIsPinnedFirstThenDisplayOrder(t *testing.T) {
	h := newHarness(t, fixture())
	if got, want := h.rowIDs(), []string{"a", "b", "c", "e", "d", "f"}; !slices.Equal(got, want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
	if h.selectedID() != "a" {
		t.Fatalf("starts on %s, want the first row", h.selectedID())
	}

	x := totpAcct("x", "Zeta", "me", "x")
	x.Order = 2
	y := totpAcct("y", "Yak", "me", "y")
	y.Order = 1
	z := totpAcct("z", "Omega", "me", "z")
	z.Pinned = true
	w := totpAcct("w", "Alpha", "me", "w")
	h = newHarness(t, []account.Account{w, x, y, z})
	if got, want := h.rowIDs(), []string{"z", "y", "x", "w"}; !slices.Equal(got, want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
}

func TestHeaderCounts(t *testing.T) {
	h := newHarness(t, fixture())
	if got := h.lines()[0]; got != "Tessera  6 accounts  1 in the Trash" {
		t.Fatalf("header %q", got)
	}
	h.press("/")
	h.typeText("work")
	if got := h.lines()[0]; got != "Tessera  2 of 6 accounts  1 in the Trash" {
		t.Fatalf("filtered header %q", got)
	}
	h.press("esc", "h")
	if got := h.lines()[0]; got != "Tessera  6 accounts  1 in the Trash  Codes hidden" {
		t.Fatalf("hidden header %q", got)
	}

	h = newSizedHarness(t, fixture(), 40, 12)
	h.press("h")
	if l := h.lines(); l[0] != "Tessera  6 accounts  1 in the Trash" || l[1] != "Codes hidden" {
		t.Fatalf("header at 40 columns does not wrap: %q", l[:2])
	}

	h = newHarness(t, many(1))
	if got := h.lines()[0]; got != "Tessera  1 account" {
		t.Fatalf("header %q", got)
	}
}

func TestEmptyStates(t *testing.T) {
	hintLine := func(h *harness) string { l := h.lines(); return l[len(l)-1] }
	h := newHarness(t, nil)
	h.mustContain("Tessera  0 accounts", "No accounts yet. Press a to add one.")
	if got := hintLine(h); got != "a add  ? keys  q quit" {
		t.Fatalf("empty vault hints %q", got)
	}
	h.press("enter", "n", "e", "p", "d")
	if len(h.changes) != 0 || len(h.copied) != 0 || h.m.mode != modeList {
		t.Fatal("keys on an empty list did something")
	}

	h = newHarness(t, fixture())
	h.press("/")
	h.typeText("zzz")
	h.mustContain("0 of 6 accounts", "Nothing matches.")
	if got := hintLine(h); got != "esc clear" {
		t.Fatalf("no match hints %q", got)
	}
	h.press("enter")
	if len(h.copied) != 0 {
		t.Fatal("copied with nothing selected")
	}
	if got := hintLine(h); got != "esc clear  ? keys  q quit" {
		t.Fatalf("kept search hints %q", got)
	}

	h = newHarness(t, many(2))
	h.press("T")
	h.mustContain("Trash", "The Trash is empty.")
	if got := hintLine(h); got != "esc back  ? keys  q quit" {
		t.Fatalf("empty Trash hints %q", got)
	}
	h.press("r", "d")
	if len(h.changes) != 0 || h.m.confirm != nil {
		t.Fatal("keys in an empty Trash did something")
	}

	h = newHarness(t, []account.Account{fixture()[6]})
	h.mustContain("Tessera  0 accounts  1 in the Trash", "No accounts yet.")
}

func TestViewportKeepsHeaderSelectionAndStatusOnScreen(t *testing.T) {
	for _, width := range []int{80, 40} {
		for _, height := range []int{5, 6, 8, 12, 16, 24, 40} {
			t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
				h := newSizedHarness(t, many(30), width, height)
				check := func(step string) {
					t.Helper()
					checkFrame(t, h, width, height, step)
					want := fmt.Sprintf("Service %02d", indexOf(h.vault, h.selectedID()))
					if l := selectedLine(h); !strings.Contains(l, want) {
						t.Fatalf("%s: selection %q is off screen:\n%s", step, want, h.screen())
					}
				}
				check("start")
				for i := range 29 {
					h.press("j")
					check(fmt.Sprintf("down %d", i))
				}
				h.press("down")
				if h.selectedID() != "id29" {
					t.Fatalf("moved past the end to %s", h.selectedID())
				}
				h.press("pgup")
				check("pgup")
				h.press("home")
				check("home")
				if h.selectedID() != "id00" {
					t.Fatalf("home selects %s", h.selectedID())
				}
				h.press("up", "k")
				check("up at the top")
				h.press("pgdown")
				check("pgdown")
				h.press("G")
				check("G")
				h.press("g")
				check("g")
				h.press("end")
				check("end")

				h.at(28)
				h.press("enter")
				check("long status")
				status := strings.Join(h.lines(), " ")
				if height >= 8 && !strings.Contains(strings.Join(strings.Fields(status), " "), "the current one ends in 2s") {
					t.Fatalf("status is cut:\n%s", h.screen())
				}
				if !strings.Contains(h.screen(), "✓ Copied the next code") {
					t.Fatalf("status is off screen:\n%s", h.screen())
				}
			})
		}
	}
}

func TestRowsFitTheWidthAndNeverCutACode(t *testing.T) {
	for _, width := range []int{120, 80, 60, 40} {
		h := newSizedHarness(t, fixture(), width, 24)
		checkFrame(t, h, width, 24, "list")
		screen := h.screen()
		for _, a := range h.m.rows() {
			c := ui.GroupCode(h.codeOf(a.ID, h.now))
			if !strings.Contains(screen, c) {
				t.Errorf("%d columns: code %q for %s is missing:\n%s", width, c, a.Issuer, screen)
			}
		}
		long := ""
		for _, l := range h.lines() {
			if strings.Contains(l, "Northwind") {
				long = l
			}
		}
		if width <= 80 && !strings.Contains(long, "…") {
			t.Errorf("%d columns: long name not shortened: %q", width, long)
		}
		gh := selectedLine(h)
		switch {
		case width >= 60:
			if !strings.Contains(gh, "█") || !strings.Contains(gh, "20s") {
				t.Errorf("%d columns: no countdown bar: %q", width, gh)
			}
			h.mustContain("hotp #4")
		default:
			if strings.Contains(gh, "█") || !strings.HasSuffix(gh, "20s") {
				t.Errorf("%d columns: narrow row %q", width, gh)
			}
			h.mustContain("#4")
		}
	}
}

func TestSearchMatchesEveryNamedField(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"github", []string{"a"}},
		{"PLAYER", []string{"f"}},
		{"bank", []string{"c"}},
		{"ops", []string{"b", "e"}},
		{"prod", []string{"b"}},
		{"work", []string{"a", "b"}},
		{"work dev", []string{"a"}},
		{"dropbox", nil},
	} {
		h := newHarness(t, fixture())
		h.press("/")
		h.typeText(tc.query)
		if got := h.rowIDs(); !slices.Equal(got, tc.want) {
			t.Errorf("search %q: rows %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestSearchKeepsTheArrowsAndTypesLetters(t *testing.T) {
	h := newHarness(t, fixture())
	h.press("/")
	h.mustContain("/ ")
	h.typeText("work")
	h.press("down")
	if h.selectedID() != "b" {
		t.Fatalf("down while searching selects %s", h.selectedID())
	}
	h.press("up")
	if h.selectedID() != "a" {
		t.Fatalf("up while searching selects %s", h.selectedID())
	}
	h.press("pgdown")
	if h.selectedID() != "b" {
		t.Fatalf("pgdown while searching selects %s", h.selectedID())
	}
	h.press("j", "k", "q", "space")
	if got := h.m.search.String(); got != "workjkq " || h.quit {
		t.Fatalf("letters while searching: %q quit=%v", got, h.quit)
	}
	h.press("backspace", "backspace", "backspace", "backspace")
	h.paste("pr\nod")
	if got := h.m.search.String(); got != "workpr od" {
		t.Fatalf("pasted search %q", got)
	}
	h.press("backspace", "backspace", "backspace", "backspace", "backspace")
	h.press("enter")
	if h.m.searching || h.m.search.String() != "work" || len(h.copied) != 1 {
		t.Fatalf("enter: searching=%v search=%q copied=%d", h.m.searching, h.m.search.String(), len(h.copied))
	}
	h.mustContain("Search work", "2 of 6 accounts")
	h.press("/")
	for range 5 {
		h.press("backspace")
	}
	if h.m.searching || h.m.search.String() != "" || len(h.m.rows()) != 6 {
		t.Fatalf("backspace on an empty search: searching=%v rows=%d", h.m.searching, len(h.m.rows()))
	}
}

func TestEscClearsSearchThenFiltersThenQuits(t *testing.T) {
	h := newHarness(t, fixture())
	h.press("/")
	h.typeText("git")
	h.press("esc")
	if h.quit || h.m.searching || h.m.search.String() != "" || len(h.m.rows()) != 6 {
		t.Fatal("esc while typing did not clear the search")
	}

	h.press("/")
	h.typeText("git")
	h.press("enter", "esc")
	if h.quit || h.m.search.String() != "" {
		t.Fatal("esc did not clear a kept search first")
	}

	h.press("f")
	if h.m.mode != modeFolders {
		t.Fatal("f did not open the folders")
	}
	h.mustContain("Folder", "All folders", "Dev", "Ops")
	h.press("j", "enter")
	if h.m.folder != "Dev" || !slices.Equal(h.rowIDs(), []string{"a"}) {
		t.Fatalf("folder %q rows %v", h.m.folder, h.rowIDs())
	}
	h.mustContain("Folder Dev")
	h.press("t")
	h.mustContain("All tags", "prod", "work")
	h.press("j", "enter")
	if h.m.tag != "prod" {
		t.Fatalf("tag %q", h.m.tag)
	}
	h.mustContain("Folder Dev   Tag prod", "Nothing matches.")

	h.press("/")
	h.typeText("x")
	h.press("enter")
	h.press("esc")
	if h.m.search.String() != "" || h.m.folder != "Dev" || h.quit {
		t.Fatal("esc did not clear the search before the filters")
	}
	h.press("esc")
	if h.m.folder != "" || h.m.tag != "" || h.quit {
		t.Fatal("esc did not clear the filters before quitting")
	}
	h.press("esc")
	if !h.quit {
		t.Fatal("esc at the top level did not quit")
	}
}

func TestPickers(t *testing.T) {
	h := newHarness(t, fixture())
	h.press("f", "G", "enter")
	if h.m.folder != "Ops" {
		t.Fatalf("folder %q", h.m.folder)
	}
	h.press("f")
	if h.m.picker.cursor != 2 {
		t.Fatalf("picker opens on %d, want the current folder", h.m.picker.cursor)
	}
	h.press("g", "enter")
	if h.m.folder != "" {
		t.Fatalf("All folders left %q", h.m.folder)
	}
	h.press("t", "pgdown", "esc")
	if h.m.mode != modeList || h.m.tag != "" {
		t.Fatal("esc in the tag list changed the filter")
	}

	h = newHarness(t, many(3))
	h.press("f")
	if h.m.mode != modeList || h.status() != "No folders yet. Set one with e." {
		t.Fatalf("no folders: %q", h.status())
	}
	h.press("t")
	if h.m.mode != modeList || h.status() != "No tags yet. Add them with e." {
		t.Fatalf("no tags: %q", h.status())
	}
}

func TestCopyNearTheBoundary(t *testing.T) {
	h := newHarness(t, fixture())
	h.press("enter")
	if len(h.copied) != 1 || h.copied[0] != h.codeOf("a", h.now) {
		t.Fatalf("copied %v", h.copied)
	}
	if !slices.Equal(h.cleared, h.copied) {
		t.Fatalf("clipboard clear scheduled for %v, want %v", h.cleared, h.copied)
	}
	if h.status() != "✓ Copied the code for GitHub" {
		t.Fatalf("status %q", h.status())
	}

	h.at(27)
	h.press("c")
	if h.copied[1] != h.codeOf("a", base.Add(27*time.Second)) {
		t.Fatal("3s left did not copy the current code")
	}

	h.at(28)
	h.press("enter")
	next := h.codeOf("a", base.Add(30*time.Second))
	if h.copied[2] != next || next == h.codeOf("a", h.now) {
		t.Fatal("2s left did not copy the next code")
	}
	if h.status() != "✓ Copied the next code for GitHub; the current one ends in 2s" {
		t.Fatalf("status %q", h.status())
	}

	h.at(31)
	if h.status() != "✓ Copied the next code for GitHub; the current one ends in 2s" {
		t.Fatal("status left before 4s")
	}
	h.at(32)
	if h.status() != "" {
		t.Fatalf("status still %q after 4s", h.status())
	}

	h.selectID("f")
	h.press("enter")
	if got := h.copied[len(h.copied)-1]; got != h.codeOf("f", h.now) || len(got) != 5 {
		t.Fatalf("steam copy %q", got)
	}
}

func TestNextCopiesTheNextCode(t *testing.T) {
	h := newHarness(t, fixture())
	h.press("n")
	if h.copied[0] != h.codeOf("a", base.Add(30*time.Second)) {
		t.Fatal("n did not copy the next code")
	}
	if h.status() != "✓ Copied the next code for GitHub; it starts in 20s" {
		t.Fatalf("status %q", h.status())
	}
}

func TestCopyFailuresSay(t *testing.T) {
	h := newHarness(t, fixture())
	h.copyErr = errors.New("no clipboard")
	h.press("enter")
	if h.status() != "✗ Copy failed: no clipboard" || len(h.cleared) != 0 {
		t.Fatalf("status %q cleared %v", h.status(), h.cleared)
	}

	h = newHarness(t, fixture())
	h.m.opts.ClearCopy = func(string) error { return errors.New("the clipboard is not cleared") }
	h.press("enter")
	if h.status() != "✗ Copied the code for GitHub; the clipboard is not cleared" {
		t.Fatalf("status %q", h.status())
	}
}

func TestHOTPGeneratesOnlyAfterConfirming(t *testing.T) {
	h := newHarness(t, fixture())
	h.selectID("c")
	h.press("enter")
	if len(h.changes) != 0 || h.copied[0] != h.codeOf("c", h.now) {
		t.Fatal("enter on a counter account changed the counter")
	}
	if h.status() != "✓ Copied code #4 for Bank" {
		t.Fatalf("status %q", h.status())
	}

	h.press("n")
	if len(h.changes) != 0 {
		t.Fatal("n generated a code without asking")
	}
	if h.status() != "Generate the next code for Bank? The counter moves to 5. y/N" {
		t.Fatalf("question %q", h.status())
	}
	h.mustContain("y yes  any other key no")
	h.press("j")
	if len(h.changes) != 0 || h.selectedID() != "c" || h.status() != "No code generated" {
		t.Fatalf("j answered no but changes=%d selected=%s status=%q", len(h.changes), h.selectedID(), h.status())
	}
	for _, open := range []string{"a", "e"} {
		h.press("n", "x", open)
		if h.status() != "" {
			t.Fatalf("%s opened its form under the old message %q", open, h.status())
		}
		h.press("esc")
	}
	h.press("n", "enter")
	if len(h.changes) != 0 {
		t.Fatal("enter answered yes")
	}

	h.press("n", "y")
	c := lastChange(t, h)
	if c.Kind != ChangeAdvance || c.ID != "c" || c.Counter != 5 {
		t.Fatalf("change %+v", c)
	}
	if got := h.copied[len(h.copied)-1]; got != h.codeOf("c", h.now) {
		t.Fatalf("copied %q, want the code at counter 5", got)
	}
	if h.status() != "✓ Generated code #5 for Bank and copied it" {
		t.Fatalf("status %q", h.status())
	}
	h.mustContain("hotp #5")

	for s := 0; s < 90; s += 5 {
		h.at(s)
	}
	h.send(pollMsg(h.now))
	if len(h.changes) != 1 {
		t.Fatal("time passing generated a code")
	}

	h.mutateErr = errors.New("the counter changed in another process")
	h.press("n", "Y")
	if h.status() != "✗ No code generated: the counter changed in another process" {
		t.Fatalf("status %q", h.status())
	}

	h.press("n", "q")
	if !h.quit {
		t.Fatal("q at the question did not quit")
	}
}

func rowLine(h *harness, name string) string {
	h.t.Helper()
	for _, l := range h.lines() {
		if strings.Contains(l, name) {
			return l
		}
	}
	h.t.Fatalf("no row for %s:\n%s", name, h.screen())
	return ""
}

func TestHiddenCodesShowOnlyTheSelectedOne(t *testing.T) {
	h := newHarness(t, fixture())
	code := ui.GroupCode(h.codeOf("a", h.now))
	h.press("h")
	h.mustContain("Codes hidden")
	if l := rowLine(h, "GitHub"); !strings.Contains(l, code) {
		t.Fatalf("selected row hides its code: %q", l)
	}
	for _, name := range []string{"AWS", "Bank", "Steam"} {
		if l := rowLine(h, name); !strings.Contains(l, "••• •••") && !strings.Contains(l, "•••••") {
			t.Fatalf("%s shows its code: %q", name, l)
		}
	}
	if l := rowLine(h, "Cloud"); !strings.Contains(l, "••••••••") || strings.Contains(l, h.codeOf("e", h.now)) {
		t.Fatalf("Cloud shows its code: %q", l)
	}
	h.press("down")
	if l := rowLine(h, "AWS"); !strings.Contains(l, code) {
		t.Fatalf("selected row hides its code: %q", l)
	}
	if l := rowLine(h, "GitHub"); strings.Contains(l, code) {
		t.Fatalf("unselected row shows its code: %q", l)
	}
	h.press("h")
	h.mustNotContain("•", "Codes hidden")
	if l := rowLine(h, "GitHub"); !strings.Contains(l, code) {
		t.Fatalf("h did not show codes again: %q", l)
	}
}

func TestAddFromAPastedLink(t *testing.T) {
	h := newHarness(t, fixture())
	h.press("a")
	h.mustContain("Add an account", "Link or setup key", "Paste an otpauth:// link or a setup key.")
	link := "otpauth://totp/Mail:me@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Mail"
	h.paste(link + "\n")
	h.mustNotContain("JBSWY3DPEHPK3PXP", "otpauth://totp")
	h.press("enter")
	c := lastChange(t, h)
	if c.Kind != ChangeAdd || c.Add != (Add{Text: link}) {
		t.Fatalf("change %+v", c)
	}
	if h.m.mode != modeList || h.status() != "✓ Added Mail" {
		t.Fatalf("mode %d status %q", h.m.mode, h.status())
	}
	if a, _ := h.m.selected(); a.Issuer != "Mail" {
		t.Fatalf("selected %q, want the new account", a.Issuer)
	}
	h.mustContain("7 accounts")
}

func TestAddASetupKeyWithNames(t *testing.T) {
	h := newHarness(t, fixture())
	h.press("a")
	h.typeText("JBSWY3DPEHPK3PXP")
	h.press("tab")
	h.mustContain("A setup key needs an issuer or account. A link keeps its own when empty.")
	h.typeText("Example")
	h.press("tab")
	h.typeText("me")
	h.press("enter")
	if c := lastChange(t, h); c.Add != (Add{Text: "JBSWY3DPEHPK3PXP", Issuer: "Example", Account: "me"}) {
		t.Fatalf("change %+v", c)
	}
}

func TestAddRefusesEmptyAndKeepsTheFormOnError(t *testing.T) {
	h := newHarness(t, fixture())
	h.press("a", "enter")
	if len(h.changes) != 0 || h.m.mode != modeAdd || h.status() != "✗ Paste an otpauth link or a setup key" {
		t.Fatalf("empty add: changes=%d status=%q", len(h.changes), h.status())
	}
	h.mutateErr = errors.New("not an otpauth link or a setup key")
	h.paste("nope")
	h.press("enter")
	if h.m.mode != modeAdd || h.status() != "✗ Not saved: not an otpauth link or a setup key" {
		t.Fatalf("mode %d status %q", h.m.mode, h.status())
	}
	if h.reloads != 1 {
		t.Fatalf("a failed write reloaded %d times, want 1", h.reloads)
	}
	h.press("esc")
	if h.m.mode != modeList || len(h.changes) != 1 {
		t.Fatal("esc did not cancel the form")
	}
}

func TestAddReportsSkippedAndProblems(t *testing.T) {
	h := newHarness(t, fixture())
	h.m.opts.Mutate = func(Change) (Result, error) {
		return Result{Accounts: h.vault, Skipped: 2, Problems: []string{"input: missing secret"}}, nil
	}
	h.press("a")
	h.paste("otpauth-migration://offline?data=x")
	h.press("enter")
	if h.status() != "✗ 2 accounts already in the vault; not added: input: missing secret" {
		t.Fatalf("status %q", h.status())
	}
	h.m.opts.Mutate = func(Change) (Result, error) {
		return Result{Accounts: h.vault, IDs: []string{"a", "b"}}, nil
	}
	h.press("a")
	h.paste("otpauth-migration://offline?data=y")
	h.press("enter")
	if h.status() != "✓ Added 2 accounts" || h.selectedID() != "a" {
		t.Fatalf("status %q", h.status())
	}
}

func TestEditSendsOnlyTheChangedFields(t *testing.T) {
	h := newHarness(t, fixture())
	h.press("e")
	h.mustContain("Edit GitHub (me@example.com)", "Issuer", "Account", "Alias", "Folder", "Tags", "Note")
	h.press("tab", "tab")
	h.mustContain("1 to 12 letters or digits, starting with a letter.")
	h.press("tab", "ctrl+u")
	h.typeText("Work")
	h.press("tab")
	h.mustContain("Separate tags with commas.")
	h.press("ctrl+u")
	h.typeText("ci, prod")
	h.press("tab")
	h.mustContain("ctrl+j new line")
	h.typeText("first")
	h.press("ctrl+j")
	h.typeText("second")
	h.mustContain("first↵second")
	h.press("enter")
	c := lastChange(t, h)
	if c.Kind != ChangeEdit || c.ID != "a" {
		t.Fatalf("change %+v", c)
	}
	e := c.Edit
	if e.Issuer != nil || e.Account != nil || e.Alias != nil {
		t.Fatalf("unchanged fields sent: %+v", e)
	}
	if *e.Folder != "Work" || *e.Tags != "ci, prod" || *e.Note != "first\nsecond" {
		t.Fatalf("edit %q %q %q", *e.Folder, *e.Tags, *e.Note)
	}
	if h.m.mode != modeList || h.status() != "✓ Saved GitHub" || h.selectedID() != "a" {
		t.Fatalf("mode %d status %q selected %s", h.m.mode, h.status(), h.selectedID())
	}
}

func TestEditThatChangesNothingWritesNothing(t *testing.T) {
	h := newHarness(t, fixture())
	h.press("e", "enter")
	if len(h.changes) != 0 || h.status() != "Nothing changed" || h.m.mode != modeList {
		t.Fatalf("changes=%d status=%q", len(h.changes), h.status())
	}
	h.press("e")
	h.typeText("x")
	h.press("backspace", "enter")
	if len(h.changes) != 0 {
		t.Fatal("an edit typed and undone was written")
	}
	h.press("e")
	h.typeText("zzz")
	h.press("esc")
	if len(h.changes) != 0 || h.m.mode != modeList {
		t.Fatal("esc did not cancel the edit")
	}
}

func TestEditKeepsTheFormOnError(t *testing.T) {
	h := newHarness(t, fixture())
	h.mutateErr = errors.New("enter an issuer or an account name")
	h.press("e", "ctrl+u", "tab", "ctrl+u", "enter")
	if h.m.mode != modeEdit || h.status() != "✗ Not saved: enter an issuer or an account name" {
		t.Fatalf("mode %d status %q", h.m.mode, h.status())
	}
}

func TestFormFocusMoves(t *testing.T) {
	h := newHarness(t, fixture())
	h.press("e")
	h.press("up")
	if h.m.form.focus != editNote {
		t.Fatalf("up from the first field lands on %d", h.m.form.focus)
	}
	h.press("down")
	if h.m.form.focus != editIssuer {
		t.Fatalf("down from the last field lands on %d", h.m.form.focus)
	}
	h.press("shift+tab", "shift+tab")
	if h.m.form.focus != editTags {
		t.Fatalf("shift+tab lands on %d", h.m.form.focus)
	}
	h.press("q", "?", "T")
	if h.quit || h.m.mode != modeEdit {
		t.Fatal("letters in a form acted as keys")
	}
}

func TestFieldEditing(t *testing.T) {
	f := newField("x", "abc")
	for _, step := range []struct {
		key  string
		want string
		at   int
	}{
		{"left", "abc", 2},
		{"Z", "abZc", 3},
		{"home", "abZc", 0},
		{"Y", "YabZc", 1},
		{"delete", "YbZc", 1},
		{"end", "YbZc", 4},
		{"delete", "YbZc", 4},
		{"right", "YbZc", 4},
		{"backspace", "YbZ", 3},
		{"ctrl+h", "Yb", 2},
		{"left", "Yb", 1},
		{"ctrl+k", "Y", 1},
		{"space", "Y ", 2},
		{"ctrl+u", "", 0},
		{"backspace", "", 0},
		{"ctrl+j", "", 0},
	} {
		f.handle(key(step.key))
		if f.String() != step.want || f.cursor != step.at {
			t.Fatalf("after %s: %q at %d, want %q at %d", step.key, f.String(), f.cursor, step.want, step.at)
		}
	}
	if f.handle(tea.KeyPressMsg{Code: 'x', Text: "x", Mod: tea.ModAlt}) || f.String() != "" {
		t.Fatal("alt+x was typed")
	}

	note := newField("Note", "")
	note.multiline = true
	note.paste("a\r\nb\rc")
	if note.String() != "a\nb\nc" {
		t.Fatalf("multiline paste %q", note.String())
	}
	masked := newField("Key", "")
	masked.masked = true
	masked.paste("  abc\tdef \n")
	if masked.String() != "abc def" {
		t.Fatalf("masked paste %q", masked.String())
	}
	pass := field{masked: true, verbatim: true}
	pass.paste(" two words \nnext line")
	if pass.String() != " two words " {
		t.Fatalf("passphrase paste %q", pass.String())
	}
	if got := ansi.Strip(masked.render(20, false, plain, cursorStyle)); got != "•••••••" {
		t.Fatalf("masked render %q", got)
	}
	long := newField("x", strings.Repeat("abcdefghij", 5))
	if got := ansi.Strip(long.render(10, true, plain, cursorStyle)); got != "bcdefghij " {
		t.Fatalf("scrolled render %q", got)
	}
}

func TestPinKeepsTheSelection(t *testing.T) {
	h := newHarness(t, fixture())
	h.selectID("c")
	h.press("p")
	if c := lastChange(t, h); c.Kind != ChangePin || c.ID != "c" || !c.Pinned {
		t.Fatalf("change %+v", c)
	}
	if got := h.rowIDs(); !slices.Equal(got, []string{"c", "a", "b", "e", "d", "f"}) {
		t.Fatalf("rows %v", got)
	}
	if h.selectedID() != "c" || h.status() != "✓ Pinned Bank" {
		t.Fatalf("selected %s status %q", h.selectedID(), h.status())
	}
	h.mustContain("★ bank")
	h.press("p")
	if c := lastChange(t, h); c.Pinned {
		t.Fatal("second p pinned again")
	}
	if h.selectedID() != "c" || h.status() != "✓ Unpinned Bank" {
		t.Fatalf("selected %s status %q", h.selectedID(), h.status())
	}
}

func TestTrashAndUndo(t *testing.T) {
	h := newHarness(t, fixture())
	h.selectID("e")
	h.press("d")
	if c := lastChange(t, h); c.Kind != ChangeTrash || c.ID != "e" {
		t.Fatalf("change %+v", c)
	}
	h.mustContain("Tessera  5 accounts  2 in the Trash")
	if h.status() != "✓ Moved Cloud to the Trash. Press u to undo." {
		t.Fatalf("status %q", h.status())
	}
	if slices.Contains(h.rowIDs(), "e") || h.selectedID() != "d" {
		t.Fatalf("rows %v selected %s", h.rowIDs(), h.selectedID())
	}
	h.press("u")
	if c := lastChange(t, h); c.Kind != ChangeRestore || c.ID != "e" {
		t.Fatalf("change %+v", c)
	}
	if h.selectedID() != "e" || h.status() != "✓ Restored Cloud" {
		t.Fatalf("selected %s status %q", h.selectedID(), h.status())
	}
	h.press("u")
	if len(h.changes) != 2 || h.status() != "Nothing to undo" {
		t.Fatalf("second undo: changes=%d status=%q", len(h.changes), h.status())
	}
}

func TestUndoSkipsAnAccountRestoredElsewhere(t *testing.T) {
	h := newHarness(t, fixture())
	h.press("d")
	restored := slices.Clone(h.vault)
	restored[indexOf(restored, "a")].DeletedAt = 0
	h.reloadFn = func() ([]account.Account, bool, error) { return restored, true, nil }
	h.send(pollMsg(h.now))
	h.press("u")
	if len(h.changes) != 1 || h.status() != "Nothing to undo" {
		t.Fatalf("changes=%d status=%q", len(h.changes), h.status())
	}
}

func TestTrashView(t *testing.T) {
	h := newHarness(t, fixture())
	h.selectID("e")
	h.press("d", "T")
	deleted := time.Unix(fixture()[6].DeletedAt, 0).UTC().Format(trashDate)
	h.mustContain("Trash", h.now.Format(trashDate)+"  cl  Cloud ops", deleted+"  db  Dropbox me", "r restore")
	if a, _ := h.m.selectedTrash(); a.ID != "e" {
		t.Fatalf("Trash opens on %s, want the newest", a.ID)
	}
	h.press("j")
	if a, _ := h.m.selectedTrash(); a.ID != "g" {
		t.Fatalf("j selects %s", a.ID)
	}
	h.press("r")
	if c := lastChange(t, h); c.Kind != ChangeRestore || c.ID != "g" {
		t.Fatalf("change %+v", c)
	}
	if h.m.mode != modeTrash || h.status() != "✓ Restored Dropbox" {
		t.Fatalf("mode %d status %q", h.m.mode, h.status())
	}
	h.press("?")
	if h.m.mode != modeHelp {
		t.Fatal("? in the Trash did not open the keys")
	}
	h.press("esc")
	if h.m.mode != modeTrash {
		t.Fatal("closing the keys did not return to the Trash")
	}
	h.press("T")
	if h.m.mode != modeList || !slices.Contains(h.rowIDs(), "g") {
		t.Fatal("T did not return to the list")
	}
}

func TestTrashDeleteAsksFirst(t *testing.T) {
	h := newHarness(t, fixture())
	h.press("T", "d")
	if len(h.changes) != 0 {
		t.Fatal("d deleted without asking")
	}
	if h.status() != "Delete Dropbox (me) for good? Its secret is removed from the vault. y/N" {
		t.Fatalf("question %q", h.status())
	}
	if !h.m.confirm.danger {
		t.Fatal("the delete question is not marked as dangerous")
	}
	h.press("n")
	if len(h.changes) != 0 || h.status() != "Nothing deleted" {
		t.Fatalf("n: changes=%d status=%q", len(h.changes), h.status())
	}
	h.press("d", "y")
	c := lastChange(t, h)
	if c.Kind != ChangePurge || c.ID != "g" || c.Snapshot.ID != "g" || c.Snapshot.DeletedAt == 0 {
		t.Fatalf("change %+v", c)
	}
	if h.status() != "✓ Deleted Dropbox for good" {
		t.Fatalf("status %q", h.status())
	}
	h.mustContain("The Trash is empty.")
	h.mustNotContain("in the Trash")
	h.press("esc")
	if h.m.mode != modeList {
		t.Fatal("esc did not leave the Trash")
	}
}

func TestTrashScrollsAtLowHeights(t *testing.T) {
	accts := many(20)
	for i := range accts {
		accts[i].DeletedAt = base.Unix() - int64(i)
	}
	h := newSizedHarness(t, accts, 40, 8)
	h.press("T")
	for i := range 19 {
		h.press("down")
		checkFrame(t, h, 40, 8, fmt.Sprintf("trash down %d", i))
		if l := selectedLine(h); !strings.Contains(l, fmt.Sprintf("Service %02d", i+1)) {
			t.Fatalf("selection off screen:\n%s", h.screen())
		}
	}
	h.press("pgup", "pgdown", "home", "end", "g", "G", "up", "k")
	checkFrame(t, h, 40, 8, "trash moves")
}

func TestHelpScrollsAndCloses(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {40, 12}, {40, 6}} {
		h := newSizedHarness(t, fixture(), size[0], size[1])
		h.press("?")
		h.mustContain("Keys", "Moving in a list")
		h.press("end")
		checkFrame(t, h, size[0], size[1], "help end")
		if !strings.Contains(h.screen(), "any other key") {
			t.Fatalf("%v: end does not show the last key:\n%s", size, h.screen())
		}
		before := h.screen()
		h.press("down", "j", "pgdown")
		if h.screen() != before {
			t.Fatalf("%v: scrolled past the end", size)
		}
		h.press("home")
		h.mustContain("Moving in a list")
		h.press("G", "pgup", "k", "up", "g")
		h.mustContain("Moving in a list")
		h.press("?")
		if h.m.mode != modeList {
			t.Fatal("? did not close the keys")
		}
	}
}

func TestEveryKeyIsInTheReference(t *testing.T) {
	documented := map[string]bool{}
	for _, l := range helpLines() {
		for _, k := range strings.Fields(l.keys) {
			documented[k] = true
		}
	}
	for _, k := range []string{
		"↑", "↓", "j", "k", "pgup", "pgdn", "g", "G", "home", "end", "enter", "c", "n", "/",
		"esc", "f", "t", "h", "a", "e", "p", "d", "u", "T", "?", "q", "ctrl+c", "r", "tab",
		"shift+tab", "←", "→", "backspace", "del", "ctrl+u", "ctrl+k", "ctrl+j", "y",
	} {
		if !documented[k] {
			t.Errorf("key %q is not in the reference", k)
		}
	}
}

type modelState struct {
	mode                     mode
	cursor, trash            int
	selected, search, filter string
	searching, hidden, quit  bool
	asking                   bool
	status                   string
	changes, copied          int
}

func (h *harness) state() modelState {
	return modelState{
		mode: h.m.mode, cursor: h.m.cursor, trash: h.m.trashCursor, selected: h.m.selectedID,
		search: h.m.search.String(), filter: h.m.folder + "\x00" + h.m.tag,
		searching: h.m.searching, hidden: h.m.hidden, quit: h.quit, asking: h.m.confirm != nil,
		status: h.m.status, changes: len(h.changes), copied: len(h.copied),
	}
}

func TestKeysOutsideTheReferenceDoNothing(t *testing.T) {
	others := []string{"tab", "shift+tab", "left", "right", "backspace", "delete", "space", "ctrl+u", "ctrl+j", "ctrl+p"}
	for r := '!'; r <= '~'; r++ {
		others = append(others, string(r))
	}
	for _, tc := range []struct {
		name  string
		setup []string
		used  string
	}{
		{"list", nil, "jkgGcn/fthaepduT?q"},
		{"trash", []string{"d", "T"}, "jkgGrdT?q"},
	} {
		for _, k := range others {
			if strings.Contains(tc.used, k) && len(k) == 1 {
				continue
			}
			h := newHarness(t, fixture())
			h.selectID("b")
			h.press(tc.setup...)
			before := h.state()
			h.press(k)
			if after := h.state(); after != before {
				t.Errorf("%s: %q changed the view: %+v -> %+v", tc.name, k, before, after)
			}
		}
	}
}

func TestQAndCtrlCQuit(t *testing.T) {
	setups := map[string][]string{
		"list":     nil,
		"search":   {"/"},
		"keys":     {"?"},
		"folders":  {"f"},
		"tags":     {"t"},
		"add":      {"a"},
		"edit":     {"e"},
		"trash":    {"T"},
		"question": {"T", "d"},
	}
	typing := map[string]bool{"search": true, "add": true, "edit": true}
	for name, setup := range setups {
		h := newHarness(t, fixture())
		h.press(setup...)
		h.press("ctrl+c")
		if !h.quit {
			t.Errorf("%s: ctrl+c did not quit", name)
		}
		h = newHarness(t, fixture())
		h.press(setup...)
		h.press("q")
		if h.quit == typing[name] {
			t.Errorf("%s: q quit=%v", name, h.quit)
		}
	}
	h := newHarness(t, fixture())
	h.reloadFn = func() ([]account.Account, bool, error) { return nil, false, ErrKeyReplaced }
	h.send(pollMsg(h.now))
	h.press("q")
	if h.quit {
		t.Fatal("q at the passphrase prompt quit instead of typing")
	}
	h.press("ctrl+c")
	if !h.quit {
		t.Fatal("ctrl+c at the passphrase prompt did not quit")
	}
	h = newHarness(t, fixture())
	h.reloadFn = func() ([]account.Account, bool, error) { return nil, false, ErrKeyReplaced }
	h.send(pollMsg(h.now))
	h.press("esc")
	if !h.quit {
		t.Fatal("esc at the passphrase prompt did not quit")
	}
}

func TestReloadKeepsTheSelection(t *testing.T) {
	h := newHarness(t, fixture())
	h.selectID("e")
	updated := append(fixture(), totpAcct("n", "Acme", "me", "acme"))
	h.reloadFn = func() ([]account.Account, bool, error) { return updated, true, nil }
	h.send(pollMsg(h.now))
	if h.reloads != 1 || len(h.m.rows()) != 7 {
		t.Fatalf("reloads=%d rows=%d", h.reloads, len(h.m.rows()))
	}
	if h.selectedID() != "e" {
		t.Fatalf("selection moved to %s", h.selectedID())
	}

	gone := slices.DeleteFunc(slices.Clone(updated), func(a account.Account) bool { return a.ID == "e" })
	h.reloadFn = func() ([]account.Account, bool, error) { return gone, true, nil }
	h.send(pollMsg(h.now))
	if h.selectedID() == "" || h.selectedID() == "e" {
		t.Fatalf("selection after removal %q", h.selectedID())
	}

	h.reloadFn = func() ([]account.Account, bool, error) { return nil, false, nil }
	h.send(pollMsg(h.now))
	if len(h.m.rows()) != 6 {
		t.Fatal("an unchanged reload replaced the accounts")
	}
}

func TestReloadErrorShowsOnce(t *testing.T) {
	h := newHarness(t, fixture())
	h.reloadFn = func() ([]account.Account, bool, error) { return nil, false, errors.New("the vault file is not valid") }
	h.send(pollMsg(h.now))
	if h.status() != "✗ Could not read the vault: the vault file is not valid" {
		t.Fatalf("status %q", h.status())
	}
	h.at(20)
	h.send(pollMsg(h.now))
	if h.status() != "" {
		t.Fatal("the same error was shown again")
	}
	if len(h.m.rows()) != 6 {
		t.Fatal("a failed reload dropped the accounts")
	}
}

func TestStaleReloadIsDropped(t *testing.T) {
	h := newHarness(t, fixture())
	stale := fixture()
	h.reloadFn = func() ([]account.Account, bool, error) { return stale, true, nil }
	pending := h.m.reloadCmd()
	h.selectID("c")
	h.press("p")
	h.send(pending())
	if a, _ := h.m.byID("c"); !a.Pinned {
		t.Fatal("a reload that started before the pin undid it")
	}
}

func TestPollWaitsWhileBusyOrLocked(t *testing.T) {
	h := newHarness(t, fixture())
	h.m.busy = true
	h.send(pollMsg(h.now))
	h.press("p")
	if h.reloads != 0 || len(h.changes) != 0 || h.status() != "Saving…" {
		t.Fatalf("reloads=%d changes=%d status=%q", h.reloads, len(h.changes), h.status())
	}
}

func TestReunlockAfterTheKeyChanged(t *testing.T) {
	h := newHarness(t, fixture())
	h.press("e")
	h.typeText("X")
	h.reloadFn = func() ([]account.Account, bool, error) { return nil, false, ErrKeyReplaced }
	h.send(pollMsg(h.now))
	if h.m.mode != modeUnlock {
		t.Fatalf("mode %d, want the passphrase prompt", h.m.mode)
	}
	h.mustContain("Unlock", "The vault key changed. Enter the passphrase to keep watching.", "enter unlock")

	h.send(pollMsg(h.now))
	if h.reloads != 1 {
		t.Fatal("polled while asking for the passphrase")
	}
	h.press("enter")
	if len(h.unlockCalls) != 0 {
		t.Fatal("an empty passphrase was tried")
	}

	h.unlockFn = func(p string) ([]account.Account, error) {
		if p != "new pass" {
			return nil, errors.New("wrong passphrase")
		}
		return slices.Clone(h.vault), nil
	}
	h.typeText("bad")
	h.press("enter")
	if h.m.mode != modeUnlock || h.status() != "✗ Not unlocked: wrong passphrase" || h.m.pass.String() != "" {
		t.Fatalf("mode %d status %q", h.m.mode, h.status())
	}
	h.typeText("new")
	h.paste(" pass")
	h.mustNotContain("new pass")
	h.press("enter")
	if !slices.Equal(h.unlockCalls, []string{"bad", "new pass"}) {
		t.Fatalf("unlock calls %q", h.unlockCalls)
	}
	if h.m.mode != modeEdit || h.status() != "✓ Unlocked" {
		t.Fatalf("mode %d status %q, want the edit back", h.m.mode, h.status())
	}
	h.press("enter")
	if c := lastChange(t, h); c.Kind != ChangeEdit || *c.Edit.Issuer != "GitHubX" {
		t.Fatalf("change %+v", c)
	}
}

func TestWriteAfterAKeyChangeAsksAndResumes(t *testing.T) {
	h := newHarness(t, fixture())
	h.mutateErr = ErrKeyReplaced
	h.press("T", "d", "y")
	if h.m.mode != modeUnlock || h.m.confirm != nil {
		t.Fatalf("mode %d, want the passphrase prompt", h.m.mode)
	}
	h.mutateErr = nil
	h.typeText("pw")
	h.press("enter")
	if h.m.mode != modeTrash {
		t.Fatalf("mode %d, want the Trash back", h.m.mode)
	}

	h = newHarness(t, fixture())
	h.press("e")
	h.typeText("X")
	h.mutateErr = ErrKeyReplaced
	h.press("enter")
	gone := slices.DeleteFunc(slices.Clone(h.vault), func(a account.Account) bool { return a.ID == "a" })
	h.unlockFn = func(string) ([]account.Account, error) { return gone, nil }
	h.typeText("pw")
	h.press("enter")
	if h.m.mode != modeList {
		t.Fatalf("mode %d: an edit of an account gone after unlocking stayed open", h.m.mode)
	}
}

func TestViewWithoutWritesSays(t *testing.T) {
	h := newHarness(t, fixture())
	h.m.opts.Mutate = nil
	h.press("p")
	if h.status() != "✗ This view cannot change the vault" {
		t.Fatalf("status %q", h.status())
	}
	h.m.opts.Unlock = nil
	h.reloadFn = func() ([]account.Account, bool, error) { return nil, false, ErrKeyReplaced }
	h.send(pollMsg(h.now))
	if h.m.mode != modeList || !strings.HasPrefix(h.status(), "✗ ") {
		t.Fatalf("mode %d status %q", h.m.mode, h.status())
	}
}

func TestControlCharactersInNamesAreShownSafely(t *testing.T) {
	a := totpAcct("a", "Evil\x1b[2J", "me\x07", "ev")
	a.Folder = "F\x1b]0;x\x07"
	h := newHarness(t, []account.Account{a})
	raw := h.m.View().Content
	if strings.Contains(raw, "\x1b[2J") || strings.Contains(raw, "\x07") || strings.Contains(raw, "\x1b]") {
		t.Fatalf("raw control bytes reached the frame: %q", raw)
	}
	h.press("f")
	if raw := h.m.View().Content; strings.Contains(raw, "\x1b]") || strings.Contains(raw, "\x07") {
		t.Fatalf("raw control bytes in the folder list: %q", raw)
	}
}

func TestEveryStateRendersAtEverySize(t *testing.T) {
	setups := [][]string{nil, {"/"}, {"?"}, {"f"}, {"t"}, {"a"}, {"e"}, {"T"}, {"T", "d"}, {"h"}, {"n"}}
	for _, size := range [][2]int{{80, 24}, {40, 12}, {40, 5}, {120, 50}, {30, 4}} {
		for _, setup := range setups {
			h := newSizedHarness(t, fixture(), size[0], size[1])
			h.selectID("c")
			h.press(setup...)
			lines := strings.Split(h.m.View().Content, "\n")
			if size[1] >= 5 && len(lines) != size[1] {
				t.Errorf("%v %v: %d lines", size, setup, len(lines))
			}
			for i, l := range lines {
				if ansi.StringWidth(l) > size[0] {
					t.Errorf("%v %v: line %d too wide", size, setup, i)
				}
			}
		}
	}
}

func TestInitAsksForTheBackgroundAndStartsTheClock(t *testing.T) {
	var scheduled []time.Duration
	m := New(Options{Accounts: fixture(), Reload: func() ([]account.Account, bool, error) { return nil, false, nil }})
	m.schedule = func(d time.Duration, _ func(time.Time) tea.Msg) tea.Cmd {
		scheduled = append(scheduled, d)
		return nil
	}
	if m.Init() == nil {
		t.Fatal("Init returned no command")
	}
	if !slices.Equal(scheduled, []time.Duration{tickInterval, pollInterval}) {
		t.Fatalf("scheduled %v", scheduled)
	}
	if !m.View().AltScreen {
		t.Fatal("the view does not use the alternate screen")
	}
}
