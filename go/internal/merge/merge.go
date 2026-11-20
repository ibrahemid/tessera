// Package merge folds the accounts of one vault into another under the vault
// merge rule in /spec/vault-format.md. It is pure: no session, no I/O, so one
// rule governs both directions of a two-way merge.
package merge

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
)

// Counts reports what folding one account list into another did. Every source
// account lands in exactly one count:
//
//   - Added: a new id, appended.
//   - Updated: a strictly newer copy replaced the account, other than a move
//     to the Trash.
//   - Trashed: a strictly newer copy replaced a live account and is in the
//     Trash.
//   - Unchanged: same id, and the copy is not strictly newer.
//   - Duplicate: a new id whose live content matches a live account.
//   - Invalid: a copy that fails validation, not applied.
type Counts struct {
	Added     int
	Updated   int
	Trashed   int
	Unchanged int
	Duplicate int
	Invalid   int
}

// Changed reports whether the destination list differs from what it was, so
// the side must be written.
func (c Counts) Changed() bool { return c.Added > 0 || c.Updated > 0 || c.Trashed > 0 }

// Into folds src into dst and returns a new slice plus counts. Identity is the
// account id; a live account matching a live one by content (ContentKey) under
// a different id counts as already present, while trashed accounts take no part
// in that check. On an id collision the copy with the STRICTLY newer UpdatedAt
// wins and carries its DeletedAt, so an equal timestamp writes nothing on either
// side and repeated merges cannot ping-pong. A copy that would replace or add an
// account is validated first and left out when it fails. Handles that would
// collide after the merge are cleared so the caller's handle assignment gives
// them unique ones.
//
// Neither input is modified, and the result shares no memory with either.
func Into(dst, src []account.Account) ([]account.Account, Counts) {
	out := make([]account.Account, len(dst), len(dst)+len(src))
	var c Counts
	byID := make(map[string]int, len(dst)+len(src))
	live := map[string]int{}
	for i, a := range dst {
		out[i] = clone(a)
		byID[a.ID] = i
		if !a.Trashed() {
			live[ContentKey(a)]++
		}
	}
	for _, a := range src {
		if idx, ok := byID[a.ID]; ok {
			cur := out[idx]
			if a.UpdatedAt <= cur.UpdatedAt {
				c.Unchanged++
				continue
			}
			next := clone(a)
			// A handle is per vault, so the local one wins over the other vault's.
			if cur.Handle != "" {
				next.Handle = cur.Handle
			}
			if next.Validate() != nil {
				c.Invalid++
				continue
			}
			if !cur.Trashed() {
				live[ContentKey(cur)]--
			}
			if !next.Trashed() {
				live[ContentKey(next)]++
			}
			if !cur.Trashed() && next.Trashed() {
				c.Trashed++
			} else {
				c.Updated++
			}
			out[idx] = next
			continue
		}
		if !a.Trashed() && live[ContentKey(a)] > 0 {
			c.Duplicate++
			continue
		}
		if a.Validate() != nil {
			c.Invalid++
			continue
		}
		byID[a.ID] = len(out)
		if !a.Trashed() {
			live[ContentKey(a)]++
		}
		out = append(out, clone(a))
		c.Added++
	}
	DedupeHandles(out)
	return out, c
}

// TwoWay folds each list into the other. Both directions start from the lists
// as they were before the run: the source receives the local accounts as they
// were, not the local result.
func TwoWay(local, source []account.Account) (localOut, sourceOut []account.Account, here, there Counts) {
	localOut, here = Into(local, source)
	sourceOut, there = Into(source, local)
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
// and account name compared case-insensitively, and the secret. Import,
// merge and dedupe all treat two accounts with the same key as the same
// account.
func ContentKey(a account.Account) string {
	return strings.Join([]string{string(a.Type), strings.ToLower(a.Issuer), strings.ToLower(a.Account), base32x.EncodeNoPad(a.Secret)}, "|")
}

// clone copies an account so the copy shares no slice or map with it.
func clone(a account.Account) account.Account {
	a.Secret = bytes.Clone(a.Secret)
	a.Tags = slices.Clone(a.Tags)
	if a.Extra != nil {
		extra := make(map[string]json.RawMessage, len(a.Extra))
		for k, v := range a.Extra {
			extra[k] = bytes.Clone(v)
		}
		a.Extra = extra
	}
	return a
}
