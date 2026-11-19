package account

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/otp"
	"github.com/ibrahemid/tessera/go/internal/spectest"
)

type v2AccountVectors struct {
	VaultV2 struct {
		Canonical []struct {
			Name                  string   `json:"name"`
			Input                 string   `json:"input"`
			Clear                 []string `json:"clear"`
			ExpectedCanonicalJSON string   `json:"expected_canonical_json"`
			ExpectedVersion       int      `json:"expected_version"`
		} `json:"canonical"`
	} `json:"vault_v2"`
	PayloadStrict struct {
		Invalid []struct {
			Name    string `json:"name"`
			Payload string `json:"payload"`
			Error   string `json:"error"`
		} `json:"invalid"`
	} `json:"payload_strict"`
	TextClean struct {
		Text []v2TextCase `json:"text"`
		Note []v2TextCase `json:"note"`
	} `json:"text_clean"`
	HOTPCounterRange struct {
		MaxCounter    int64 `json:"max_counter"`
		PayloadAccept []struct {
			Counter int64  `json:"counter"`
			Payload string `json:"payload"`
		} `json:"payload_accept"`
		PayloadReject []struct {
			Counter int64  `json:"counter"`
			Payload string `json:"payload"`
			Error   string `json:"error"`
		} `json:"payload_reject"`
		Advance []struct {
			From int64 `json:"from"`
			To   int64 `json:"to"`
		} `json:"advance"`
		AdvanceRefused []int64 `json:"advance_refused"`
		CodeAtMax      struct {
			SecretB64 string `json:"secret_b64"`
			Counter   int64  `json:"counter"`
			Digits    int    `json:"digits"`
			Algorithm string `json:"algorithm"`
			Code      string `json:"code"`
		} `json:"code_at_max"`
	} `json:"hotp_counter_range"`
	DisplayOrder struct {
		Cases []struct {
			Name     string `json:"name"`
			Accounts []struct {
				ID        string `json:"id"`
				Issuer    string `json:"issuer"`
				Account   string `json:"account"`
				Order     int    `json:"order"`
				DeletedAt int64  `json:"deleted_at"`
			} `json:"accounts"`
			ExpectedIDs []string `json:"expected_ids"`
		} `json:"cases"`
	} `json:"display_order"`
}

type v2TextCase struct {
	Name string `json:"name"`
	In   string `json:"in"`
	Out  string `json:"out"`
}

func loadV2AccountVectors(t *testing.T) v2AccountVectors {
	t.Helper()
	var v v2AccountVectors
	spectest.Load(t, &v)
	return v
}

var v2PayloadErrors = map[string]error{
	"duplicate_key":        ErrDuplicateKey,
	"non_canonical_number": ErrNonCanonicalNumber,
	"field_type":           ErrFieldType,
	"non_canonical_base64": ErrNonCanonicalBase64,
	"malformed_payload":    ErrMalformedPayload,
	"counter_range":        ErrCounterRange,
}

func v2PayloadError(t *testing.T, code string) error {
	t.Helper()
	want, ok := v2PayloadErrors[code]
	if !ok {
		t.Fatalf("unknown error code %q", code)
	}
	return want
}

func TestV2VectorsCanonical(t *testing.T) {
	v := loadV2AccountVectors(t)
	if len(v.VaultV2.Canonical) == 0 {
		t.Fatal("vault_v2.canonical is empty")
	}
	for _, c := range v.VaultV2.Canonical {
		t.Run(c.Name, func(t *testing.T) {
			accts, err := ParseCanonicalJSON([]byte(c.Input))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			for i := range accts {
				for _, key := range c.Clear {
					switch key {
					case "deleted_at":
						accts[i].DeletedAt = 0
					case "note":
						accts[i].Note = ""
					case "order":
						accts[i].Order = 0
					default:
						if _, ok := accts[i].Extra[key]; !ok {
							t.Fatalf("clear %q: key not present", key)
						}
						delete(accts[i].Extra, key)
					}
				}
			}
			got, err := CanonicalJSON(accts)
			if err != nil {
				t.Fatalf("canonical: %v", err)
			}
			if string(got) != c.ExpectedCanonicalJSON {
				t.Fatalf("canonical mismatch\n got  %s\n want %s", got, c.ExpectedCanonicalJSON)
			}
			if ver := FormatVersion(accts); ver != c.ExpectedVersion {
				t.Fatalf("FormatVersion = %d, want %d", ver, c.ExpectedVersion)
			}
		})
	}
}

func TestV2VectorsPayloadStrict(t *testing.T) {
	v := loadV2AccountVectors(t)
	if len(v.PayloadStrict.Invalid) == 0 {
		t.Fatal("payload_strict.invalid is empty")
	}
	for _, c := range v.PayloadStrict.Invalid {
		t.Run(c.Name, func(t *testing.T) {
			want := v2PayloadError(t, c.Error)
			if _, err := ParseCanonicalJSON([]byte(c.Payload)); !errors.Is(err, want) {
				t.Fatalf("err = %v, want %v", err, want)
			}
		})
	}
}

func TestV2VectorsTextClean(t *testing.T) {
	v := loadV2AccountVectors(t)
	if len(v.TextClean.Text) == 0 || len(v.TextClean.Note) == 0 {
		t.Fatal("text_clean is empty")
	}
	for _, c := range v.TextClean.Text {
		if got := CleanText(c.In); got != c.Out {
			t.Errorf("text %s: CleanText(%q) = %q, want %q", c.Name, c.In, got, c.Out)
		}
	}
	for _, c := range v.TextClean.Note {
		if got := CleanNote(c.In); got != c.Out {
			t.Errorf("note %s: CleanNote(%q) = %q, want %q", c.Name, c.In, got, c.Out)
		}
	}
}

