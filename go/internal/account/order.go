package account

import (
	"sort"
	"strings"
	"unicode"
)

func SortForDisplay(accounts []Account) {
	sort.SliceStable(accounts, func(i, j int) bool {
		return compareDisplay(accounts[i], accounts[j]) < 0
	})
}

func compareDisplay(a, b Account) int {
	aOrdered, bOrdered := a.Order > 0, b.Order > 0
	switch {
	case aOrdered && !bOrdered:
		return -1
	case !aOrdered && bOrdered:
		return 1
	case aOrdered && bOrdered:
		if a.Order != b.Order {
			if a.Order < b.Order {
				return -1
			}
			return 1
		}
	default:
		if c := naturalCompare(a.Issuer, b.Issuer); c != 0 {
			return c
		}
		if c := naturalCompare(a.Account, b.Account); c != 0 {
			return c
		}
	}
	return strings.Compare(a.ID, b.ID)
}

func isASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }

func naturalCompare(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		if isASCIIDigit(ra[i]) && isASCIIDigit(rb[j]) {
			si := i
			for i < len(ra) && isASCIIDigit(ra[i]) {
				i++
			}
			sj := j
			for j < len(rb) && isASCIIDigit(rb[j]) {
				j++
			}
			if c := compareDigitRuns(string(ra[si:i]), string(rb[sj:j])); c != 0 {
				return c
			}
			continue
		}
		ca, cb := unicode.ToLower(ra[i]), unicode.ToLower(rb[j])
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	switch {
	case i < len(ra):
		return 1
	case j < len(rb):
		return -1
	}
	return 0
}

func compareDigitRuns(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}
