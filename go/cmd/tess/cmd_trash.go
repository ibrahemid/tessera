package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/spf13/cobra"
)

// trashDateLayout is how the Trash shows the day an account was trashed.
const trashDateLayout = "2006-01-02"

func newTrashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trash",
		Short: "List the accounts in the Trash",
		Long: `List the accounts in the Trash, most recently trashed first. Trashed
accounts give no codes and stay until tess restore brings them back or
tess trash empty deletes them for good.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			defer s.close()
			trashed := trashedNewestFirst(s.accounts)
			if len(trashed) == 0 {
				out(cmd, "%s", ui.SubtleStyle.Render("The Trash is empty."))
				return nil
			}
			printTrash(cmd, trashed)
			return nil
		},
	}
	cmd.AddCommand(newTrashEmptyCmd())
	return cmd
}

// trashedNewestFirst returns the trashed accounts, most recently trashed first
// and in display order within the same second.
func trashedNewestFirst(accts []account.Account) []account.Account {
	trashed := account.Trashed(accts)
	account.SortForDisplay(trashed)
	sort.SliceStable(trashed, func(i, j int) bool { return trashed[i].DeletedAt > trashed[j].DeletedAt })
	return trashed
}

// printTrash writes the alias, issuer, account and trash date of each account.
func printTrash(cmd *cobra.Command, accts []account.Account) {
	handles := make([]string, len(accts))
	issuers := make([]string, len(accts))
	names := make([]string, len(accts))
	for i, a := range accts {
		handles[i], issuers[i], names[i] = displayHandle(a), ui.Sanitize(a.Issuer), ui.Sanitize(a.Account)
	}
	hw, iw, aw := columnWidth("alias", handles), columnWidth("issuer", issuers), columnWidth("account", names)
	out(cmd, "%s", ui.SubtleStyle.Render(fmt.Sprintf("%s  %s  %s  %s",
		padRight("alias", hw), padRight("issuer", iw), padRight("account", aw), "trashed")))
	for i, a := range accts {
		out(cmd, "%s  %s  %s  %s", ui.Handle(a.Issuer+a.Account, padRight(handles[i], hw)),
			padRight(issuers[i], iw), padRight(names[i], aw),
			time.Unix(a.DeletedAt, 0).Format(trashDateLayout))
	}
}

func newTrashEmptyCmd() *cobra.Command {
	var (
		olderThan string
		yes       bool
	)
	cmd := &cobra.Command{
		Use:   "empty",
		Short: "Delete the accounts in the Trash for good",
		Long: `Delete the accounts in the Trash and their secrets for good. --older-than
limits it to accounts trashed at least that long ago (30d, 12h). It asks
first; --yes skips the question and is required when stdin is not a
terminal.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var age time.Duration
			if olderThan != "" {
				d, err := parseAge(olderThan)
				if err != nil {
					return err
				}
				age = d
			}
			s, err := openSession()
			if err != nil {
				return err
			}
			cutoff := now().Add(-age).Unix()
			var snap []account.Account
			for _, a := range trashedNewestFirst(s.accounts) {
				if age == 0 || a.DeletedAt <= cutoff {
					snap = append(snap, a)
				}
			}
			s.close()
			if len(snap) == 0 {
				if age > 0 {
					out(cmd, "Nothing in the Trash is older than %s", olderThan)
				} else {
					out(cmd, "The Trash is empty.")
				}
				return nil
			}
			ok, err := confirmPurge(cmd, yes, snap)
			if err != nil || !ok {
				return err
			}
			if err := s.mutate(func(fresh *session) error { return purgeUnchanged(fresh, snap) }); err != nil {
				return err
			}
			if len(snap) == 1 {
				out(cmd, "Deleted %s for good", label(snap[0]))
				return nil
			}
			out(cmd, "Deleted %d accounts for good", len(snap))
			return nil
		},
	}
	cmd.Flags().StringVar(&olderThan, "older-than", "", "only accounts trashed at least this long ago, such as 30d or 12h")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask before deleting for good")
	return cmd
}

// parseAge reads a positive age written as whole days (30d) or as a Go
// duration (12h, 90m).
func parseAge(s string) (time.Duration, error) {
	bad := fmt.Errorf("invalid --older-than %q: use days like 30d or a duration like 12h", s)
	t := strings.TrimSpace(s)
	if days, ok := strings.CutSuffix(t, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n <= 0 || n > 100000 {
			return 0, bad
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(t)
	if err != nil || d <= 0 {
		return 0, bad
	}
	return d, nil
}

func newRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore <query>...",
		Short: "Bring accounts back from the Trash",
		Long: `Bring accounts back from the Trash. Queries match trashed accounts only;
tess trash lists them. Every query must name exactly one account, or nothing
changes.`,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeTrashedArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			defer s.close()
			idxs, err := s.resolveEach(cmd, args, trashScope)
			if err != nil {
				return err
			}
			ts := now().Unix()
			for _, i := range idxs {
				s.accounts[i].DeletedAt = 0
				s.accounts[i].UpdatedAt = ts
			}
			if err := s.save(); err != nil {
				return err
			}
			for _, i := range idxs {
				out(cmd, "Restored %s", label(s.accounts[i]))
			}
			return nil
		},
	}
}
