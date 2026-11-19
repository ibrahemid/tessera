package main

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/tui"
	"github.com/ibrahemid/tessera/go/internal/vault"
)

// stubWatchView captures the options watch hands the view instead of opening
// it.
func stubWatchView(t *testing.T) *tui.Options {
	t.Helper()
	got := &tui.Options{}
	old := runWatchView
	runWatchView = func(opts tui.Options) error {
		*got = opts
		return nil
	}
	t.Cleanup(func() { runWatchView = old })
	return got
}

func runWatchCmd(t *testing.T, env string) error {
	t.Helper()
	t.Setenv(clipClearEnv, env)
	c := newWatchCmd()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(nil)
	return c.Execute()
}

func TestWatchClearsEachCopyAfterThirtySeconds(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubBackend(t, &fakeClipboard{text: "123456"})
	calls := recordClipClear(t)
	opts := stubWatchView(t)

	if err := runWatchCmd(t, ""); err != nil {
		t.Fatalf("watch: %v", err)
	}
	if opts.ClearCopy == nil {
		t.Fatal("watch must hand the view a clear hook")
	}
	if err := opts.ClearCopy("123456"); err != nil {
		t.Fatalf("clear hook: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0] != (clipClearCall{30, "123456"}) {
		t.Errorf("scheduled %+v, want one clear after 30s", *calls)
	}
}

func TestWatchClearEnvironmentZeroKeepsTheCode(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubBackend(t, &fakeClipboard{text: "123456"})
	calls := recordClipClear(t)
	opts := stubWatchView(t)

	if err := runWatchCmd(t, "0"); err != nil {
		t.Fatalf("watch: %v", err)
	}
	if err := opts.ClearCopy("123456"); err != nil {
		t.Fatalf("clear hook: %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("scheduled %+v, want nothing", *calls)
	}
}

func TestWatchRejectsABadClearDelayBeforeOpening(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	opts := stubWatchView(t)

	err := runWatchCmd(t, "soon")
	var delayErr *clipClearDelayError
	if !errors.As(err, &delayErr) {
		t.Fatalf("err = %v, want a clipClearDelayError", err)
	}
	if opts.ClearCopy != nil || opts.Mutate != nil {
		t.Error("the view must not open")
	}
}

func TestWatchHandsTheViewEveryAccountAndEveryCallback(t *testing.T) {
	path := withVault(t)
	gone := totp("b", "Old", "y", 2)
	gone.DeletedAt = 5
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1), gone})
	fake := stubBackend(t, &fakeClipboard{})
	recordClipClear(t)
	opts := stubWatchView(t)

	if err := runWatchCmd(t, ""); err != nil {
		t.Fatalf("watch: %v", err)
	}
	if len(opts.Accounts) != 2 {
		t.Fatalf("accounts = %d, want the live one and the trashed one", len(opts.Accounts))
	}
	for _, a := range opts.Accounts {
		if a.Handle == "" {
			t.Errorf("%s has no handle", a.ID)
		}
	}
	if opts.Reload == nil || opts.Mutate == nil || opts.Unlock == nil || opts.Copy == nil || opts.Now == nil {
		t.Fatal("watch must hand the view reload, mutate, unlock, copy and a clock")
	}
	if err := opts.Copy("654321"); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if fake.text != "654321" || !fake.sensitive {
		t.Errorf("clipboard = %q sensitive=%v, want the code marked sensitive", fake.text, fake.sensitive)
	}
}

func openTestWatch(t *testing.T) (*watchVault, string) {
	t.Helper()
	path := withVault(t)
	h := hotpAcct("h", "Bank", "me")
	h.Counter = 4
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1), totp("b", "GitHub", "dev", 2), h})
	w, err := openWatchVault()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return w, path
}

func diskAccount(t *testing.T, path, id string) (account.Account, bool) {
	t.Helper()
	for _, a := range openVaultFile(t, path, "testpass123") {
		if a.ID == id {
			return a, true
		}
	}
	return account.Account{}, false
}

func strPtr(s string) *string { return &s }

