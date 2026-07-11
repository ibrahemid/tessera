// Package qr decodes QR codes from images, returning the embedded payloads
// (typically otpauth:// URIs). It reads PNG, JPEG, WebP, TIFF, and BMP source
// files, and decodes every QR code present in a single image.
package qr

import (
	"fmt"
	"image"
	"image/png"
	"os"

	"github.com/makiuchi-d/gozxing"
	multiqr "github.com/makiuchi-d/gozxing/multi/qrcode"
	"github.com/makiuchi-d/gozxing/qrcode"

	_ "image/jpeg"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
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

// DecodeAll decodes every QR code in an in-memory image and returns their
// payloads in detection order. It tries the multi-QR reader first (TRY_HARDER,
// QR-only), then falls back to the single-QR reader so images the multi
// detector cannot segment still decode. It errors only when no QR is found.
func DecodeAll(img image.Image) ([]string, error) {
	if img == nil {
		return nil, fmt.Errorf("qr: nil image")
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return nil, fmt.Errorf("qr: prepare bitmap: %w", err)
	}
	hints := map[gozxing.DecodeHintType]interface{}{
		gozxing.DecodeHintType_TRY_HARDER:       true,
		gozxing.DecodeHintType_POSSIBLE_FORMATS: []gozxing.BarcodeFormat{gozxing.BarcodeFormat_QR_CODE},
	}
	if results, err := multiqr.NewQRCodeMultiReader().DecodeMultiple(bmp, hints); err == nil && len(results) > 0 {
		out := make([]string, 0, len(results))
		for _, r := range results {
			out = append(out, r.GetText())
		}
		return out, nil
	}
	result, err := qrcode.NewQRCodeReader().Decode(bmp, hints)
	if err != nil {
		return nil, fmt.Errorf("qr: decode: %w", err)
	}
	return []string{result.GetText()}, nil
}

// DecodeFileAll opens an image file and returns every QR payload it contains.
func DecodeFileAll(path string) ([]string, error) {
	img, err := openImage(path)
	if err != nil {
		return nil, err
	}
	texts, err := DecodeAll(img)
	if err != nil {
		return nil, fmt.Errorf("qr: %q: %w", path, err)
	}
	return texts, nil
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

// DecodeFile opens an image file, decodes the first QR code it contains, and
// returns the payload string. Errors are wrapped with the file path.
func DecodeFile(path string) (string, error) {
	img, err := openImage(path)
	if err != nil {
		return "", err
	}
	text, err := DecodeImage(img)
	if err != nil {
		return "", fmt.Errorf("qr: %q: %w", path, err)
	}
	return text, nil
}

// openImage decodes an image file in any registered format (PNG, JPEG, WebP,
// TIFF, BMP). Errors are wrapped with the file path.
func openImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("qr: open %q: %w", path, err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("qr: decode image %q: %w", path, err)
	}
	return img, nil
}
