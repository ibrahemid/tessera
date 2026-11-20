// Package merge folds the accounts of one vault into another under the vault
// merge rule in /spec/vault-format.md. It is pure: no session, no I/O, so one
// rule governs both directions of a two-way merge.
package merge

import (
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
)

// Counts reports what folding one account list into another did: accounts
// added, accounts replaced by a newer copy, and accounts left as they were
// (unchanged, a duplicate by content, or failing validation).
type Counts struct {
	Added   int
	Updated int
	Skipped int
}

// Changed reports whether the destination list differs from what it was.
func (c Counts) Changed() bool { return c.Added > 0 || c.Updated > 0 }

// Into folds src into dst and returns the new slice plus counts. Identity is
// the account id; a live account matching a live one by content (ContentKey)
// under a different id counts as already present, while trashed accounts take
// no part in that check. On an id collision the copy with the STRICTLY newer
// UpdatedAt wins and carries its DeletedAt, so an equal timestamp writes
// nothing on either side and repeated merges cannot ping-pong. A new account
// that fails validation is skipped. Handles that would collide after the merge
// are cleared so the caller's handle assignment gives them unique ones.
//
// The result reuses dst's backing array: an updated account is replaced in
// place.
func Into(dst, src []account.Account) ([]account.Account, Counts) {
	out := dst
	var c Counts
	byID := map[string]int{}
	live := map[string]int{}
	for i, a := range out {
		byID[a.ID] = i
		if !a.Trashed() {
			live[ContentKey(a)]++
		}
	}
	for _, a := range src {
		if idx, ok := byID[a.ID]; ok {
			if a.UpdatedAt > out[idx].UpdatedAt {
				if !out[idx].Trashed() {
					live[ContentKey(out[idx])]--
				}
				if !a.Trashed() {
					live[ContentKey(a)]++
				}
				// The handle is how the user refers to this account in this
				// vault; the other side is a different vault's namespace, so
				// the local one is kept rather than silently renamed.
				local := out[idx].Handle
				out[idx] = a
				if local != "" {
					out[idx].Handle = local
				}
				c.Updated++
			} else {
				c.Skipped++
			}
			continue
		}
		if !a.Trashed() && live[ContentKey(a)] > 0 {
			c.Skipped++
			continue
		}
		if err := a.Validate(); err != nil {
			c.Skipped++
			continue
		}
		byID[a.ID] = len(out)
		if !a.Trashed() {
			live[ContentKey(a)]++
		}
		out = append(out, a)
		c.Added++
	}
	DedupeHandles(out)
	return out, c
}

// TwoWay folds each list into the other. Both directions start from the lists
// as they were before the run: the source receives the local accounts as they
// were, not the local result. local is copied first, so the local result may
// reuse local's backing array without changing what the source receives.
func TwoWay(local, source []account.Account) (localOut, sourceOut []account.Account, here, there Counts) {
	before := make([]account.Account, len(local))
	copy(before, local)
	localOut, here = Into(local, source)
	sourceOut, there = Into(source, before)
	return localOut, sourceOut, here, there
}

// DedupeHandles clears any handle that repeats one already seen earlier in the
// slice, keeping the first occurrence. Cleared handles are regenerated
// uniquely by account.AssignHandles.
func DedupeHandles(accts []account.Account) {
	seen := map[string]bool{}
	for i := range accts {
		h := accts[i].Handle
		if h == "" {
			continue
		}
		if seen[h] {
			accts[i].Handle = ""
			continue
		}
		seen[h] = true
	}
}

// ContentKey identifies an account by content rather than id: type, issuer
// and account name compared case-insensitively, and the secret.
func ContentKey(a account.Account) string {
	return strings.Join([]string{string(a.Type), strings.ToLower(a.Issuer), strings.ToLower(a.Account), base32x.EncodeNoPad(a.Secret)}, "|")
}
