// Package importers parses plaintext (unencrypted) account exports from other
// authenticator apps and password managers into the canonical account model.
// The container switch and the format ladders are the interop contract in
// /spec/otpauth.md ("Input detection"); the Swift Importers must match.
// Encrypted exports are detected and rejected with a clear error rather than
// parsed wrong. Secrets are decoded to raw bytes here and never logged.
package importers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
)

// Parse detects a supported app export and returns its accounts. ok is false
// when data is not a recognized app export (the caller may fall back to parsing
// otpauth lines). A recognized-but-encrypted export, and a recognized export
// that holds no OTP entries, return ok=true with an error so the caller
// surfaces the real reason.
func Parse(data []byte) (accts []account.Account, source string, ok bool, err error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, "", false, nil
	}
	switch trimmed[0] {
	case '[':
		return parseJSONArrayExport(trimmed)
	case '{':
		return parseJSONObjectExport(trimmed)
	}
	if f, has := matchCSVHeader(data); has {
		accts, err := parseCSV(data, f)
		return recognized(f.name, accts, err)
	}
	if IsZip(data) {
		accts, err := parse1PUX(data)
		return recognized("1Password", accts, err)
	}
	if IsStratumEncrypted(data) {
		return nil, "Stratum", true, errors.New("Stratum backup is encrypted; in Stratum choose Settings > Backup > Export unencrypted and try again")
	}
	return nil, "", false, nil
}

// recognized finishes one format branch. A recognized export that parsed
// cleanly but yielded no accounts is a failure, not an empty success: silent
// skipping of items without a second factor would otherwise make a
// password-manager export indistinguishable from an empty input.
func recognized(source string, accts []account.Account, err error) ([]account.Account, string, bool, error) {
	if err == nil && len(accts) == 0 {
		err = fmt.Errorf("%s export has no one-time-password entries; only items with a 2FA code are imported", source)
	}
	return accts, source, true, err
}

// parseJSONObjectExport walks the top-level key ladder of /spec/otpauth.md.
// Each app's encrypted probe precedes its plaintext probe.
func parseJSONObjectExport(trimmed []byte) ([]account.Account, string, bool, error) {
	var probe map[string]json.RawMessage
	if json.Unmarshal(trimmed, &probe) != nil {
		return nil, "", false, nil
	}
	if _, has := probe["db"]; has {
		accts, err := parseAegis(trimmed)
		return recognized("Aegis", accts, err)
	}
	// Encrypted 2FAS backups carry BOTH an empty "services" array and the
	// ciphertext in "servicesEncrypted" — check the ciphertext first.
	if raw, has := probe["servicesEncrypted"]; has {
		var enc string
		if json.Unmarshal(raw, &enc) == nil && enc != "" {
			return nil, "2FAS", true, errors.New("2FAS export is encrypted; in 2FAS turn off the backup password (or decrypt) and export again")
		}
	}
	if _, has := probe["services"]; has {
		accts, err := parse2FAS(trimmed)
		return recognized("2FAS", accts, err)
	}
	_, hasEncryptedData := probe["encryptedData"]
	if _, hasNonce := probe["encryptionNonce"]; hasEncryptedData && hasNonce {
		return nil, "Ente Auth", true, errors.New("Ente Auth export is encrypted; in Ente Auth choose Export > plain text (unencrypted) and try again")
	}
	if raw, has := probe["encrypted"]; has {
		var encrypted bool
		if json.Unmarshal(raw, &encrypted) == nil && encrypted {
			return nil, "Bitwarden", true, errors.New("Bitwarden export is encrypted; in Bitwarden export again as unencrypted .json (File format: .json, not 'Password protected')")
		}
	}
	if raw, has := probe["items"]; has && isJSONArray(raw) {
		// A password-manager export always carries folders or collections; an
		// authenticator export carries neither. A stripped export can collide,
		// which changes only the reported source: one walker parses both.
		_, hasFolders := probe["folders"]
		_, hasCollections := probe["collections"]
		source := "Bitwarden Authenticator"
		if hasFolders || hasCollections {
			source = "Bitwarden"
		}
		accts, err := parseBitwardenItems(trimmed)
		return recognized(source, accts, err)
	}
	_, hasEntries := probe["entries"]
	_, hasSalt := probe["salt"]
	if _, hasContent := probe["content"]; hasSalt && hasContent && !hasEntries {
		return nil, "Proton Authenticator", true, errors.New("Proton Authenticator export is encrypted; in Proton Authenticator export again without a password")
	}
	if raw, has := probe["entries"]; has && isJSONArray(raw) {
		accts, err := parseProton(trimmed)
		return recognized("Proton Authenticator", accts, err)
	}
	_, hasTokens := probe["tokens"]
	if _, hasOrder := probe["tokenOrder"]; hasTokens && hasOrder {
		accts, err := parseFreeOTPPlus(trimmed)
		return recognized("FreeOTP+", accts, err)
	}
	if _, has := probe["Authenticators"]; has {
		accts, err := parseStratum(trimmed)
		return recognized("Stratum", accts, err)
	}
	if raw, has := probe["accounts"]; has && firstElementHasKey(raw, "vaults") {
		accts, err := parse1PUXData(trimmed)
		return recognized("1Password", accts, err)
	}
	return nil, "", false, nil
}

// parseJSONArrayExport probes the first element's keys: Raivo types its numbers
// as strings and uses "kind"; andOTP uses "type" plus "label".
func parseJSONArrayExport(trimmed []byte) ([]account.Account, string, bool, error) {
	var elems []map[string]json.RawMessage
	if json.Unmarshal(trimmed, &elems) != nil {
		return nil, "", false, nil
	}
	if len(elems) == 0 {
		return recognized("Raivo", nil, nil)
	}
	if _, has := elems[0]["kind"]; has {
		accts, err := parseRaivo(trimmed)
		return recognized("Raivo", accts, err)
	}
	_, hasType := elems[0]["type"]
	if _, hasLabel := elems[0]["label"]; hasType && hasLabel {
		accts, err := parseAndOTP(trimmed)
		return recognized("andOTP", accts, err)
	}
	return nil, "", false, nil
}

func isJSONArray(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '['
}

func firstElementHasKey(raw json.RawMessage, key string) bool {
	var elems []map[string]json.RawMessage
	if json.Unmarshal(raw, &elems) != nil || len(elems) == 0 {
		return false
	}
	_, has := elems[0][key]
	return has
}
