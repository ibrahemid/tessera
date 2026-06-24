package main

import (
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/otp"
	"github.com/spf13/cobra"
)

func newCodeCmd() *cobra.Command {
	var next bool
	cmd := &cobra.Command{
		Use:   "code [query]",
		Short: "Print the current code for a matching account (or all if omitted)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return runCodeAll(cmd)
			}
			s, err := openSession()
			if err != nil {
				return err
			}
			idx, err := s.find(args[0])
			if err != nil {
				return err
			}
			a := &s.accounts[idx]
			if a.Type == account.HOTP && next {
				a.Counter++
				a.UpdatedAt = now().Unix()
			}
			code, err := genCode(*a, now())
			if err != nil {
				return err
			}
			out(cmd, "%s", code)
			if a.Type == account.HOTP && next {
				return s.save()
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&next, "next", false, "for HOTP, advance and persist the counter")
	return cmd
}

func runCodeAll(cmd *cobra.Command) error {
	s, err := openSession()
	if err != nil {
		return err
	}
	accts := filterAccounts(s.accounts, "", "")
	if len(accts) == 0 {
		out(cmd, "No accounts. Add one with `tess add`.")
		return nil
	}
	t := now()
	for _, a := range accts {
		if a.Type == account.HOTP {
			out(cmd, "%-30s  %s  (hotp, counter %d; use `tess code <q> --next`)", label(a), "------", a.Counter)
			continue
		}
		code, err := genCode(a, t)
		if err != nil {
			out(cmd, "%-30s  error: %v", label(a), err)
			continue
		}
		out(cmd, "%-30s  %s  (%ds)", label(a), code, otp.RemainingSeconds(t, a.Period))
	}
	return nil
}
