package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/spectest"
)

func readSpecFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(spectest.Path(t)), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

func TestVectorsFileMatchesGenerator(t *testing.T) {
	file := readSpecFile(t, "testvectors.json")
	var out bytes.Buffer
	if err := runV2(bytes.NewReader(file), &out); err != nil {
		t.Fatalf("runV2: %v", err)
	}
	if !bytes.Equal(out.Bytes(), file) {
		t.Fatal("spec/testvectors.json differs from the generator output; regenerate with `vectorgen v2`")
	}
	for i, c := range out.Bytes() {
		if c > 0x7e || (c < 0x20 && c != '\n') {
			t.Fatalf("byte %d is 0x%02x; the vectors file must stay printable ASCII", i, c)
		}
	}
}

func TestSpliceV2RejectsUnterminatedInput(t *testing.T) {
	if _, err := spliceV2([]byte(`{"a": 1`), nil); !errors.Is(err, errVectorsShape) {
		t.Fatalf("err = %v, want errVectorsShape", err)
	}
}

func TestSpliceV2AppendsAfterExistingKeys(t *testing.T) {
	got, err := spliceV2([]byte("{\n  \"a\": 1\n}\n"), []member{{"b", object{{"c", "\u00e9"}}}})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": 1,\n  \"b\": {\n    \"c\": \"\\u00e9\"\n  }\n}\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	again, err := spliceV2(got, []member{{"vault_v2", 1}})
	if err != nil {
		t.Fatal(err)
	}
	if again2, err := spliceV2(again, []member{{"vault_v2", 2}}); err != nil || string(again2) != "{\n  \"a\": 1,\n  \"b\": {\n    \"c\": \"\\u00e9\"\n  },\n  \"vault_v2\": 2\n}\n" {
		t.Fatalf("re-splice = %q, %v", again2, err)
	}
}

func TestCanonicalEdgeFilesMatchGenerator(t *testing.T) {
	if got := readSpecFile(t, "canonical_edge.json"); !bytes.Equal(edgeCanonical(), got) {
		t.Fatal("spec/canonical_edge.json differs from `vectorgen edge`")
	}
	if got := readSpecFile(t, "canonical_edge_v2.json"); !bytes.Equal(edgeCanonicalV2(), got) {
		t.Fatal("spec/canonical_edge_v2.json differs from `vectorgen edge2`")
	}
}

func TestASCIIJSONEscapesNonASCII(t *testing.T) {
	got := string(asciiJSON([]byte("a\u00e9\u2028\x7f\U0001F510")))
	want := `a\u00e9\u2028\u007f\ud83d\udd10`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