func TestV2VectorsDisplayOrder(t *testing.T) {
	v := loadV2AccountVectors(t)
	if len(v.DisplayOrder.Cases) == 0 {
		t.Fatal("display_order.cases is empty")
	}
	for _, c := range v.DisplayOrder.Cases {
		t.Run(c.Name, func(t *testing.T) {
			accts := make([]Account, len(c.Accounts))
			for i, a := range c.Accounts {
				accts[i] = Account{ID: a.ID, Issuer: a.Issuer, Account: a.Account, Order: a.Order, DeletedAt: a.DeletedAt}
			}
			live := Live(accts)
			SortForDisplay(live)
			ids := make([]string, len(live))
			for i, a := range live {
				ids[i] = a.ID
			}
			if !slices.Equal(ids, c.ExpectedIDs) {
				t.Fatalf("order = %v, want %v", ids, c.ExpectedIDs)
			}
		})
	}
}

func TestV2VectorsHOTPCounterRange(t *testing.T) {
	v := loadV2AccountVectors(t).HOTPCounterRange
	if v.MaxCounter != MaxCounter {
		t.Fatalf("max_counter = %d, want %d", v.MaxCounter, MaxCounter)
	}
	if len(v.PayloadAccept) == 0 || len(v.PayloadReject) == 0 || len(v.Advance) == 0 || len(v.AdvanceRefused) == 0 {
		t.Fatal("hotp_counter_range is incomplete")
	}
	for _, c := range v.PayloadAccept {
		accts, err := ParseCanonicalJSON([]byte(c.Payload))
		if err != nil {
			t.Fatalf("accept %d: %v", c.Counter, err)
		}
		if len(accts) != 1 || accts[0].Counter != c.Counter {
			t.Fatalf("accept %d: parsed %+v", c.Counter, accts)
		}
		out, err := CanonicalJSON(accts)
		if err != nil || string(out) != c.Payload {
			t.Fatalf("accept %d: round trip = %s, %v", c.Counter, out, err)
		}
	}
	for _, c := range v.PayloadReject {
		want := v2PayloadError(t, c.Error)
		if _, err := ParseCanonicalJSON([]byte(c.Payload)); !errors.Is(err, want) {
			t.Fatalf("reject %d: err = %v, want %v", c.Counter, err, want)
		}
	}
	for _, c := range v.Advance {
		got, err := NextCounter(c.From)
		if err != nil || got != c.To {
			t.Fatalf("NextCounter(%d) = %d, %v; want %d", c.From, got, err, c.To)
		}
	}
	for _, from := range v.AdvanceRefused {
		if _, err := NextCounter(from); !errors.Is(err, ErrCounterRange) {
			t.Fatalf("NextCounter(%d) err = %v, want ErrCounterRange", from, err)
		}
	}
	code := v.CodeAtMax
	secret, err := base64.StdEncoding.DecodeString(code.SecretB64)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	alg, err := otp.ParseAlgorithm(code.Algorithm)
	if err != nil {
		t.Fatalf("algorithm: %v", err)
	}
	got, err := otp.HOTP(secret, uint64(code.Counter), code.Digits, alg)
	if err != nil || got != code.Code {
		t.Fatalf("HOTP at %d = %q, %v; want %q", code.Counter, got, err, code.Code)
	}
}

func TestV2VectorsCanonicalEdge(t *testing.T) {
	path := filepath.Join(filepath.Dir(spectest.Path(t)), "canonical_edge_v2.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	accts, err := ParseCanonicalJSON(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, err := CanonicalJSON(accts)
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("round trip mismatch\n got  %s\n want %s", got, data)
	}
	if ver := FormatVersion(accts); ver != 2 {
		t.Fatalf("FormatVersion = %d, want 2", ver)
	}
}

func TestNextCounter(t *testing.T) {
	cases := []struct {
		from, to int64
		err      bool
	}{
		{0, 1, false},
		{MaxCounter - 1, MaxCounter, false},
		{MaxCounter, 0, true},
		{-1, 0, true},
	}
	for _, c := range cases {
		got, err := NextCounter(c.from)
		if c.err {
			if !errors.Is(err, ErrCounterRange) {
				t.Errorf("NextCounter(%d) err = %v, want ErrCounterRange", c.from, err)
			}
			continue
		}
		if err != nil || got != c.to {
			t.Errorf("NextCounter(%d) = %d, %v; want %d", c.from, got, err, c.to)
		}
	}
}

func TestParseRejectsCounterOutsideRange(t *testing.T) {
	for _, counter := range []string{"-1", "9223372036854775807"} {
		payload := `[{"account":"ops","algorithm":"SHA1","counter":` + counter + `,"created_at":1700000000,` +
			`"digits":6,"folder":"","id":"id1","issuer":"Bank","period":30,"pinned":false,` +
			`"secret":"MTIzNDU2Nzg5MDEyMzQ1Njc4OTA=","tags":[],"type":"hotp","updated_at":1700000000}]`
		if _, err := ParseCanonicalJSON([]byte(payload)); !errors.Is(err, ErrCounterRange) {
			t.Errorf("counter %s: err = %v, want ErrCounterRange", counter, err)
		}
	}
}
