//go:build unix && !darwin

package clipboard

import (
	"io"

	"github.com/aymanbagabas/go-osc52/v2"
)

// osc52Clipboard asks the terminal to set its clipboard. Inside tmux the
// sequence goes out plain, which needs `set -g set-clipboard on`.
type osc52Clipboard struct {
	openTTY func() (io.WriteCloser, error)
	screen  bool
}

func findTerminal(e env) (Clipboard, bool) {
	tty, err := e.openTTY()
	if err != nil {
		return nil, false
	}
	_ = tty.Close()
	screen := e.getenv("TMUX") == "" && e.getenv("STY") != ""
	return osc52Clipboard{openTTY: e.openTTY, screen: screen}, true
}

func (osc52Clipboard) Name() string { return "OSC 52" }

// Write sends the whole sequence in one write so it cannot interleave with
// other output to the terminal.
func (c osc52Clipboard) Write(text string, _ bool) error {
	seq := c.sequence(text)
	tty, err := c.openTTY()
	if err != nil {
		return &OpError{Backend: c.Name(), Op: "write", Err: err}
	}
	n, err := tty.Write(seq)
	if cerr := tty.Close(); err == nil {
		err = cerr
	}
	if err == nil && n < len(seq) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return &OpError{Backend: c.Name(), Op: "write", Err: err}
	}
	return nil
}

func (c osc52Clipboard) sequence(text string) []byte {
	s := osc52.New(text)
	if c.screen {
		s = s.Screen()
	}
	return []byte(s.String())
}

func (osc52Clipboard) Read() (string, error) { return "", ErrReadUnsupported }

func (osc52Clipboard) Clear() error { return ErrClearUnsupported }
