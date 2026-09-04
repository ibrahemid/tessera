// This file lives in the external test package: the table drives detect for
// entries marked "via": "detect", and detect imports importers.
package importers_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/detect"
	"github.com/ibrahemid/tessera/go/internal/importers"
)

// expectedAccount is one row of testdata/expected.json, the fixture table both
// cores run against.
type expectedAccount struct {
	Type      string `json:"type"`
	Issuer    string `json:"issuer"`
	Account   string `json:"account"`
	SecretB32 string `json:"secret_b32"`
	Algorithm string `json:"algorithm"`
	Digits    int    `json:"digits"`
	Period    int    `json:"period"`
	Counter   int64  `json:"counter"`
}

type expectedEntry struct {
	File            string            `json:"file"`
	Source          string            `json:"source"`
	Via             string            `json:"via"`
	Cores           []string          `json:"cores"`
	Accounts        []expectedAccount `json:"accounts"`
	Reject          string            `json:"reject"`
	MessageContains string            `json:"message_contains"`
}

func TestTestdataTable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "expected.json"))
	if err != nil {
		t.Fatalf("read expected.json: %v", err)
	}
	var entries []expectedEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("parse expected.json: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected.json is empty")
	}
	for _, e := range entries {
		if !runsOnGo(e.Cores) {
			continue
		}
		t.Run(e.File, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", e.File))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			accts, source, err := runFixture(t, e, data)
			if e.Source != "" && source != e.Source {
				t.Fatalf("source = %q, want %q", source, e.Source)
			}
			if e.Reject != "" {
				if err == nil {
					t.Fatalf("expected the export to be rejected (%s), got %d accounts", e.Reject, len(accts))
				}
				if !strings.Contains(err.Error(), e.MessageContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), e.MessageContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			assertAccounts(t, accts, e.Accounts)
		})
	}
}

// runFixture parses one fixture the way its "via" says: through importers.Parse
// (the default) or through detect.ParseText, which is how a plain-text export
// reaches the same accounts.
func runFixture(t *testing.T, e expectedEntry, data []byte) ([]account.Account, string, error) {
	t.Helper()
	switch e.Via {
	case "", "importers":
		accts, source, ok, err := importers.Parse(data)
		if !ok {
			t.Fatalf("importers.Parse did not recognize the fixture as an export")
		}
		return accts, source, err
	case "detect":
		accts, errs := detect.ParseText(string(data))
		if len(errs) > 0 {
			return accts, "", errs[0]
		}
		return accts, "", nil
	default:
		t.Fatalf("unknown via %q", e.Via)
		return nil, "", nil
	}
}

func assertAccounts(t *testing.T, got []account.Account, want []expectedAccount) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d accounts, want %d", len(got), len(want))
	}
	for i, w := range want {
		a := got[i]
		if string(a.Type) != w.Type {
			t.Errorf("account %d: type = %q, want %q", i, a.Type, w.Type)
		}
		if a.Issuer != w.Issuer {
			t.Errorf("account %d: issuer = %q, want %q", i, a.Issuer, w.Issuer)
		}
		if a.Account != w.Account {
			t.Errorf("account %d: account = %q, want %q", i, a.Account, w.Account)
		}
		secret, err := base32x.Decode(w.SecretB32)
		if err != nil {
			t.Fatalf("account %d: expected secret is not base32: %v", i, err)
		}
		if !bytes.Equal(a.Secret, secret) {
			t.Errorf("account %d: secret mismatch", i)
		}
		if a.Algorithm != w.Algorithm {
			t.Errorf("account %d: algorithm = %q, want %q", i, a.Algorithm, w.Algorithm)
		}
		if a.Digits != w.Digits {
			t.Errorf("account %d: digits = %d, want %d", i, a.Digits, w.Digits)
		}
		if a.Period != w.Period {
			t.Errorf("account %d: period = %d, want %d", i, a.Period, w.Period)
		}
		if a.Counter != w.Counter {
			t.Errorf("account %d: counter = %d, want %d", i, a.Counter, w.Counter)
		}
	}
}

func runsOnGo(cores []string) bool {
	if len(cores) == 0 {
		return true
	}
	for _, c := range cores {
		if c == "go" {
			return true
		}
	}
	return false
}
