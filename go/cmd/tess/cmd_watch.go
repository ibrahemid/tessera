package main

import (
	"fmt"

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
			// The view stays open indefinitely, so it must not hold the vault
			// lock. Advancing an HOTP counter re-reads under the lock instead.
			s.close()
			advance := func(id string, counter int64) error {
				return s.mutate(func(fresh *session) error {
					i := indexByID(fresh.accounts, id)
					if i < 0 {
						return fmt.Errorf("account is no longer in the vault")
					}
					fresh.accounts[i].Counter = counter
					fresh.accounts[i].UpdatedAt = now().Unix()
					return nil
				})
			}
			return tui.Run(s.accounts, advance)
		},
	}
}
