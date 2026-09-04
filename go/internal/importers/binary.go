package importers

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"

	"github.com/ibrahemid/tessera/go/internal/account"
)

// max1PUXDataBytes caps how much of export.data is read out of an archive, so a
// zip bomb cannot exhaust memory during an import.
const max1PUXDataBytes = 64 << 20

// IsZip reports whether data begins with the zip local-file-header magic.
func IsZip(data []byte) bool {
	return bytes.HasPrefix(data, []byte("PK\x03\x04"))
}

// IsStratumEncrypted reports the 16-byte ASCII magic of an encrypted Stratum /
// Authenticator Pro backup (all-caps is the current format, mixed case the
// legacy one). This check MUST run before the base32 setup-key rule: both
// spellings are 16 letters that decode cleanly as base32.
func IsStratumEncrypted(data []byte) bool {
	return bytes.HasPrefix(data, []byte("AUTHENTICATORPRO")) ||
		bytes.HasPrefix(data, []byte("AuthenticatorPro"))
}

// parse1PUX reads export.data out of a 1Password 1PUX archive. The zip
// container is a CLI-only transport; the shared interop contract is the inner
// document, which both cores parse.
func parse1PUX(data []byte) ([]account.Account, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("1password: open archive: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "export.data" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("1password: open export.data: %w", err)
		}
		inner, err := io.ReadAll(io.LimitReader(rc, max1PUXDataBytes))
		closeErr := rc.Close()
		if err != nil {
			return nil, fmt.Errorf("1password: read export.data: %w", err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("1password: read export.data: %w", closeErr)
		}
		return parse1PUXData(inner)
	}
	return nil, fmt.Errorf("archive holds no export.data; a 1Password .1pux export does")
}
