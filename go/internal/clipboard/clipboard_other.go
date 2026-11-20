//go:build !unix && !windows

package clipboard

func platformDetect(e env) (Clipboard, error) {
	if _, err := override(e); err != nil {
		return nil, err
	}
	return nil, &NoClipboardError{}
}
