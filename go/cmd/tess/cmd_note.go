package main

import (
	"errors"
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/spf13/cobra"
)

// errEmptyNote reports note text that is empty once control characters and
// surrounding whitespace are removed.
var errEmptyNote = errors.New("the note is empty; --clear removes a note")

func newNoteCmd() *cobra.Command {
	var clearFlag bool
	cmd := &cobra.Command{
		Use:   "note <query> [text]",
		Short: "Set or clear an account's note",
		Long: `Set the note on an account. tess show prints it; tess list does not.

  tess note github "recovery codes in the safe"
  pbpaste | tess note github -     # read the note from stdin
  tess note --clear github`,
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: completeFirstArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			if clearFlag {
				if len(args) > 1 {
					return fmt.Errorf("--clear takes no note text")
				}
				return clearNote(cmd, args[0])
			}
			if len(args) < 2 {
				return fmt.Errorf("pass the note text, - to read it from stdin, or --clear")
			}
			text := args[1]
			if text == "-" {
				data, err := readCapped(stdinReader, "stdin")
				if err != nil {
					return err
				}
				text = string(data)
			}
			note := account.CleanNote(text)
			if note == "" {
				return errEmptyNote
			}
			return setNote(cmd, args[0], note)
		},
	}
	cmd.Flags().BoolVar(&clearFlag, "clear", false, "remove the note")
	return cmd
}

// setNote stores note on the account query names. An unchanged note leaves the
// vault file alone.
func setNote(cmd *cobra.Command, query, note string) error {
	s, err := openSession()
	if err != nil {
		return err
	}
	defer s.close()
	idx, err := s.single(cmd, query)
	if err != nil {
		return err
	}
	a := &s.accounts[idx]
	if a.Note == note {
		out(cmd, "%s already has that note", label(*a))
		return nil
	}
	a.Note = note
	a.UpdatedAt = now().Unix()
	if err := s.save(); err != nil {
		return err
	}
	out(cmd, "Saved the note on %s", label(*a))
	return nil
}

// clearNote removes the note from the account query names.
func clearNote(cmd *cobra.Command, query string) error {
	s, err := openSession()
	if err != nil {
		return err
	}
	defer s.close()
	idx, err := s.single(cmd, query)
	if err != nil {
		return err
	}
	a := &s.accounts[idx]
	if a.Note == "" {
		out(cmd, "%s has no note", label(*a))
		return nil
	}
	a.Note = ""
	a.UpdatedAt = now().Unix()
	if err := s.save(); err != nil {
		return err
	}
	out(cmd, "Cleared the note on %s", label(*a))
	return nil
}
