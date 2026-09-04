package exporters

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/migration"
	"github.com/ibrahemid/tessera/go/internal/qr"
)

// migrationFixture builds a vault big enough to span several QR codes, mixing
// every combination the schema can carry plus three it cannot.
func migrationFixture(t *testing.T) (eligible, ineligible []account.Account) {
	t.Helper()
	algos := []string{"SHA1", "SHA256", "SHA512"}
	for i := 0; i < 60; i++ {
		secret := []byte(fmt.Sprintf("secret-bytes-%02d", i))
		a := account.Account{
			ID:        fmt.Sprintf("id%02d", i),
			Type:      account.TOTP,
			Issuer:    fmt.Sprintf("Issuer %02d", i),
			Account:   fmt.Sprintf("user%02d@example.com", i),
			Secret:    secret,
			Algorithm: algos[i%3],
			Digits:    6,
			Period:    30,
		}
		if i%5 == 0 {
			a.Digits = 8
		}
		if i%7 == 0 {
			a.Type = account.HOTP
			a.Counter = int64(i) * 3
			a.Period = 0
		}
		eligible = append(eligible, a)
	}
	ineligible = []account.Account{
		{ID: "s1", Type: account.Steam, Issuer: "Steam", Account: "gaben",
			Secret: mustSecret(t, "NBSWY3DPEB3W64TM"), Algorithm: "SHA1", Digits: 5, Period: 30},
		{ID: "s2", Type: account.TOTP, Issuer: "Seven", Account: "digits",
			Secret: mustSecret(t, "JBSWY3DPEHPK3PXP"), Algorithm: "SHA1", Digits: 7, Period: 30},
		{ID: "s3", Type: account.TOTP, Issuer: "Slow", Account: "period",
			Secret: mustSecret(t, "GEZDGNBVGY3TQOJQ"), Algorithm: "SHA1", Digits: 6, Period: 60},
	}
	return eligible, ineligible
}

// TestMigrationQRRoundTrip is the guarantee behind the batch caps: 60 mixed
// accounts are exported, the PNGs are read back with the same decoder `tess
// add --qr` uses, and every eligible account comes back exactly as it went in.
// A decode failure here means maxPayloadBytes is too high, never too low.
func TestMigrationQRRoundTrip(t *testing.T) {
	eligible, ineligible := migrationFixture(t)
	all := append(append([]account.Account{}, eligible...), ineligible...)

	e, ok := Lookup("google-migration")
	if !ok {
		t.Fatal("google-migration is not registered")
	}
	files, skipped, err := e.Render(all)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(files) < 6 {
		t.Fatalf("60 accounts should span at least 6 batches, got %d", len(files))
	}
	if len(skipped) != len(ineligible) {
		t.Fatalf("want %d skipped accounts, got %+v", len(ineligible), skipped)
	}

	dir := t.TempDir()
	var got []account.Account
	for i, f := range files {
		want := fmt.Sprintf("google-migration-%d-of-%d.png", i+1, len(files))
		if f.Name != want {
			t.Fatalf("file %d is named %q, want %q", i, f.Name, want)
		}
		path := filepath.Join(dir, f.Name)
		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		payloads, err := qr.DecodeFileAll(path)
		if err != nil {
			t.Fatalf("decode %s: %v", f.Name, err)
		}
		if len(payloads) != 1 {
			t.Fatalf("%s holds %d QR codes, want 1", f.Name, len(payloads))
		}
		parsed, err := migration.Parse(payloads[0])
		if err != nil {
			t.Fatalf("parse %s: %v", f.Name, err)
		}
		got = append(got, parsed...)
	}

	if len(got) != len(eligible) {
		t.Fatalf("round-tripped %d accounts, want %d", len(got), len(eligible))
	}
	for i := range eligible {
		sameAccount(t, got[i], eligible[i], fmt.Sprintf("migration #%d", i))
	}

	// Every ineligible account is reported and none of them reached a QR.
	for _, a := range ineligible {
		if !isSkipped(skipped, a) {
			t.Errorf("%s should have been reported as skipped", label(a))
		}
		for _, g := range got {
			if base32x.EncodeNoPad(g.Secret) == base32x.EncodeNoPad(a.Secret) {
				t.Errorf("%s was excluded from the export but appears in a QR", label(a))
			}
		}
	}
}

