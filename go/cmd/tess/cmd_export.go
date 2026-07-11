package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/otpauth"
	"github.com/ibrahemid/tessera/go/internal/qr"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	var (
		asURI    bool
		asSecret bool
		filePath string
		qrDir    string
	)
	cmd := &cobra.Command{
		Use:   "export [query]",
		Short: "Export accounts as otpauth URIs, base32 secrets, QR images, or an encrypted vault copy",
		Long: `Export account data (CLEARTEXT secrets — handle with care):

  tess export --uri                 # all accounts as otpauth:// URIs (bulk backup / migrate)
  tess export --uri github          # one account's otpauth URI
  tess export --secret github       # just the base32 secret (the raw key)
  tess export --qr ./qrcodes        # a QR PNG per account (scan into a phone)
  tess export --qr ./qrcodes github # one account's QR PNG
  tess export --file backup.json    # an encrypted copy of the whole vault (safe to store)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if filePath != "" {
				return exportEncrypted(cmd, filePath)
			}
			if !asURI && !asSecret && qrDir == "" {
				return fmt.Errorf("use --uri, --secret, --qr, or --file (see `tess export --help`)")
			}
			s, err := openSession()
			if err != nil {
				return err
			}
			accts := s.accounts
			if len(args) == 1 {
				idx, err := s.single(cmd, args[0])
				if err != nil {
					return err
				}
				accts = accts[idx : idx+1]
			}
			if qrDir != "" {
				return exportQR(cmd, accts, qrDir)
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
	cmd.Flags().StringVar(&qrDir, "qr", "", "write a QR PNG per account to this directory (cleartext secrets)")
	cmd.Flags().StringVar(&filePath, "file", "", "write an encrypted copy of the vault to this path")
	return cmd
}

func exportQR(cmd *cobra.Command, accts []account.Account, dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %q: %w", dir, err)
	}
	for i, a := range accts {
		name := qrFileName(a, i)
		path := filepath.Join(dir, name)
		if err := qr.EncodePNG(otpauth.Format(a), path, 512); err != nil {
			return err
		}
		out(cmd, "Wrote %s", path)
	}
	return nil
}

// qrFileName builds a filesystem-safe PNG name from an account, falling back to
// an index so two similar accounts don't collide.
func qrFileName(a account.Account, i int) string {
	base := strings.TrimSpace(a.Issuer + "-" + a.Account)
	base = strings.Trim(base, "-")
	if base == "" {
		base = fmt.Sprintf("account-%d", i+1)
	}
	repl := func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', ' ':
			return '_'
		}
		return r
	}
	return strings.Map(repl, base) + fmt.Sprintf("-%d.png", i+1)
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
