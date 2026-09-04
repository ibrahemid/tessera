// Package exporters renders vault accounts into other applications' export
// formats, so a user can leave Tessera the same way they arrived. Rendering is
// pure: an exporter builds files in memory and performs no I/O.
//
// These files are third-party documents, not vault payloads: the canonical-JSON
// rules in /spec/vault-format.md do NOT apply here. JSON is written indented
// with Go's default HTML escaping so the output looks like the app's own export.
// Secrets appear in the rendered files (that is the point of an export) but
// never in an error or a skip label.
package exporters

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
)

// File is one artifact an exporter produces. Name is a base name, never a path.
type File struct {
	Name string
	Data []byte
}

// Skipped names one account a format cannot represent, with the reason. Label
// never contains a secret.
type Skipped struct {
	Label  string
	Reason string
}

// Exporter renders accounts into one or more files.
type Exporter interface {
	// Name is the --format value: lowercase, hyphenated.
	Name() string
	// Description is one line for `tess export --help`.
	Description() string
	// Render builds the export files. Account order is vault order.
	Render(accts []account.Account) (files []File, skipped []Skipped, err error)
}

var registry = map[string]Exporter{}

func register(e Exporter) {
	registry[e.Name()] = e
}

func init() {
	register(aegisExporter{})
	register(twofasExporter{})
	register(bitwardenExporter{})
	register(protonExporter{})
	register(andOTPExporter{})
	register(appleCSVExporter{})
	register(migrationExporter{})
}

// Lookup returns the exporter registered under name.
func Lookup(name string) (Exporter, bool) {
	e, ok := registry[strings.ToLower(strings.TrimSpace(name))]
	return e, ok
}

// Names returns every registered format name, sorted.
func Names() []string {
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// All returns every registered exporter, ordered by name.
func All() []Exporter {
	out := make([]Exporter, 0, len(registry))
	for _, name := range Names() {
		out = append(out, registry[name])
	}
	return out
}

// marshalJSON renders v as indented JSON with a trailing newline, the shape a
// hand-inspected export file has.
func marshalJSON(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// label names an account for a skip line. It carries no secret.
func label(a account.Account) string {
	switch {
	case a.Issuer != "" && a.Account != "":
		return a.Issuer + " (" + a.Account + ")"
	case a.Issuer != "":
		return a.Issuer
	case a.Account != "":
		return a.Account
	default:
		return "unnamed account"
	}
}

// displayName is the label these formats show in their own list: the issuer
// when there is one, else the account.
func displayName(a account.Account) string {
	if a.Issuer != "" {
		return a.Issuer
	}
	return a.Account
}

// otpauthLabel rebuilds the "issuer:account" label string these formats store
// alongside the URI.
func otpauthLabel(a account.Account) string {
	if a.Issuer != "" {
		return a.Issuer + ":" + a.Account
	}
	return a.Account
}

// exportID derives a stable identifier for the id/uuid field these formats
// carry. It is a hash of the vault id, so re-exporting an unchanged vault
// produces byte-identical files, and it is well formed for any id (test and
// legacy vaults hold ids that are not 32 hex characters). No importer reads it.
func exportID(id string) string {
	sum := sha256.Sum256([]byte("tessera-export-id:" + id))
	b := sum[:16]
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// tokenType renders the account type the way the JSON authenticators spell it.
func tokenType(t account.Type) string {
	return strings.ToUpper(string(t))
}

func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }
func strPtr(v string) *string { return &v }
