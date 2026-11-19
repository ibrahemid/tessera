package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/detect"
	"github.com/ibrahemid/tessera/go/internal/qr"
	"github.com/ibrahemid/tessera/go/internal/ui"
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
  cat export.json | tess import -                 # read the input from stdin
  tess import --file accounts.txt                 # otpauth:// / otpauth-migration:// lines, or a setup key per line
  tess import --file export.json                  # an app export (see the list below)
  tess import --qr a.png --qr b.png               # QR images (multiple codes per image are all read)
  tess import --migration "otpauth-migration://offline?data=..."
  tess import --otpauth "otpauth://totp/...."

Exports it reads, recognized by content rather than by file name:

  Aegis, 2FAS, Raivo, andOTP, FreeOTP+, Stratum (Authenticator Pro),
  Bitwarden Authenticator, Bitwarden, Proton Authenticator, Ente Auth,
  Apple Passwords (CSV), 1Password (CSV and .1pux), and Google Authenticator
  (an otpauth-migration:// URI or a photo of its transfer QR code)

An export must be unencrypted; an encrypted one is refused by name rather than
half-read. Items with no one-time-password field, which is most of a password
manager's export, are left out. Duplicate accounts (same type, issuer, account
and secret) are skipped. docs/TRANSFER.md lists what to export from each app.

` + argumentNote,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(migrationURIs)+len(otpauthURIs) > 0 && stdinIsTTY() {
				defer errOut(cmd, "%s", historyWarning)
			}
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
			defer s.close()
			added, skipped, failed := mergeAccounts(s, batch.accounts)
			batch.problems = append(batch.problems, failed...)
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
	f.StringArrayVar(&filePaths, "file", nil, "text, JSON, CSV or .1pux export file, - for stdin (repeatable)")
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
		b.problems = append(b.problems, importProblem{path, imageProblem(err)})
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
			b.problems = append(b.problems, importProblem{src, userMessage(e.Err)})
		}
	}
}

// imageProblem is the reason an image yielded no QR payload: the operating
// system's reason, a size limit, an undecodable file, or no code in it.
func imageProblem(err error) string {
	var pathErr *os.PathError
	switch {
	case errors.Is(err, qr.ErrTooManyPixels):
		return qr.ErrTooManyPixels.Error()
	case errors.Is(err, qr.ErrImageFileTooLarge):
		return qr.ErrImageFileTooLarge.Error()
	case errors.As(err, &pathErr):
		return pathErr.Err.Error()
	case strings.Contains(err.Error(), "decode image"):
		return "unreadable image"
	}
	return "no QR code found"
}

// addTextFile reads a file and parses it as text/JSON, recording per-line
// failures against the file path and line number.
func (b *importBatch) addTextFile(path string) {
	if path == "-" {
		b.addStdin()
		return
	}
	data, err := readFileCapped(path)
	if err != nil {
		b.problems = append(b.problems, importProblem{path, readProblem(err)})
		return
	}
	accts, errs := detect.ParseText(string(data))
	b.accounts = append(b.accounts, accts...)
	b.problems = append(b.problems, itemProblems(path, errs)...)
}

// addStdin reads all of stdin as import text, so tess can end a pipeline.
func (b *importBatch) addStdin() {
	data, err := readCapped(stdinReader, "stdin")
	if err != nil {
		b.problems = append(b.problems, importProblem{"stdin", readProblem(err)})
		return
	}
	accts, errs := detect.ParseText(string(data))
	b.accounts = append(b.accounts, accts...)
	b.problems = append(b.problems, itemProblems("stdin", errs)...)
}

// addPayload parses a single URI passed via a flag.
func (b *importBatch) addPayload(source, text string) {
	accts, errs := detect.ParseText(text)
	b.accounts = append(b.accounts, accts...)
	for _, e := range errs {
		b.problems = append(b.problems, importProblem{source, userMessage(e.Err)})
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

// printProblems lists the items that could not be imported on stderr, so a
// script reading stdout sees only what was added.
func printProblems(cmd *cobra.Command, problems []importProblem) {
	if len(problems) == 0 {
		return
	}
	if len(problems) == 1 {
		errOut(cmd, "Could not import 1 item:")
	} else {
		errOut(cmd, "Could not import %d items:", len(problems))
	}
	for _, p := range problems {
		errOut(cmd, "  %s: %s", ui.Sanitize(p.source), ui.Sanitize(p.reason))
	}
}

// mergeResult is what merging a batch into the session did with each account.
type mergeResult struct {
	added   []string // ids of the accounts appended
	skipped []string // labels of the accounts the vault already holds
	failed  []importProblem
}

// mergeAccounts stamps and appends imported accounts to the session, skipping
// duplicates (same type/issuer/account/secret) already present or within the
// batch. An account that fails validation is a failure, not a duplicate: it is
// returned as a problem with the reason so the summary does not claim the user
// already had it.
func mergeAccounts(s *session, imported []account.Account) (added, skipped int, failed []importProblem) {
	r := mergeAccountsDetailed(s, imported)
	return len(r.added), len(r.skipped), r.failed
}

// mergeAccountsDetailed is mergeAccounts reporting which accounts went where.
func mergeAccountsDetailed(s *session, imported []account.Account) mergeResult {
	var r mergeResult
	seen := map[string]bool{}
	for _, a := range account.Live(s.accounts) {
		seen[dedupeKey(a)] = true
	}
	ts := now().Unix()
	for _, a := range imported {
		key := dedupeKey(a)
		if seen[key] {
			r.skipped = append(r.skipped, importedLabel(a))
			continue
		}
		a.ID = newID()
		a.CreatedAt = ts
		a.UpdatedAt = ts
		if err := a.Validate(); err != nil {
			r.failed = append(r.failed, importProblem{importedLabel(a), userMessage(err)})
			continue
		}
		seen[key] = true
		s.accounts = append(s.accounts, a)
		r.added = append(r.added, a.ID)
	}
	return r
}

// importedLabel names an imported account in a problem line without exposing
// its secret. An account with neither issuer nor account gets a placeholder.
func importedLabel(a account.Account) string {
	if l := label(a); strings.TrimSpace(l) != "" {
		return l
	}
	return "unnamed account"
}

func dedupeKey(a account.Account) string {
	return strings.Join([]string{
		string(a.Type), strings.ToLower(a.Issuer), strings.ToLower(a.Account),
		base32x.EncodeNoPad(a.Secret),
	}, "|")
}
