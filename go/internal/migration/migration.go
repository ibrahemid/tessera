// Package migration parses Google Authenticator export URIs
// (otpauth-migration://offline?data=...) into the canonical account model.
// The payload is a base64-encoded protobuf (MigrationPayload); it is decoded
// here with low-level wire decoding (no protoc/codegen). Secrets are stored as
// raw key bytes, never base32.
package migration

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/ibrahemid/tessera/go/internal/account"
)

// MigrationPayload wire field numbers.
const (
	fieldOtpParameters protowire.Number = 1
	fieldVersion       protowire.Number = 2
	fieldBatchSize     protowire.Number = 3
	fieldBatchIndex    protowire.Number = 4
	fieldBatchID       protowire.Number = 5
)

// OtpParameters wire field numbers.
const (
	paramSecret    protowire.Number = 1
	paramName      protowire.Number = 2
	paramIssuer    protowire.Number = 3
	paramAlgorithm protowire.Number = 4
	paramDigits    protowire.Number = 5
	paramType      protowire.Number = 6
	paramCounter   protowire.Number = 7
)

// Algorithm enum values.
const (
	algUnspecified int64 = 0
	algSHA1        int64 = 1
	algSHA256      int64 = 2
	algSHA512      int64 = 3
	algMD5         int64 = 4
)

// DigitCount enum values.
const (
	digitsUnspecified int64 = 0
	digitsSix         int64 = 1
	digitsEight       int64 = 2
)

// OtpType enum values.
const (
	typeUnspecified int64 = 0
	typeHOTP        int64 = 1
	typeTOTP        int64 = 2
)

// Parse decodes an otpauth-migration:// URI into one or more accounts. When the
// export spans multiple QR codes (batch_size > 1), only the parameters present
// in this single URI are returned; the caller merges batches.
func Parse(uri string) ([]account.Account, error) {
	u, err := url.Parse(strings.TrimSpace(uri))
	if err != nil {
		return nil, fmt.Errorf("migration: parse uri: %w", err)
	}
	if u.Scheme != "otpauth-migration" {
		return nil, fmt.Errorf("migration: not an otpauth-migration uri (scheme %q)", u.Scheme)
	}
	if !strings.EqualFold(u.Host, "offline") {
		return nil, fmt.Errorf("migration: unexpected host %q (want \"offline\")", u.Host)
	}

	q := u.Query()
	if !q.Has("data") {
		return nil, fmt.Errorf("migration: missing data query parameter")
	}
	data := q.Get("data")
	if data == "" {
		return nil, fmt.Errorf("migration: empty data query parameter")
	}

	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("migration: base64-decode data: %w", err)
	}

	params, err := decodePayload(raw)
	if err != nil {
		return nil, err
	}
	if len(params) == 0 {
		return nil, fmt.Errorf("migration: payload contained no otp parameters")
	}

	accounts := make([]account.Account, 0, len(params))
	for i, p := range params {
		a, err := p.toAccount()
		if err != nil {
			return nil, fmt.Errorf("migration: otp parameter %d: %w", i, err)
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}

// otpParams holds the decoded fields of one OtpParameters message.
type otpParams struct {
	secret    []byte
	name      string
	issuer    string
	algorithm int64
	digits    int64
	otpType   int64
	counter   int64
}

// decodePayload walks the MigrationPayload wire bytes, collecting every
// OtpParameters submessage. Unknown fields are skipped. Malformed wire data is
// rejected with context.
func decodePayload(b []byte) ([]otpParams, error) {
	var out []otpParams
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, fmt.Errorf("migration: malformed payload tag: %w", protowire.ParseError(n))
		}
		b = b[n:]

		switch num {
		case fieldOtpParameters:
			if typ != protowire.BytesType {
				return nil, fmt.Errorf("migration: otp_parameters has wire type %d, want bytes", typ)
			}
			sub, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return nil, fmt.Errorf("migration: malformed otp_parameters: %w", protowire.ParseError(n))
			}
			b = b[n:]
			p, err := decodeOtpParameters(sub)
			if err != nil {
				return nil, err
			}
			out = append(out, p)
		case fieldVersion, fieldBatchSize, fieldBatchIndex, fieldBatchID:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return nil, fmt.Errorf("migration: malformed payload field %d: %w", num, protowire.ParseError(n))
			}
			b = b[n:]
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return nil, fmt.Errorf("migration: malformed unknown field %d: %w", num, protowire.ParseError(n))
			}
			b = b[n:]
		}
	}
	return out, nil
}

