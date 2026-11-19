package importers

import (
	"archive/zip"
	"bytes"
	"errors"
	"testing"
)

func parseOne(t *testing.T, data string) ([]string, []string, []string) {
	t.Helper()
	accts, _, ok, err := Parse([]byte(data))
	if !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	var issuers, names, notes []string
	for _, a := range accts {
		issuers = append(issuers, a.Issuer)
		names = append(names, a.Account)
		notes = append(notes, a.Note)
	}
	return issuers, names, notes
}

func TestImportersCleanText(t *testing.T) {
	cases := []struct {
		name, data, issuer, account string
	}{
		{
			"aegis",
			`{"version":1,"header":{},"db":{"version":3,"entries":[{"type":"totp","name":"\ufeffjohn\u200d\u0007","issuer":" \u202eGit\u0085Hub\u2069 ","info":{"secret":"JBSWY3DPEHPK3PXP","algo":"SHA1","digits":6,"period":30}}]}}`,
			"GitHub", "john\u200d",
		},
		{
			"bitwarden setup key",
			`{"folders":[],"items":[{"name":"\u202eBank\u0000","login":{"username":" ops\u001b","totp":"JBSWY3DPEHPK3PXP"}}]}`,
			"Bank", "ops",
		},
		{
			"bitwarden steam fallback",
			`{"folders":[],"items":[{"name":"Valve\u2066","login":{"username":"\ufeff ","totp":"steam://JBSWY3DPEHPK3PXP"}}]}`,
			"Steam", "Valve",
		},
		{
			"apple csv",
			"Title,URL,Username,Password,Notes,OTPAuth\n\"\u202eExample\",https://e.com,\"alice\u0007\",pw,,JBSWY3DPEHPK3PXP\n",
			"Example", "alice",
		},
	}
	for _, c := range cases {
		issuers, names, _ := parseOne(t, c.data)
		if len(issuers) != 1 || issuers[0] != c.issuer || names[0] != c.account {
			t.Errorf("%s: got %q/%q, want %q/%q", c.name, issuers, names, c.issuer, c.account)
		}
	}
}

func TestImportersMapNote(t *testing.T) {
	const raw = "line1\r\nline2\rline3\u001b[0m\tend\u202e "
	const want = "line1\nline2\nline3[0m\tend"
	cases := []struct {
		name, data string
		notes      []string
	}{
		{
			"aegis",
			`{"version":1,"header":{},"db":{"version":3,"entries":[{"type":"totp","name":"a","issuer":"b","note":"line1\r\nline2\rline3\u001b[0m\tend\u202e ","info":{"secret":"JBSWY3DPEHPK3PXP"}},{"type":"totp","name":"c","issuer":"d","info":{"secret":"JBSWY3DPEHPK3PXP"}}]}}`,
			[]string{want, ""},
		},
		{
			"bitwarden",
			`{"folders":[],"items":[{"name":"a","notes":"line1\r\nline2\rline3\u001b[0m\tend\u202e ","login":{"totp":"JBSWY3DPEHPK3PXP"}},{"name":"b","notes":null,"login":{"totp":"JBSWY3DPEHPK3PXP"}}]}`,
			[]string{want, ""},
		},
		{
			"proton",
			`{"version":1,"entries":[{"note":"line1\r\nline2\rline3\u001b[0m\tend\u202e ","content":{"uri":"otpauth://totp/a:b?secret=JBSWY3DPEHPK3PXP","entry_type":"Totp","name":"a"}},{"note":null,"content":{"uri":"otpauth://totp/c:d?secret=JBSWY3DPEHPK3PXP","entry_type":"Totp","name":"c"}}]}`,
			[]string{want, ""},
		},
		{
			"apple csv",
			"Title,URL,Username,Password,Notes,OTPAuth\na,,b,,\"" + raw + "\",JBSWY3DPEHPK3PXP\nc,,d,,,JBSWY3DPEHPK3PXP\n",
			[]string{want, ""},
		},
		{
			"1password csv",
			"Title,Url,Username,Password,OTPAuth,Favorite,Archived,Tags,Notes\na,,b,,JBSWY3DPEHPK3PXP,false,false,,\"" + raw + "\"\nc,,d,,JBSWY3DPEHPK3PXP,false,false,\n",
			[]string{want, ""},
		},
		{
			"1pux",
			`{"accounts":[{"vaults":[{"items":[{"details":{"notesPlain":"line1\r\nline2\rline3\u001b[0m\tend\u202e ","sections":[{"fields":[{"value":{"totp":"JBSWY3DPEHPK3PXP"}}]}]},"overview":{"title":"a"}},{"details":{"sections":[{"fields":[{"value":{"totp":"JBSWY3DPEHPK3PXP"}}]}]},"overview":{"title":"b"}}]}]}]}`,
			[]string{want, ""},
		},
	}
	for _, c := range cases {
		_, _, notes := parseOne(t, c.data)
		if len(notes) != len(c.notes) {
			t.Fatalf("%s: got %d accounts, want %d", c.name, len(notes), len(c.notes))
		}
		for i := range notes {
			if notes[i] != c.notes[i] {
				t.Errorf("%s account %d: note = %q, want %q", c.name, i, notes[i], c.notes[i])
			}
		}
	}
}

func TestParse1PUXRejectsOversizedExportData(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("export.data")
	if err != nil {
		t.Fatal(err)
	}
	chunk := make([]byte, 1<<20)
	for range 64 {
		if _, err := w.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.Write([]byte{' '}); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_, source, ok, err := Parse(buf.Bytes())
	if !ok || source != "1Password" || !errors.Is(err, ErrExportDataTooLarge) {
		t.Fatalf("ok=%v source=%q err=%v", ok, source, err)
	}
}
