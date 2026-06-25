package main

import (
	"encoding/json"
	"fmt"

	"github.com/atotto/clipboard"
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/code"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/spf13/cobra"
)

func newCodeCmd() *cobra.Command {
	var next, doCopy, asJSON bool
	cmd := &cobra.Command{
		Use:   "code [query]",
		Short: "Print the current code for a matching account (or all if omitted)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return runCodeAll(cmd, asJSON)
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
			c, err := genCode(*a, now())
			if err != nil {
				return err
			}
			if asJSON {
				out(cmd, "%s", mustJSON(map[string]any{
					"issuer": a.Issuer, "account": a.Account, "code": c, "type": string(a.Type),
				}))
			} else {
				out(cmd, "%s", c)
			}
			if doCopy {
				_ = clipboard.WriteAll(c)
			}
			if a.Type == account.HOTP && next {
				return s.save()
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&next, "next", false, "for HOTP, advance and persist the counter")
	cmd.Flags().BoolVarP(&doCopy, "copy", "c", false, "copy the code to the clipboard")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func runCodeAll(cmd *cobra.Command, asJSON bool) error {
	s, err := openSession()
	if err != nil {
		return err
	}
	accts := filterAccounts(s.accounts, "", "")
	if asJSON {
		type row struct {
			Issuer  string `json:"issuer"`
			Account string `json:"account"`
			Type    string `json:"type"`
			Code    string `json:"code"`
			Expires int    `json:"expires_in,omitempty"`
		}
		rows := make([]row, 0, len(accts))
		t := now()
		for _, a := range accts {
			c, _ := genCode(a, t)
			rows = append(rows, row{a.Issuer, a.Account, string(a.Type), c, code.Remaining(a, t)})
		}
		out(cmd, "%s", mustJSON(rows))
		return nil
	}
	if len(accts) == 0 {
		out(cmd, "%s", ui.SubtleStyle.Render("No accounts. Add one with `tess add`."))
		return nil
	}
	t := now()
	for _, a := range accts {
		mono := ui.Monogram(a.Issuer+a.Account, labelText(a))
		name := ui.IssuerStyle.Render(padRight(labelText(a), 26))
		if a.Type == account.HOTP {
			meta := ui.SubtleStyle.Render(fmt.Sprintf("hotp #%d  (tess code %q --next)", a.Counter, shortQuery(a)))
			out(cmd, "%s %s  %s  %s", mono, name, ui.CodeStyle.Render(padRight("------", 9)), meta)
			continue
		}
		c, err := genCode(a, t)
		if err != nil {
			out(cmd, "%s %s  %s", mono, name, ui.WarnStyle.Render(err.Error()))
			continue
		}
		rem := code.Remaining(a, t)
		bar := ui.Bar(rem, a.Period, 10)
		out(cmd, "%s %s  %s  %s %s", mono, name,
			ui.CodeStyle.Render(padRight(ui.GroupCode(c), 9)), bar,
			ui.SubtleStyle.Render(fmt.Sprintf("%2ds", rem)))
	}
	return nil
}

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}