// decodeOtpParameters walks one OtpParameters submessage.
func decodeOtpParameters(b []byte) (otpParams, error) {
	var p otpParams
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return p, fmt.Errorf("migration: malformed otp parameter tag: %w", protowire.ParseError(n))
		}
		b = b[n:]

		switch num {
		case paramSecret:
			if typ != protowire.BytesType {
				return p, fmt.Errorf("migration: secret has wire type %d, want bytes", typ)
			}
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return p, fmt.Errorf("migration: malformed secret: %w", protowire.ParseError(n))
			}
			b = b[n:]
			p.secret = append([]byte(nil), v...)
		case paramName:
			if typ != protowire.BytesType {
				return p, fmt.Errorf("migration: name has wire type %d, want bytes", typ)
			}
			v, n := protowire.ConsumeString(b)
			if n < 0 {
				return p, fmt.Errorf("migration: malformed name: %w", protowire.ParseError(n))
			}
			b = b[n:]
			p.name = v
		case paramIssuer:
			if typ != protowire.BytesType {
				return p, fmt.Errorf("migration: issuer has wire type %d, want bytes", typ)
			}
			v, n := protowire.ConsumeString(b)
			if n < 0 {
				return p, fmt.Errorf("migration: malformed issuer: %w", protowire.ParseError(n))
			}
			b = b[n:]
			p.issuer = v
		case paramAlgorithm:
			if typ != protowire.VarintType {
				return p, fmt.Errorf("migration: algorithm has wire type %d, want varint", typ)
			}
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return p, fmt.Errorf("migration: malformed algorithm: %w", protowire.ParseError(n))
			}
			b = b[n:]
			p.algorithm = int64(v)
		case paramDigits:
			if typ != protowire.VarintType {
				return p, fmt.Errorf("migration: digits has wire type %d, want varint", typ)
			}
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return p, fmt.Errorf("migration: malformed digits: %w", protowire.ParseError(n))
			}
			b = b[n:]
			p.digits = int64(v)
		case paramType:
			if typ != protowire.VarintType {
				return p, fmt.Errorf("migration: type has wire type %d, want varint", typ)
			}
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return p, fmt.Errorf("migration: malformed type: %w", protowire.ParseError(n))
			}
			b = b[n:]
			p.otpType = int64(v)
		case paramCounter:
			if typ != protowire.VarintType {
				return p, fmt.Errorf("migration: counter has wire type %d, want varint", typ)
			}
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return p, fmt.Errorf("migration: malformed counter: %w", protowire.ParseError(n))
			}
			b = b[n:]
			p.counter = int64(v)
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return p, fmt.Errorf("migration: malformed unknown otp field %d: %w", num, protowire.ParseError(n))
			}
			b = b[n:]
		}
	}
	return p, nil
}

// toAccount maps decoded parameters onto the canonical account model. The
// caller stamps CreatedAt/UpdatedAt; they are left zero here.
func (p otpParams) toAccount() (account.Account, error) {
	if len(p.secret) == 0 {
		return account.Account{}, fmt.Errorf("empty secret")
	}

	a := account.Account{
		Secret:    p.secret,
		Algorithm: "SHA1",
		Digits:    6,
		Period:    30,
		Counter:   p.counter,
	}

	id, err := newID()
	if err != nil {
		return account.Account{}, err
	}
	a.ID = id

	// Label name may be "issuer:account"; split on the first ':'.
	name := strings.TrimSpace(p.name)
	if i := strings.Index(name, ":"); i >= 0 {
		a.Issuer = strings.TrimSpace(name[:i])
		a.Account = strings.TrimSpace(name[i+1:])
	} else {
		a.Account = name
	}
	// The explicit issuer field, when present, is authoritative.
	if iss := strings.TrimSpace(p.issuer); iss != "" {
		a.Issuer = iss
	}

	switch p.algorithm {
	case algUnspecified, algSHA1:
		a.Algorithm = "SHA1"
	case algSHA256:
		a.Algorithm = "SHA256"
	case algSHA512:
		a.Algorithm = "SHA512"
	case algMD5:
		return account.Account{}, fmt.Errorf("algorithm MD5 is not supported by RFC TOTP clients")
	default:
		return account.Account{}, fmt.Errorf("unknown algorithm enum %d", p.algorithm)
	}

	switch p.digits {
	case digitsUnspecified, digitsSix:
		a.Digits = 6
	case digitsEight:
		a.Digits = 8
	default:
		return account.Account{}, fmt.Errorf("unknown digit-count enum %d", p.digits)
	}

	switch p.otpType {
	case typeTOTP, typeUnspecified:
		a.Type = account.TOTP
	case typeHOTP:
		a.Type = account.HOTP
	default:
		return account.Account{}, fmt.Errorf("unknown otp-type enum %d", p.otpType)
	}

	// Steam heuristic: issuer "Steam" is treated as a Steam credential.
	if a.Type == account.TOTP && strings.EqualFold(a.Issuer, "Steam") {
		a.Type = account.Steam
	}

	return a, nil
}

// newID returns a random 32-character hex identifier (16 bytes).
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("migration: generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
