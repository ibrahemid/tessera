package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"unicode"

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

func TestVectorsFileCarriesExportsAndMerge(t *testing.T) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(readSpecFile(t, "testvectors.json"), &top); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"exports", "merge"} {
		if _, ok := top[key]; !ok {
			t.Errorf("spec/testvectors.json has no %q set", key)
		}
	}
}

func TestHandlesBlockMatchesGenerator(t *testing.T) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(readSpecFile(t, "testvectors.json"), &top); err != nil {
		t.Fatal(err)
	}
	var file, gen any
	if err := json.Unmarshal(top["handles"], &file); err != nil {
		t.Fatalf("handles: %v", err)
	}
	b, err := json.Marshal(handlesBlock())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &gen); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(file, gen) {
		t.Fatal("spec/testvectors.json handles differs from the generator; paste the handles object printed by `go -C go run ./internal/vectorgen`")
	}
}

func TestCaseMappingFilesMatchGenerator(t *testing.T) {
	if !bytes.Equal(readSpecFile(t, "case_mapping.json"), caseMappingJSON()) {
		t.Fatalf("spec/case_mapping.json differs from the Go toolchain's tables; regenerate with `%s`", caseMapCommand)
	}
	swift, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(spectest.Path(t))), "swift", "Sources", "TesseraCore", "CaseTables.swift"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(swift, caseSwift()) {
		t.Fatalf("swift/Sources/TesseraCore/CaseTables.swift differs from the Go toolchain's tables; regenerate with `%s`", caseSwiftCommand)
	}
}

func TestCaseRunsExpandToEveryMapping(t *testing.T) {
	for name, f := range map[string]func(rune) rune{"lower": unicode.ToLower, "upper": unicode.ToUpper} {
		runs := caseRuns(casePairs(f))
		for i := 1; i < len(runs); i++ {
			if runs[i].lo <= runs[i-1].hi {
				t.Fatalf("%s: run %d overlaps the run before it", name, i)
			}
		}
		lookup := func(r rune) rune {
			i := sort.Search(len(runs), func(i int) bool { return runs[i].hi >= r })
			if i == len(runs) || runs[i].lo > r || (r-runs[i].lo)%runs[i].stride != 0 {
				return r
			}
			return r + runs[i].delta
		}
		for r := rune(0); r <= unicode.MaxRune; r++ {
			if isScalar(r) && lookup(r) != f(r) {
				t.Fatalf("%s: U+%04X maps to U+%04X through the runs, want U+%04X", name, r, lookup(r), f(r))
			}
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
