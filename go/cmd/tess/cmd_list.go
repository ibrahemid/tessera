package main

import (
	"sort"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var folder, tag string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List accounts (without codes)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			accts := filterAccounts(s.accounts, folder, tag)
			if len(accts) == 0 {
				out(cmd, "No accounts.")
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
				if a.Pinned {
					meta += " *"
				}
				out(cmd, "%s  %-30s  [%s]", a.ID[:8], label(a), meta)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&folder, "folder", "", "filter by folder")
	cmd.Flags().StringVar(&tag, "tag", "", "filter by tag")
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
