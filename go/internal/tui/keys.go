package tui

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/code"
	"github.com/ibrahemid/tessera/go/internal/ui"
)

func (m Model) handleKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := k.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	at := m.clock()
	if m.idle(at) {
		m.lock(lockIdle)
		return m, nil
	}
	m.lastInput = at
	if m.confirm != nil {
		return m.answer(key)
	}
	switch m.mode {
	case modeLocked:
		return m.lockedKey(k)
	case modeHelp:
		return m.helpKey(key)
	case modeFolders, modeTags:
		return m.pickerKey(key)
	case modeEdit, modeAdd:
		return m.formKey(k)
	case modeTrash:
		return m.trashKey(key)
	}
	if m.searching {
		return m.searchKey(k)
	}
	return m.listKey(key)
}

func (m Model) answer(key string) (tea.Model, tea.Cmd) {
	c := m.confirm
	m.confirm = nil
	switch key {
	case "y", "Y":
		return m.startMutate(c.change)
	case "q":
		return m, tea.Quit
	}
	m.info(c.cancelled)
	return m, nil
}

func (m Model) listKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.moveList(-1)
	case "down", "j":
		m.moveList(1)
	case "pgup":
		m.moveList(-m.bodyHeight())
	case "pgdown":
		m.moveList(m.bodyHeight())
	case "home", "g":
		m.moveList(-len(m.accounts))
	case "end", "G":
		m.moveList(len(m.accounts))
	case "enter", "c":
		return m.copySelected(false)
	case "n":
		return m.copySelected(true)
	case "/":
		m.searching = true
		m.search.cursor = len(m.search.value)
	case "esc":
		switch {
		case m.search.String() != "":
			m.clearSearch()
		case m.folder != "" || m.tag != "":
			m.folder, m.tag = "", ""
			m.syncList()
		default:
			return m, tea.Quit
		}
	case "f":
		m.openPicker(modeFolders)
	case "t":
		m.openPicker(modeTags)
	case "h":
		m.hidden = !m.hidden
	case "?":
		m.openHelp()
	case "a":
		m.openAdd()
	case "e":
		m.openEdit()
	case "p":
		if a, ok := m.selected(); ok {
			return m.startMutate(Change{Kind: ChangePin, ID: a.ID, Pinned: !a.Pinned})
		}
	case "d":
		if a, ok := m.selected(); ok {
			return m.startMutate(Change{Kind: ChangeTrash, ID: a.ID})
		}
	case "u":
		return m.undoTrash()
	case "T":
		m.mode = modeTrash
		m.syncTrash()
	}
	return m, nil
}

func (m *Model) clearSearch() {
	m.search.set("")
	m.searching = false
	m.syncList()
}

func (m Model) searchKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "up":
		m.moveList(-1)
	case "down":
		m.moveList(1)
	case "pgup":
		m.moveList(-m.bodyHeight())
	case "pgdown":
		m.moveList(m.bodyHeight())
	case "enter":
		m.searching = false
		return m.copySelected(false)
	case "esc":
		m.clearSearch()
	case "backspace", "ctrl+h":
		if len(m.search.value) == 0 {
			m.searching = false
			return m, nil
		}
		m.editSearch(k)
	default:
		m.editSearch(k)
	}
	return m, nil
}

func (m *Model) editSearch(k tea.KeyPressMsg) {
	before := m.search.String()
	if !m.search.handle(k) || m.search.String() == before {
		return
	}
	m.cursor, m.offset, m.selectedID = 0, 0, ""
	m.syncList()
}

func (m Model) copySelected(next bool) (tea.Model, tea.Cmd) {
	a, ok := m.selected()
	if !ok {
		return m, nil
	}
	m.now = m.clock()
	name := labelOf(a)
	if a.Type == account.HOTP {
		if !next {
			c, err := code.For(a, m.now)
			if err != nil {
				m.fail("No code: " + ui.Sanitize(err.Error()))
				return m, nil
			}
			return m, m.copyCmd(c, "Copied code #"+itoa(a.Counter)+" for "+name)
		}
		counter, err := account.NextCounter(a.Counter)
		if err != nil {
			m.fail("No code generated: " + ui.Sanitize(err.Error()))
			return m, nil
		}
		m.confirm = &confirmation{
			question:  "Generate the next code for " + name + "? The counter moves to " + itoa(counter) + ".",
			change:    Change{Kind: ChangeAdvance, ID: a.ID, Counter: counter},
			cancelled: "No code generated",
		}
		return m, nil
	}
	rem := code.Remaining(a, m.now)
	if !next && rem >= freshFloor {
		c, err := code.For(a, m.now)
		if err != nil {
			m.fail("No code: " + ui.Sanitize(err.Error()))
			return m, nil
		}
		return m, m.copyCmd(c, "Copied the code for "+name)
	}
	c, err := code.For(a, time.Unix(m.now.Unix()+int64(rem), 0))
	if err != nil {
		m.fail("No code: " + ui.Sanitize(err.Error()))
		return m, nil
	}
	what := "Copied the next code for " + name + "; it starts in " + itoa(int64(rem)) + "s"
	if !next {
		what = "Copied the next code for " + name + "; the current one ends in " + itoa(int64(rem)) + "s"
	}
	return m, m.copyCmd(c, what)
}

