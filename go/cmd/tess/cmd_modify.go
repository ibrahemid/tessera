package main

import (
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/spf13/cobra"
)

func newRemoveCmd() *cobra.Command {
	var yes, purge bool
	cmd := &cobra.Command{
		Use:     "rm <query>...",
		Aliases: []string{"remove", "delete"},
		Short:   "Move accounts to the Trash",
		Long: `Move accounts to the Trash. A trashed account gives no codes and is left out
of every list; tess restore brings it back.

--purge deletes the accounts and their secrets for good, from the Trash or
from the live accounts. It asks first; --yes skips the question and is
required when stdin is not a terminal.

Every query must name exactly one account, or nothing changes.`,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeRemoveArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if purge {
				return purgeAccounts(cmd, args, yes)
			}
			return trashAccounts(cmd, args)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask before deleting for good")
	cmd.Flags().BoolVar(&purge, "purge", false, "delete for good instead of moving to the Trash")
	return cmd
}

// trashAccounts moves the accounts the queries name to the Trash.
func trashAccounts(cmd *cobra.Command, queries []string) error {
	s, err := openSession()
	if err != nil {
		return err
	}
	defer s.close()
	idxs, err := s.resolveEach(cmd, queries, liveScope)
	if err != nil {
		return err
	}
	ts := now().Unix()
	for _, i := range idxs {
		s.accounts[i].DeletedAt = ts
		s.accounts[i].UpdatedAt = ts
	}
	if err := s.save(); err != nil {
		return err
	}
	trashed := snapshotAccounts(s.accounts, idxs)
	for _, a := range trashed {
		out(cmd, "Moved %s to the Trash", label(a))
	}
	out(cmd, "%s", restoreHint(trashed))
	return nil
}

// purgeAccounts deletes the accounts the queries name for good. The vault lock
// is released while the user answers.
func purgeAccounts(cmd *cobra.Command, queries []string, yes bool) error {
	s, err := openSession()
	if err != nil {
		return err
	}
	idxs, err := s.resolveEach(cmd, queries, liveThenTrashScope)
	if err != nil {
		s.close()
		return err
	}
	snap := snapshotAccounts(s.accounts, idxs)
	s.close()
	ok, err := confirmPurge(cmd, yes, snap)
	if err != nil || !ok {
		return err
	}
	if err := s.mutate(func(fresh *session) error { return purgeUnchanged(fresh, snap) }); err != nil {
		return err
	}
	for _, a := range snap {
		out(cmd, "Deleted %s for good", label(a))
	}
	return nil
}

func newRenameCmd() *cobra.Command {
	var issuer, acct, alias string
	cmd := &cobra.Command{
		Use:               "rename <query>",
		Short:             "Rename an account's issuer, account label, or alias",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeFirstArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			changed := cmd.Flags().Changed
			aliasSet := changed("alias") || changed("handle")
			if !aliasSet && !changed("issuer") && !changed("account") {
				return fmt.Errorf("nothing to change: pass --issuer, --account or --alias")
			}
			s, err := openSession()
			if err != nil {
				return err
			}
			defer s.close()
			idx, err := s.single(cmd, args[0])
			if err != nil {
				return err
			}
			if aliasSet {
				if err := setHandle(s, idx, alias); err != nil {
					return err
				}
			}
			if cmd.Flags().Changed("issuer") {
				s.accounts[idx].Issuer = account.CleanText(issuer)
			}
			if cmd.Flags().Changed("account") {
				s.accounts[idx].Account = account.CleanText(acct)
			}
			s.accounts[idx].UpdatedAt = now().Unix()
			if err := s.save(); err != nil {
				return err
			}
			out(cmd, "Renamed %s (alias %s)", label(s.accounts[idx]), displayHandle(s.accounts[idx]))
			return nil
		},
	}
	cmd.Flags().StringVar(&issuer, "issuer", "", "new issuer")
	cmd.Flags().StringVar(&acct, "account", "", "new account label")
	cmd.Flags().StringVar(&alias, "alias", "", "new alias (the short name you type to reference the account)")
	cmd.Flags().StringVar(&alias, "handle", "", "new alias")
	_ = cmd.Flags().MarkHidden("handle")
	return cmd
}

func newAliasCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "alias <account> <alias>",
		Short:             "Set an account's alias (the short name you type to reference it)",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeFirstArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			defer s.close()
			idx, err := s.single(cmd, args[0])
			if err != nil {
				return err
			}
			if err := setHandle(s, idx, args[1]); err != nil {
				return err
			}
			if err := s.save(); err != nil {
				return err
			}
			out(cmd, "Set alias of %s to %s", label(s.accounts[idx]), displayHandle(s.accounts[idx]))
			return nil
		},
	}
}

func newMoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "move <query>... <folder>",
		Short:             "Move accounts into a folder (empty folder to clear)",
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: completeEveryArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			queries, folder := args[:len(args)-1], account.CleanText(args[len(args)-1])
			s, err := openSession()
			if err != nil {
				return err
			}
			defer s.close()
			idxs, err := s.resolveEach(cmd, queries, liveScope)
			if err != nil {
				return err
			}
			ts := now().Unix()
			for _, i := range idxs {
				s.accounts[i].Folder = folder
				s.accounts[i].UpdatedAt = ts
			}
			if err := s.save(); err != nil {
				return err
			}
			for _, i := range idxs {
				out(cmd, "Moved %s to /%s", label(s.accounts[i]), ui.Sanitize(folder))
			}
			return nil
		},
	}
}

func newTagCmd() *cobra.Command {
	var remove bool
	cmd := &cobra.Command{
		Use:               "tag <query>... <tag>",
		Short:             "Add or remove a tag on accounts",
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: completeEveryArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			queries, tag := args[:len(args)-1], account.CleanText(args[len(args)-1])
			if tag == "" {
				return fmt.Errorf("the tag is empty")
			}
			s, err := openSession()
			if err != nil {
				return err
			}
			defer s.close()
			idxs, err := s.resolveEach(cmd, queries, liveScope)
			if err != nil {
				return err
			}
			lines := make([]string, 0, len(idxs))
			changedAny := false
			ts := now().Unix()
			for _, i := range idxs {
				a := &s.accounts[i]
				tags, changed := withTag(a.Tags, tag, !remove)
				switch {
				case changed:
					a.Tags, a.UpdatedAt = tags, ts
					changedAny = true
					lines = append(lines, fmt.Sprintf("Updated tags on %s", label(*a)))
				case remove:
					lines = append(lines, fmt.Sprintf("%s has no tag %s", label(*a), ui.Sanitize(tag)))
				default:
					lines = append(lines, fmt.Sprintf("%s already has the tag %s", label(*a), ui.Sanitize(tag)))
				}
			}
			if changedAny {
				if err := s.save(); err != nil {
					return err
				}
			}
			for _, line := range lines {
				out(cmd, "%s", line)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&remove, "remove", false, "remove the tag instead of adding it")
	return cmd
}

// withTag returns tags with tag added (add) or removed, and whether anything
// changed. A stored tag written before tags were cleaned matches its cleaned
// form, so it can still be removed.
func withTag(tags []string, tag string, add bool) ([]string, bool) {
	matches := func(t string) bool { return t == tag || account.CleanText(t) == tag }
	if add {
		for _, t := range tags {
			if matches(t) {
				return tags, false
			}
		}
		return append(tags, tag), true
	}
	var kept []string
	for _, t := range tags {
		if !matches(t) {
			kept = append(kept, t)
		}
	}
	return kept, len(kept) != len(tags)
}
