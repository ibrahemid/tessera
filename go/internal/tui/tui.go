// Package tui implements `tess watch`: the interactive terminal view of the
// vault, with live codes, search, filters, copy, editing and the Trash.
package tui

import (
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/clipboard"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"golang.org/x/term"
)

// ErrKeyReplaced reports a vault whose key changed since the view unlocked it.
// The view locks and unlocks again.
var ErrKeyReplaced = errors.New("the vault key changed; enter the passphrase again")

// ErrPassphraseNeeded reports an unlock without a passphrase to use: none in
// the environment and none remembered. The view asks for one.
var ErrPassphraseNeeded = errors.New("enter the passphrase to unlock the vault")

// ClearFunc schedules the clipboard clear for a code the view just copied.
type ClearFunc func(code string) error

// ChangeKind names one write the view asks for.
type ChangeKind int

const (
	ChangeAdd ChangeKind = iota + 1
	ChangeEdit
	ChangePin
	ChangeTrash
	ChangeRestore
	ChangePurge
	ChangeAdvance
)

// Change is one write to the vault. The writer re-reads the vault under its
// lock and applies the change to the accounts it finds there.
type Change struct {
	Kind ChangeKind
	// ID is the account the change applies to; empty for ChangeAdd.
	ID string
	// Pinned is the new pin state for ChangePin.
	Pinned bool
	// Counter is the HOTP counter ChangeAdvance moves to; the write fails when
	// the stored counter is no longer the one before it.
	Counter int64
	Edit    Edit
	Add     Add
	// Snapshot is the account as the user saw it when confirming ChangePurge;
	// the write fails when the stored account differs.
	Snapshot account.Account
}

// Edit holds the fields an edit changes; nil leaves a field as it is.
type Edit struct {
	Issuer  *string
	Account *string
	Alias   *string
	Folder  *string
	Tags    *string
	Note    *string
}

// Add is an account to add from an otpauth link or a setup key.
type Add struct {
	Text    string
	Issuer  string
	Account string
}

// Result is the vault after a change.
type Result struct {
	// Accounts is every account in the vault, the Trash included.
	Accounts []account.Account
	// IDs are the accounts ChangeAdd added.
	IDs []string
	// Skipped counts accounts ChangeAdd found already in the vault.
	Skipped int
	// Problems describe accounts ChangeAdd could not add.
	Problems []string
}

// Options connects the view to the vault.
type Options struct {
	// Accounts is every account in the vault, the Trash included.
	Accounts []account.Account
	// Reload returns the vault's accounts when the file changed since the last
	// call, and changed = false otherwise.
	Reload func() (accounts []account.Account, changed bool, err error)
	// Mutate writes a change through the vault's locked write path.
	Mutate func(Change) (Result, error)
	// Unlock opens the locked vault with a typed passphrase. Without it the
	// view never locks.
	Unlock func(passphrase string) ([]account.Account, error)
	// UnlockStored opens the locked vault without asking, or returns
	// ErrPassphraseNeeded; nil always asks.
	UnlockStored func() ([]account.Account, error)
	// Lock runs when the view locks: the vault drops the key it holds.
	Lock func()
	// LockAfter is how long the view waits for a key press before it locks;
	// zero never locks.
	LockAfter time.Duration
	// Copy places text on the clipboard; nil uses the detected clipboard.
	Copy func(string) error
	// ClearCopy, when not nil, runs after each successful copy.
	ClearCopy ClearFunc
	// Now is the clock; nil uses time.Now.
	Now func() time.Time
}

const (
	tickInterval = 250 * time.Millisecond
	pollInterval = time.Second
	statusFor    = 4 * time.Second
	// freshFloor is the seconds left under which Enter copies the next code.
	freshFloor = 3
)

type mode int

const (
	modeList mode = iota
	modeHelp
	modeFolders
	modeTags
	modeEdit
	modeAdd
	modeTrash
	modeLocked
)

type lockReason int

const (
	lockIdle lockReason = iota
	lockRekeyed
)

type statusKind int

const (
	statusNone statusKind = iota
	statusOK
	statusFail
	statusInfo
)

type confirmation struct {
	question  string
	cancelled string
	change    Change
	danger    bool
}

type picker struct {
	items  []string
	cursor int
}

type form struct {
	id      string
	fields  []field
	initial []string
	focus   int
}