func TestWatchDoesNotHoldTheLockWhileIdle(t *testing.T) {
	_, path := openTestWatch(t)
	lock, err := store.TryAcquire(path)
	if err != nil {
		t.Fatalf("the view holds the vault lock while idle: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestWatchReloadReportsNoChangeForAnUntouchedVault(t *testing.T) {
	w, _ := openTestWatch(t)
	accts, changed, err := w.reload()
	if err != nil || changed || accts != nil {
		t.Fatalf("reload = %d accounts, changed=%v, err=%v; want no change", len(accts), changed, err)
	}
}

func TestWatchReloadPicksUpAnotherProcessWrite(t *testing.T) {
	w, _ := openTestWatch(t)
	other, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	other.accounts = append(other.accounts, totp("c", "Mail", "me", 3))
	if err := other.save(); err != nil {
		t.Fatal(err)
	}
	other.close()

	accts, changed, err := w.reload()
	if err != nil || !changed {
		t.Fatalf("reload changed=%v err=%v, want a change", changed, err)
	}
	if indexByID(accts, "c") < 0 || accts[indexByID(accts, "c")].Handle == "" {
		t.Errorf("reload missed the new account or its handle: %+v", accts)
	}
	if _, changed, _ := w.reload(); changed {
		t.Error("a second reload of the same file must report no change")
	}
}

func TestWatchReloadFollowsANewKeyUnderTheSamePassphrase(t *testing.T) {
	w, path := openTestWatch(t)
	sealVault(t, path, []account.Account{totp("z", "Fresh", "me", 9)})

	accts, changed, err := w.reload()
	if err != nil || !changed || len(accts) != 1 || accts[0].ID != "z" {
		t.Fatalf("reload = %+v changed=%v err=%v, want the re-keyed vault", accts, changed, err)
	}
}

func sealWithPassphrase(t *testing.T, path, pass string, accts []account.Account) {
	t.Helper()
	env, err := vault.Seal(accts, pass)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(path, env); err != nil {
		t.Fatal(err)
	}
}

func TestWatchAsksForThePassphraseWhenTheKeyWasReplaced(t *testing.T) {
	w, path := openTestWatch(t)
	sealWithPassphrase(t, path, "newpass456", []account.Account{totp("z", "Fresh", "me", 9)})

	if _, _, err := w.reload(); !errors.Is(err, tui.ErrKeyReplaced) {
		t.Fatalf("reload err = %v, want ErrKeyReplaced", err)
	}
	if _, changed, err := w.reload(); err != nil || changed {
		t.Errorf("a repeat poll must not re-derive the key: changed=%v err=%v", changed, err)
	}
	if _, err := w.mutate(tui.Change{Kind: tui.ChangePin, ID: "a", Pinned: true}); !errors.Is(err, tui.ErrKeyReplaced) {
		t.Errorf("mutate err = %v, want ErrKeyReplaced", err)
	}
	if _, err := w.unlock("testpass123"); err == nil || errors.Is(err, tui.ErrKeyReplaced) {
		t.Errorf("unlock with the old passphrase = %v, want a wrong-passphrase error", err)
	}
	accts, err := w.unlock("newpass456")
	if err != nil || len(accts) != 1 || accts[0].ID != "z" {
		t.Fatalf("unlock = %+v, %v", accts, err)
	}
	if _, err := w.mutate(tui.Change{Kind: tui.ChangePin, ID: "z", Pinned: true}); err != nil {
		t.Fatalf("mutate after unlock: %v", err)
	}
	for _, a := range openVaultFile(t, path, "newpass456") {
		if a.ID == "z" && !a.Pinned {
			t.Error("the pin was not saved with the new key")
		}
	}
}

func TestWatchAddsFromAnOtpauthLink(t *testing.T) {
	w, path := openTestWatch(t)
	res, err := w.mutate(tui.Change{Kind: tui.ChangeAdd, Add: tui.Add{
		Text: "  otpauth://totp/Mail:me@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Mail \n",
	}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(res.IDs) != 1 || res.Skipped != 0 || len(res.Problems) != 0 {
		t.Fatalf("result = %+v, want one added", res)
	}
	got, ok := diskAccount(t, path, res.IDs[0])
	if !ok || got.Issuer != "Mail" || got.Account != "me@example.com" || got.Handle == "" {
		t.Errorf("saved %+v", got)
	}
	if indexByID(res.Accounts, res.IDs[0]) < 0 {
		t.Error("the result must carry the new account")
	}
}

func TestWatchAddsASetupKeyWithTheNamesTyped(t *testing.T) {
	w, path := openTestWatch(t)
	res, err := w.mutate(tui.Change{Kind: tui.ChangeAdd, Add: tui.Add{
		Text: "JBSWY3DPEHPK3PXP", Issuer: " Shop\t", Account: "me",
	}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	got, ok := diskAccount(t, path, res.IDs[0])
	if !ok || got.Issuer != "Shop" || got.Account != "me" || got.Type != account.TOTP {
		t.Errorf("saved %+v", got)
	}
}

func TestWatchAddRefusesAnUnnamedSetupKey(t *testing.T) {
	w, path := openTestWatch(t)
	before := readVaultBytes(t, path)
	_, err := w.mutate(tui.Change{Kind: tui.ChangeAdd, Add: tui.Add{Text: "JBSWY3DPEHPK3PXP"}})
	if err == nil || !strings.Contains(err.Error(), "issuer or an account name") {
		t.Fatalf("err = %v, want a request for a name", err)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Error("a refused add must not write the vault")
	}
}

func TestWatchAddRejectsTextThatIsNotAnAccount(t *testing.T) {
	w, path := openTestWatch(t)
	before := readVaultBytes(t, path)
	if _, err := w.mutate(tui.Change{Kind: tui.ChangeAdd, Add: tui.Add{Text: "hello there"}}); err == nil ||
		err.Error() != "not an otpauth link or a setup key" {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Error("a refused add must not write the vault")
	}
}

func TestWatchAddSkipsAnAccountAlreadyInTheVault(t *testing.T) {
	w, path := openTestWatch(t)
	link := "otpauth://totp/Mail:me?secret=JBSWY3DPEHPK3PXP&issuer=Mail"
	if _, err := w.mutate(tui.Change{Kind: tui.ChangeAdd, Add: tui.Add{Text: link}}); err != nil {
		t.Fatal(err)
	}
	before := readVaultBytes(t, path)
	res, err := w.mutate(tui.Change{Kind: tui.ChangeAdd, Add: tui.Add{Text: link}})
	if err != nil {
		t.Fatalf("add again: %v", err)
	}
	if len(res.IDs) != 0 || res.Skipped != 1 {
		t.Errorf("result = %+v, want one skipped", res)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Error("a skipped add must not write the vault")
	}
	if len(res.Accounts) != 4 {
		t.Errorf("accounts = %d, want 4", len(res.Accounts))
	}
}

func TestWatchEditsEveryField(t *testing.T) {
	w, path := openTestWatch(t)
	at := time.Unix(1_800_000_000, 0)
	stubClock(t, at)
	_, err := w.mutate(tui.Change{Kind: tui.ChangeEdit, ID: "a", Edit: tui.Edit{
		Issuer:  strPtr(" Acme Corp "),
		Account: strPtr("ops"),
		Alias:   strPtr(" Work "),
		Folder:  strPtr("Jobs\x07"),
		Tags:    strPtr(" ci, prod ,ci,, "),
		Note:    strPtr("line one\nline two"),
	}})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	got, _ := diskAccount(t, path, "a")
	if got.Issuer != "Acme Corp" || got.Account != "ops" || got.Handle != "work" || got.Folder != "Jobs" ||
		!slices.Equal(got.Tags, []string{"ci", "prod"}) || got.Note != "line one\nline two" || got.UpdatedAt != at.Unix() {
		t.Errorf("saved %+v", got)
	}
}

func TestWatchEditThatChangesNothingDoesNotWrite(t *testing.T) {
	w, path := openTestWatch(t)
	before := readVaultBytes(t, path)
	res, err := w.mutate(tui.Change{Kind: tui.ChangeEdit, ID: "a", Edit: tui.Edit{Issuer: strPtr("ACME ")}})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Error("an edit that changes nothing must not write the vault")
	}
	if len(res.Accounts) != 3 {
		t.Errorf("accounts = %d, want 3", len(res.Accounts))
	}
}

func TestWatchEditRefusesBadInput(t *testing.T) {
	w, path := openTestWatch(t)
	b, _ := diskAccount(t, path, "b")
	cases := map[string]tui.Edit{
		"taken alias":   {Alias: strPtr(b.Handle)},
		"invalid alias": {Alias: strPtr("9lives")},
		"no name":       {Issuer: strPtr(" "), Account: strPtr("")},
	}
	before := readVaultBytes(t, path)
	for name, e := range cases {
		if _, err := w.mutate(tui.Change{Kind: tui.ChangeEdit, ID: "a", Edit: e}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := w.mutate(tui.Change{Kind: tui.ChangeEdit, ID: "missing", Edit: tui.Edit{Issuer: strPtr("X")}}); err == nil || err.Error() != errWatchGone.Error() {
		t.Errorf("missing account err = %v", err)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Error("refused edits must not write the vault")
	}
}

func TestWatchPinsTrashesRestoresAndDeletes(t *testing.T) {
	w, path := openTestWatch(t)
	at := time.Unix(1_800_000_000, 0)
	stubClock(t, at)

	if _, err := w.mutate(tui.Change{Kind: tui.ChangePin, ID: "b", Pinned: true}); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if got, _ := diskAccount(t, path, "b"); !got.Pinned || got.UpdatedAt != at.Unix() {
		t.Errorf("pin saved %+v", got)
	}

	res, err := w.mutate(tui.Change{Kind: tui.ChangeTrash, ID: "b"})
	if err != nil {
		t.Fatalf("trash: %v", err)
	}
	if got, _ := diskAccount(t, path, "b"); got.DeletedAt != at.Unix() || got.UpdatedAt != at.Unix() {
		t.Errorf("trash saved %+v", got)
	}
	if _, err := w.mutate(tui.Change{Kind: tui.ChangePin, ID: "b", Pinned: false}); err == nil {
		t.Error("pinning a trashed account must fail")
	}

	later := at.Add(time.Minute)
	stubClock(t, later)
	if _, err := w.mutate(tui.Change{Kind: tui.ChangeRestore, ID: "b"}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got, _ := diskAccount(t, path, "b"); got.DeletedAt != 0 || got.UpdatedAt != later.Unix() {
		t.Errorf("restore saved %+v", got)
	}
	if _, err := w.mutate(tui.Change{Kind: tui.ChangeRestore, ID: "b"}); err == nil {
		t.Error("restoring a live account must fail")
	}

	res, err = w.mutate(tui.Change{Kind: tui.ChangeTrash, ID: "b"})
	if err != nil {
		t.Fatal(err)
	}
	snap := res.Accounts[indexByID(res.Accounts, "b")]
	if _, err := w.mutate(tui.Change{Kind: tui.ChangePurge, ID: "b", Snapshot: snap}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, ok := diskAccount(t, path, "b"); ok {
		t.Error("purge left the account in the vault")
	}
}

func TestWatchPurgeRefusesAnAccountThatChangedWhileAsking(t *testing.T) {
	w, path := openTestWatch(t)
	res, err := w.mutate(tui.Change{Kind: tui.ChangeTrash, ID: "b"})
	if err != nil {
		t.Fatal(err)
	}
	snap := res.Accounts[indexByID(res.Accounts, "b")]

	other, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	other.accounts[indexByID(other.accounts, "b")].Note = "changed"
	if err := other.save(); err != nil {
		t.Fatal(err)
	}
	other.close()

	_, err = w.mutate(tui.Change{Kind: tui.ChangePurge, ID: "b", Snapshot: snap})
	if err == nil || !strings.Contains(err.Error(), "nothing was deleted") {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if _, ok := diskAccount(t, path, "b"); !ok {
		t.Error("a changed account must stay in the Trash")
	}
}

func TestWatchAdvancesAnHOTPCounterOnlyFromTheCountShown(t *testing.T) {
	w, path := openTestWatch(t)
	if _, err := w.mutate(tui.Change{Kind: tui.ChangeAdvance, ID: "h", Counter: 9}); err == nil ||
		!strings.Contains(err.Error(), "counter changed") {
		t.Fatalf("stale counter err = %v", err)
	}
	if got, _ := diskAccount(t, path, "h"); got.Counter != 4 {
		t.Fatalf("counter = %d after a refused advance", got.Counter)
	}
	if _, err := w.mutate(tui.Change{Kind: tui.ChangeAdvance, ID: "h", Counter: 5}); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if got, _ := diskAccount(t, path, "h"); got.Counter != 5 {
		t.Errorf("counter = %d, want 5", got.Counter)
	}
	if _, err := w.mutate(tui.Change{Kind: tui.ChangeAdvance, ID: "a", Counter: 1}); err == nil {
		t.Error("advancing a TOTP account must fail")
	}
}

func TestWatchMutateKeepsAnotherProcessEdit(t *testing.T) {
	w, path := openTestWatch(t)
	other, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	other.accounts = append(other.accounts, totp("c", "Mail", "me", 3))
	if err := other.save(); err != nil {
		t.Fatal(err)
	}
	other.close()

	res, err := w.mutate(tui.Change{Kind: tui.ChangePin, ID: "a", Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := diskAccount(t, path, "c"); !ok || indexByID(res.Accounts, "c") < 0 {
		t.Error("a write from the view dropped another process's account")
	}
}