// TestMigrationNameSplitEdge pins the one documented lossy case: with no issuer
// and a ':' inside the account name, a reader splits the name field on that
// colon. Google Authenticator's own format has nowhere else to put the label,
// so this is the observed behavior, not a bug to work around.
func TestMigrationNameSplitEdge(t *testing.T) {
	a := account.Account{ID: "x", Type: account.TOTP, Account: "host:port",
		Secret: mustSecret(t, "JBSWY3DPEHPK3PXP"), Algorithm: "SHA1", Digits: 6, Period: 30}
	params, reason := encodeOtpParameters(a)
	if reason != "" {
		t.Fatalf("account should be encodable, got %q", reason)
	}
	payload := assemblePayload([][]byte{params}, 1, 0, 1)
	uri := "otpauth-migration://offline?data=" + urlQueryEscape(payload)
	back, err := migration.Parse(uri)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(back) != 1 {
		t.Fatalf("want 1 account, got %d", len(back))
	}
	if back[0].Issuer != "host" || back[0].Account != "port" {
		t.Fatalf("documented split expected issuer %q account %q, got %q / %q",
			"host", "port", back[0].Issuer, back[0].Account)
	}
}

// TestMigrationBatchIDIsShared: every QR of one export carries the same
// batch_id, which is how a phone knows the codes belong together.
func TestMigrationBatchIDIsShared(t *testing.T) {
	eligible, _ := migrationFixture(t)
	e, _ := Lookup("google-migration")
	files, _, err := e.Render(eligible)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	dir := t.TempDir()
	ids := map[int64]bool{}
	for _, f := range files {
		path := filepath.Join(dir, f.Name)
		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		payloads, err := qr.DecodeFileAll(path)
		if err != nil {
			t.Fatalf("decode %s: %v", f.Name, err)
		}
		ids[batchIDOf(t, payloads[0])] = true
	}
	if len(ids) != 1 {
		t.Fatalf("batches carry %d different batch ids, want 1", len(ids))
	}
}

// TestMigrationEmptyExport: a vault of nothing but Steam accounts writes no QR
// and reports every account.
func TestMigrationEmptyExport(t *testing.T) {
	steam := account.Account{ID: "s", Type: account.Steam, Issuer: "Steam", Account: "gaben",
		Secret: mustSecret(t, "NBSWY3DPEB3W64TM"), Algorithm: "SHA1", Digits: 5, Period: 30}
	e, _ := Lookup("google-migration")
	files, skipped, err := e.Render([]account.Account{steam})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("want no files, got %d", len(files))
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0].Reason, "Steam") {
		t.Fatalf("want the Steam account reported, got %+v", skipped)
	}
}

// urlQueryEscape renders a payload the way Render does, so the split-edge test
// exercises the same URI form the QR carries.
func urlQueryEscape(payload []byte) string {
	return url.QueryEscape(base64.StdEncoding.EncodeToString(payload))
}

// batchIDOf reads field 5 (batch_id) out of a migration URI's payload.
func batchIDOf(t *testing.T, uri string) int64 {
	t.Helper()
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parse uri: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(u.Query().Get("data"))
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	for len(raw) > 0 {
		num, typ, n := protowire.ConsumeTag(raw)
		if n < 0 {
			t.Fatalf("malformed payload")
		}
		raw = raw[n:]
		if num == fieldBatchID {
			v, n := protowire.ConsumeVarint(raw)
			if n < 0 {
				t.Fatalf("malformed batch id")
			}
			return int64(v)
		}
		n = protowire.ConsumeFieldValue(num, typ, raw)
		if n < 0 {
			t.Fatalf("malformed field %d", num)
		}
		raw = raw[n:]
	}
	t.Fatal("payload has no batch id")
	return 0
}
