package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/code"
)

// base is the start of a 30-second window.
var base = time.Unix(1_800_000_000, 0).UTC()

func totpAcct(id, issuer, acct, handle string) account.Account {
	return account.Account{
		ID: id, Type: account.TOTP, Issuer: issuer, Account: acct, Handle: handle,
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30,
		CreatedAt: 1, UpdatedAt: 1,
	}
}

// fixture is a vault with every kind of row: pinned, HOTP, Steam, 8 digits, a
// long name, folders, tags, and one account in the Trash.
func fixture() []account.Account {
	gh := totpAcct("a", "GitHub", "me@example.com", "gh")
	gh.Pinned, gh.Folder, gh.Tags = true, "Dev", []string{"work"}
	aws := totpAcct("b", "AWS", "root", "aws")
	aws.Folder, aws.Tags = "Ops", []string{"work", "prod"}
	bank := totpAcct("c", "Bank", "me", "bank")
	bank.Type, bank.Counter = account.HOTP, 4
	long := totpAcct("d", "Northwind Traders International", "someone.with.a.long.address@example.com", "nw")
	cloud := totpAcct("e", "Cloud", "ops", "cl")
	cloud.Digits, cloud.Algorithm = 8, "SHA256"
	steam := totpAcct("f", "Steam", "player", "st")
	steam.Type, steam.Digits = account.Steam, 5
	gone := totpAcct("g", "Dropbox", "me", "db")
	gone.DeletedAt = base.Add(-48 * time.Hour).Unix()
	return []account.Account{gh, aws, bank, long, cloud, steam, gone}
}

// many is n live accounts named in display order.
func many(n int) []account.Account {
	out := make([]account.Account, 0, n)
	for i := range n {
		out = append(out, totpAcct(fmt.Sprintf("id%02d", i), fmt.Sprintf("Service %02d", i), "me", fmt.Sprintf("s%02d", i)))
	}
	return out
}

// harness drives a Model by messages and runs the commands it returns
// synchronously against an in-memory vault.
type harness struct {
	t     *testing.T
	m     Model
	now   time.Time
	vault []account.Account

	quit        bool
	copied      []string
	cleared     []string
	changes     []Change
	reloads     int
	unlockCalls []string

	copyErr   error
	mutateErr error
	reloadFn  func() ([]account.Account, bool, error)
	unlockFn  func(string) ([]account.Account, error)
}

func newHarness(t *testing.T, accts []account.Account) *harness {
	t.Helper()
	return newSizedHarness(t, accts, 80, 24)
}

func newSizedHarness(t *testing.T, accts []account.Account, width, height int) *harness {
	t.Helper()
	h := &harness{t: t, now: base.Add(10 * time.Second), vault: slices.Clone(accts)}
	h.m = New(Options{
		Accounts: slices.Clone(accts),
		Reload: func() ([]account.Account, bool, error) {
			h.reloads++
			if h.reloadFn != nil {
				return h.reloadFn()
			}
			return nil, false, nil
		},
		Mutate: h.mutate,
		Unlock: func(p string) ([]account.Account, error) {
			h.unlockCalls = append(h.unlockCalls, p)
			if h.unlockFn != nil {
				return h.unlockFn(p)
			}
			return slices.Clone(h.vault), nil
		},
		Copy: func(s string) error {
			if h.copyErr != nil {
				return h.copyErr
			}
			h.copied = append(h.copied, s)
			return nil
		},
		ClearCopy: func(s string) error {
			h.cleared = append(h.cleared, s)
			return nil
		},
		Now: func() time.Time { return h.now },
	})
	h.m.schedule = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
	h.send(tea.WindowSizeMsg{Width: width, Height: height})
	return h
}

func (h *harness) mutate(c Change) (Result, error) {
	h.changes = append(h.changes, c)
	if h.mutateErr != nil {
		return Result{}, h.mutateErr
	}
	var res Result
	i := indexOf(h.vault, c.ID)
	switch c.Kind {
	case ChangeAdd:
		a := totpAcct(fmt.Sprintf("new%d", len(h.vault)), c.Add.Issuer, c.Add.Account, "new")
		if a.Issuer == "" {
			a.Issuer = "Mail"
		}
		h.vault = append(h.vault, a)
		res.IDs = []string{a.ID}
	case ChangeEdit:
		a := &h.vault[i]
		for _, f := range []struct {
			dst *string
			src *string
		}{{&a.Issuer, c.Edit.Issuer}, {&a.Account, c.Edit.Account}, {&a.Handle, c.Edit.Alias}, {&a.Folder, c.Edit.Folder}, {&a.Note, c.Edit.Note}} {
			if f.src != nil {
				*f.dst = *f.src
			}
		}
		if c.Edit.Tags != nil {
			a.Tags = nil
			for _, t := range strings.Split(*c.Edit.Tags, ",") {
				if t = strings.TrimSpace(t); t != "" {
					a.Tags = append(a.Tags, t)
				}
			}
		}
	case ChangePin:
		h.vault[i].Pinned = c.Pinned
	case ChangeTrash:
		h.vault[i].DeletedAt = h.now.Unix()
	case ChangeRestore:
		h.vault[i].DeletedAt = 0
	case ChangePurge:
		h.vault = slices.Delete(h.vault, i, i+1)
	case ChangeAdvance:
		h.vault[i].Counter = c.Counter
	}
	res.Accounts = slices.Clone(h.vault)
	return res, nil
}

