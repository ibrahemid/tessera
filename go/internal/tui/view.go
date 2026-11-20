package tui

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/code"
	"github.com/ibrahemid/tessera/go/internal/ui"
)

const (
	barWidth = 10
	// minNameWidth is the narrowest name column before the bar is dropped.
	minNameWidth = 12
	trashDate    = "2006-01-02"
)

func (m Model) View() tea.View {
	v := tea.NewView(m.frame())
	v.AltScreen = true
	return v
}

func (m Model) hasGap() bool { return m.height >= 16 }

// layout is the frame split into its parts. The header, context and status
// take extra lines only while the body keeps at least one.
type layout struct {
	header, context, status []string
	body                    int
}

func (m Model) layout() layout {
	spare := m.height - 5
	if m.hasGap() {
		spare -= 2
	}
	fit := func(lines []string, most int) []string {
		if len(lines) == 0 {
			return []string{""}
		}
		n := min(len(lines), 1+max(min(most, spare), 0))
		spare -= n - 1
		if n < len(lines) {
			lines[n-1] = ansi.Truncate(lines[n-1], m.width-1, "") + "…"
		}
		return lines[:n]
	}
	var l layout
	l.status = fit(m.statusLines(), 2)
	l.header = fit(m.headerLines(), 1)
	l.context = fit(m.contextLines(), 1)
	l.body = max(spare+1, 1)
	return l
}

// bodyHeight is the number of lines between the header and the status line.
func (m Model) bodyHeight() int { return m.layout().body }

func (m Model) frame() string {
	l := m.layout()
	lines := append(l.header, l.context...)
	if m.hasGap() {
		lines = append(lines, "")
	}
	body := m.body(l.body)
	if len(body) > l.body {
		body = body[:l.body]
	}
	for len(body) < l.body {
		body = append(body, "")
	}
	lines = append(lines, body...)
	if m.hasGap() {
		lines = append(lines, "")
	}
	lines = append(lines, l.status...)
	lines = append(lines, m.hintLine())
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, m.width, "")
	}
	return strings.Join(lines, "\n")
}

// flow lays styled parts out left to right, starting a new line when the
// next part does not fit in the width.
func (m Model) flow(parts []string, sep string) []string {
	var lines []string
	line, used := "", 0
	for _, p := range parts {
		w := ansi.StringWidth(p)
		switch {
		case used == 0:
			line, used = p, w
		case used+len(sep)+w <= m.width:
			line += sep + p
			used += len(sep) + w
		default:
			lines = append(lines, line)
			line, used = p, w
		}
	}
	if used > 0 {
		lines = append(lines, line)
	}
	return lines
}

