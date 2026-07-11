//go:build !darwin

package keychain

// Supported reports whether keychain integration is available (never off darwin).
func Supported() bool { return false }

// Store is unsupported off darwin.
func Store(account, passphrase string) error { return ErrUnsupported }

// Lookup reports no entry off darwin.
func Lookup(account string) (string, bool, error) { return "", false, nil }

// Has reports no entry off darwin.
func Has(account string) (bool, error) { return false, nil }

// Delete is a no-op off darwin.
func Delete(account string) error { return nil }