func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	next, cmd := h.m.Update(msg)
	h.m = next.(Model)
	h.run(cmd)
}

func (h *harness) run(cmd tea.Cmd) {
	h.t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case tea.QuitMsg:
			h.quit = true
		default:
			next, more := h.m.Update(msg)
			h.m = next.(Model)
			queue = append(queue, more)
		}
	}
}

func key(s string) tea.KeyPressMsg {
	special := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "up": tea.KeyUp, "down": tea.KeyDown,
		"left": tea.KeyLeft, "right": tea.KeyRight, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
		"home": tea.KeyHome, "end": tea.KeyEnd, "backspace": tea.KeyBackspace, "delete": tea.KeyDelete,
		"tab": tea.KeyTab,
	}
	switch {
	case s == "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case s == "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case strings.HasPrefix(s, "ctrl+"):
		return tea.KeyPressMsg{Code: rune(s[len("ctrl+")]), Mod: tea.ModCtrl}
	}
	if r, ok := special[s]; ok {
		return tea.KeyPressMsg{Code: r}
	}
	r, _ := utf8.DecodeRuneInString(s)
	return tea.KeyPressMsg{Code: r, Text: s}
}

func (h *harness) press(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		h.send(key(k))
	}
}

func (h *harness) typeText(s string) {
	h.t.Helper()
	for _, r := range s {
		if r == ' ' {
			h.send(key("space"))
			continue
		}
		h.send(key(string(r)))
	}
}

func (h *harness) paste(s string) {
	h.t.Helper()
	h.send(tea.PasteMsg{Content: s})
}

// at moves the clock to seconds into the window and lets the view tick.
func (h *harness) at(seconds int) {
	h.t.Helper()
	h.now = base.Add(time.Duration(seconds) * time.Second)
	h.send(tickMsg(h.now))
}

func (h *harness) lines() []string {
	return strings.Split(ansi.Strip(h.m.View().Content), "\n")
}

func (h *harness) screen() string { return strings.Join(h.lines(), "\n") }

func (h *harness) status() string {
	l := h.lines()
	return strings.TrimSpace(l[len(l)-2])
}

func (h *harness) selectedID() string {
	a, _ := h.m.selected()
	return a.ID
}

func (h *harness) rowIDs() []string {
	var ids []string
	for _, a := range h.m.rows() {
		ids = append(ids, a.ID)
	}
	return ids
}

// selectID moves the cursor to the account id by key presses.
func (h *harness) selectID(id string) {
	h.t.Helper()
	h.press("home")
	for range len(h.m.rows()) {
		if h.selectedID() == id {
			return
		}
		h.press("down")
	}
	if h.selectedID() != id {
		h.t.Fatalf("cannot select %s in %v", id, h.rowIDs())
	}
}

func (h *harness) codeOf(id string, at time.Time) string {
	h.t.Helper()
	for _, a := range h.vault {
		if a.ID == id {
			c, err := code.For(a, at)
			if err != nil {
				h.t.Fatal(err)
			}
			return c
		}
	}
	h.t.Fatalf("no account %s", id)
	return ""
}

func (h *harness) mustContain(want ...string) {
	h.t.Helper()
	s := h.screen()
	for _, w := range want {
		if !strings.Contains(s, w) {
			h.t.Errorf("screen lacks %q:\n%s", w, s)
		}
	}
}

func (h *harness) mustNotContain(unwanted ...string) {
	h.t.Helper()
	s := h.screen()
	for _, w := range unwanted {
		if strings.Contains(s, w) {
			h.t.Errorf("screen shows %q:\n%s", w, s)
		}
	}
}

func lastChange(t *testing.T, h *harness) Change {
	t.Helper()
	if len(h.changes) == 0 {
		t.Fatal("no change written")
	}
	return h.changes[len(h.changes)-1]
}
