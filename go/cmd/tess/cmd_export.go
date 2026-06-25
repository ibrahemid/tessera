package main

import (
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/otpauth"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	var (
		asURI    bool
		asSecret bool
		filePath string
	)
	cmd := &cobra.Command{
		Use:   "export [query]",
		Short: "Export accounts as otpauth URIs or base32 secrets, or copy the encrypted vault",
		Long: `Export account data (CLEARTEXT secrets — handle with care):

  tess export --uri                 # all accounts as otpauth:// URIs (bulk backup / migrate)
  tess export --uri github          # one account's otpauth URI
  tess export --secret github       # just the base32 secret (the raw key)
  tess export --file backup.json    # an encrypted copy of the whole vault (safe to store)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if filePath != "" {
				return exportEncrypted(cmd, filePath)
			}
			if !asURI && !asSecret {
				return fmt.Errorf("use --uri, --secret, or --file (see `tess export --help`)")
			}
			s, err := openSession()
			if err != nil {
				return err
			}
			accts := s.accounts
			if len(args) == 1 {
				idx, err := s.find(args[0])
				if err != nil {
					return err
				}
				accts = accts[idx : idx+1]
			}
			for _, a := range accts {
				if asSecret {
					out(cmd, "%s", base32x.EncodeNoPad(a.Secret))
				} else {
					out(cmd, "%s", otpauth.Format(a))
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asURI, "uri", false, "print otpauth:// URIs (secrets in cleartext)")
	cmd.Flags().BoolVar(&asSecret, "secret", false, "print only the base32 secret(s) (cleartext)")
	cmd.Flags().StringVar(&filePath, "file", "", "write an encrypted copy of the vault to this path")
	return cmd
}

func exportEncrypted(cmd *cobra.Command, dst string) error {
	path, err := store.Resolve(vaultPath)
	if err != nil {
		return err
	}
	env, err := store.Load(path)
	if err != nil {
		return err
	}
	if err := store.Save(dst, env); err != nil {
		return err
	}
	out(cmd, "Wrote encrypted vault copy to %s", dst)
	return nil
}