func (m Model) undoTrash() (tea.Model, tea.Cmd) {
	a, ok := m.byID(m.lastTrashed)
	if !ok || !a.Trashed() {
		m.lastTrashed = ""
		m.info("Nothing to undo")
		return m, nil
	}
	return m.startMutate(Change{Kind: ChangeRestore, ID: a.ID})
}

func (m *Model) openHelp() {
	m.helpFrom = m.mode
	m.mode = modeHelp
	m.helpOffset = 0
}

func (m Model) helpKey(key string) (tea.Model, tea.Cmd) {
	maxOffset := max(len(m.helpRows())-m.bodyHeight(), 0)
	switch key {
	case "q":
		return m, tea.Quit
	case "esc", "?":
		m.mode = m.helpFrom
	case "up", "k":
		m.helpOffset--
	case "down", "j":
		m.helpOffset++
	case "pgup":
		m.helpOffset -= m.bodyHeight()
	case "pgdown":
		m.helpOffset += m.bodyHeight()
	case "home", "g":
		m.helpOffset = 0
	case "end", "G":
		m.helpOffset = maxOffset
	}
	m.helpOffset = clamp(m.helpOffset, 0, maxOffset)
	return m, nil
}

func (m *Model) openPicker(md mode) {
	values := m.distinct(md == modeTags)
	current := m.folder
	if md == modeTags {
		current = m.tag
	}
	if len(values) == 0 && current == "" {
		if md == modeTags {
			m.info("No tags yet. Add them with e.")
		} else {
			m.info("No folders yet. Set one with e.")
		}
		return
	}
	if current != "" && !contains(values, current) {
		values = append(values, current)
	}
	all := "All folders"
	if md == modeTags {
		all = "All tags"
	}
	m.picker = picker{items: append([]string{all}, values...)}
	for i, v := range values {
		if v == current {
			m.picker.cursor = i + 1
		}
	}
	m.mode = md
}

// distinct is every folder (or tag) on a live account, sorted.
func (m Model) distinct(tags bool) []string {
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, a := range account.Live(m.accounts) {
		if tags {
			for _, t := range a.Tags {
				add(t)
			}
			continue
		}
		add(a.Folder)
	}
	sort.Slice(out, func(i, j int) bool {
		li, lj := strings.ToLower(out[i]), strings.ToLower(out[j])
		if li != lj {
			return li < lj
		}
		return out[i] < out[j]
	})
	return out
}

func contains(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

func (m Model) pickerKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m, tea.Quit
	case "esc":
		m.mode = modeList
	case "up", "k":
		m.picker.cursor = clamp(m.picker.cursor-1, 0, len(m.picker.items)-1)
	case "down", "j":
		m.picker.cursor = clamp(m.picker.cursor+1, 0, len(m.picker.items)-1)
	case "pgup":
		m.picker.cursor = clamp(m.picker.cursor-m.bodyHeight(), 0, len(m.picker.items)-1)
	case "pgdown":
		m.picker.cursor = clamp(m.picker.cursor+m.bodyHeight(), 0, len(m.picker.items)-1)
	case "home", "g":
		m.picker.cursor = 0
	case "end", "G":
		m.picker.cursor = len(m.picker.items) - 1
	case "enter":
		value := ""
		if m.picker.cursor > 0 {
			value = m.picker.items[m.picker.cursor]
		}
		if m.mode == modeTags {
			m.tag = value
		} else {
			m.folder = value
		}
		m.mode = modeList
		m.cursor, m.offset = 0, 0
		m.syncList()
	}
	return m, nil
}

