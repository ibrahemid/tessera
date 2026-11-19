package qr

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"strings"
	"testing"
)

// moduleImage paints modules (true for dark) at scale pixels per module,
// adding border light modules on every side.
func moduleImage(rows [][]bool, scale, border int) image.Image {
	size := (len(rows) + 2*border) * scale
	img := image.NewGray(image.Rect(0, 0, size, size))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	for y, row := range rows {
		for x, isDark := range row {
			if !isDark {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetGray((x+border)*scale+dx, (y+border)*scale+dy, color.Gray{})
				}
			}
		}
	}
	return img
}

// parseTerminal turns WriteTerminal output back into module rows, checking that
// every line carries the explicit colors.
func parseTerminal(t *testing.T, out string) [][]bool {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	var rows [][]bool
	for i, line := range lines {
		if !strings.HasPrefix(line, terminalColors) || !strings.HasSuffix(line, terminalReset) {
			t.Fatalf("line %d is not wrapped in the black-on-white colors: %q", i, line)
		}
		body := strings.TrimSuffix(strings.TrimPrefix(line, terminalColors), terminalReset)
		var top, bottom []bool
		for _, r := range body {
			switch r {
			case '█':
				top, bottom = append(top, true), append(bottom, true)
			case '▀':
				top, bottom = append(top, true), append(bottom, false)
			case '▄':
				top, bottom = append(top, false), append(bottom, true)
			case ' ':
				top, bottom = append(top, false), append(bottom, false)
			default:
				t.Fatalf("line %d has an unexpected rune %q", i, r)
			}
		}
		rows = append(rows, top, bottom)
	}
	return rows
}

func TestMatrixDecodesBack(t *testing.T) {
	rows, err := Matrix(testURI)
	if err != nil {
		t.Fatalf("Matrix: %v", err)
	}
	if len(rows) == 0 || len(rows) != len(rows[0]) {
		t.Fatalf("matrix is not square: %d rows", len(rows))
	}
	got, err := DecodeImage(moduleImage(rows, 8, 4))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if got != testURI {
		t.Fatalf("payload mismatch:\n got %q\nwant %q", got, testURI)
	}
}

func TestMatrixRejectsAnEmptyPayload(t *testing.T) {
	if _, err := Matrix(""); err == nil {
		t.Fatal("Matrix(\"\") should fail")
	}
}

func TestWriteTerminalDecodesBack(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteTerminal(&buf, testURI); err != nil {
		t.Fatalf("WriteTerminal: %v", err)
	}
	rows := parseTerminal(t, buf.String())
	got, err := DecodeImage(moduleImage(rows, 8, 0))
	if err != nil {
		t.Fatalf("decode the terminal rendering: %v", err)
	}
	if got != testURI {
		t.Fatalf("payload mismatch:\n got %q\nwant %q", got, testURI)
	}
}

func TestWriteTerminalDrawsTheQuietZone(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteTerminal(&buf, testURI); err != nil {
		t.Fatalf("WriteTerminal: %v", err)
	}
	rendered := parseTerminal(t, buf.String())
	modules, err := Matrix(testURI)
	if err != nil {
		t.Fatalf("Matrix: %v", err)
	}
	size := len(modules) + 2*TerminalQuietZone
	if len(rendered) < size || len(rendered) > size+1 {
		t.Fatalf("rendered %d module rows, want %d plus at most one padding row", len(rendered), size)
	}
	for y, row := range rendered {
		if len(row) != size {
			t.Fatalf("row %d is %d modules wide, want %d", y, len(row), size)
		}
		for x, isDark := range row {
			inCode := x >= TerminalQuietZone && x < size-TerminalQuietZone &&
				y >= TerminalQuietZone && y < size-TerminalQuietZone
			if !inCode && isDark {
				t.Fatalf("module (%d,%d) in the quiet zone is dark", x, y)
			}
			if inCode && isDark != modules[y-TerminalQuietZone][x-TerminalQuietZone] {
				t.Fatalf("module (%d,%d) differs from the matrix", x, y)
			}
		}
	}
}

func TestWriteTerminalRejectsAnEmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteTerminal(&buf, ""); err == nil {
		t.Fatal("WriteTerminal with an empty payload should fail")
	}
	if buf.Len() != 0 {
		t.Fatalf("nothing should be written on failure, got %q", buf.String())
	}
}

func TestDecodeReportsErrNoQRCode(t *testing.T) {
	blank := moduleImage(nil, 1, 32)
	if _, err := DecodeImage(blank); !errors.Is(err, ErrNoQRCode) {
		t.Fatalf("DecodeImage on a blank image = %v, want ErrNoQRCode", err)
	}
	if _, err := DecodeAll(blank); !errors.Is(err, ErrNoQRCode) {
		t.Fatalf("DecodeAll on a blank image = %v, want ErrNoQRCode", err)
	}
}
