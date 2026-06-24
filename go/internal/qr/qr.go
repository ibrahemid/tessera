// Package qr decodes QR codes from images, returning the embedded payload
// (typically an otpauth:// URI). It supports PNG and JPEG source files.
package qr

import (
	"fmt"
	"image"
	"os"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"

	_ "image/jpeg"
	_ "image/png"
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
