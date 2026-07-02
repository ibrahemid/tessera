package qr

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
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