type (
	tickMsg       time.Time
	pollMsg       time.Time
	reloadDoneMsg struct {
		gen      int
		accounts []account.Account
		changed  bool
		err      error
	}
	mutateDoneMsg struct {
		change Change
		label  string
		result Result
		err    error
	}
	copyDoneMsg struct {
		what     string
		err      error
		clearErr error
	}
	unlockDoneMsg struct {
		gen      int
		stored   bool
		accounts []account.Account
		err      error
	}
)

// Model is the bubbletea model for the watch view.
type Model struct {
	opts     Options
	clock    func() time.Time
	schedule func(time.Duration, func(time.Time) tea.Msg) tea.Cmd

	accounts []account.Account
	theme    ui.Theme
	now      time.Time
	width    int
	height   int

	mode      mode
	helpFrom  mode
	searching bool
	search    field
	folder    string
	tag       string
	hidden    bool

	cursor     int
	offset     int
	selectedID string

	trashCursor int
	trashOffset int
	helpOffset  int

	picker  picker
	form    form
	pass    field
	confirm *confirmation

	// lastInput is the last key press or paste; the countdown does not count.
	lastInput time.Time
	locked    lockReason
	// asking shows the passphrase field on the locked screen.
	asking bool

	lastTrashed string
	// gen counts the writes, locks and unlocks this view made; a reload or an
	// unlock that started before the latest one is dropped.
	gen       int
	busy      bool
	reloading bool
	unlocking bool
	reloadErr string

	status      string
	statusKind  statusKind
	statusUntil time.Time
}

// New builds the watch model.
func New(opts Options) Model {
	if opts.Copy == nil {
		opts.Copy = writeClipboard
	}
	clock := opts.Now
	if clock == nil {
		clock = time.Now
	}
	m := Model{
		opts:     opts,
		clock:    clock,
		schedule: tea.Tick,
		accounts: opts.Accounts,
		theme:    ui.NewTheme(true),
		now:      clock(),
		width:    80,
		height:   24,
		search:   newField("", ""),
		pass:     field{label: "Passphrase", masked: true, verbatim: true},
	}
	m.lastInput = m.now
	m.syncList()
	return m
}

// ErrNoTerminal reports a view started without a terminal on stdin and stdout.
var ErrNoTerminal = errors.New("watch needs a terminal")

// Run starts the watch view and returns when the user quits.
func Run(opts Options) error {
	return run(opts, os.Stdin, os.Stdout, os.Environ())
}

func run(opts Options, in, out *os.File, env []string) error {
	if !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return ErrNoTerminal
	}
	_, err := tea.NewProgram(New(opts), programOptions(in, out, env)...).Run()
	return err
}

func programOptions(in io.Reader, out io.Writer, env []string) []tea.ProgramOption {
	return []tea.ProgramOption{
		tea.WithInput(in),
		tea.WithOutput(out),
		tea.WithEnvironment(env),
		tea.WithColorProfile(ui.ViewProfile(out, env)),
	}
}

func writeClipboard(text string) error {
	b, err := clipboard.Detect()
	if err != nil {
		return err
	}
	return b.Write(text, true)
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.tick(), m.poll())
}

