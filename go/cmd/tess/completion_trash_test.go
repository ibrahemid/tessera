package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
)

// completeVia runs the shell completion for args and returns the offered
// handles.
func completeVia(t *testing.T, args ...string) []string {
	t.Helper()
	stdout, _, err := runRootStreams(t, append([]string{"__complete"}, args...)...)
	if err != nil {
		t.Fatalf("__complete %v: %v", args, err)
	}
	var got []string
	for _, line := range strings.Split(stdout, "\n") {
		if line == "" || strings.HasPrefix(line, ":") {
			break
		}
		handle, _, _ := strings.Cut(line, "\t")
		got = append(got, handle)
	}
	slices.Sort(got)
	return got
}

func TestCompletionOffersTheRightAccounts(t *testing.T) {
	stubCompletionAccounts(t, func() ([]account.Account, error) {
		return []account.Account{
			handled("a", "GitHub", "me", "gi"),
			handled("b", "Slack", "me", "sl"),
			trashedAt(handled("c", "Dropbox", "me", "dr"), dropboxTrashedAt),
		}, nil
	})
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"dr"}, nil},
		{[]string{"s"}, []string{"show", "sl"}},
		{[]string{"code", ""}, []string{"gi", "sl"}},
		{[]string{"show", ""}, []string{"gi", "sl"}},
		{[]string{"edit", ""}, []string{"gi", "sl"}},
		{[]string{"note", ""}, []string{"gi", "sl"}},
		{[]string{"rm", ""}, []string{"gi", "sl"}},
		{[]string{"rm", "gi", ""}, []string{"sl"}},
		{[]string{"rm", "--purge", ""}, []string{"dr", "gi", "sl"}},
		{[]string{"rm", "--purge", "dr", ""}, []string{"gi", "sl"}},
		{[]string{"restore", ""}, []string{"dr"}},
		{[]string{"restore", "dr", ""}, nil},
		{[]string{"pin", "gi", ""}, []string{"sl"}},
		{[]string{"tag", "SL", ""}, []string{"gi"}},
		{[]string{"order", ""}, []string{"gi", "sl"}},
		{[]string{"order", "gi", ""}, nil},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			if got := completeVia(t, c.args...); !slices.Equal(got, c.want) {
				t.Errorf("offered %v, want %v", got, c.want)
			}
		})
	}
}
