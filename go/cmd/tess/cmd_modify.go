package main

import (
	"github.com/spf13/cobra"
)

func newRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <query>",
		Short: "Remove an account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			idx, err := s.single(cmd, args[0])
			if err != nil {
				return err
			}
			removed := label(s.accounts[idx])
			s.accounts = append(s.accounts[:idx], s.accounts[idx+1:]...)
			if err := s.save(); err != nil {
				return err
			}
			out(cmd, "Removed %s", removed)
			return nil
		},
	}
}

func newRenameCmd() *cobra.Command {
	var issuer, acct, handle string
	cmd := &cobra.Command{
		Use:   "rename <query>",
		Short: "Rename an account's issuer, account label, and/or handle",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			idx, err := s.single(cmd, args[0])
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("handle") {
				if err := setHandle(s, idx, handle); err != nil {
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
			out(cmd, "Renamed %s (handle %s)", label(s.accounts[idx]), s.accounts[idx].Handle)
			return nil
		},
	}
	cmd.Flags().StringVar(&issuer, "issuer", "", "new issuer")
	cmd.Flags().StringVar(&acct, "account", "", "new account label")
	cmd.Flags().StringVar(&handle, "handle", "", "new handle (short identifier)")
	return cmd
}

func newAliasCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "alias <account> <handle>",
		Short: "Set an account's handle (the short name you type to reference it)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
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
			out(cmd, "Set handle of %s to %s", label(s.accounts[idx]), s.accounts[idx].Handle)
			return nil
		},
	}
}

func newMoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "move <query> <folder>",
		Short: "Move an account into a folder (empty folder to clear)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			idx, err := s.single(cmd, args[0])
			if err != nil {
				return err
			}
			s.accounts[idx].Folder = args[1]
			s.accounts[idx].UpdatedAt = now().Unix()
			if err := s.save(); err != nil {
				return err
			}
			out(cmd, "Moved %s to /%s", label(s.accounts[idx]), args[1])
			return nil
		},
	}
}

func newTagCmd() *cobra.Command {
	var remove bool
	cmd := &cobra.Command{
		Use:   "tag <query> <tag>",
		Short: "Add or remove a tag on an account",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			idx, err := s.single(cmd, args[0])
			if err != nil {
				return err
			}
			tag := args[1]
			a := &s.accounts[idx]
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
