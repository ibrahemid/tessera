// Package account defines the canonical Tessera account model and the
// canonical-JSON serialization that is sealed into the vault payload. The
// canonical form is the interop contract: the Swift core must produce identical
// bytes, so the rules in /spec/vault-format.md are enforced here and the Go
// encoder is the reference implementation.
package account

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Type is the OTP scheme of an account.
type Type string

const (
	TOTP  Type = "totp"
	HOTP  Type = "hotp"
	Steam Type = "steam"
)

// Account is one stored credential. Secret is the RAW key bytes.
type Account struct {
	ID        string
	Type      Type
	Issuer    string
	Account   string
	Secret    []byte
	Algorithm string // SHA1 | SHA256 | SHA512
	Digits    int
	Period    int
	Counter   int64
	Folder    string
	Handle    string // OPTIONAL short unique identifier; see /spec/vault-format.md
	Tags      []string
	Pinned    bool
	CreatedAt int64
	UpdatedAt int64
}

// Validate checks invariants before an account is persisted.
func (a Account) Validate() error {
	switch a.Type {
	case TOTP, HOTP, Steam:
	default:
		return fmt.Errorf("account %q: invalid type %q", a.ID, a.Type)
	}
	if a.ID == "" {
		return fmt.Errorf("account: empty id")
	}
	if len(a.Secret) == 0 {
		return fmt.Errorf("account %q: empty secret", a.ID)
	}
	switch a.Algorithm {
	case "SHA1", "SHA256", "SHA512":
	default:
		return fmt.Errorf("account %q: invalid algorithm %q", a.ID, a.Algorithm)
	}
	if a.Type != Steam && (a.Digits < 6 || a.Digits > 8) {
		return fmt.Errorf("account %q: digits must be 6-8, got %d", a.ID, a.Digits)
	}
	if a.Type != HOTP && a.Period <= 0 {
		return fmt.Errorf("account %q: period must be positive, got %d", a.ID, a.Period)
	}
	if a.Handle != "" && !ValidHandle(a.Handle) {
		return fmt.Errorf("account %q: invalid handle %q", a.ID, a.Handle)
	}
	return nil
}

// toMap renders an account as a map keyed exactly as the canonical schema. Map
// marshaling sorts keys by byte order, satisfying rule 1 of canonical JSON.
func (a Account) toMap() map[string]any {
	m := map[string]any{
		"account":    a.Account,
		"algorithm":  a.Algorithm,
		"counter":    a.Counter,
		"created_at": a.CreatedAt,
		"digits":     a.Digits,
		"folder":     a.Folder,
		"id":         a.ID,
		"issuer":     a.Issuer,
		"period":     a.Period,
		"pinned":     a.Pinned,
		"secret":     a.Secret, // []byte -> base64-standard with padding (json default)
		"tags":       tagsOrEmpty(a.Tags),
		"type":       string(a.Type),
		"updated_at": a.UpdatedAt,
	}
	// Optional; omitted when empty. Sorts between "folder" and "id" by byte order.
	if a.Handle != "" {
		m["handle"] = a.Handle
	}
	return m
}

func tagsOrEmpty(t []string) []string {
	if t == nil {
		return []string{}
	}
	return t
}

// CanonicalJSON serializes accounts to the canonical payload bytes. Accounts are
// ordered by ID so the output is deterministic regardless of input order.
func CanonicalJSON(accounts []Account) ([]byte, error) {
	sorted := make([]Account, len(accounts))
	copy(sorted, accounts)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	maps := make([]map[string]any, len(sorted))
	for i, a := range sorted {
		maps[i] = a.toMap()
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(maps); err != nil {
		return nil, err
	}
	// json.Encoder appends a trailing newline; canonical form has none.
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// ParseCanonicalJSON decodes canonical payload bytes, rejecting duplicate keys.
// Unknown account fields are tolerated (not DisallowUnknownFields): the payload
// is AEAD-authenticated, so a field a newer writer added is safe to ignore, and
// this keeps an older reader forward-compatible with additive schema changes. On
// re-seal the account is re-encoded from the spec fields only, so an unknown
// field is dropped rather than preserved.
func ParseCanonicalJSON(data []byte) ([]Account, error) {
	if err := rejectDuplicateKeys(data); err != nil {
		return nil, err
	}
	var raw []struct {
		Account   string   `json:"account"`
		Algorithm string   `json:"algorithm"`
		Counter   int64    `json:"counter"`
		CreatedAt int64    `json:"created_at"`
		Digits    int      `json:"digits"`
		Folder    string   `json:"folder"`
		Handle    string   `json:"handle"`
		ID        string   `json:"id"`
		Issuer    string   `json:"issuer"`
		Period    int      `json:"period"`
		Pinned    bool     `json:"pinned"`
		Secret    []byte   `json:"secret"`
		Tags      []string `json:"tags"`
		Type      string   `json:"type"`
		UpdatedAt int64    `json:"updated_at"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("account: parse canonical json: %w", err)
	}
	out := make([]Account, len(raw))
	for i, r := range raw {
		out[i] = Account{
			ID: r.ID, Type: Type(r.Type), Issuer: r.Issuer, Account: r.Account,
			Secret: r.Secret, Algorithm: r.Algorithm, Digits: r.Digits, Period: r.Period,
			Counter: r.Counter, Folder: r.Folder, Handle: r.Handle, Tags: tagsOrEmpty(r.Tags), Pinned: r.Pinned,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		}
	}
	return out, nil
}

// rejectDuplicateKeys walks the JSON token stream and errors on any repeated key
// within the same object (canonical JSON rule 6).
func rejectDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	return walk(dec, nil)
}

func walk(dec *json.Decoder, _ any) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			key := keyTok.(string)
			if seen[key] {
				return fmt.Errorf("account: duplicate key %q in object", key)
			}
			seen[key] = true
			if err := walk(dec, nil); err != nil {
				return err
			}
		}
		_, err := dec.Token() // closing }
		return err
	case '[':
		for dec.More() {
			if err := walk(dec, nil); err != nil {
				return err
			}
		}
		_, err := dec.Token() // closing ]
		return err
	}
	return nil
}
