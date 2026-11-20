package importers

import (
	"archive/zip"
	"bytes"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParse1PUXDamagedArchives(t *testing.T) {
	cases := []struct {
		file string
		is   error
		want string
	}{
		{"1password-bomb.1pux", zip.ErrFormat, "1password: read export.data: zip: not a valid zip file"},
		{"1password-damaged.1pux", zip.ErrChecksum, "1password: read export.data: zip: checksum error"},
		{"1password-truncated.1pux", zip.ErrFormat, "1password: open archive: zip: not a valid zip file"},
		{"1password-oversized.1pux", ErrExportDataTooLarge, "1password: export.data in the archive is larger than 64 MiB"},
	}
	for _, c := range cases {
		_, source, ok, err := Parse(readFixture(t, c.file))
		if !ok || source != "1Password" || !errors.Is(err, c.is) || err.Error() != c.want {
			t.Errorf("%s: ok=%v source=%q err=%v, want %q", c.file, ok, source, err, c.want)
		}
	}
}

func TestParse1PUXChecksEntryHeaders(t *testing.T) {
	data := readFixture(t, "1password-export.data.json")
	crc := crc32.ChecksumIEEE(data)
	cases := []struct {
		name   string
		header zip.FileHeader
		want   string
	}{
		{"short body", zip.FileHeader{Method: zip.Store, CRC32: crc, UncompressedSize64: uint64(len(data)) + 5},
			"1password: read export.data: unexpected EOF"},
		{"wrong checksum", zip.FileHeader{Method: zip.Store, CRC32: crc ^ 1, UncompressedSize64: uint64(len(data))},
			"1password: read export.data: zip: checksum error"},
		{"unknown method", zip.FileHeader{Method: 12, CRC32: crc, UncompressedSize64: uint64(len(data))},
			"1password: open export.data: zip: unsupported compression algorithm"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		h := c.header
		h.Name = "export.data"
		h.CompressedSize64 = uint64(len(data))
		w, err := zw.CreateRaw(&h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		_, _, ok, err := Parse(buf.Bytes())
		if !ok || err == nil || err.Error() != c.want {
			t.Errorf("%s: ok=%v err=%v, want %q", c.name, ok, err, c.want)
		}
	}
}

func TestParse1PUXReadsOnlyTheRootExportData(t *testing.T) {
	_, _, ok, err := Parse(readFixture(t, "1password-traversal.1pux"))
	if !ok || err == nil || err.Error() != "archive holds no export.data; a 1Password .1pux export does" {
		t.Fatalf("traversal names: ok=%v err=%v", ok, err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct {
		name string
		data []byte
	}{
		{"../export.data", []byte(`{"accounts":[]}`)},
		{"files/export.data", []byte(`not json`)},
		{"export.data", readFixture(t, "1password-export.data.json")},
	} {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	accts, _, ok, err := Parse(buf.Bytes())
	if !ok || err != nil || len(accts) != 2 {
		t.Fatalf("ok=%v err=%v accounts=%d, want the 2 accounts of the root export.data", ok, err, len(accts))
	}
}
