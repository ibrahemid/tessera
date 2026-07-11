//go:build darwin

package keychain

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Supported reports whether keychain integration is available (always on darwin).
func Supported() bool { return true }

// Store saves passphrase for account (the absolute vault path) under the
// "tessera" service, updating any existing entry. The passphrase is fed to
// `security` over stdin batch mode so it never appears in argv (which is
// transiently visible via ps). The write is verified by reading it back, because
// `security -i` does not propagate a failed add's exit code.
func Store(account, passphrase string) error {
	if strings.ContainsAny(passphrase, "\r\n") {
		return errors.New("keychain: passphrase must not contain newlines")
	}
	// security -i tokenizes each line with shell-like quoting: inside double
	// quotes, backslash escapes " and \, and there is no variable expansion.
	line := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n",
		quote(service), quote(account), quote(passphrase))
	cmd := exec.Command("/usr/bin/security", "-i")
	cmd.Stdin = strings.NewReader(line)
	if _, err := cmd.CombinedOutput(); err != nil {
		return errors.New("keychain: failed to write login keychain entry")
	}
	got, ok, err := Lookup(account)
	if err != nil {
		return err
	}
	if !ok || got != passphrase {
		return errors.New("keychain: login keychain entry did not persist")
	}
	return nil
}

// Lookup returns the stored passphrase for account. The boolean is false (with a
// nil error) when no entry exists.
func Lookup(account string) (string, bool, error) {
	cmd := exec.Command("/usr/bin/security", "find-generic-password",
		"-s", service, "-a", account, "-w")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err == nil {
		return strings.TrimSuffix(out.String(), "\n"), true, nil
	}
	if isNotFound(err) {
		return "", false, nil
	}
	return "", false, errors.New("keychain: failed to read login keychain entry")
}

// Has reports whether an entry exists for account without retrieving the secret
// (so it never triggers an access-control prompt).
func Has(account string) (bool, error) {
	cmd := exec.Command("/usr/bin/security", "find-generic-password",
		"-s", service, "-a", account)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}
	return false, errors.New("keychain: failed to query login keychain")
}

// Delete removes the entry for account. A missing entry is not an error.
func Delete(account string) error {
	cmd := exec.Command("/usr/bin/security", "delete-generic-password",
		"-s", service, "-a", account)
	err := cmd.Run()
	if err == nil || isNotFound(err) {
		return nil
	}
	return errors.New("keychain: failed to delete login keychain entry")
}

// isNotFound reports whether err is security's errSecItemNotFound (exit 44).
func isNotFound(err error) bool {
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ExitCode() == 44
}

// quote wraps s for security's interactive tokenizer: escape backslash and
// double quote, then surround with double quotes.
func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}
