// Package qr decodes QR codes from images, returning the embedded payload
// (typically an otpauth:// URI). It supports PNG and JPEG source files.
package qr

import (
	"fmt"
	"image"
	"image/png"
	"os"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"

	_ "image/jpeg"
)

// DecodeImage decodes a single QR code from an in-memory image and returns its
// payload string. It returns an error if no QR code can be read.
func DecodeImage(img image.Image) (string, error) {
	if img == nil {
		return "", fmt.Errorf("qr: nil image")
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", fmt.Errorf("qr: prepare bitmap: %w", err)
	}
	result, err := qrcode.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		return "", fmt.Errorf("qr: decode: %w", err)
	}
	return result.GetText(), nil
}

// EncodePNG renders text (typically an otpauth:// URI) as a QR code and writes it
// as a PNG to path. size is the image edge length in pixels.
func EncodePNG(text, path string, size int) error {
	if text == "" {
		return fmt.Errorf("qr: empty payload")
	}
	if size <= 0 {
		size = 512
	}
	bits, err := qrcode.NewQRCodeWriter().Encode(text, gozxing.BarcodeFormat_QR_CODE, size, size, nil)
	if err != nil {
		return fmt.Errorf("qr: encode: %w", err)
	}
	// The payload is a cleartext secret; keep the file owner-only like the vault.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("qr: create %q: %w", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, bits); err != nil {
		return fmt.Errorf("qr: write %q: %w", path, err)
	}
	return nil
}

// DecodeFile opens an image file (PNG or JPEG), decodes the QR code it contains,
// and returns the payload string. Errors are wrapped with the file path.
func DecodeFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("qr: open %q: %w", path, err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return "", fmt.Errorf("qr: decode image %q: %w", path, err)
	}

	text, err := DecodeImage(img)
	if err != nil {
		return "", fmt.Errorf("qr: %q: %w", path, err)
	}
	return text, nil
}