func (m *Model) openAdd() {
	link := newField("Link or setup key", "")
	link.masked = true
	m.form = form{fields: []field{link, newField("Issuer", ""), newField("Account", "")}}
	m.form.initial = []string{"", "", ""}
	m.mode = modeAdd
	m.clearStatus()
}

const (
	editIssuer = iota
	editAccount
	editAlias
	editFolder
	editTags
	editNote
)

func (m *Model) openEdit() {
	a, ok := m.selected()
	if !ok {
		return
	}
	note := newField("Note", a.Note)
	note.multiline = true
	m.form = form{id: a.ID, fields: []field{
		newField("Issuer", a.Issuer),
		newField("Account", a.Account),
		newField("Alias", a.Handle),
		newField("Folder", a.Folder),
		newField("Tags", strings.Join(a.Tags, ", ")),
		note,
	}}
	for _, f := range m.form.fields {
		m.form.initial = append(m.form.initial, f.String())
	}
	m.mode = modeEdit
	m.clearStatus()
}

func (m Model) formKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	n := len(m.form.fields)
	switch k.String() {
	case "esc":
		m.mode = modeList
		m.form = form{}
		return m, nil
	case "tab", "down":
		m.form.focus = (m.form.focus + 1) % n
		return m, nil
	case "shift+tab", "up":
		m.form.focus = (m.form.focus + n - 1) % n
		return m, nil
	case "enter":
		if m.mode == modeAdd {
			return m.submitAdd()
		}
		return m.submitEdit()
	}
	m.form.fields[m.form.focus].handle(k)
	return m, nil
}

func (m Model) submitAdd() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.form.fields[0].String())
	if text == "" {
		m.form.focus = 0
		m.fail("Paste an otpauth link or a setup key")
		return m, nil
	}
	return m.startMutate(Change{Kind: ChangeAdd, Add: Add{
		Text:    text,
		Issuer:  strings.TrimSpace(m.form.fields[1].String()),
		Account: strings.TrimSpace(m.form.fields[2].String()),
	}})
}

func (m Model) submitEdit() (tea.Model, tea.Cmd) {
	var e Edit
	changed := false
	targets := []**string{&e.Issuer, &e.Account, &e.Alias, &e.Folder, &e.Tags, &e.Note}
	for i, f := range m.form.fields {
		v := f.String()
		if v == m.form.initial[i] {
			continue
		}
		*targets[i] = &v
		changed = true
	}
	if !changed {
		m.mode = modeList
		m.form = form{}
		m.info("Nothing changed")
		return m, nil
	}
	return m.startMutate(Change{Kind: ChangeEdit, ID: m.form.id, Edit: e})
}

func (m Model) trashKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m, tea.Quit
	case "esc", "T":
		m.mode = modeList
		m.syncList()
	case "up", "k":
		m.moveTrash(-1)
	case "down", "j":
		m.moveTrash(1)
	case "pgup":
		m.moveTrash(-m.bodyHeight())
	case "pgdown":
		m.moveTrash(m.bodyHeight())
	case "home", "g":
		m.moveTrash(-len(m.accounts))
	case "end", "G":
		m.moveTrash(len(m.accounts))
	case "?":
		m.openHelp()
	case "r":
		if a, ok := m.selectedTrash(); ok {
			return m.startMutate(Change{Kind: ChangeRestore, ID: a.ID})
		}
	case "d":
		if a, ok := m.selectedTrash(); ok {
			m.confirm = &confirmation{
				question:  "Delete " + fullLabel(a) + " for good? Its secret is removed from the vault.",
				change:    Change{Kind: ChangePurge, ID: a.ID, Snapshot: a},
				cancelled: "Nothing deleted",
				danger:    true,
			}
		}
	}
	return m, nil
}

// lockedKey unlocks on any key: with the stored passphrase when there is one,
// else with the one typed into the field the first key shows.
func (m Model) lockedKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.unlocking {
		return m, nil
	}
	if !m.asking {
		if m.opts.UnlockStored == nil {
			m.asking = true
			return m, nil
		}
		m.unlocking = true
		return m, m.unlockCmd(true, "")
	}
	switch k.String() {
	case "esc":
		return m, tea.Quit
	case "enter":
		pass := m.pass.String()
		if pass == "" {
			return m, nil
		}
		m.unlocking = true
		return m, m.unlockCmd(false, pass)
	}
	m.pass.handle(k)
	return m, nil
}
