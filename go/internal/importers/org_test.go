package importers

import (
	"slices"
	"strings"
	"testing"
)

type orgWant struct {
	folder string
	tags   []string
	pinned bool
	note   string
}

type orgCase struct {
	name string
	data string
	want []orgWant
}

func orgCases() []orgCase {
	return []orgCase{
		{
			"aegis groups in file order, legacy group last",
			`{"version":1,"db":{"version":3,"groups":[{"uuid":"g2","name":"Personal"},{"uuid":"g1","name":"Work"}],"entries":[` +
				`{"type":"totp","name":"a","info":{"secret":"JBSWY3DPEHPK3PXP"},"groups":["g1","g2","g1"],"group":"Old","favorite":true}]}}`,
			[]orgWant{{folder: "Work", tags: []string{"Personal", "Old"}, pinned: true}},
		},
		{
			"aegis wrong-typed organisation members are ignored",
			`{"version":1,"db":{"version":3,"groups":{"uuid":"g1"},"entries":[` +
				`{"type":"totp","name":"a","info":{"secret":"JBSWY3DPEHPK3PXP"},"groups":"g1","group":7,"favorite":"true"},` +
				`{"type":"totp","name":"b","info":{"secret":"JBSWY3DPEHPK3PXP"},"groups":[1,null,{"uuid":"g1"}],"group":null,"favorite":1}]}}`,
			[]orgWant{{}, {}},
		},
		{
			"aegis group list skips malformed rows; a repeated id keeps the later name",
			`{"version":1,"db":{"version":3,"groups":[5,null,{"uuid":1,"name":"N"},{"uuid":"g1","name":2},{"UUID":"g1","Name":"First"},{"uuid":"g1","name":"Second"}],"entries":[` +
				`{"type":"totp","name":"a","info":{"secret":"JBSWY3DPEHPK3PXP"},"groups":["g1"]}]}}`,
			[]orgWant{{folder: "Second"}},
		},
		{
			"2fas group by id; a missing group leaves the folder empty",
			`{"services":[{"name":"A","secret":"JBSWY3DPEHPK3PXP","otp":{"tokenType":"TOTP"},"groupId":"x"},` +
				`{"name":"B","secret":"JBSWY3DPEHPK3PXP","otp":{"tokenType":"TOTP"},"groupId":"nope"},` +
				`{"name":"C","secret":"JBSWY3DPEHPK3PXP","otp":{"tokenType":"TOTP"},"groupId":5}],` +
				`"groups":[{"id":"x","name":" \u202eWork\u0007 "}]}`,
			[]orgWant{{folder: "Work"}, {}, {}},
		},
		{
			"bitwarden folder first, collections after, duplicates dropped",
			`{"folders":[{"id":"f","name":"Ops"}],"collections":[{"id":"c1","name":"Ops"},{"id":"c2","name":"Shared"},{"id":"c3","name":"  "}],"items":[` +
				`{"name":"A","notes":"n\u0000ote","favorite":true,"folderId":"f","collectionIds":["c3","c2","c1","c9"],"login":{"totp":"JBSWY3DPEHPK3PXP"}},` +
				`{"name":"B","favorite":"yes","folderId":null,"collectionIds":["c2"],"login":{"totp":"JBSWY3DPEHPK3PXP"}}]}`,
			[]orgWant{{folder: "Ops", tags: []string{"Shared"}, pinned: true, note: "note"}, {folder: "Shared"}},
		},
		{
			"stratum categories joined through the secret",
			`{"Authenticators":[{"Type":2,"Issuer":"A","Secret":"JBSWY3DPEHPK3PXP","Digits":6,"Period":30},{"Type":2,"Issuer":"B","Secret":"GEZDGNBVGY3TQOJQ","Digits":6,"Period":30}],` +
				`"Categories":[{"Id":"k1","Name":"Work"},{"Id":"k2","Name":"Home"}],` +
				`"AuthenticatorCategories":[{"CategoryId":"k2","AuthenticatorSecret":"JBSWY3DPEHPK3PXP"},{"CategoryId":"k1","AuthenticatorSecret":"JBSWY3DPEHPK3PXP"},5,{"CategoryId":"k1","AuthenticatorSecret":7}]}`,
			[]orgWant{{folder: "Home", tags: []string{"Work"}}, {}},
		},
		{
			"andotp tags keep order, drop blanks and repeats, ignore non-strings",
			`[{"secret":"JBSWY3DPEHPK3PXP","issuer":"A","label":"a","type":"TOTP","tags":["b","a",""," b ",3,null,"a"]},` +
				`{"secret":"JBSWY3DPEHPK3PXP","issuer":"B","label":"b","type":"TOTP","tags":"a"}]`,
			[]orgWant{{tags: []string{"b", "a"}}, {}},
		},
		{
			"raivo pinned as string or bool",
			`[{"issuer":"A","secret":"JBSWY3DPEHPK3PXP","kind":"TOTP","pinned":"true"},{"issuer":"B","secret":"JBSWY3DPEHPK3PXP","kind":"TOTP","pinned":true},` +
				`{"issuer":"C","secret":"JBSWY3DPEHPK3PXP","kind":"TOTP","pinned":"TRUE"},{"issuer":"D","secret":"JBSWY3DPEHPK3PXP","kind":"TOTP","pinned":1}]`,
			[]orgWant{{pinned: true}, {pinned: true}, {}, {}},
		},
		{
			"1password vault name, tags and favourite index",
			`{"accounts":[{"vaults":[{"attrs":{"name":" Private "},"items":[` +
				onePUXTestItem(`"favIndex":1,"overview":{"title":"A","tags":["x","x","y"]}`) + `,` +
				onePUXTestItem(`"favIndex":0,"overview":{"title":"B"}`) + `,` +
				onePUXTestItem(`"favIndex":-1,"overview":{"title":"C"}`) + `,` +
				onePUXTestItem(`"favIndex":0.5,"overview":{"title":"D"}`) + `,` +
				onePUXTestItem(`"favIndex":0e7,"overview":{"title":"E"}`) + `,` +
				onePUXTestItem(`"favIndex":"1","overview":{"title":"F","tags":"x"}`) + `]},` +
				`{"attrs":"Shared","items":[` + onePUXTestItem(`"overview":{"title":"G"}`) + `]}]}]}`,
			[]orgWant{
				{folder: "Private", tags: []string{"x", "y"}, pinned: true},
				{folder: "Private"},
				{folder: "Private"},
				{folder: "Private", pinned: true},
				{folder: "Private"},
				{folder: "Private"},
				{},
			},
		},
		{
			"1password csv favourite and semicolon tags",
			"Title,Url,Username,Password,OTPAuth,Favorite,Archived,Tags,Notes\n" +
				"A,,,,JBSWY3DPEHPK3PXP, TRUE ,false,;b;a;b; ;,\n" +
				"B,,,,JBSWY3DPEHPK3PXP,yes,false,,\n" +
				"C,,,,JBSWY3DPEHPK3PXP\n",
			[]orgWant{{tags: []string{"b", "a"}, pinned: true}, {}, {}},
		},
	}
}

