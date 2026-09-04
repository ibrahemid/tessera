package exporters

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/url"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/qr"
)

// migrationExporter writes Google Authenticator transfer QR codes
// (otpauth-migration://offline?data=...). The payload is the same MigrationPayload
// protobuf the migration package decodes, encoded here with protowire — no
// protoc, no codegen.
type migrationExporter struct{}

func (migrationExporter) Name() string { return "google-migration" }

func (migrationExporter) Description() string {
	return "Google Authenticator transfer QR codes (PNG per batch)"
}

// MigrationPayload wire field numbers, mirroring internal/migration.
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

const (
	// maxAccountsPerBatch and maxPayloadBytes are conservative: Google
	// publishes no per-QR limit, and a QR that is too dense fails to scan on a
	// phone camera long before it fails to decode here. maxPayloadBytes is the
	// budget for the otp_parameters submessages; batchTrailerBytes reserves room
	// for version, batch_size, batch_index and batch_id. If a rendered QR ever
	// fails to decode, LOWER maxPayloadBytes; never raise it.
	maxAccountsPerBatch = 10
	maxPayloadBytes     = 800
	batchTrailerBytes   = 24
	qrPixelSize         = 768
)

// Algorithm, DigitCount and OtpType enum values.
const (
	algSHA1   = 1
	algSHA256 = 2
	algSHA512 = 3

	digitsSix   = 1
	digitsEight = 2

	typeHOTP = 1
	typeTOTP = 2
)

func (migrationExporter) Render(accts []account.Account) ([]File, []Skipped, error) {
	var (
		encoded [][]byte
		skipped []Skipped
	)
	for _, a := range accts {
		params, reason := encodeOtpParameters(a)
		if reason != "" {
			skipped = append(skipped, Skipped{label(a), reason})
			continue
		}
		encoded = append(encoded, params)
	}
	if len(encoded) == 0 {
		return nil, skipped, nil
	}

	batches := partitionBatches(encoded)
	batchID, err := randomBatchID()
	if err != nil {
		return nil, nil, err
	}
	files := make([]File, 0, len(batches))
	for i, batch := range batches {
		payload := assemblePayload(batch, len(batches), i, batchID)
		uri := "otpauth-migration://offline?data=" +
			url.QueryEscape(base64.StdEncoding.EncodeToString(payload))
		png, err := qr.EncodePNGBytes(uri, qrPixelSize)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, File{
			Name: fmt.Sprintf("google-migration-%d-of-%d.png", i+1, len(batches)),
			Data: png,
		})
	}
	return files, skipped, nil
}

// encodeOtpParameters renders one account as an OtpParameters submessage. An
// account the schema cannot carry is refused with a reason rather than
// downgraded: rewriting a period or a digit count would generate wrong codes
// for as long as the account lived in the other app.
func encodeOtpParameters(a account.Account) (params []byte, reason string) {
	var typ uint64
	switch a.Type {
	case account.TOTP:
		typ = typeTOTP
	case account.HOTP:
		typ = typeHOTP
	default:
		return nil, "Google Authenticator has no Steam entry type"
	}
	var alg uint64
	switch a.Algorithm {
	case "SHA1":
		alg = algSHA1
	case "SHA256":
		alg = algSHA256
	case "SHA512":
		alg = algSHA512
	default:
		return nil, fmt.Sprintf("Google Authenticator does not store the %s algorithm", a.Algorithm)
	}
	var digits uint64
	switch a.Digits {
	case 6:
		digits = digitsSix
	case 8:
		digits = digitsEight
	default:
		return nil, fmt.Sprintf("Google Authenticator stores only 6- and 8-digit codes, this one has %d", a.Digits)
	}
	if a.Type == account.TOTP && a.Period != 30 {
		return nil, fmt.Sprintf("Google Authenticator stores only 30-second codes, this one uses %ds", a.Period)
	}

	var b []byte
	b = protowire.AppendTag(b, paramSecret, protowire.BytesType)
	b = protowire.AppendBytes(b, a.Secret)
	if name := migrationName(a); name != "" {
		b = protowire.AppendTag(b, paramName, protowire.BytesType)
		b = protowire.AppendString(b, name)
	}
	if a.Issuer != "" {
		b = protowire.AppendTag(b, paramIssuer, protowire.BytesType)
		b = protowire.AppendString(b, a.Issuer)
	}
	b = protowire.AppendTag(b, paramAlgorithm, protowire.VarintType)
	b = protowire.AppendVarint(b, alg)
	b = protowire.AppendTag(b, paramDigits, protowire.VarintType)
	b = protowire.AppendVarint(b, digits)
	b = protowire.AppendTag(b, paramType, protowire.VarintType)
	b = protowire.AppendVarint(b, typ)
	if a.Type == account.HOTP {
		b = protowire.AppendTag(b, paramCounter, protowire.VarintType)
		b = protowire.AppendVarint(b, uint64(a.Counter))
	}
	return b, ""
}

// migrationName builds the name field the way Google Authenticator writes it:
// "issuer:account", or the account alone when there is no issuer. A reader
// splits on the first ':' and then lets the explicit issuer field win, so an
// account name that itself contains a ':' and has no issuer is re-read with the
// text before the colon as its issuer.
func migrationName(a account.Account) string {
	if a.Issuer != "" {
		return a.Issuer + ":" + a.Account
	}
	return a.Account
}

// partitionBatches greedily fills batches in vault order under both caps. An
// account whose own encoding exceeds the byte budget still gets a batch of its
// own rather than being dropped.
func partitionBatches(encoded [][]byte) [][][]byte {
	var (
		batches [][][]byte
		current [][]byte
		size    int
	)
	for _, p := range encoded {
		n := protowire.SizeTag(fieldOtpParameters) + protowire.SizeBytes(len(p))
		if len(current) > 0 && (len(current) >= maxAccountsPerBatch || size+n > maxPayloadBytes) {
			batches = append(batches, current)
			current, size = nil, 0
		}
		current = append(current, p)
		size += n
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}

// assemblePayload writes one MigrationPayload: every otp_parameters submessage
// first, then version, batch_size, batch_index and batch_id.
func assemblePayload(batch [][]byte, batchSize, batchIndex int, batchID int32) []byte {
	b := make([]byte, 0, maxPayloadBytes+batchTrailerBytes)
	for _, p := range batch {
		b = protowire.AppendTag(b, fieldOtpParameters, protowire.BytesType)
		b = protowire.AppendBytes(b, p)
	}
	b = protowire.AppendTag(b, fieldVersion, protowire.VarintType)
	b = protowire.AppendVarint(b, 1)
	b = protowire.AppendTag(b, fieldBatchSize, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(batchSize))
	b = protowire.AppendTag(b, fieldBatchIndex, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(batchIndex))
	b = protowire.AppendTag(b, fieldBatchID, protowire.VarintType)
	// batch_id is an int32 in Google's schema and is routinely negative; a
	// negative int32 is written sign-extended to 64 bits.
	b = protowire.AppendVarint(b, uint64(int64(batchID)))
	return b
}

// randomBatchID draws the int32 every batch of one export shares.
func randomBatchID() (int32, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0, fmt.Errorf("google-migration: generate batch id: %w", err)
	}
	return int32(binary.BigEndian.Uint32(buf[:])), nil
}
