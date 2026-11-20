package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/merge"
	"github.com/spf13/cobra"
)

// duplicateGroup is one set of live accounts with the same type, issuer,
// account and secret: the one kept and the extras moved to the Trash.
type duplicateGroup struct {
	keep   account.Account
	extras []account.Account
}

func newDedupeCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "dedupe",
		Short: "Move duplicate accounts to the Trash",
		Long: `Find accounts with the same type, issuer, account and secret. The oldest of each
set stays; the others move to the Trash after you confirm.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return dedupeAccounts(cmd, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

// findDuplicates groups the live accounts that share a dedupe key. Groups come
// in display order of the account kept.
func findDuplicates(accts []account.Account) []duplicateGroup {
	byKey := map[string][]account.Account{}
	var keys []string
	for _, a := range filterAccounts(accts, "", "") {
		k := merge.ContentKey(a)
		if _, ok := byKey[k]; !ok {
			keys = append(keys, k)
		}
		byKey[k] = append(byKey[k], a)
	}
	var groups []duplicateGroup
	for _, k := range keys {
		members := byKey[k]
		if len(members) < 2 {
			continue
		}
		sort.SliceStable(members, func(i, j int) bool {
			if members[i].CreatedAt != members[j].CreatedAt {
				return members[i].CreatedAt < members[j].CreatedAt
			}
			return members[i].ID < members[j].ID
		})
		groups = append(groups, duplicateGroup{keep: members[0], extras: members[1:]})
	}
	return groups
}

// dedupeAccounts moves every duplicate but the oldest of each set to the
// Trash. The question is asked with the vault lock released.
func dedupeAccounts(cmd *cobra.Command, yes bool) error {
	s, err := openSession()
	if err != nil {
		return err
	}
	defer s.close()
	groups := findDuplicates(s.accounts)
	s.releaseLock()
	if len(groups) == 0 {
		out(cmd, "No duplicates.")
		return nil
	}
	var snap, extras []account.Account
	for _, g := range groups {
		snap = append(snap, g.keep)
		snap = append(snap, g.extras...)
		extras = append(extras, g.extras...)
		handles := make([]string, 0, len(g.extras))
		for _, e := range g.extras {
			handles = append(handles, displayHandle(e))
		}
		out(cmd, "%s: keeping %s, moving %s to the Trash", label(g.keep), displayHandle(g.keep), strings.Join(handles, ", "))
	}
	if !yes {
		if !stdinIsTTY() {
			return fmt.Errorf("refusing to move duplicates to the Trash without --yes (stdin is not a terminal)")
		}
		if !confirmYN(fmt.Sprintf("Move %s to the Trash? [y/N] ", duplicateCount(len(extras)))) {
			return nil
		}
	}
	err = s.mutate(func(fresh *session) error {
		if _, err := unchangedIndices(fresh.accounts, snap); err != nil {
			return err
		}
		ts := now().Unix()
		for _, e := range extras {
			i := indexByID(fresh.accounts, e.ID)
			fresh.accounts[i].DeletedAt = ts
			fresh.accounts[i].UpdatedAt = ts
		}
		return nil
	})
	if err != nil {
		return err
	}
	out(cmd, "Moved %s to the Trash", duplicateCount(len(extras)))
	out(cmd, "%s", restoreHint(extras))
	return nil
}

func duplicateCount(n int) string {
	if n == 1 {
		return "1 duplicate"
	}
	return fmt.Sprintf("%d duplicates", n)
}
