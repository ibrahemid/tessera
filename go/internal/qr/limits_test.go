package qr

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// pngDeclaring returns a valid 1x1 PNG whose IHDR claims width x height, with
// the chunk CRC recomputed so the header parses.
func pngDeclaring(t *testing.T, width, height uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	const ihdrData = 8 + 8
	binary.BigEndian.PutUint32(data[ihdrData:], width)
	binary.BigEndian.PutUint32(data[ihdrData+4:], height)
	crc := crc32.ChecksumIEEE(data[12 : ihdrData+13])
	binary.BigEndian.PutUint32(data[ihdrData+13:], crc)
	return data
}

func TestHugeDeclaredDimensionsAreRefusedBeforeDecoding(t *testing.T) {
	data := pngDeclaring(t, 50_000, 50_000)
	if cfg, err := png.DecodeConfig(bytes.NewReader(data)); err != nil || cfg.Width != 50_000 {
		t.Fatalf("fixture header does not parse: cfg=%+v err=%v", cfg, err)
	}
	path := filepath.Join(t.TempDir(), "huge.png")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := DecodeFileAll(path)
	if !errors.Is(err, ErrTooManyPixels) {
		t.Fatalf("got %v, want ErrTooManyPixels", err)
	}
}

func TestDimensionsAtTheLimitStillReachTheDecoder(t *testing.T) {
	data := pngDeclaring(t, 8_000, 5_000)
	_, err := decodeLimited(bytes.NewReader(data))
	if errors.Is(err, ErrTooManyPixels) {
		t.Fatalf("8000x5000 is exactly the limit and must not be refused as too large")
	}
}

func TestOversizedImageFileIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxImageFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	_, err = DecodeFileAll(path)
	if !errors.Is(err, ErrImageFileTooLarge) {
		t.Fatalf("got %v, want ErrImageFileTooLarge", err)
	}
}

type endlessReader struct{}

func (endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestOversizedImageStreamIsRefused(t *testing.T) {
	_, err := decodeLimited(io.Reader(endlessReader{}))
	if !errors.Is(err, ErrImageFileTooLarge) {
		t.Fatalf("got %v, want ErrImageFileTooLarge", err)
	}
}
