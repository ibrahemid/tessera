package main

import (
	"fmt"
	"unicode/utf8"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/exporters"
)

const (
	exportsBatchID  = int32(-1450267893)
	migrationFormat = "google-migration"
)

func exportAccount(suffix, issuer, name string, secret []byte, updated int64) account.Account {
	return account.Account{
		ID: "00000000-0000-4000-8000-0000000002" + suffix, Type: account.TOTP, Issuer: issuer, Account: name,
		Secret: secret, Algorithm: "SHA1", Digits: 6, Period: 30,
		CreatedAt: v2Created, UpdatedAt: v2Created + updated,
	}
}

func exportAccounts() []account.Account {
	github := exportAccount("01", "GitHub", "octocat@example.com", []byte("12345678901234567890"), 3600)
	github.Pinned = true
	github.Folder = "Work"
	github.Tags = []string{"code", "oss"}
	github.Note = "Recovery codes are in the safe"
	github.Handle = "gh"

	cloud := exportAccount("02", "Cloud", "ops@example.com", pattern(32, 0x30), 60)
	cloud.Algorithm, cloud.Digits, cloud.Period = "SHA256", 8, 60

	vault := exportAccount("03", "Vault", "root", pattern(64, 0x50), 120)
	vault.Algorithm, vault.Digits = "SHA512", 7

	bank := exportAccount("04", "Bank", "customer-42", pattern(20, 0x70), 180)
	bank.Type, bank.Counter = account.HOTP, 7

	steam := exportAccount("05", "Steam", "gaben", pattern(20, 0x90), 240)
	steam.Type, steam.Digits = account.Steam, 5

	escapes := exportAccount("06", "Café \"Zürich\", <&> \\ 100%", "naïve user+tag@例え.jp", pattern(20, 0xa0), 300)
	escapes.Note = "Line one, \"quoted\"\nline two and \U0001F510"

	formulaName := exportAccount("07", "=1+1", "formula", pattern(20, 0xb4), 360)

	formulaNote := exportAccount("08", "Notes", "formula-note", pattern(20, 0xc8), 420)
	formulaNote.Note = "+44 20 7946 0000 is the backup line"

	noIssuer := exportAccount("09", "", "solo@example.com", pattern(20, 0xdc), 480)
	issuerOnly := exportAccount("10", "IssuerOnly", "", pattern(20, 0xf0), 540)

	accts := []account.Account{github, cloud, vault, bank, steam, escapes, formulaName, formulaNote, noIssuer, issuerOnly}
	fillers := []struct {
		algorithm string
		digits    int
	}{{"SHA256", 8}, {"SHA512", 6}, {"SHA1", 8}, {"SHA1", 6}, {"SHA1", 6}, {"SHA1", 6}}
	for i, f := range fillers {
		n := len(accts) + 1
		a := exportAccount(fmt.Sprintf("%02d", n), fmt.Sprintf("Service %02d", i+1), fmt.Sprintf("user%02d@example.com", i+1),
			pattern(20, byte(0x11*(i+1))), int64(60*n))
		a.Algorithm, a.Digits = f.algorithm, f.digits
		accts = append(accts, a)
	}
	return accts
}

func exportsBlock() object {
	accts := exportAccounts()
	formats := make([]object, 0, len(exporters.Names()))
	for _, e := range exporters.All() {
		if e.Name() == migrationFormat {
			codes, skipped := exporters.MigrationCodes(accts, exportsBatchID)
			if len(codes) < 2 {
				panic(fmt.Sprintf("vectorgen: exports: %d migration code(s), want at least 2", len(codes)))
			}
			list := make([]object, len(codes))
			for i, c := range codes {
				list[i] = object{{"name", c.Name}, {"uri", c.URI}}
			}
			formats = append(formats, object{{"format", e.Name()}, {"codes", list}, {"skipped", skippedObjects(skipped)}})
			continue
		}
		files, skipped, err := e.Render(accts)
		if err != nil {
			panic(fmt.Sprintf("vectorgen: exports: %s: %v", e.Name(), err))
		}
		list := make([]object, len(files))
		for i, f := range files {
			if !utf8.Valid(f.Data) {
				panic(fmt.Sprintf("vectorgen: exports: %s writes %s as non-UTF-8", e.Name(), f.Name))
			}
			list[i] = object{{"name", f.Name}, {"content", string(f.Data)}}
		}
		formats = append(formats, object{{"format", e.Name()}, {"files", list}, {"skipped", skippedObjects(skipped)}})
	}
	return object{
		{"batch_id", int64(exportsBatchID)},
		{"canonical_json", string(mustCanonical(accts...))},
		{"formats", formats},
	}
}

func skippedObjects(skipped []exporters.Skipped) []object {
	out := make([]object, len(skipped))
	for i, s := range skipped {
		out[i] = object{{"label", s.Label}, {"kind", string(s.Kind)}, {"reason", s.Reason}}
	}
	return out
}
