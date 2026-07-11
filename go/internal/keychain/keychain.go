// Package keychain is a thin macOS login-keychain integration for storing the
// vault passphrase so the CLI stops prompting on every invocation. It is an OS
// integration only: it shells out to /usr/bin/security and holds no crypto. The
// stored item lives in the login keychain (service "tessera", account = the
// resolved absolute vault path, so multiple vaults coexist) and is protected at
// the same trust level as ssh keys on disk. Non-darwin builds are stubs.
package keychain

import "errors"

// service is the generic-password service name for all Tessera entries. It is a
// var (not const) so tests can point at a throwaway service.
var service = "tessera"

// ErrUnsupported is returned by every operation on non-darwin platforms.
var ErrUnsupported = errors.New("keychain: login keychain integration is macOS-only")
