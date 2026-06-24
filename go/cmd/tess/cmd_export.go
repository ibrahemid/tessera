package main

import (
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/otpauth"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	var (
		asURI    bool
		filePath string
	)
	cmd := &cobra.Command{
		Use:   "export [query]",
		Short: "Export accounts as otpauth URIs, or copy the encrypted vault file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if filePath != "" {
				return exportEncrypted(cmd, filePath)
			}
			if !asURI {
				return fmt.Errorf("use --uri to print otpauth URIs or --file <path> to copy the encrypted vault")
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
				out(cmd, "%s", otpauth.Format(a))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asURI, "uri", false, "print otpauth:// URIs (secrets in cleartext)")
	cmd.Flags().StringVar(&filePath, "file", "", "write a copy of the encrypted vault to this path")
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
