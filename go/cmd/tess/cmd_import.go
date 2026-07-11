package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/detect"
	"github.com/ibrahemid/tessera/go/internal/qr"
	"github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
	var (
		migrationURIs []string
		otpauthURIs   []string
		qrPaths       []string
		filePaths     []string
	)
	cmd := &cobra.Command{
		Use:   "import [path...]",
		Short: "Bulk-import accounts from files, other apps, Google exports, otpauth URIs, or QR images",
		Args:  cobra.ArbitraryArgs,
		Long: `Import accounts in bulk. Sources combine and can be repeated; everything that
parses is imported, and items that fail are listed without aborting the batch:

  tess import accounts.txt export.json code.png   # paths auto-detect by content
  tess import --file accounts.txt                 # otpauth:// / otpauth-migration:// lines, or a setup key per line
  tess import --file export.json                  # an Aegis, 2FAS, or Raivo export (unencrypted)
  tess import --qr a.png --qr b.png               # QR images (multiple codes per image are all read)
  tess import --migration "otpauth-migration://offline?data=..."
  tess import --otpauth "otpauth://totp/...."

App exports must be unencrypted (Aegis/2FAS: export with the backup password off).
Duplicate accounts (same type, issuer, account and secret) are skipped.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			batch := collectImport(qrPaths, filePaths, migrationURIs, otpauthURIs, args)
			if len(batch.accounts) == 0 && len(batch.problems) == 0 {
				return fmt.Errorf("specify --file, --qr, --migration, --otpauth, or a file path")
			}
			if len(batch.accounts) == 0 {
				printProblems(cmd, batch.problems)
				return fmt.Errorf("no accounts imported")
			}

			s, err := openSession()
			if err != nil {
				return err
			}
			added, skipped := mergeAccounts(s, batch.accounts)
			if added > 0 {
				if err := s.save(); err != nil {
					return err
				}
			}
			printImportSummary(cmd, added, skipped)
			printProblems(cmd, batch.problems)
			if added == 0 && len(batch.problems) > 0 {
				return fmt.Errorf("no accounts imported")
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringArrayVar(&filePaths, "file", nil, "text or JSON export file (repeatable)")
	f.StringArrayVar(&migrationURIs, "migration", nil, "Google Authenticator otpauth-migration:// URI (repeatable)")
	f.StringArrayVar(&otpauthURIs, "otpauth", nil, "single otpauth:// URI (repeatable)")
	f.StringArrayVar(&qrPaths, "qr", nil, "QR image file (repeatable; png/jpeg/webp/tiff/bmp)")
	return cmd
}

// importBatch accumulates the accounts parsed from every source in one run along
// with a per-item problem for each source that failed. Collecting the batch does
// no vault I/O, so it is unit-testable without a session.
type importBatch struct {
	accounts []account.Account
	problems []importProblem
}

// importProblem names one item that could not be imported. reason never contains
// a raw secret; source is a file path (optionally with a line or QR index).
type importProblem struct {
	source string
	reason string
}

// collectImport parses every source into one batch. QR flags decode images;
// --file reads text/JSON; --migration/--otpauth parse a single URI; positional
// paths auto-detect image vs text by extension.
func collectImport(qrPaths, filePaths, migrationURIs, otpauthURIs, args []string) *importBatch {
	b := &importBatch{}
	for _, p := range qrPaths {
		b.addImage(p)
	}
	for _, p := range filePaths {
		b.addTextFile(p)
	}
	for _, uri := range migrationURIs {
		b.addPayload("--migration", uri)
	}
	for _, uri := range otpauthURIs {
		b.addPayload("--otpauth", uri)
	}
	for _, arg := range args {
		if isImagePath(arg) {
			b.addImage(arg)
		} else {
			b.addTextFile(arg)
		}
	}
	return b
}

// addImage decodes every QR code in an image file and parses each payload.
func (b *importBatch) addImage(path string) {
	payloads, err := qr.DecodeFileAll(path)
	if err != nil {
		reason := "no QR code found"
		if strings.Contains(err.Error(), "decode image") {
			reason = "unreadable image"
		}
		b.problems = append(b.problems, importProblem{path, reason})
		return
	}
	for i, payload := range payloads {
		src := path
		if len(payloads) > 1 {
			src = fmt.Sprintf("%s (QR %d)", path, i+1)
		}
		accts, errs := detect.ParseText(payload)
		b.accounts = append(b.accounts, accts...)
		for _, e := range errs {
			b.problems = append(b.problems, importProblem{src, e.Err.Error()})
		}
	}
}

// addTextFile reads a file and parses it as text/JSON, recording per-line
// failures against the file path and line number.
func (b *importBatch) addTextFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		b.problems = append(b.problems, importProblem{path, "could not read file"})
		return
	}
	accts, errs := detect.ParseText(string(data))
	b.accounts = append(b.accounts, accts...)
	for _, e := range errs {
		src := path
		if e.Line > 0 {
			src = fmt.Sprintf("%s line %d", path, e.Line)
		}
		b.problems = append(b.problems, importProblem{src, e.Err.Error()})
	}
}

// addPayload parses a single URI passed via a flag.
func (b *importBatch) addPayload(source, text string) {
	accts, errs := detect.ParseText(text)
	b.accounts = append(b.accounts, accts...)
	for _, e := range errs {
		b.problems = append(b.problems, importProblem{source, e.Err.Error()})
	}
}

// imageExts are the file extensions routed through QR decoding.
var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true,
	".webp": true, ".tiff": true, ".tif": true, ".bmp": true, ".gif": true,
}

func isImagePath(path string) bool {
	return imageExts[strings.ToLower(filepath.Ext(path))]
}

func printImportSummary(cmd *cobra.Command, added, skipped int) {
	if skipped > 0 {
		out(cmd, "Imported %d, skipped %d duplicate(s)", added, skipped)
	} else {
		out(cmd, "Imported %d", added)
	}
}

func printProblems(cmd *cobra.Command, problems []importProblem) {
	if len(problems) == 0 {
		return
	}
	out(cmd, "Could not import %d item(s):", len(problems))
	for _, p := range problems {
		out(cmd, "  %s: %s", p.source, p.reason)
	}
}

// mergeAccounts stamps and appends imported accounts to the session, skipping
// duplicates (same type/issuer/account/secret) already present or within the
// batch. Returns counts of added and skipped.
func mergeAccounts(s *session, imported []account.Account) (added, skipped int) {
	seen := map[string]bool{}
	for _, a := range s.accounts {
		seen[dedupeKey(a)] = true
	}
	ts := now().Unix()
	for _, a := range imported {
		key := dedupeKey(a)
		if seen[key] {
			skipped++
			continue
		}
		seen[key] = true
		a.ID = newID()
		a.CreatedAt = ts
		a.UpdatedAt = ts
		if err := a.Validate(); err != nil {
			skipped++
			continue
		}
		s.accounts = append(s.accounts, a)
		added++
	}
	return added, skipped
}

func dedupeKey(a account.Account) string {
	return strings.Join([]string{
		string(a.Type), strings.ToLower(a.Issuer), strings.ToLower(a.Account),
		base32x.EncodeNoPad(a.Secret),
	}, "|")
}
