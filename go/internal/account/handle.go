package account

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// handlePattern is the handle charset: lowercase, leading letter, 1-12 chars.
var handlePattern = regexp.MustCompile(`^[a-z][a-z0-9]{0,11}$`)

// ValidHandle reports whether h satisfies the handle charset.
func ValidHandle(h string) bool { return handlePattern.MatchString(h) }

// normalizeHandleSource lowercases s, keeps only [a-z0-9 ], and collapses
// whitespace runs to single spaces with the ends trimmed.
func normalizeHandleSource(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// handleBase derives the bare handle base for an account, per the spec:
// a normalized issuer (or the local part of account, or the literal "acct")
// yields two characters, x-prefixed when it would otherwise lead with a digit.
func handleBase(issuer, acct string) string {
	src := normalizeHandleSource(issuer)
	if src == "" {
		local := acct
		if i := strings.IndexByte(acct, '@'); i >= 0 {
			local = acct[:i]
		}
		src = normalizeHandleSource(local)
	}
	if src == "" {
		return "acct" // literal fallback, used verbatim as the base
	}
	words := strings.Split(src, " ")
	var base string
	if len(words) == 1 {
		w := words[0]
		if len(w) >= 2 {
			base = w[:2]
		} else {
			base = w
		}
	} else {
		base = string(words[0][0]) + string(words[1][0])
	}
	if base[0] >= '0' && base[0] <= '9' {
		base = "x" + base
	}
	return base
}

// AssignHandles assigns a deterministic handle to every account that lacks one,
// mutating accounts in place, and reports whether any handle was assigned.
// Accounts that already have a handle (original or user-edited) are never
// changed or renumbered. Assignment order is ascending created_at, then
// ascending id, so the result is independent of storage order. The smallest
// free integer N>=2 disambiguates a base already taken by any existing or
// just-assigned handle.
func AssignHandles(accounts []Account) bool {
	taken := make(map[string]bool)
	var missing []int
	for i, a := range accounts {
		if a.Handle != "" {
			taken[a.Handle] = true
		} else {
			missing = append(missing, i)
		}
	}
	if len(missing) == 0 {
		return false
	}
	sort.SliceStable(missing, func(x, y int) bool {
		ax, ay := accounts[missing[x]], accounts[missing[y]]
		if ax.CreatedAt != ay.CreatedAt {
			return ax.CreatedAt < ay.CreatedAt
		}
		return ax.ID < ay.ID
	})
	for _, i := range missing {
		base := handleBase(accounts[i].Issuer, accounts[i].Account)
		h := base
		for n := 2; taken[h]; n++ {
			h = base + strconv.Itoa(n)
		}
		accounts[i].Handle = h
		taken[h] = true
	}
	return true
}

// CheckHandleUniqueness errors if any account carries an invalid handle or two
// accounts share one. It is a vault-level invariant enforced before persisting.
func CheckHandleUniqueness(accounts []Account) error {
	seen := make(map[string]string)
	for _, a := range accounts {
		if a.Handle == "" {
			continue
		}
		if !ValidHandle(a.Handle) {
			return fmt.Errorf("account %q: invalid handle %q", a.ID, a.Handle)
		}
		if other, ok := seen[a.Handle]; ok {
			return fmt.Errorf("duplicate handle %q on accounts %q and %q", a.Handle, other, a.ID)
		}
		seen[a.Handle] = a.ID
	}
	return nil
}
