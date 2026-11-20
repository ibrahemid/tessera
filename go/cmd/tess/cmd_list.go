package main

import (
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var folder, tag string
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List accounts (without codes)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			defer s.close()
			accts := filterAccounts(s.accounts, folder, tag)
			if asJSON {
				rows := make([]listRow, 0, len(accts))
				for _, a := range accts {
					rows = append(rows, newListRow(a))
				}
				out(cmd, "%s", mustJSON(rows))
				return nil
			}
			if len(account.Live(s.accounts)) == 0 {
				out(cmd, "%s", ui.SubtleStyle.Render("No accounts. Add one with `tess add`."))
				return nil
			}
			if len(accts) == 0 {
				out(cmd, "%s", ui.SubtleStyle.Render("No accounts."))
				return nil
			}
			hw := handleWidth(accts)
			for _, a := range accts {
				meta := string(a.Type)
				if a.Folder != "" {
					meta += " /" + a.Folder
				}
				if len(a.Tags) > 0 {
					meta += " #" + strings.Join(a.Tags, " #")
				}
				meta = ui.Sanitize(meta)
				pin := "  "
				if a.Pinned {
					pin = ui.AccentStyle.Render("★ ")
				}
				out(cmd, "%s%s  %s  %s", pin, ui.Handle(a.Issuer+a.Account, padRight(displayHandle(a), hw)),
					ui.IssuerStyle.Render(padRight(label(a), 34)), ui.SubtleStyle.Render(meta))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&folder, "folder", "", "filter by folder")
	cmd.Flags().StringVar(&tag, "tag", "", "filter by tag")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

// listRow is the JSON shape of one account in `tess list --json`;
// docs/CLI-JSON.md is its contract. It carries whether a note exists, never
// the note.
type listRow struct {
	ID      string   `json:"id"`
	Handle  string   `json:"handle"`
	Issuer  string   `json:"issuer"`
	Account string   `json:"account"`
	Type    string   `json:"type"`
	Folder  string   `json:"folder,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Pinned  bool     `json:"pinned"`
	HasNote bool     `json:"has_note"`
	Order   int      `json:"order,omitempty"`
}

func newListRow(a account.Account) listRow {
	return listRow{
		ID: a.ID, Handle: a.Handle, Issuer: a.Issuer, Account: a.Account, Type: string(a.Type),
		Folder: a.Folder, Tags: a.Tags, Pinned: a.Pinned, HasNote: a.Note != "", Order: a.Order,
	}
}

func filterAccounts(in []account.Account, folder, tag string) []account.Account {
	var out []account.Account
	for _, a := range account.Live(in) {
		if folder != "" && a.Folder != folder {
			continue
		}
		if tag != "" && !containsTag(a.Tags, tag) {
			continue
		}
		out = append(out, a)
	}
	sortForDisplay(out)
	return out
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}
