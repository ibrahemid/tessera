// Package account defines the canonical Tessera account model and the
// canonical-JSON serialization that is sealed into the vault payload. The
// canonical form is the interop contract: the Swift core must produce identical
// bytes, so the rules in /spec/vault-format.md are enforced here and the Go
// encoder is the reference implementation.
package account

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
)

// Type is the OTP scheme of an account.
type Type string

const (
	TOTP  Type = "totp"
	HOTP  Type = "hotp"
	Steam Type = "steam"
)

const MaxCounter int64 = math.MaxInt64 - 1

var (
	ErrDuplicateKey       = errors.New("account: duplicate key")
	ErrNonCanonicalNumber = errors.New("account: number is not a canonical int64 integer")
	ErrFieldType          = errors.New("account: field has the wrong JSON type")
	ErrNonCanonicalBase64 = errors.New("account: secret is not canonical standard base64")
	ErrMalformedPayload   = errors.New("account: payload is not an array of account objects")
	ErrExtensionKey       = errors.New("account: extension field uses a key the spec defines")
	ErrCounterRange       = errors.New("counter outside 0..9223372036854775806")
	ErrNegativeDeletedAt  = errors.New("deleted_at must not be negative")
	ErrNegativeOrder      = errors.New("order must not be negative")
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
	DeletedAt int64
	Note      string
	Order     int
	Extra     map[string]json.RawMessage
}

var definedKeys = map[string]bool{
	"account": true, "algorithm": true, "counter": true, "created_at": true,
	"deleted_at": true, "digits": true, "folder": true, "handle": true,
	"id": true, "issuer": true, "note": true, "order": true, "period": true,
	"pinned": true, "secret": true, "tags": true, "type": true, "updated_at": true,
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
	if a.Counter < 0 || a.Counter > MaxCounter {
		return fmt.Errorf("account %q: %w, got %d", a.ID, ErrCounterRange, a.Counter)
	}
	if a.Handle != "" && !ValidHandle(a.Handle) {
		return fmt.Errorf("account %q: invalid handle %q", a.ID, a.Handle)
	}
	if a.DeletedAt < 0 {
		return fmt.Errorf("account %q: %w, got %d", a.ID, ErrNegativeDeletedAt, a.DeletedAt)
	}
	if a.Order < 0 {
		return fmt.Errorf("account %q: %w, got %d", a.ID, ErrNegativeOrder, a.Order)
	}
	for _, k := range sortedKeys(a.Extra) {
		if _, err := extensionValue(k, a.Extra[k]); err != nil {
			return fmt.Errorf("account %q: %w", a.ID, err)
		}
	}
	return nil
}

func (a Account) usesV2() bool {
	return a.DeletedAt != 0 || a.Note != "" || a.Order != 0 || len(a.Extra) > 0
}

func FormatVersion(accounts []Account) int {
	for _, a := range accounts {
		if a.usesV2() {
			return 2
		}
	}
	return 1
}

// toMap renders an account as a map keyed exactly as the canonical schema. Map
// marshaling sorts keys by byte order, satisfying rule 1 of canonical JSON.
func (a Account) toMap() (map[string]any, error) {
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
	if a.DeletedAt != 0 {
		m["deleted_at"] = a.DeletedAt
	}
	if a.Note != "" {
		m["note"] = a.Note
	}
	if a.Order != 0 {
		m["order"] = a.Order
	}
	for k, v := range a.Extra {
		canon, err := extensionValue(k, v)
		if err != nil {
			return nil, fmt.Errorf("account %q: %w", a.ID, err)
		}
		m[k] = json.RawMessage(canon)
	}
	return m, nil
}

func extensionValue(key string, raw json.RawMessage) ([]byte, error) {
	if definedKeys[key] {
		return nil, fmt.Errorf("%w: %q", ErrExtensionKey, key)
	}
	canon, err := canonicalValue(raw)
	if err != nil {
		return nil, fmt.Errorf("extension field %q: %w", key, err)
	}
	return canon, nil
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
		m, err := a.toMap()
		if err != nil {
			return nil, err
		}
		maps[i] = m
	}
	return encodeCanonical(maps)
}

func encodeCanonical(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// json.Encoder appends a trailing newline; canonical form has none.
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func canonicalValue(raw []byte) ([]byte, error) {
	if err := checkStrictJSON(raw); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedPayload, err)
	}
	return encodeCanonical(v)
}

// ParseCanonicalJSON decodes payload bytes under canonical JSON rules 4 and 6, keeping undefined keys in Extra.
func ParseCanonicalJSON(data []byte) ([]Account, error) {
	if err := checkStrictJSON(data); err != nil {
		return nil, err
	}
	var elems []json.RawMessage
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, ErrMalformedPayload
	}
	if err := json.Unmarshal(trimmed, &elems); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedPayload, err)
	}
	out := make([]Account, len(elems))
	for i, raw := range elems {
		if len(raw) == 0 || raw[0] != '{' {
			return nil, fmt.Errorf("%w: element %d is not an object", ErrMalformedPayload, i)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrMalformedPayload, err)
		}
		a, err := decodeAccount(obj)
		if err != nil {
			return nil, err
		}
		out[i] = a
	}
	return out, nil
}

