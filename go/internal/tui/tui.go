// Package tui implements `tess watch`: a live, auto-refreshing authenticator
// view with countdown bars, search, and copy-to-clipboard.
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/code"
	"github.com/ibrahemid/tessera/go/internal/ui"
)

// SaveFunc persists mutated accounts (e.g. an advanced HOTP counter).
type SaveFunc func([]account.Account) error

type tickMsg time.Time

// Model is the bubbletea model for the watch view.
type Model struct {
	accounts    []account.Account
	save        SaveFunc
	cursor      int
	query       string
	searching   bool
	now         time.Time
	status      string
	statusUntil time.Time
	width       int
	height      int
}

// New builds a watch model over the given accounts.
func New(accounts []account.Account, save SaveFunc) Model {
	return Model{accounts: accounts, save: save, now: time.Now(), width: 72}
}

// Run starts the TUI program.
func Run(accounts []account.Account, save SaveFunc) error {
	_, err := tea.NewProgram(New(accounts, save), tea.WithAltScreen()).Run()
	return err
}

func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Init() tea.Cmd { return tick() }

func (m Model) filtered() []account.Account {
	out := make([]account.Account, 0, len(m.accounts))
	q := strings.ToLower(m.query)
	for _, a := range m.accounts {
		if q == "" || strings.Contains(strings.ToLower(a.Issuer+" "+a.Account), q) {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		return strings.ToLower(out[i].Issuer) < strings.ToLower(out[j].Issuer)
	})
	return out
}

func (m *Model) setStatus(s string) {
	m.status = s
	m.statusUntil = time.Now().Add(2 * time.Second)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		m.now = time.Time(msg)
		if m.status != "" && time.Now().After(m.statusUntil) {
			m.status = ""
		}
		return m, tick()
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.filtered()
	if m.searching {
		switch msg.Type {
		case tea.KeyEsc:
			m.searching = false
			m.query = ""
		case tea.KeyEnter:
			m.searching = false
		case tea.KeyBackspace:
			if len(m.query) > 0 {
				m.query = m.query[:len(m.query)-1]
			}
		case tea.KeyRunes, tea.KeySpace:
			m.query += string(msg.Runes)
			m.cursor = 0
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "ctrl+c", "esc":
		return m, tea.Quit
	case "/":
		m.searching = true
		m.query = ""
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(rows)-1 {
			m.cursor++
		}
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = max(len(rows)-1, 0)
	case "enter", "c":
		if a, ok := at(rows, m.cursor); ok {
			if c, err := code.For(a, m.now); err == nil {
				_ = clipboard.WriteAll(c)
				m.setStatus("Copied " + labelOf(a))
			}
		}
	case "r":
		if a, ok := at(rows, m.cursor); ok && a.Type == account.HOTP {
			m.advance(a.ID)
		}
	}
	return m, nil
}

func (m *Model) advance(id string) {
	for i := range m.accounts {
		if m.accounts[i].ID == id {
			m.accounts[i].Counter++
			m.accounts[i].UpdatedAt = time.Now().Unix()
			if m.save != nil {
				if err := m.save(m.accounts); err != nil {
					m.setStatus("Save failed: " + err.Error())
					return
				}
			}
			if c, err := code.For(m.accounts[i], m.now); err == nil {
				_ = clipboard.WriteAll(c)
				m.setStatus("Advanced + copied " + labelOf(m.accounts[i]))
			}
			return
		}
	}
}

func (m Model) View() string {
	rows := m.filtered()
	var b strings.Builder

	title := ui.TitleStyle.Render("◧ Tessera")
	count := ui.SubtleStyle.Render(fmt.Sprintf("%d accounts", len(m.accounts)))
	b.WriteString(title + "  " + count + "\n")

	if m.searching {
		b.WriteString(ui.AccentStyle.Render("/") + m.query + ui.SubtleStyle.Render("▌") + "\n")
	} else if m.query != "" {
		b.WriteString(ui.SubtleStyle.Render("filter: ") + m.query + "\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if len(rows) == 0 {
		b.WriteString(ui.SubtleStyle.Render("  No accounts match.\n"))
	}

	for i, a := range rows {
		b.WriteString(m.renderRow(a, i == m.cursor))
	}

	b.WriteString("\n")
	if m.status != "" {
		b.WriteString(ui.AccentStyle.Render("✓ "+m.status) + "\n")
	} else {
		b.WriteString("\n")
	}
	help := "↑/↓ move · enter/c copy · r advance HOTP · / search · q quit"
	b.WriteString(ui.SubtleStyle.Render(help))
	return b.String()
}

func (m Model) renderRow(a account.Account, selected bool) string {
	cursor := "  "
	if selected {
		cursor = ui.AccentStyle.Render("▸ ")
	}
	mono := ui.Monogram(a.Issuer+a.Account, label1(a))
	name := ui.IssuerStyle.Render(pad(labelOf(a), 26))

	c, err := code.For(a, m.now)
	if err != nil {
		c = "------"
	}
	codeCell := ui.CodeStyle.Render(pad(ui.GroupCode(c), 9))

	var meter string
	if a.Type == account.HOTP {
		meter = ui.SubtleStyle.Render(fmt.Sprintf("hotp #%d", a.Counter))
	} else {
		rem := code.Remaining(a, m.now)
		meter = ui.Bar(rem, a.Period, 10) + ui.SubtleStyle.Render(fmt.Sprintf(" %2ds", rem))
	}

	pin := "  "
	if a.Pinned {
		pin = ui.AccentStyle.Render("★ ")
	}
	line := fmt.Sprintf("%s%s%s %s  %s  %s", cursor, pin, mono, name, codeCell, meter)
	if selected {
		line = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#F0ECE0", Dark: "#242732"}).Render(line)
	}
	return line + "\n"
}

func labelOf(a account.Account) string {
	if a.Issuer != "" {
		return a.Issuer
	}
	return a.Account
}

func label1(a account.Account) string { return labelOf(a) }

func at(rows []account.Account, i int) (account.Account, bool) {
	if i < 0 || i >= len(rows) {
		return account.Account{}, false
	}
	return rows[i], true
}

func pad(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s + strings.Repeat(" ", n-len(r))
}