func (m Model) tick() tea.Cmd {
	return m.schedule(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) poll() tea.Cmd {
	if m.opts.Reload == nil {
		return nil
	}
	return m.schedule(pollInterval, func(t time.Time) tea.Msg { return pollMsg(t) })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.syncList()
		m.syncTrash()
	case tea.BackgroundColorMsg:
		m.theme = ui.NewTheme(msg.IsDark())
	case tickMsg:
		m.now = m.clock()
		if m.idle(m.now) {
			m.lock(lockIdle)
		}
		if m.status != "" && !m.now.Before(m.statusUntil) {
			m.clearStatus()
		}
		return m, m.tick()
	case pollMsg:
		if m.reloading || m.busy || m.mode == modeLocked {
			return m, m.poll()
		}
		m.reloading = true
		return m, tea.Batch(m.reloadCmd(), m.poll())
	case reloadDoneMsg:
		return m.reloaded(msg)
	case mutateDoneMsg:
		return m.mutated(msg)
	case copyDoneMsg:
		m.copied(msg)
	case unlockDoneMsg:
		m.unlocked(msg)
	case tea.PasteMsg:
		if at := m.clock(); m.idle(at) {
			m.lock(lockIdle)
		} else {
			m.lastInput = at
			m.pasted(msg.Content)
		}
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) reloadCmd() tea.Cmd {
	reload, gen := m.opts.Reload, m.gen
	return func() tea.Msg {
		accts, changed, err := reload()
		return reloadDoneMsg{gen: gen, accounts: accts, changed: changed, err: err}
	}
}

func (m Model) reloaded(msg reloadDoneMsg) (tea.Model, tea.Cmd) {
	m.reloading = false
	if msg.gen != m.gen {
		return m, nil
	}
	if msg.err != nil {
		if errors.Is(msg.err, ErrKeyReplaced) {
			if m.opts.Unlock == nil {
				m.fail(ErrKeyReplaced.Error())
			} else {
				m.lock(lockRekeyed)
			}
			return m, nil
		}
		if text := msg.err.Error(); text != m.reloadErr {
			m.reloadErr = text
			m.fail("Could not read the vault: " + ui.Sanitize(text))
		}
		return m, nil
	}
	m.reloadErr = ""
	if msg.changed {
		m.setAccounts(msg.accounts)
	}
	return m, nil
}

func (m *Model) setAccounts(accts []account.Account) {
	m.accounts = accts
	m.syncList()
	m.syncTrash()
}

func (m *Model) setStatus(kind statusKind, text string) {
	m.status, m.statusKind = text, kind
	m.statusUntil = m.now.Add(statusFor)
}

func (m *Model) clearStatus() { m.status, m.statusKind = "", statusNone }

func (m *Model) ok(text string)   { m.setStatus(statusOK, text) }
func (m *Model) fail(text string) { m.setStatus(statusFail, text) }
func (m *Model) info(text string) { m.setStatus(statusInfo, text) }

// idle reports whether LockAfter has passed at at since the last key press or
// paste. A view that cannot unlock never locks.
func (m Model) idle(at time.Time) bool {
	if m.opts.LockAfter <= 0 || m.opts.Unlock == nil || m.unlocking {
		return false
	}
	if m.mode == modeLocked && !m.asking {
		return false
	}
	return !at.Before(m.lastInput.Add(m.opts.LockAfter))
}

// lock drops the accounts and everything typed or open, an unanswered question
// included, and has the vault drop its key.
func (m *Model) lock(reason lockReason) {
	if m.opts.Lock != nil {
		m.opts.Lock()
	}
	if m.mode != modeLocked {
		m.locked = reason
	}
	m.mode = modeLocked
	m.asking, m.unlocking = false, false
	m.gen++
	m.accounts = nil
	m.searching = false
	m.search.wipe()
	m.folder, m.tag = "", ""
	m.cursor, m.offset, m.selectedID = 0, 0, ""
	m.trashCursor, m.trashOffset, m.helpOffset = 0, 0, 0
	m.picker = picker{}
	for i := range m.form.fields {
		m.form.fields[i].wipe()
	}
	m.form = form{}
	m.pass.wipe()
	m.confirm = nil
	m.lastTrashed = ""
	m.reloadErr = ""
	m.clearStatus()
}

// unlockCmd opens the vault with the stored passphrase, or with pass.
func (m Model) unlockCmd(stored bool, pass string) tea.Cmd {
	unlock, unlockStored, gen := m.opts.Unlock, m.opts.UnlockStored, m.gen
	return func() tea.Msg {
		var accts []account.Account
		var err error
		if stored {
			accts, err = unlockStored()
		} else {
			accts, err = unlock(pass)
		}
		return unlockDoneMsg{gen: gen, stored: stored, accounts: accts, err: err}
	}
}

func (m *Model) unlocked(msg unlockDoneMsg) {
	if msg.gen != m.gen || m.mode != modeLocked {
		return
	}
	m.unlocking = false
	if msg.err != nil {
		m.pass.wipe()
		m.asking = true
		if !msg.stored || !errors.Is(msg.err, ErrPassphraseNeeded) {
			m.fail("Not unlocked: " + ui.Sanitize(msg.err.Error()))
		}
		return
	}
	m.gen++
	m.mode = modeList
	m.asking = false
	m.lastInput = m.clock()
	m.setAccounts(msg.accounts)
	m.ok("Unlocked")
}

func (m Model) startMutate(c Change) (tea.Model, tea.Cmd) {
	if m.opts.Mutate == nil {
		m.fail("This view cannot change the vault")
		return m, nil
	}
	if m.busy {
		return m, nil
	}
	m.busy = true
	label := ""
	if a, ok := m.byID(c.ID); ok {
		label = labelOf(a)
	}
	mutate := m.opts.Mutate
	return m, func() tea.Msg {
		r, err := mutate(c)
		return mutateDoneMsg{change: c, label: label, result: r, err: err}
	}
}

func (m Model) mutated(msg mutateDoneMsg) (tea.Model, tea.Cmd) {
	m.busy = false
	if m.mode == modeLocked {
		return m, nil
	}
	if msg.err != nil {
		if errors.Is(msg.err, ErrKeyReplaced) && m.opts.Unlock != nil {
			m.lock(lockRekeyed)
			return m, nil
		}
		text := ui.Sanitize(msg.err.Error())
		if msg.change.Kind == ChangeAdvance {
			m.fail("No code generated: " + text)
		} else {
			m.fail("Not saved: " + text)
		}
		if m.opts.Reload != nil && !m.reloading {
			m.reloading = true
			return m, m.reloadCmd()
		}
		return m, nil
	}
	m.gen++
	m.setAccounts(msg.result.Accounts)
	c := msg.change
	switch c.Kind {
	case ChangeAdd:
		m.mode = modeList
		m.form = form{}
		m.addedStatus(msg.result)
	case ChangeEdit:
		m.mode = modeList
		m.form = form{}
		if a, ok := m.byID(c.ID); ok {
			m.ok("Saved " + labelOf(a))
		}
	case ChangePin:
		m.selectID(c.ID)
		if c.Pinned {
			m.ok("Pinned " + msg.label)
		} else {
			m.ok("Unpinned " + msg.label)
		}
	case ChangeTrash:
		m.lastTrashed = c.ID
		m.ok("Moved " + msg.label + " to the Trash. Press u to undo.")
	case ChangeRestore:
		if m.lastTrashed == c.ID {
			m.lastTrashed = ""
		}
		if m.mode == modeList {
			m.selectID(c.ID)
		}
		m.ok("Restored " + msg.label)
	case ChangePurge:
		m.ok("Deleted " + msg.label + " for good")
	case ChangeAdvance:
		a, ok := m.byID(c.ID)
		if !ok {
			m.fail("No code generated: the account is no longer in the vault")
			return m, nil
		}
		code, err := codeAt(a, m.now)
		if err != nil {
			m.fail("No code generated: " + ui.Sanitize(err.Error()))
			return m, nil
		}
		return m, m.copyCmd(code, "Generated code #"+itoa(a.Counter)+" for "+labelOf(a)+" and copied it")
	}
	return m, nil
}

func (m *Model) addedStatus(r Result) {
	var parts []string
	switch len(r.IDs) {
	case 0:
	case 1:
		if a, ok := m.byID(r.IDs[0]); ok {
			parts = append(parts, "Added "+labelOf(a))
		}
	default:
		parts = append(parts, "Added "+itoa(int64(len(r.IDs)))+" accounts")
	}
	if len(r.IDs) > 0 {
		m.selectID(r.IDs[0])
	}
	if r.Skipped > 0 {
		parts = append(parts, plural(r.Skipped, "account")+" already in the vault")
	}
	if len(r.Problems) > 0 {
		parts = append(parts, "not added: "+ui.Sanitize(strings.Join(r.Problems, "; ")))
	}
	text := capitalize(strings.Join(parts, "; "))
	switch {
	case len(r.IDs) > 0:
		m.ok(text)
	case len(r.Problems) > 0:
		m.fail(text)
	default:
		m.info(text)
	}
}

func (m Model) copyCmd(code, what string) tea.Cmd {
	write, clear := m.opts.Copy, m.opts.ClearCopy
	return func() tea.Msg {
		if err := write(code); err != nil {
			return copyDoneMsg{what: what, err: err}
		}
		var clearErr error
		if clear != nil {
			clearErr = clear(code)
		}
		return copyDoneMsg{what: what, clearErr: clearErr}
	}
}

func (m *Model) copied(msg copyDoneMsg) {
	switch {
	case m.mode == modeLocked:
	case msg.err != nil:
		m.fail("Copy failed: " + ui.Sanitize(msg.err.Error()))
	case msg.clearErr != nil:
		m.fail(msg.what + "; " + ui.Sanitize(msg.clearErr.Error()))
	default:
		m.ok(msg.what)
	}
}

func (m *Model) pasted(text string) {
	switch {
	case m.mode == modeLocked:
		if m.asking && !m.unlocking {
			m.pass.paste(text)
		}
	case m.mode == modeEdit || m.mode == modeAdd:
		m.form.fields[m.form.focus].paste(text)
	case m.mode == modeList && m.searching:
		m.search.paste(text)
		m.cursor, m.offset = 0, 0
		m.syncList()
	}
}

func (m Model) byID(id string) (account.Account, bool) {
	if id == "" {
		return account.Account{}, false
	}
	for _, a := range m.accounts {
		if a.ID == id {
			return a, true
		}
	}
	return account.Account{}, false
}

// liveSorted is every live account in display order, pinned first.
func (m Model) liveSorted() []account.Account {
	live := account.Live(m.accounts)
	account.SortForDisplay(live)
	sort.SliceStable(live, func(i, j int) bool { return live[i].Pinned && !live[j].Pinned })
	return live
}

// trashSorted is the Trash, newest first.
func (m Model) trashSorted() []account.Account {
	trashed := account.Trashed(m.accounts)
	account.SortForDisplay(trashed)
	sort.SliceStable(trashed, func(i, j int) bool { return trashed[i].DeletedAt > trashed[j].DeletedAt })
	return trashed
}

func (m Model) filtersActive() bool {
	return m.search.String() != "" || m.folder != "" || m.tag != ""
}

// rows is the list as filtered by the search, folder and tag.
func (m Model) rows() []account.Account {
	live := m.liveSorted()
	if !m.filtersActive() {
		return live
	}
	terms := strings.Fields(strings.ToLower(m.search.String()))
	out := live[:0]
	for _, a := range live {
		if m.folder != "" && a.Folder != m.folder {
			continue
		}
		if m.tag != "" && !hasTag(a, m.tag) {
			continue
		}
		if !matchesTerms(a, terms) {
			continue
		}
		out = append(out, a)
	}
	return out
}

func hasTag(a account.Account, tag string) bool {
	for _, t := range a.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

func matchesTerms(a account.Account, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	fields := append([]string{a.Issuer, a.Account, a.Handle, a.Folder}, a.Tags...)
	hay := strings.ToLower(strings.Join(fields, "\n"))
	for _, t := range terms {
		if !strings.Contains(hay, t) {
			return false
		}
	}
	return true
}

func (m Model) selected() (account.Account, bool) {
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return account.Account{}, false
	}
	return rows[m.cursor], true
}

func (m Model) selectedTrash() (account.Account, bool) {
	rows := m.trashSorted()
	if m.trashCursor < 0 || m.trashCursor >= len(rows) {
		return account.Account{}, false
	}
	return rows[m.trashCursor], true
}

// syncList keeps the selected account selected after the list changes, or
// keeps the cursor in range when it is gone, and scrolls it into view.
func (m *Model) syncList() {
	rows := m.rows()
	if i := indexOf(rows, m.selectedID); i >= 0 {
		m.cursor = i
	}
	m.cursor = clamp(m.cursor, 0, len(rows)-1)
	m.selectedID = ""
	if m.cursor < len(rows) {
		m.selectedID = rows[m.cursor].ID
	}
	m.offset = scrollTo(m.cursor, m.offset, len(rows), m.bodyHeight())
}

func (m *Model) syncTrash() {
	n := len(m.trashSorted())
	m.trashCursor = clamp(m.trashCursor, 0, n-1)
	m.trashOffset = scrollTo(m.trashCursor, m.trashOffset, n, m.bodyHeight())
}

func (m *Model) selectID(id string) {
	m.selectedID = id
	m.syncList()
}

func (m *Model) moveList(delta int) {
	rows := m.rows()
	m.cursor = clamp(m.cursor+delta, 0, len(rows)-1)
	m.selectedID = ""
	if m.cursor < len(rows) {
		m.selectedID = rows[m.cursor].ID
	}
	m.offset = scrollTo(m.cursor, m.offset, len(rows), m.bodyHeight())
}

func (m *Model) moveTrash(delta int) {
	m.trashCursor += delta
	m.syncTrash()
}

func indexOf(accts []account.Account, id string) int {
	if id == "" {
		return -1
	}
	for i, a := range accts {
		if a.ID == id {
			return i
		}
	}
	return -1
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return min(max(v, lo), hi)
}

// scrollTo returns the first visible row so that cursor is on screen in a
// window of height rows over n.
func scrollTo(cursor, offset, n, height int) int {
	height = max(height, 1)
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+height {
		offset = cursor - height + 1
	}
	return clamp(offset, 0, max(n-height, 0))
}
