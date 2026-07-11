package qr

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	"golang.org/x/image/tiff"
)

const testURI = "otpauth://totp/ACME:john@example.com?secret=JBSWY3DPEHPK3PXP&issuer=ACME"

// writeQRPNG encodes text as a QR PNG at path and fails the test on any error.
func writeQRPNG(t *testing.T, path, text string) {
	t.Helper()
	matrix, err := qrcode.NewQRCodeWriter().Encode(text, gozxing.BarcodeFormat_QR_CODE, 256, 256, nil)
	if err != nil {
		t.Fatalf("encode qr: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, matrix); err != nil {
		t.Fatalf("png encode: %v", err)
	}
}

func TestDecodeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "code.png")
	writeQRPNG(t, path, testURI)

	got, err := DecodeFile(path)
	if err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if got != testURI {
		t.Fatalf("payload mismatch:\n got %q\nwant %q", got, testURI)
	}
}

func TestEncodePNGRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.png")
	if err := EncodePNG(testURI, path, 512); err != nil {
		t.Fatalf("EncodePNG: %v", err)
	}
	got, err := DecodeFile(path)
	if err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if got != testURI {
		t.Fatalf("round-trip mismatch:\n got %q\nwant %q", got, testURI)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("QR PNG mode = %o, want 600 (cleartext secret)", perm)
	}
}

func TestEncodePNGEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.png")
	if err := EncodePNG("", path, 512); err == nil {
		t.Fatal("expected error for empty payload, got nil")
	}
}

func TestDecodeFileNoQR(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blank.png")
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.White)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatalf("png encode: %v", err)
	}
	f.Close()

	if _, err := DecodeFile(path); err == nil {
		t.Fatal("expected error for image with no QR code, got nil")
	}
}

func TestDecodeFileNonexistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.png")
	if _, err := DecodeFile(path); err == nil {
		t.Fatal("expected error for nonexistent path, got nil")
	}
}

// qrMatrix encodes text as a QR bit matrix for compositing tests.
func qrMatrix(t *testing.T, text string) *gozxing.BitMatrix {
	t.Helper()
	m, err := qrcode.NewQRCodeWriter().Encode(text, gozxing.BarcodeFormat_QR_CODE, 200, 200, nil)
	if err != nil {
		t.Fatalf("encode qr: %v", err)
	}
	return m
}

func TestDecodeAllMultipleQRs(t *testing.T) {
	const uriA = "otpauth://totp/A:one?secret=JBSWY3DPEHPK3PXP&issuer=A"
	const uriB = "otpauth://totp/B:two?secret=GEZDGNBVGY3TQOJQ&issuer=B"

	a := qrMatrix(t, uriA)
	b := qrMatrix(t, uriB)

	// White canvas holding both codes side by side with a white gutter between,
	// so the multi detector segments them cleanly.
	const gutter = 40
	w := a.GetWidth() + gutter + b.GetWidth()
	h := a.GetHeight()
	if b.GetHeight() > h {
		h = b.GetHeight()
	}
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	drawMatrix(canvas, a, 0)
	drawMatrix(canvas, b, a.GetWidth()+gutter)

	got, err := DecodeAll(canvas)
	if err != nil {
		t.Fatalf("DecodeAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 payloads, got %d: %v", len(got), got)
	}
	sort.Strings(got)
	want := []string{uriA, uriB}
	sort.Strings(want)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("payload %d: got %q want %q", i, got[i], want[i])
		}
	}
}

// drawMatrix paints a QR bit matrix onto canvas with its top-left at (xOffset, 0).
func drawMatrix(canvas *image.RGBA, m *gozxing.BitMatrix, xOffset int) {
	for y := 0; y < m.GetHeight(); y++ {
		for x := 0; x < m.GetWidth(); x++ {
			if m.Get(x, y) {
				canvas.Set(xOffset+x, y, color.Black)
			}
		}
	}
}

func TestDecodeAllSingleFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "single.png")
	writeQRPNG(t, path, testURI)
	got, err := DecodeFileAll(path)
	if err != nil {
		t.Fatalf("DecodeFileAll: %v", err)
	}
	if len(got) != 1 || got[0] != testURI {
		t.Fatalf("expected [%q], got %v", testURI, got)
	}
}

func TestDecodeTIFFRoundTrip(t *testing.T) {
	matrix, err := qrcode.NewQRCodeWriter().Encode(testURI, gozxing.BarcodeFormat_QR_CODE, 256, 256, nil)
	if err != nil {
		t.Fatalf("encode qr: %v", err)
	}
	path := filepath.Join(t.TempDir(), "code.tiff")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := tiff.Encode(f, matrix, nil); err != nil {
		f.Close()
		t.Fatalf("tiff encode: %v", err)
	}
	f.Close()

	got, err := DecodeFileAll(path)
	if err != nil {
		t.Fatalf("DecodeFileAll tiff: %v", err)
	}
	if len(got) != 1 || got[0] != testURI {
		t.Fatalf("tiff round-trip: got %v want [%q]", got, testURI)
	}
}
