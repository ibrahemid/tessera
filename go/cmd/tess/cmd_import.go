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
		asJSON        bool
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

tess lists each account it added or skipped, each item it could not import
and why, then the totals. --json writes the same report as JSON.

` + argumentNote,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(migrationURIs)+len(otpauthURIs) > 0 && stdinIsTTY() {
				defer errOut(cmd, "%s", historyWarning)
			}
			batch := collectImport(qrPaths, filePaths, migrationURIs, otpauthURIs, args)
			if len(batch.accounts) == 0 && len(batch.problems) == 0 {
				return errImportNoSource
			}
			if len(batch.accounts) == 0 {
				reportImport(cmd, nil, mergeResult{}, batch.problems, asJSON)
				return errNothingImported
			}

			s, err := openSession()
			if err != nil {
				return err
			}
			defer s.close()
			merged := mergeAccountsDetailed(s, batch.accounts)
			if len(merged.added) > 0 {
				if err := s.save(); err != nil {
					return err
				}
			}
			reportImport(cmd, s, merged, batch.problems, asJSON)
			if len(merged.added) == 0 && len(batch.problems)+len(merged.failed) > 0 {
				return errNothingImported
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringArrayVar(&filePaths, "file", nil, "text, JSON, CSV or .1pux export file, - for stdin (repeatable)")
	f.StringArrayVar(&migrationURIs, "migration", nil, "Google Authenticator otpauth-migration:// URI (repeatable)")
	f.StringArrayVar(&otpauthURIs, "otpauth", nil, "single otpauth:// URI (repeatable)")
	f.StringArrayVar(&qrPaths, "qr", nil, "QR image file (repeatable; png/jpeg/webp/tiff/bmp)")
	f.BoolVar(&asJSON, "json", false, "write the report as JSON")
	return cmd
}

var (
	errImportNoSource  = errors.New("specify --file, --qr, --migration, --otpauth, or a file path")
	errNothingImported = errors.New("no accounts imported")
)

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
// system's reason, a size limit, no code in it, or an undecodable file.
func imageProblem(err error) string {
	var pathErr *os.PathError
	switch {
	case errors.Is(err, qr.ErrTooManyPixels):
		return qr.ErrTooManyPixels.Error()
	case errors.Is(err, qr.ErrImageFileTooLarge):
		return qr.ErrImageFileTooLarge.Error()
	case errors.As(err, &pathErr):
		return pathErr.Err.Error()
	case errors.Is(err, qr.ErrNoQRCode):
		return qr.ErrNoQRCode.Error()
	}
	return "unreadable image"
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

// importItem is the JSON shape of one reported item in `tess import --json`;
// docs/CLI-JSON.md is its contract. An account carries issuer, account and
// type; an item that never became an account carries source instead.
type importItem struct {
	Status  string  `json:"status"`
	ID      string  `json:"id,omitempty"`
	Handle  string  `json:"handle,omitempty"`
	Issuer  *string `json:"issuer,omitempty"`
	Account *string `json:"account,omitempty"`
	Type    *string `json:"type,omitempty"`
	Source  string  `json:"source,omitempty"`
	Reason  string  `json:"reason,omitempty"`
}

// importReport is the JSON shape of `tess import --json`.
type importReport struct {
	Items   []importItem `json:"items"`
	Added   int          `json:"added"`
	Skipped int          `json:"skipped"`
	Failed  int          `json:"failed"`
}

const reasonAlreadyInVault = "already in the vault"

// reportImport writes what the import did with each item, then the totals.
// Without --json, added and skipped accounts go to stdout and the items that
// failed to stderr, so a script reading stdout sees only the vault's side.
// s is nil when nothing reached the vault.
func reportImport(cmd *cobra.Command, s *session, merged mergeResult, problems []importProblem, asJSON bool) {
	failed := len(problems)
	for _, o := range merged.outcomes {
		if o.status == outcomeFailed {
			failed++
		}
	}
	if asJSON {
		r := importReport{Items: make([]importItem, 0, len(merged.outcomes)+len(problems)),
			Added: len(merged.added), Skipped: len(merged.skipped), Failed: failed}
		for _, o := range merged.outcomes {
			item := importItem{Status: string(o.status), Issuer: &o.issuer, Account: &o.account, Type: &o.typ, Reason: o.reason}
			if o.status == outcomeAdded {
				item.ID = o.id
				if s != nil {
					if i := indexByID(s.accounts, o.id); i >= 0 {
						item.Handle = s.accounts[i].Handle
					}
				}
			}
			r.Items = append(r.Items, item)
		}
		for _, p := range problems {
			r.Items = append(r.Items, importItem{Status: string(outcomeFailed), Source: p.source, Reason: p.reason})
		}
		out(cmd, "%s", mustJSON(r))
		return
	}
	var failures []importProblem
	for _, o := range merged.outcomes {
		switch o.status {
		case outcomeAdded:
			h := ""
			if i := indexByID(s.accounts, o.id); i >= 0 {
				h = displayHandle(s.accounts[i])
			}
			out(cmd, "Added %s as %s", o.label, h)
		case outcomeSkipped:
			out(cmd, "Skipped %s: %s", o.label, o.reason)
		case outcomeFailed:
			failures = append(failures, importProblem{o.label, o.reason})
		}
	}
	printProblems(cmd, append(append([]importProblem{}, problems...), failures...))
	if len(merged.outcomes) == 0 {
		return
	}
	summary := fmt.Sprintf("Imported %d", len(merged.added))
	if n := len(merged.skipped); n > 0 {
		summary += fmt.Sprintf(", skipped %d", n)
	}
	if failed > 0 {
		summary += fmt.Sprintf(", failed %d", failed)
	}
	out(cmd, "%s", summary)
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
	added    []string // ids of the accounts appended
	skipped  []string // labels of the accounts the vault already holds
	failed   []importProblem
	outcomes []mergeOutcome // every account of the batch, in batch order
}

type mergeStatus string

const (
	outcomeAdded   mergeStatus = "added"
	outcomeSkipped mergeStatus = "skipped"
	outcomeFailed  mergeStatus = "failed"
)

// mergeOutcome is what happened to one account of a batch. It holds the
// account's names, never its secret.
type mergeOutcome struct {
	status  mergeStatus
	id      string // added only
	label   string // escaped for display
	issuer  string
	account string
	typ     string
	reason  string // skipped and failed only
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
		o := mergeOutcome{label: importedLabel(a), issuer: a.Issuer, account: a.Account, typ: string(a.Type)}
		if seen[key] {
			r.skipped = append(r.skipped, o.label)
			o.status, o.reason = outcomeSkipped, reasonAlreadyInVault
			r.outcomes = append(r.outcomes, o)
			continue
		}
		a.ID = newID()
		a.CreatedAt = ts
		a.UpdatedAt = ts
		if err := a.Validate(); err != nil {
			r.failed = append(r.failed, importProblem{o.label, userMessage(err)})
			o.status, o.reason = outcomeFailed, userMessage(err)
			r.outcomes = append(r.outcomes, o)
			continue
		}
		seen[key] = true
		s.accounts = append(s.accounts, a)
		r.added = append(r.added, a.ID)
		o.status, o.id = outcomeAdded, a.ID
		r.outcomes = append(r.outcomes, o)
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
