package main

import (
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/merge"
)

type mergeCase struct {
	name          string
	local, source []account.Account
}

func mergeAccount(suffix, issuer, name, handle string, updated int64) account.Account {
	return account.Account{
		ID: "00000000-0000-4000-8000-0000000003" + suffix, Type: account.TOTP,
		Issuer: issuer, Account: name, Secret: []byte("merge-secret-" + suffix + "-pad!"),
		Algorithm: "SHA1", Digits: 6, Period: 30, Handle: handle,
		CreatedAt: v2Created, UpdatedAt: v2Created + updated,
	}
}

func touched(a account.Account, at int64) account.Account {
	a.UpdatedAt = v2Created + at
	return a
}

func trashed(a account.Account, at int64) account.Account {
	a = touched(a, at)
	a.DeletedAt = a.UpdatedAt
	return a
}

func mergeCases() []mergeCase {
	mail := mergeAccount("01", "Mail", "me@example.com", "mail", 1000)
	bank := mergeAccount("02", "Bank", "customer-42", "bank", 1000)

	renamed := mail
	renamed.Issuer, renamed.Note = "Mail Renamed", "differs"

	localMail := mail
	localMail.Handle = "work"
	sourceMail := touched(mail, 2000)
	sourceMail.Issuer, sourceMail.Handle = "Mail Pro", "inbox"
	unhandledBank := bank
	unhandledBank.Handle = ""

	dupLocal := mergeAccount("01", "GitHub", "octocat", "gh", 1000)
	dupLive := dupLocal
	dupLive.ID, dupLive.Issuer, dupLive.Account, dupLive.Handle = "00000000-0000-4000-8000-000000000302", "github", "OCTOCAT", "git"
	dupTrashed := trashed(dupLocal, 1500)
	dupTrashed.ID, dupTrashed.Handle = "00000000-0000-4000-8000-000000000303", ""

	invalid := mergeAccount("02", "Broken", "digits", "", 1000)
	invalid.Digits = 9

	work := mergeAccount("02", "GitHub", "work", "gi", 1000)
	home := mergeAccount("01", "GitHub", "home", "gi", 1000)

	otherBank := bank
	otherBank.Handle = "bk"

	brokenEdit := touched(mail, 2000)
	brokenEdit.Issuer, brokenEdit.Digits = "Mail Broken", 9

	editedInTrash := trashed(mail, 2000)
	editedInTrash.Note = "edited in the Trash"

	return []mergeCase{
		{
			name:   "strictly-newer-wins-both-ways",
			local:  []account.Account{mail, touched(bank, 2000)},
			source: []account.Account{trashed(mail, 2000), trashed(bank, 1000)},
		},
		{
			name:   "equal-timestamps-change-nothing",
			local:  []account.Account{mail},
			source: []account.Account{renamed},
		},
		{
			name:   "local-handle-kept",
			local:  []account.Account{localMail, touched(bank, 2000)},
			source: []account.Account{sourceMail, unhandledBank},
		},
		{
			name:   "content-duplicate-not-added",
			local:  []account.Account{dupLocal},
			source: []account.Account{dupLive, dupTrashed},
		},
		{
			name:   "invalid-account-not-added",
			local:  []account.Account{mail},
			source: []account.Account{mail, invalid},
		},
		{
			name:   "invalid-replacement-not-applied",
			local:  []account.Account{mail},
			source: []account.Account{brokenEdit},
		},
		{
			name:   "newer-copy-in-the-trash",
			local:  []account.Account{trashed(mail, 1500)},
			source: []account.Account{editedInTrash},
		},
		{
			name:   "handle-collisions-reassigned",
			local:  []account.Account{work},
			source: []account.Account{home},
		},
		{
			name:   "purged-account-returns",
			local:  []account.Account{trashed(mail, 1500), bank},
			source: []account.Account{bank},
		},
		{
			name:   "source-receives-the-pre-run-local",
			local:  []account.Account{mail},
			source: []account.Account{otherBank},
		},
	}
}

func mergeBlock() object {
	cases := mergeCases()
	out := make([]object, len(cases))
	for i, c := range cases {
		out[i] = mergeCaseObject(c)
	}
	return object{{"cases", out}}
}

// mergeCaseObject runs a case the way `tess merge --two-way` does: both files
// are read as stored, local handles are already assigned, each side is written
// only when it changed, and a written side gets its handles assigned and
// checked first.
func mergeCaseObject(c mergeCase) object {
	localIn := mustCanonical(c.local...)
	sourceIn := mustCanonical(c.source...)
	local, err := account.ParseCanonicalJSON(localIn)
	if err != nil {
		panic(fmt.Sprintf("vectorgen: merge %s: %v", c.name, err))
	}
	source, err := account.ParseCanonicalJSON(sourceIn)
	if err != nil {
		panic(fmt.Sprintf("vectorgen: merge %s: %v", c.name, err))
	}
	if account.AssignHandles(local) {
		panic(fmt.Sprintf("vectorgen: merge %s: every local account needs a handle", c.name))
	}
	localOut, sourceOut, here, there := merge.TwoWay(local, source)
	return object{
		{"name", c.name},
		{"local", string(localIn)},
		{"source", string(sourceIn)},
		{"local_after", mergedSide(c.name, localOut, here.Changed(), localIn)},
		{"source_after", mergedSide(c.name, sourceOut, there.Changed(), sourceIn)},
		{"local_written", here.Changed()},
		{"source_written", there.Changed()},
		{"local_counts", countsObject(here)},
		{"source_counts", countsObject(there)},
	}
}

func mergedSide(name string, accts []account.Account, written bool, input []byte) string {
	if !written {
		return string(input)
	}
	merge.DedupeHandles(accts)
	account.AssignHandles(accts)
	if err := account.CheckHandleUniqueness(accts); err != nil {
		panic(fmt.Sprintf("vectorgen: merge %s: %v", name, err))
	}
	for _, a := range accts {
		if err := a.Validate(); err != nil {
			panic(fmt.Sprintf("vectorgen: merge %s: writes an invalid account: %v", name, err))
		}
	}
	return string(mustCanonical(accts...))
}

func countsObject(c merge.Counts) object {
	return object{
		{"added", c.Added}, {"updated", c.Updated}, {"trashed", c.Trashed},
		{"unchanged", c.Unchanged}, {"duplicate", c.Duplicate}, {"invalid", c.Invalid},
	}
}