func onePUXTestItem(extra string) string {
	return `{"details":{"sections":[{"fields":[{"value":{"totp":"JBSWY3DPEHPK3PXP"}}]}]},` + extra + `}`
}

func TestImportersOrganisation(t *testing.T) {
	for _, c := range orgCases() {
		t.Run(c.name, func(t *testing.T) {
			accts, _, ok, err := Parse([]byte(c.data))
			if !ok || err != nil {
				t.Fatalf("ok=%v err=%v", ok, err)
			}
			if len(accts) != len(c.want) {
				t.Fatalf("got %d accounts, want %d", len(accts), len(c.want))
			}
			for i, w := range c.want {
				a := accts[i]
				if a.Folder != w.folder || !slices.Equal(a.Tags, w.tags) || a.Pinned != w.pinned || a.Note != w.note {
					t.Errorf("account %d: folder=%q tags=%q pinned=%v note=%q, want folder=%q tags=%q pinned=%v note=%q",
						i, a.Folder, a.Tags, a.Pinned, a.Note, w.folder, w.tags, w.pinned, w.note)
				}
			}
		})
	}
}

func TestOnePUXOrganisationMembersKeepTypeErrors(t *testing.T) {
	cases := []struct{ input, want string }{
		{
			`{"accounts":[{"vaults":[1]}]}`,
			`1password: json: cannot unmarshal number into onePUXFile.accounts.0.vaults.0 of type struct { Attrs jsontext.Value "json:\"attrs\""; Items []importers.onePUXItem "json:\"items\"" }`,
		},
		{
			`{"accounts":[{"vaults":[]},{"vaults":"x"}]}`,
			`1password: json: cannot unmarshal string into Go struct field onePUXFile.accounts.1.vaults of type []struct { Attrs jsontext.Value "json:\"attrs\""; Items []importers.onePUXItem "json:\"items\"" }`,
		},
		{
			`{"accounts":[{"vaults":[{"items":[{"overview":1}]}]}]}`,
			`1password: json: cannot unmarshal number into Go struct field onePUXFile.accounts.0.vaults.0.items.0.overview of type struct { Title string "json:\"title\""; Tags jsontext.Value "json:\"tags\"" }`,
		},
	}
	for _, c := range cases {
		_, _, ok, err := Parse([]byte(c.input))
		if !ok || err == nil || err.Error() != c.want {
			t.Errorf("%s: ok=%v err=%v\nwant %s", c.input, ok, err, c.want)
		}
	}
}

func TestPlaceGroupsKeepsLongNames(t *testing.T) {
	long := strings.Repeat("n", 10000)
	folder, tags := placeGroups([]string{"", "  ", long, long, "b"})
	if folder != long || !slices.Equal(tags, []string{"b"}) {
		t.Fatalf("folder length %d, tags %q", len(folder), tags)
	}
}

func TestCleanTagsLargeInputIsLinear(t *testing.T) {
	tags := make([]string, 200000)
	for i := range tags {
		tags[i] = "t" + strings.Repeat("x", i%7)
	}
	if got := cleanTags(tags); len(got) != 7 {
		t.Fatalf("got %d tags, want 7", len(got))
	}
}
