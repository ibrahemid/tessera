package main

import (
	"sort"
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
			accts := filterAccounts(s.accounts, folder, tag)
			if asJSON {
				type row struct {
					ID      string   `json:"id"`
					Issuer  string   `json:"issuer"`
					Account string   `json:"account"`
					Type    string   `json:"type"`
					Folder  string   `json:"folder,omitempty"`
					Tags    []string `json:"tags,omitempty"`
					Pinned  bool     `json:"pinned"`
				}
				rows := make([]row, 0, len(accts))
				for _, a := range accts {
					rows = append(rows, row{a.ID, a.Issuer, a.Account, string(a.Type), a.Folder, a.Tags, a.Pinned})
				}
				out(cmd, "%s", mustJSON(rows))
				return nil
			}
			if len(accts) == 0 {
				out(cmd, "%s", ui.SubtleStyle.Render("No accounts."))
				return nil
			}
			for _, a := range accts {
				meta := string(a.Type)
				if a.Folder != "" {
					meta += " /" + a.Folder
				}
				if len(a.Tags) > 0 {
					meta += " #" + strings.Join(a.Tags, " #")
				}
				pin := "  "
				if a.Pinned {
					pin = ui.AccentStyle.Render("★ ")
				}
				out(cmd, "%s%s %s  %s", pin, ui.Monogram(a.Issuer+a.Account, labelText(a)),
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

func filterAccounts(in []account.Account, folder, tag string) []account.Account {
	var out []account.Account
	for _, a := range in {
		if folder != "" && a.Folder != folder {
			continue
		}
		if tag != "" && !containsTag(a.Tags, tag) {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		return strings.ToLower(label(out[i])) < strings.ToLower(label(out[j]))
	})
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
