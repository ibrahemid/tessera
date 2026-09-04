package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/exporters"
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
		format   string
		outPath  string
	)
	cmd := &cobra.Command{
		Use:   "export [query]",
		Short: "Export accounts as otpauth URIs, base32 secrets, QR images, another app's export, or an encrypted vault copy",
		Long: `Export account data (CLEARTEXT secrets — handle with care):

  tess export --uri                 # all accounts as otpauth:// URIs (bulk backup / migrate)
  tess export --uri github          # one account's otpauth URI
  tess export --secret github       # just the base32 secret (the raw key)
  tess export --qr ./qrcodes        # a QR PNG per account (scan into a phone)
  tess export --qr ./qrcodes github # one account's QR PNG
  tess export --file backup.json    # an encrypted copy of the whole vault (safe to store)
  tess export --format aegis        # the vault in another app's export format, on stdout
  tess export --format aegis --out aegis.json          # ... or written to a file
  tess export --format google-migration --out ./qr     # transfer QR codes for Google Authenticator

Formats for --format:
`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeFirstArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkExportModes(asURI, asSecret, qrDir, filePath, format); err != nil {
				return err
			}
			if filePath != "" {
				return exportEncrypted(cmd, filePath)
			}
			if !asURI && !asSecret && qrDir == "" && format == "" {
				return fmt.Errorf("use --uri, --secret, --qr, --file, or --format (see `tess export --help`)")
			}
			s, err := openSession()
			if err != nil {
				return err
			}
			defer s.close()
			accts := s.accounts
			if len(args) == 1 {
				idx, err := s.single(cmd, args[0])
				if err != nil {
					return err
				}
				accts = accts[idx : idx+1]
			}
			if format != "" {
				return exportFormat(cmd, accts, format, outPath)
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
	cmd.Long = strings.Replace(cmd.Long, "Formats for --format:\n", "Formats for --format:\n"+formatHelpLines(), 1)
	cmd.Flags().BoolVar(&asURI, "uri", false, "print otpauth:// URIs (secrets in cleartext)")
	cmd.Flags().BoolVar(&asSecret, "secret", false, "print only the base32 secret(s) (cleartext)")
	cmd.Flags().StringVar(&qrDir, "qr", "", "write a QR PNG per account to this directory (cleartext secrets)")
	cmd.Flags().StringVar(&filePath, "file", "", "write an encrypted copy of the vault to this path")
	cmd.Flags().StringVar(&format, "format", "", "rewrite the vault as another app's export: "+strings.Join(exporters.Names(), ", "))
	cmd.Flags().StringVar(&outPath, "out", "", "where --format writes: a file, or a directory for multi-file formats (default stdout)")
	return cmd
}

// formatHelpLines renders one help line per registered export format so the
// help text cannot drift from the registry.
func formatHelpLines() string {
	var b strings.Builder
	width := 0
	for _, name := range exporters.Names() {
		width = max(width, len(name))
	}
	for _, e := range exporters.All() {
		fmt.Fprintf(&b, "  %s  %s\n", padRight(e.Name(), width), e.Description())
	}
	return b.String()
}

// checkExportModes enforces that exactly one output mode is asked for. --format
// rewrites the whole vault in another app's shape and cannot be combined with
// the per-account or whole-vault modes.
func checkExportModes(asURI, asSecret bool, qrDir, filePath, format string) error {
	n := 0
	for _, on := range []bool{asURI, asSecret, qrDir != "", filePath != "", format != ""} {
		if on {
			n++
		}
	}
	if n > 1 {
		return fmt.Errorf("use exactly one of --uri, --secret, --qr, --file, --format")
	}
	return nil
}

// exportFormat renders accounts in another app's export format. Everything is
// rendered and checked before anything is written, so a rejected run leaves no
// partial file behind.
func exportFormat(cmd *cobra.Command, accts []account.Account, format, outPath string) error {
	e, ok := exporters.Lookup(format)
	if !ok {
		return fmt.Errorf("unknown format %q: use one of %s", format, strings.Join(exporters.Names(), ", "))
	}
	files, skipped, err := e.Render(accts)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		reportSkipped(cmd, skipped)
		return fmt.Errorf("nothing to export: %s cannot store any of these accounts", e.Name())
	}
	if outPath == "" {
		if e.MultiFile() {
			return fmt.Errorf("--format %s writes several files; pass --out <directory>", e.Name())
		}
		// A rendered image cannot be piped through the text output path.
		if isBinaryFile(files[0].Name) {
			return fmt.Errorf("--format %s writes image files; pass --out <directory>", e.Name())
		}
		fmt.Fprint(cmd.OutOrStdout(), string(files[0].Data))
		reportSkipped(cmd, skipped)
		return nil
	}
	if err := writeExportFiles(cmd, files, outPath, e.MultiFile()); err != nil {
		return err
	}
	reportSkipped(cmd, skipped)
	return nil
}

// writeExportFiles puts the rendered files at outPath. outPath is a directory
// when the format can render several files, when it ends in a separator, or
// when it already exists as one; otherwise it is the single file to write.
// multi is a property of the format rather than of this run: a batching format
// that happened to render one file still writes into a directory, so a small
// vault does not turn --out ./qr into an extensionless PNG.
func writeExportFiles(cmd *cobra.Command, files []exporters.File, outPath string, multi bool) error {
	asDir := multi || strings.HasSuffix(outPath, string(os.PathSeparator))
	if info, err := os.Stat(outPath); err == nil && info.IsDir() {
		asDir = true
	}
	if !asDir {
		if err := os.WriteFile(outPath, files[0].Data, 0o600); err != nil {
			return fmt.Errorf("write %q: %w", outPath, err)
		}
		out(cmd, "Wrote %s", outPath)
		return nil
	}
	dir := strings.TrimSuffix(outPath, string(os.PathSeparator))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %q: %w", dir, err)
	}
	for _, f := range files {
		path := filepath.Join(dir, f.Name)
		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			return fmt.Errorf("write %q: %w", path, err)
		}
		out(cmd, "Wrote %s", path)
	}
	return nil
}

// reportSkipped lists the accounts the format could not carry. It goes to
// stderr so it never lands in a piped export file.
func reportSkipped(cmd *cobra.Command, skipped []exporters.Skipped) {
	for _, s := range skipped {
		errOut(cmd, "Skipped %s: %s", s.Label, s.Reason)
	}
}

func isBinaryFile(name string) bool {
	return imageExts[strings.ToLower(filepath.Ext(name))]
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
