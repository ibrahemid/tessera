package main

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/spf13/cobra"
)

// errChangedWhileAsking reports that an account a question was about changed
// between the question and the answer. Commands that ask release the vault lock
// while they wait, so another tess process or the app may write meanwhile.
var errChangedWhileAsking = errors.New("an account changed while tess waited for an answer; nothing was changed, run the command again")

// snapshotAccounts copies the accounts at idxs, so a later write can check
// they are still exactly what the user was asked about.
func snapshotAccounts(accts []account.Account, idxs []int) []account.Account {
	snap := make([]account.Account, 0, len(idxs))
	for _, i := range idxs {
		snap = append(snap, accts[i])
	}
	return snap
}

// unchangedIndices finds each snapshot account in accts by id and returns its
// position. Any account that is gone or differs from its snapshot fails the
// whole check with errChangedWhileAsking.
func unchangedIndices(accts []account.Account, snap []account.Account) ([]int, error) {
	idxs := make([]int, 0, len(snap))
	for _, want := range snap {
		i := indexByID(accts, want.ID)
		if i < 0 || !reflect.DeepEqual(accts[i], want) {
			return nil, errChangedWhileAsking
		}
		idxs = append(idxs, i)
	}
	return idxs, nil
}

// purgeUnchanged removes the snapshot accounts from the session for good, after
// checking none of them changed since the snapshot.
func purgeUnchanged(fresh *session, snap []account.Account) error {
	if _, err := unchangedIndices(fresh.accounts, snap); err != nil {
		return err
	}
	drop := make(map[string]bool, len(snap))
	for _, a := range snap {
		drop[a.ID] = true
	}
	kept := make([]account.Account, 0, len(fresh.accounts))
	for _, a := range fresh.accounts {
		if !drop[a.ID] {
			kept = append(kept, a)
		}
	}
	fresh.accounts = kept
	return nil
}

// confirmPurge asks before accounts are deleted for good. yes skips the
// question; without it stdin must be a terminal. The vault lock must not be
// held while it runs.
func confirmPurge(cmd *cobra.Command, yes bool, accts []account.Account) (bool, error) {
	if yes {
		return true, nil
	}
	if !stdinIsTTY() {
		return false, fmt.Errorf("refusing to delete %s for good without --yes (stdin is not a terminal)", countOrLabel(accts))
	}
	if len(accts) == 1 {
		return confirmYN(fmt.Sprintf("Delete %s (%s) for good? Its secret is removed from the vault. [y/N] ",
			label(accts[0]), displayHandle(accts[0]))), nil
	}
	printAccountRows(cmd.ErrOrStderr(), accts)
	return confirmYN(fmt.Sprintf("Delete %d accounts for good? Their secrets are removed from the vault. [y/N] ", len(accts))), nil
}

// countOrLabel names one account by its label and several by their count.
func countOrLabel(accts []account.Account) string {
	if len(accts) == 1 {
		return label(accts[0])
	}
	return fmt.Sprintf("%d accounts", len(accts))
}

// printAccountRows writes one aligned alias and label line per account.
func printAccountRows(w io.Writer, accts []account.Account) {
	hw := handleWidth(accts)
	for _, a := range accts {
		fmt.Fprintf(w, "  %s  %s\n", ui.Handle(a.Issuer+a.Account, padRight(displayHandle(a), hw)), label(a))
	}
}

// restoreHint is the command that brings the given accounts back from the Trash.
func restoreHint(accts []account.Account) string {
	handles := make([]string, 0, len(accts))
	for _, a := range accts {
		handles = append(handles, displayHandle(a))
	}
	return "Restore with `tess restore " + strings.Join(handles, " ") + "`"
}

// columnWidth is the widest of header and the given cells, in runes.
func columnWidth(header string, cells []string) int {
	w := utf8.RuneCountInString(header)
	for _, c := range cells {
		w = max(w, utf8.RuneCountInString(c))
	}
	return w
}
