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
			idx, err := s.find(args[0])
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
	var issuer, acct string
	cmd := &cobra.Command{
		Use:   "rename <query>",
		Short: "Rename an account's issuer and/or account label",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			idx, err := s.find(args[0])
			if err != nil {
				return err
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
			out(cmd, "Renamed to %s", label(s.accounts[idx]))
			return nil
		},
	}
	cmd.Flags().StringVar(&issuer, "issuer", "", "new issuer")
	cmd.Flags().StringVar(&acct, "account", "", "new account label")
	return cmd
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
			idx, err := s.find(args[0])
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
			idx, err := s.find(args[0])
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
