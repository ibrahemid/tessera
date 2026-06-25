package main

import (
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/tui"
	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Live, auto-refreshing view with countdown bars and copy",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			save := func(accts []account.Account) error {
				s.accounts = accts
				return s.save()
			}
			return tui.Run(s.accounts, save)
		},
	}
}
