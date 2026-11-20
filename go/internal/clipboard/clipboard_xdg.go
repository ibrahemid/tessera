//go:build unix && !darwin

package clipboard

import (
	"bytes"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

const (
	toolWlClipboard = "wl-clipboard"
	toolXclip       = "xclip"
	toolXsel        = "xsel"
)

var (
	wlEmptyMarkers = []string{
		"Nothing is copied",
		"No selection",
		"No suitable type of content copied",
		"not available as requested type",
	}
	xclipEmptyMarkers = []string{"not available"}
)

func platformDetect(e env) (Clipboard, error) {
	forced, err := override(e)
	if err != nil {
		return nil, err
	}
	switch forced {
	case overrideWayland:
		if c, ok := findWayland(e); ok {
			return c, nil
		}
		return nil, &NoClipboardError{Session: overrideWayland, Missing: []string{toolWlClipboard}}
	case overrideX11:
		if c, ok := findX11(e); ok {
			return c, nil
		}
		return nil, &NoClipboardError{Session: overrideX11, Missing: []string{toolXclip, toolXsel}}
	case overrideOSC52:
		if c, ok := findTerminal(e); ok {
			return c, nil
		}
		return nil, &NoClipboardError{Session: overrideOSC52}
	}

	wayland := e.getenv("WAYLAND_DISPLAY") != ""
	x11 := e.getenv("DISPLAY") != ""
	if wayland {
		if c, ok := findWayland(e); ok {
			return c, nil
		}
	}
	if x11 {
		if c, ok := findX11(e); ok {
			return c, nil
		}
	}
	if c, ok := findTerminal(e); ok {
		return c, nil
	}
	switch {
	case wayland && x11:
		return nil, &NoClipboardError{Session: overrideWayland, Missing: []string{toolWlClipboard, toolXclip, toolXsel}}
	case wayland:
		return nil, &NoClipboardError{Session: overrideWayland, Missing: []string{toolWlClipboard}}
	case x11:
		return nil, &NoClipboardError{Session: overrideX11, Missing: []string{toolXclip, toolXsel}}
	}
	return nil, &NoClipboardError{Missing: []string{toolWlClipboard, toolXclip, toolXsel}}
}

func findWayland(e env) (Clipboard, bool) {
	copyPath, err := e.lookPath("wl-copy")
	if err != nil {
		return nil, false
	}
	pastePath, err := e.lookPath("wl-paste")
	if err != nil {
		return nil, false
	}
	return wlClipboard{copyPath: copyPath, pastePath: pastePath}, true
}

func findX11(e env) (Clipboard, bool) {
	if p, err := e.lookPath(toolXclip); err == nil {
		return xclip{path: p}, true
	}
	if p, err := e.lookPath(toolXsel); err == nil {
		return xsel{path: p}, true
	}
	return nil, false
}

type wlClipboard struct {
	copyPath  string
	pastePath string
}

func (wlClipboard) Name() string { return toolWlClipboard }

func (c wlClipboard) Write(text string, sensitive bool) error {
	args := []string{"--type", "text/plain"}
	if sensitive && wlCopyHasSensitive(c.copyPath) {
		args = append(args, "--sensitive")
	}
	return run(c.Name(), "write", c.copyPath, args, strings.NewReader(text), nil)
}

func (c wlClipboard) Read() (string, error) {
	var out bytes.Buffer
	err := run(c.Name(), "read", c.pastePath, []string{"--no-newline", "--type", "text/plain"}, nil, &out)
	return readResult(out.String(), err, wlEmptyMarkers)
}

func (c wlClipboard) Clear() error {
	return run(c.Name(), "clear", c.copyPath, []string{"--clear"}, nil, nil)
}

type xclip struct{ path string }

func (xclip) Name() string { return toolXclip }

func (c xclip) Write(text string, _ bool) error {
	return run(c.Name(), "write", c.path, []string{"-selection", "clipboard"}, strings.NewReader(text), nil)
}

func (c xclip) Read() (string, error) {
	var out bytes.Buffer
	err := run(c.Name(), "read", c.path, []string{"-selection", "clipboard", "-out"}, nil, &out)
	return readResult(out.String(), err, xclipEmptyMarkers)
}

func (c xclip) Clear() error {
	return run(c.Name(), "clear", c.path, []string{"-selection", "clipboard"}, strings.NewReader(""), nil)
}

type xsel struct{ path string }

func (xsel) Name() string { return toolXsel }

func (c xsel) Write(text string, _ bool) error {
	return run(c.Name(), "write", c.path, []string{"--clipboard", "--input"}, strings.NewReader(text), nil)
}

func (c xsel) Read() (string, error) {
	var out bytes.Buffer
	err := run(c.Name(), "read", c.path, []string{"--clipboard", "--output"}, nil, &out)
	return readResult(out.String(), err, nil)
}

func (c xsel) Clear() error {
	return run(c.Name(), "clear", c.path, []string{"--clipboard", "--clear"}, nil, nil)
}

// readResult maps a tool's "nothing to paste" exit to an empty read.
func readResult(out string, err error, emptyMarkers []string) (string, error) {
	if err == nil {
		return out, nil
	}
	var op *OpError
	var exit *exec.ExitError
	if errors.As(err, &op) && errors.As(op.Err, &exit) {
		for _, m := range emptyMarkers {
			if strings.Contains(op.Detail, m) {
				return "", nil
			}
		}
	}
	return "", err
}

var wlSensitive = struct {
	sync.Mutex
	byPath map[string]bool
}{byPath: map[string]bool{}}

// wlCopyHasSensitive probes `wl-copy --version` once per binary for 2.3.0+.
func wlCopyHasSensitive(path string) bool {
	wlSensitive.Lock()
	defer wlSensitive.Unlock()
	if ok, probed := wlSensitive.byPath[path]; probed {
		return ok
	}
	var out bytes.Buffer
	ok := run(toolWlClipboard, "version", path, []string{"--version"}, nil, &out) == nil &&
		versionAtLeast(out.String(), [3]int{2, 3, 0})
	wlSensitive.byPath[path] = ok
	return ok
}

// versionAtLeast reads the first dotted number in the first line of out.
func versionAtLeast(out string, want [3]int) bool {
	line, _, _ := strings.Cut(out, "\n")
	for _, field := range strings.Fields(line) {
		if field[0] < '0' || field[0] > '9' {
			continue
		}
		var got [3]int
		for i, part := range strings.SplitN(field, ".", 3) {
			end := 0
			for end < len(part) && part[end] >= '0' && part[end] <= '9' {
				end++
			}
			n, err := strconv.Atoi(part[:end])
			if err != nil {
				return false
			}
			got[i] = n
		}
		for i := range got {
			if got[i] != want[i] {
				return got[i] > want[i]
			}
		}
		return true
	}
	return false
}