func decodeAccount(obj map[string]json.RawMessage) (Account, error) {
	var a Account
	for _, k := range sortedKeys(obj) {
		raw := obj[k]
		var err error
		switch k {
		case "account":
			a.Account, err = jsonString(raw)
		case "algorithm":
			a.Algorithm, err = jsonString(raw)
		case "folder":
			a.Folder, err = jsonString(raw)
		case "handle":
			a.Handle, err = jsonString(raw)
		case "id":
			a.ID, err = jsonString(raw)
		case "issuer":
			a.Issuer, err = jsonString(raw)
		case "note":
			a.Note, err = jsonString(raw)
		case "type":
			var s string
			s, err = jsonString(raw)
			a.Type = Type(s)
		case "counter":
			a.Counter, err = jsonInt64(raw)
		case "created_at":
			a.CreatedAt, err = jsonInt64(raw)
		case "deleted_at":
			a.DeletedAt, err = jsonInt64(raw)
		case "updated_at":
			a.UpdatedAt, err = jsonInt64(raw)
		case "digits":
			a.Digits, err = jsonInt(raw)
		case "order":
			a.Order, err = jsonInt(raw)
		case "period":
			a.Period, err = jsonInt(raw)
		case "pinned":
			a.Pinned, err = jsonBool(raw)
		case "secret":
			a.Secret, err = jsonBase64(raw)
		case "tags":
			a.Tags, err = jsonStrings(raw)
		default:
			var canon []byte
			canon, err = canonicalValue(raw)
			if err == nil {
				if a.Extra == nil {
					a.Extra = make(map[string]json.RawMessage)
				}
				a.Extra[k] = canon
			}
		}
		if err != nil {
			return Account{}, fmt.Errorf("account: field %q: %w", k, err)
		}
	}
	if a.Counter < 0 || a.Counter > MaxCounter {
		return Account{}, fmt.Errorf("account: field %q: %w, got %d", "counter", ErrCounterRange, a.Counter)
	}
	a.Tags = tagsOrEmpty(a.Tags)
	return a, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func jsonString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", ErrFieldType
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("%w: %w", ErrFieldType, err)
	}
	return s, nil
}

func jsonInt64(raw json.RawMessage) (int64, error) {
	s := string(raw)
	if !isCanonicalInt(s) {
		return 0, ErrFieldType
	}
	return strconv.ParseInt(s, 10, 64)
}

func jsonInt(raw json.RawMessage) (int, error) {
	s := string(raw)
	if !isCanonicalInt(s) {
		return 0, ErrFieldType
	}
	n, err := strconv.ParseInt(s, 10, strconv.IntSize)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrNonCanonicalNumber, err)
	}
	return int(n), nil
}

func jsonBool(raw json.RawMessage) (bool, error) {
	switch string(raw) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, ErrFieldType
}

func jsonBase64(raw json.RawMessage) ([]byte, error) {
	s, err := jsonString(raw)
	if err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.Strict().DecodeString(s)
	if err != nil || base64.StdEncoding.EncodeToString(b) != s {
		return nil, ErrNonCanonicalBase64
	}
	return b, nil
}

func jsonStrings(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || raw[0] != '[' {
		return nil, ErrFieldType
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(raw, &elems); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFieldType, err)
	}
	out := make([]string, len(elems))
	for i, e := range elems {
		s, err := jsonString(e)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

func isCanonicalInt(s string) bool {
	if s == "0" {
		return true
	}
	body := s
	if len(body) > 0 && body[0] == '-' {
		body = body[1:]
	}
	if body == "" || body[0] < '1' || body[0] > '9' {
		return false
	}
	for i := 1; i < len(body); i++ {
		if body[i] < '0' || body[i] > '9' {
			return false
		}
	}
	_, err := strconv.ParseInt(s, 10, 64)
	return err == nil
}

func checkStrictJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := walk(dec); err != nil {
		if errors.Is(err, ErrDuplicateKey) || errors.Is(err, ErrNonCanonicalNumber) {
			return err
		}
		return fmt.Errorf("%w: %w", ErrMalformedPayload, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("%w: trailing data after the value", ErrMalformedPayload)
	}
	return nil
}

func walk(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch v := tok.(type) {
	case json.Number:
		if !isCanonicalInt(string(v)) {
			return fmt.Errorf("%w: %s", ErrNonCanonicalNumber, v)
		}
		return nil
	case json.Delim:
		switch v {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyTok.(string)
				if !ok {
					return fmt.Errorf("object key is not a string")
				}
				if seen[key] {
					return fmt.Errorf("%w %q in object", ErrDuplicateKey, key)
				}
				seen[key] = true
				if err := walk(dec); err != nil {
					return err
				}
			}
			_, err := dec.Token()
			return err
		case '[':
			for dec.More() {
				if err := walk(dec); err != nil {
					return err
				}
			}
			_, err := dec.Token()
			return err
		}
	}
	return nil
}
