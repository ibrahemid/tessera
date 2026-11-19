package main

import (
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/spf13/cobra"
)

func newRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "rm <query>",
		Aliases: []string{"remove", "delete"},
		Short:   "Remove an account",
		Long: `Remove an account and its secret from the vault. On a terminal it asks
first; --yes skips the question and is required when stdin is not a terminal.`,
		Args:              cobra.ExactArgs(1),
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
			removed := label(s.accounts[idx])
			if !yes {
				if !stdinIsTTY() {
					return fmt.Errorf("refusing to remove %s without --yes (stdin is not a terminal)", removed)
				}
				if !confirmYN(fmt.Sprintf("Remove %s (%s)? Its secret is deleted from the vault. [y/N] ", removed, displayHandle(s.accounts[idx]))) {
					return nil
				}
			}
			s.accounts = append(s.accounts[:idx], s.accounts[idx+1:]...)
			if err := s.save(); err != nil {
				return err
			}
			out(cmd, "Removed %s", removed)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "remove without asking")
	return cmd
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
				s.accounts[idx].Issuer = issuer
			}
			if cmd.Flags().Changed("account") {
				s.accounts[idx].Account = acct
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
		Use:               "move <query> <folder>",
		Short:             "Move an account into a folder (empty folder to clear)",
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
			s.accounts[idx].Folder = args[1]
			s.accounts[idx].UpdatedAt = now().Unix()
			if err := s.save(); err != nil {
				return err
			}
			out(cmd, "Moved %s to /%s", label(s.accounts[idx]), ui.Sanitize(args[1]))
			return nil
		},
	}
}

func newTagCmd() *cobra.Command {
	var remove bool
	cmd := &cobra.Command{
		Use:               "tag <query> <tag>",
		Short:             "Add or remove a tag on an account",
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
			tag := args[1]
			a := &s.accounts[idx]
			if remove && !containsTag(a.Tags, tag) {
				out(cmd, "%s has no tag %s", label(*a), ui.Sanitize(tag))
				return nil
			}
			if remove {
				var kept []string
				for _, t := range a.Tags {
					if t != tag {
						kept = append(kept, t)
					}
				}
				a.Tags = kept
			} else if !containsTag(a.Tags, tag) {
				a.Tags = append(a.Tags, tag)
			}
			a.UpdatedAt = now().Unix()
			if err := s.save(); err != nil {
				return err
			}
			out(cmd, "Updated tags on %s", label(*a))
			return nil
		},
	}
	cmd.Flags().BoolVar(&remove, "remove", false, "remove the tag instead of adding it")
	return cmd
}
