package main

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/spf13/cobra"
)

// sortForDisplay puts accounts in the spec display order with pinned accounts
// grouped first.
func sortForDisplay(accts []account.Account) {
	account.SortForDisplay(accts)
	sort.SliceStable(accts, func(i, j int) bool { return accts[i].Pinned && !accts[j].Pinned })
}

// liveDisplayIndices returns the positions in accts of the live accounts, in
// display order.
func liveDisplayIndices(accts []account.Account) []int {
	live := account.Live(accts)
	sortForDisplay(live)
	idxs := make([]int, len(live))
	for n, a := range live {
		idxs[n] = indexByID(accts, a.ID)
	}
	return idxs
}

func newPinCmd() *cobra.Command {
	return newPinToggleCmd(true)
}

func newUnpinCmd() *cobra.Command {
	return newPinToggleCmd(false)
}

// newPinToggleCmd builds `tess pin` (pin set) or `tess unpin`.
func newPinToggleCmd(pin bool) *cobra.Command {
	use, short, done, already := "pin", "Pin accounts to the top of every list", "Pinned %s", "%s is already pinned"
	if !pin {
		use, short, done, already = "unpin", "Unpin accounts", "Unpinned %s", "%s is not pinned"
	}
	return &cobra.Command{
		Use:               use + " <query>...",
		Short:             short,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeEveryArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			defer s.close()
			idxs, err := s.resolveEach(cmd, args, liveScope)
			if err != nil {
				return err
			}
			lines := make([]string, 0, len(idxs))
			changed := false
			ts := now().Unix()
			for _, i := range idxs {
				a := &s.accounts[i]
				if a.Pinned == pin {
					lines = append(lines, fmt.Sprintf(already, label(*a)))
					continue
				}
				a.Pinned, a.UpdatedAt = pin, ts
				changed = true
				lines = append(lines, fmt.Sprintf(done, label(*a)))
			}
			if changed {
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
}

func newOrderCmd() *cobra.Command {
	var clearFlag bool
	cmd := &cobra.Command{
		Use:   "order <query> <position>",
		Short: "Move an account to a position in every list",
		Long: `Move an account to a position in every list, counting from 1. Pinned
accounts stay ahead of the others. --clear drops the account's position, so it
sorts by issuer and account again.

  tess order github 1
  tess order --clear github`,
		Args: func(cmd *cobra.Command, args []string) error {
			if clearFlag {
				return cobra.ExactArgs(1)(cmd, args)
			}
			return cobra.ExactArgs(2)(cmd, args)
		},
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
			if clearFlag {
				return clearOrder(cmd, s, idx)
			}
			return moveToPosition(cmd, s, idx, args[1])
		},
	}
	cmd.Flags().BoolVar(&clearFlag, "clear", false, "drop the account's position")
	return cmd
}

// clearOrder drops the manual position of account idx.
func clearOrder(cmd *cobra.Command, s *session, idx int) error {
	a := &s.accounts[idx]
	if a.Order == 0 {
		out(cmd, "%s has no set position", label(*a))
		return nil
	}
	a.Order = 0
	if err := s.save(); err != nil {
		return err
	}
	out(cmd, "Cleared the position of %s", label(*a))
	return nil
}

// moveToPosition moves account idx to the 1-based position arg in the display
// order and numbers every live account 1..n to match. updated_at is left alone.
func moveToPosition(cmd *cobra.Command, s *session, idx int, arg string) error {
	order := liveDisplayIndices(s.accounts)
	pos, err := strconv.Atoi(arg)
	if err != nil || pos < 1 || pos > len(order) {
		return fmt.Errorf("position must be a whole number from 1 to %d", len(order))
	}
	rest := make([]int, 0, len(order))
	for _, i := range order {
		if i != idx {
			rest = append(rest, i)
		}
	}
	moved := make([]int, 0, len(order))
	moved = append(moved, rest[:pos-1]...)
	moved = append(moved, idx)
	moved = append(moved, rest[pos-1:]...)
	for n, i := range moved {
		s.accounts[i].Order = n + 1
	}
	if err := s.save(); err != nil {
		return err
	}
	got := 0
	for n, i := range liveDisplayIndices(s.accounts) {
		if i == idx {
			got = n + 1
		}
	}
	if got != pos {
		out(cmd, "Moved %s to position %d; pinned accounts stay first", label(s.accounts[idx]), got)
		return nil
	}
	out(cmd, "Moved %s to position %d", label(s.accounts[idx]), got)
	return nil
}