// wrap breaks plain text into lines of at most width cells.
func wrap(text string, width int) []string {
	lines := strings.Split(ansi.Wrap(text, max(width, 1), ""), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return lines
}

// headerLines is the title and the counts on one line. Counts that do not fit
// whole are left out from the last one back.
func (m Model) headerLines() []string {
	t := m.theme
	line := t.Title.Render("Tessera")
	if m.mode == modeLocked {
		return []string{line}
	}
	live := len(account.Live(m.accounts))
	count := plural(live, "account")
	if m.filtersActive() {
		count = itoa(int64(len(m.rows()))) + " of " + count
	}
	parts := []string{count}
	if n := len(account.Trashed(m.accounts)); n > 0 {
		parts = append(parts, itoa(int64(n))+" in the Trash")
	}
	if m.hidden {
		parts = append(parts, "Codes hidden")
	}
	used := ansi.StringWidth(line)
	for _, p := range parts {
		w := 2 + ansi.StringWidth(p)
		if used+w > m.width {
			break
		}
		line += "  " + t.Subtle.Render(p)
		used += w
	}
	return []string{line}
}

func (m Model) contextLines() []string {
	t := m.theme
	section := func(s string) []string { return []string{t.Section.Render(s)} }
	switch m.mode {
	case modeHelp:
		return section("Keys")
	case modeFolders:
		return section("Folder")
	case modeTags:
		return section("Tag")
	case modeAdd:
		return section("Add an account")
	case modeEdit:
		if a, ok := m.byID(m.form.id); ok {
			return section("Edit " + fullLabel(a))
		}
		return section("Edit")
	case modeTrash:
		return section("Trash")
	case modeLocked:
		return section("Locked")
	}
	if m.searching {
		return []string{t.Accent.Render("/") + " " + m.search.render(m.width-2, true, t.Primary, cursorStyle)}
	}
	var parts []string
	if q := m.search.String(); q != "" {
		parts = append(parts, t.Subtle.Render("Search ")+t.Primary.Render(ui.Sanitize(q)))
	}
	if m.folder != "" {
		parts = append(parts, t.Subtle.Render("Folder ")+t.Primary.Render(ui.Sanitize(m.folder)))
	}
	if m.tag != "" {
		parts = append(parts, t.Subtle.Render("Tag ")+t.Primary.Render(ui.Sanitize(m.tag)))
	}
	return m.flow(parts, "   ")
}

var cursorStyle = lipgloss.NewStyle().Reverse(true)

// statusLines is the status or question, wrapped to the width.
func (m Model) statusLines() []string {
	t := m.theme
	text, style := m.status, t.Subtle
	switch {
	case m.confirm != nil:
		style = t.Accent
		if m.confirm.danger {
			style = t.Warn
		}
		lines := wrap(m.confirm.question+" y/N", m.width)
		last := len(lines) - 1
		for i, l := range lines {
			if i == last && strings.HasSuffix(l, "y/N") {
				lines[i] = style.Render(strings.TrimSuffix(l, "y/N")) + t.Subtle.Render("y/N")
				continue
			}
			lines[i] = style.Render(l)
		}
		return lines
	case m.busy:
		text = "Saving…"
	case m.unlocking:
		text = "Unlocking…"
	case m.status == "":
		return nil
	case m.statusKind == statusOK:
		text, style = "✓ "+m.status, t.Accent
	case m.statusKind == statusFail:
		text, style = "✗ "+m.status, t.Warn
	}
	lines := wrap(text, m.width)
	for i, l := range lines {
		lines[i] = style.Render(l)
	}
	return lines
}

type hint struct{ key, desc string }

func (m Model) hints() []hint {
	if m.confirm != nil {
		return []hint{{"y", "yes"}, {"any other key", "no"}}
	}
	switch m.mode {
	case modeHelp:
		return []hint{{"↑↓", "scroll"}, {"esc", "close"}, {"q", "quit"}}
	case modeFolders, modeTags:
		return []hint{{"↑↓", "move"}, {"enter", "choose"}, {"esc", "close"}}
	case modeAdd, modeEdit:
		h := []hint{{"enter", "save"}, {"esc", "cancel"}, {"tab", "next field"}}
		if m.mode == modeEdit && m.form.focus == editNote {
			h = append(h, hint{"ctrl+j", "new line"})
		}
		return h
	case modeTrash:
		if len(m.trashSorted()) == 0 {
			return []hint{{"esc", "back"}, {"?", "keys"}, {"q", "quit"}}
		}
		return []hint{{"r", "restore"}, {"d", "delete"}, {"esc", "back"}, {"?", "keys"}, {"q", "quit"}}
	case modeLocked:
		if m.asking {
			return []hint{{"enter", "unlock"}, {"esc", "quit"}}
		}
		return []hint{{"any key", "unlock"}, {"ctrl+c", "quit"}}
	}
	empty := len(m.rows()) == 0
	switch {
	case m.searching && empty:
		return []hint{{"esc", "clear"}}
	case m.searching:
		return []hint{{"↑↓", "move"}, {"enter", "copy"}, {"esc", "clear"}}
	case empty && (m.search.String() != "" || m.folder != "" || m.tag != ""):
		return []hint{{"esc", "clear"}, {"?", "keys"}, {"q", "quit"}}
	case empty:
		return []hint{{"a", "add"}, {"?", "keys"}, {"q", "quit"}}
	}
	return []hint{
		{"enter", "copy"}, {"/", "search"}, {"?", "keys"}, {"q", "quit"},
		{"n", "next"}, {"a", "add"}, {"e", "edit"}, {"d", "trash"},
	}
}

func (m Model) hintLine() string {
	t := m.theme
	var b strings.Builder
	used := 0
	for i, h := range m.hints() {
		w := ansi.StringWidth(h.key) + 1 + ansi.StringWidth(h.desc)
		if i > 0 {
			w += 2
		}
		if used+w > m.width {
			break
		}
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(t.Accent.Render(h.key) + " " + t.Subtle.Render(h.desc))
		used += w
	}
	return b.String()
}

func (m Model) body(height int) []string {
	switch m.mode {
	case modeHelp:
		return m.helpBody(height)
	case modeFolders, modeTags:
		return m.pickerBody(height)
	case modeAdd, modeEdit:
		return m.formBody(height)
	case modeTrash:
		return m.trashBody(height)
	case modeLocked:
		return m.lockedBody()
	}
	return m.listBody(height)
}

// row paints a line segment by segment, with the selection background under
// every segment of the selected line.
type row struct {
	b   strings.Builder
	bg  color.Color
	sel bool
}

func (r *row) add(st lipgloss.Style, s string) {
	if s == "" {
		return
	}
	if r.sel {
		st = st.Background(r.bg)
	}
	r.b.WriteString(st.Render(s))
}

func (m Model) newRow(selected bool) *row {
	return &row{bg: m.theme.Selected.GetBackground(), sel: selected}
}

var plain = lipgloss.NewStyle()

func codeWidth(a account.Account) int {
	if a.Type == account.Steam {
		return 5
	}
	if a.Digits >= 6 && a.Digits%2 == 0 {
		return a.Digits + 1
	}
	return a.Digits
}

func (m Model) listBody(height int) []string {
	t := m.theme
	rows := m.rows()
	if len(rows) == 0 {
		if len(account.Live(m.accounts)) == 0 {
			return []string{t.Subtle.Render("No accounts yet. Press a to add one.")}
		}
		return []string{t.Subtle.Render("Nothing matches.")}
	}
	hw, cw, hotpW := 0, 0, 0
	for _, a := range rows {
		hw = max(hw, ansi.StringWidth(ui.Sanitize(a.Handle)))
		cw = max(cw, codeWidth(a))
		if a.Type == account.HOTP {
			hotpW = max(hotpW, len("#"+itoa(a.Counter)))
		}
	}
	meterW := barWidth + 4
	nameW := m.width - (2 + 2 + hw + 2 + 2 + cw + 2 + meterW)
	showBar := nameW >= minNameWidth
	if !showBar {
		meterW = max(3, hotpW)
		nameW = m.width - (2 + 2 + hw + 2 + 2 + cw + 2 + meterW)
	}
	nameW = max(nameW, 1)

	offset := scrollTo(m.cursor, m.offset, len(rows), height)
	end := min(offset+height, len(rows))
	lines := make([]string, 0, end-offset)
	for i := offset; i < end; i++ {
		lines = append(lines, m.accountRow(rows[i], i == m.cursor, hw, nameW, cw, meterW, showBar))
	}
	return lines
}

func (m Model) accountRow(a account.Account, selected bool, hw, nameW, cw, meterW int, showBar bool) string {
	t := m.theme
	r := m.newRow(selected)
	if selected {
		r.add(t.Accent, "▸ ")
	} else {
		r.add(plain, "  ")
	}
	if a.Pinned {
		r.add(t.Accent, "★ ")
	} else {
		r.add(plain, "  ")
	}
	r.add(t.HandleStyle(a.Issuer+a.Account), padRight(ui.Sanitize(a.Handle), hw))
	r.add(plain, "  ")
	m.addName(r, a, nameW)
	r.add(plain, "  ")

	c, err := code.For(a, m.now)
	codeStyle := t.Code
	switch {
	case err != nil:
		c, codeStyle = strings.Repeat("-", cw), t.Warn
	case m.hidden && !selected:
		c = maskCode(ui.GroupCode(c))
		codeStyle = t.Subtle
	default:
		c = ui.GroupCode(c)
	}
	r.add(codeStyle, padRight(c, cw))
	r.add(plain, "  ")

	if a.Type == account.HOTP {
		label := "#" + itoa(a.Counter)
		if showBar {
			label = "hotp " + label
		}
		r.add(t.Subtle, padLeft(label, meterW))
		return r.b.String()
	}
	rem := code.Remaining(a, m.now)
	secs := t.Subtle
	if rem <= 5 {
		secs = t.Warn
	}
	if showBar {
		filled := ui.BarFill(rem, a.Period, barWidth)
		r.add(t.BarStyle(rem), strings.Repeat("█", filled))
		r.add(t.Track(), strings.Repeat("░", barWidth-filled))
		r.add(plain, " ")
		r.add(secs, fmt.Sprintf("%2ds", rem))
		return r.b.String()
	}
	r.add(secs, padLeft(strconv.Itoa(rem)+"s", meterW))
	return r.b.String()
}

// addName writes the issuer and account into width cells, shortening the
// text with an ellipsis when it does not fit.
func (m Model) addName(r *row, a account.Account, width int) {
	t := m.theme
	issuer := ui.Sanitize(a.Issuer)
	acct := ui.Sanitize(a.Account)
	if issuer == "" {
		issuer, acct = acct, ""
	}
	issuer = ansi.Truncate(issuer, width, "…")
	used := ansi.StringWidth(issuer)
	r.add(t.Issuer, issuer)
	if acct != "" && width-used >= 2 {
		acct = ansi.Truncate(acct, width-used-1, "…")
		r.add(t.Subtle, " "+acct)
		used += 1 + ansi.StringWidth(acct)
	}
	r.add(plain, strings.Repeat(" ", max(width-used, 0)))
}

func maskCode(c string) string {
	var b strings.Builder
	for _, r := range c {
		if r == ' ' {
			b.WriteRune(r)
			continue
		}
		b.WriteString("•")
	}
	return b.String()
}

func (m Model) trashBody(height int) []string {
	t := m.theme
	rows := m.trashSorted()
	if len(rows) == 0 {
		return []string{t.Subtle.Render("The Trash is empty.")}
	}
	hw := 0
	for _, a := range rows {
		hw = max(hw, ansi.StringWidth(ui.Sanitize(a.Handle)))
	}
	nameW := max(m.width-(2+len(trashDate)+2+hw+2), 1)
	offset := scrollTo(m.trashCursor, m.trashOffset, len(rows), height)
	end := min(offset+height, len(rows))
	lines := make([]string, 0, end-offset)
	for i := offset; i < end; i++ {
		a := rows[i]
		r := m.newRow(i == m.trashCursor)
		if i == m.trashCursor {
			r.add(t.Accent, "▸ ")
		} else {
			r.add(plain, "  ")
		}
		r.add(t.Subtle, time.Unix(a.DeletedAt, 0).In(m.now.Location()).Format(trashDate))
		r.add(plain, "  ")
		r.add(t.HandleStyle(a.Issuer+a.Account), padRight(ui.Sanitize(a.Handle), hw))
		r.add(plain, "  ")
		m.addName(r, a, nameW)
		lines = append(lines, r.b.String())
	}
	return lines
}

type helpLine struct{ section, keys, desc string }

func helpLines() []helpLine {
	return []helpLine{
		{section: "Moving in a list"},
		{keys: "↑ ↓  j k", desc: "Move"},
		{keys: "pgup pgdn", desc: "Move a page"},
		{keys: "g G  home end", desc: "First or last"},
		{section: "Accounts"},
		{keys: "enter  c", desc: fmt.Sprintf("Copy the code; under %ds left, copy the next one", FreshnessFloor)},
		{keys: "n", desc: "Copy the next code; a counter account asks first"},
		{keys: "/", desc: "Search issuer, account, alias, folder and tags"},
		{keys: "esc", desc: "Clear the search, then the filters, then quit"},
		{keys: "f", desc: "Filter by folder"},
		{keys: "t", desc: "Filter by tag"},
		{keys: "h", desc: "Hide codes except the selected one"},
		{keys: "a", desc: "Add an account from a link or setup key"},
		{keys: "e", desc: "Edit the selected account"},
		{keys: "p", desc: "Pin or unpin"},
		{keys: "d", desc: "Move to the Trash"},
		{keys: "u", desc: "Undo the last move to the Trash"},
		{keys: "T", desc: "Open the Trash"},
		{keys: "?", desc: "Show or close this list"},
		{keys: "q", desc: "Quit, except while typing"},
		{keys: "ctrl+c", desc: "Quit"},
		{section: "Search"},
		{keys: "↑ ↓  pgup pgdn", desc: "Move while typing"},
		{keys: "enter", desc: "Copy and stop typing"},
		{keys: "backspace", desc: "Delete a character; on an empty search, stop typing"},
		{keys: "esc", desc: "Clear the search"},
		{section: "Trash"},
		{keys: "r", desc: "Restore"},
		{keys: "d", desc: "Delete for good, after you confirm"},
		{keys: "esc  T", desc: "Back to the list"},
		{section: "Folder and tag lists"},
		{keys: "enter", desc: "Show only the one chosen"},
		{keys: "esc", desc: "Close"},
		{section: "Forms"},
		{keys: "tab  shift+tab", desc: "Next or previous field"},
		{keys: "↑ ↓", desc: "Previous or next field"},
		{keys: "← →  home end", desc: "Move in the field"},
		{keys: "backspace  del", desc: "Delete a character"},
		{keys: "ctrl+u  ctrl+k", desc: "Delete before or after the cursor"},
		{keys: "ctrl+j", desc: "New line in the note"},
		{keys: "enter", desc: "Save"},
		{keys: "esc", desc: "Cancel"},
		{section: "This list"},
		{keys: "esc  ?", desc: "Close"},
		{section: "Locked"},
		{keys: "any key", desc: "Unlock"},
		{keys: "enter", desc: "Unlock with the typed passphrase"},
		{keys: "esc", desc: "Quit at the passphrase"},
		{section: "Questions"},
		{keys: "y", desc: "Yes"},
		{keys: "any other key", desc: "No"},
	}
}

const helpKeyWidth = 15

// helpRows is the key reference laid out for the width, descriptions
// wrapped under their own column.
func (m Model) helpRows() []string {
	t := m.theme
	indent := 2 + helpKeyWidth + 2
	descW := m.width - indent
	var rows []string
	for _, h := range helpLines() {
		if h.section != "" {
			rows = append(rows, t.Section.Render(h.section))
			continue
		}
		keys := "  " + t.Accent.Render(padRight(h.keys, helpKeyWidth)) + "  "
		if descW < 12 {
			rows = append(rows, keys)
			for _, l := range wrap(h.desc, m.width-4) {
				rows = append(rows, "    "+t.Primary.Render(l))
			}
			continue
		}
		for i, l := range wrap(h.desc, descW) {
			if i == 0 {
				rows = append(rows, keys+t.Primary.Render(l))
				continue
			}
			rows = append(rows, strings.Repeat(" ", indent)+t.Primary.Render(l))
		}
	}
	return rows
}

func (m Model) helpBody(height int) []string {
	rows := m.helpRows()
	offset := clamp(m.helpOffset, 0, max(len(rows)-height, 0))
	return rows[offset:min(offset+height, len(rows))]
}

func (m Model) pickerBody(height int) []string {
	t := m.theme
	items := m.picker.items
	offset := scrollTo(m.picker.cursor, 0, len(items), height)
	end := min(offset+height, len(items))
	lines := make([]string, 0, end-offset)
	for i := offset; i < end; i++ {
		label := ui.Sanitize(items[i])
		if i == m.picker.cursor {
			r := m.newRow(true)
			r.add(t.Accent, "▸ ")
			r.add(t.Primary, label)
			lines = append(lines, r.b.String())
			continue
		}
		style := t.Primary
		if i == 0 {
			style = t.Subtle
		}
		lines = append(lines, "  "+style.Render(label))
	}
	return lines
}

func (m Model) formBody(height int) []string {
	t := m.theme
	lw := 0
	for _, f := range m.form.fields {
		lw = max(lw, ansi.StringWidth(f.label))
	}
	valueW := max(m.width-lw-2, 1)
	lines := make([]string, 0, len(m.form.fields)+3)
	for i, f := range m.form.fields {
		label := t.Subtle
		if i == m.form.focus {
			label = t.Accent
		}
		lines = append(lines, label.Render(padRight(f.label, lw))+"  "+f.render(valueW, i == m.form.focus, t.Primary, cursorStyle))
	}
	if note := wrap(m.fieldNote(), m.width); note[0] != "" && len(lines)+1+len(note) <= height {
		lines = append(lines, "")
		for _, l := range note {
			lines = append(lines, t.Subtle.Render(l))
		}
	}
	if len(lines) <= height {
		return lines
	}
	offset := scrollTo(m.form.focus, 0, len(lines), height)
	return lines[offset:min(offset+height, len(lines))]
}

func (m Model) fieldNote() string {
	if m.mode == modeAdd {
		if m.form.focus == 0 {
			return "Paste an otpauth:// link or a setup key."
		}
		return "A setup key needs an issuer or account. A link keeps its own when empty."
	}
	switch m.form.focus {
	case editAlias:
		return "1 to 12 letters or digits, starting with a letter."
	case editTags:
		return "Separate tags with commas."
	case editNote:
		return "ctrl+j starts a new line."
	}
	return ""
}

func (m Model) lockedBody() []string {
	t := m.theme
	var text []string
	if m.locked == lockRekeyed {
		text = append(text, "The vault was re-keyed.")
	}
	var lines []string
	for _, s := range text {
		for _, l := range wrap(s, m.width) {
			lines = append(lines, t.Primary.Render(l))
		}
	}
	if !m.asking {
		return lines
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	label := "Passphrase"
	return append(lines,
		t.Accent.Render(label)+"  "+m.pass.render(max(m.width-len(label)-2, 1), !m.unlocking, t.Primary, cursorStyle))
}

func padRight(s string, w int) string {
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}

func padLeft(s string, w int) string {
	return strings.Repeat(" ", max(w-ansi.StringWidth(s), 0)) + s
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// labelOf names an account in a status message: the issuer, or the account
// name when there is no issuer.
func labelOf(a account.Account) string {
	switch {
	case a.Issuer != "":
		return ui.Sanitize(a.Issuer)
	case a.Account != "":
		return ui.Sanitize(a.Account)
	}
	return ui.Sanitize(a.Handle)
}

// fullLabel is "Issuer (account)" when both are set.
func fullLabel(a account.Account) string {
	if a.Issuer != "" && a.Account != "" {
		return ui.Sanitize(a.Issuer) + " (" + ui.Sanitize(a.Account) + ")"
	}
	return labelOf(a)
}

func codeAt(a account.Account, t time.Time) (string, error) { return code.For(a, t) }
