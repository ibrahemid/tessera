//go:build gendocs

package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func init() {
	extraCommands = append(extraCommands, newGendocsCmd)
}

// sourceDateError reports a SOURCE_DATE_EPOCH that is unset or not a Unix time.
type sourceDateError struct {
	value string
}

func (e *sourceDateError) Error() string {
	if e.value == "" {
		return "SOURCE_DATE_EPOCH is not set; set it to the commit time in Unix seconds (git log -1 --format=%ct)"
	}
	return fmt.Sprintf("SOURCE_DATE_EPOCH %q is not a Unix time in seconds; set it to the commit time (git log -1 --format=%%ct)", e.value)
}

func newGendocsCmd() *cobra.Command {
	var manDir, completionDir string
	cmd := &cobra.Command{
		Use:    "__gendocs --man <dir> --completions <dir>",
		Short:  "Write man pages and shell completion scripts",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			date, err := sourceDate()
			if err != nil {
				return err
			}
			root := cmd.Root()
			if err := writeManPages(root, manDir, date); err != nil {
				return err
			}
			return writeCompletions(root, completionDir)
		},
	}
	cmd.Flags().StringVar(&manDir, "man", "", "directory for the gzipped man pages")
	cmd.Flags().StringVar(&completionDir, "completions", "", "directory for the shell completion scripts")
	_ = cmd.MarkFlagRequired("man")
	_ = cmd.MarkFlagRequired("completions")
	return cmd
}

func sourceDate() (time.Time, error) {
	raw := os.Getenv("SOURCE_DATE_EPOCH")
	if raw == "" {
		return time.Time{}, &sourceDateError{}
	}
	secs, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || secs < 0 {
		return time.Time{}, &sourceDateError{value: raw}
	}
	return time.Unix(secs, 0).UTC(), nil
}

// writeManPages writes one gzipped page per visible command, named after its
// command path (tess.1.gz, tess-vault-init.1.gz).
func writeManPages(root *cobra.Command, dir string, date time.Time) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	root.InitDefaultVersionFlag()
	header := doc.GenManHeader{Section: "1", Source: "tess " + root.Version, Date: &date}
	return writeManTree(root, dir, header)
}

func writeManTree(c *cobra.Command, dir string, header doc.GenManHeader) error {
	c.DisableAutoGenTag = true
	for _, child := range c.Commands() {
		if !child.IsAvailableCommand() || child.IsAdditionalHelpTopicCommand() {
			continue
		}
		if err := writeManTree(child, dir, header); err != nil {
			return err
		}
	}
	name := strings.ReplaceAll(c.CommandPath(), " ", "-") + "." + header.Section + ".gz"
	return writeFile(filepath.Join(dir, name), func(w io.Writer) error {
		zw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
		if err != nil {
			return err
		}
		use, short, long := c.Use, c.Short, c.Long
		defer func() { c.Use, c.Short, c.Long = use, short, long }()
		c.Use, c.Short, c.Long = markdownEscaper.Replace(use), markdownEscaper.Replace(short), manDescription(long)
		h := header
		if err := doc.GenMan(c, &h, zw); err != nil {
			return err
		}
		return zw.Close()
	})
}

// markdownEscaper escapes what md2man reads as emphasis, a code span or an
// HTML tag, so `<query>` and `*` reach the man page as written.
var markdownEscaper = strings.NewReplacer(`\`, `\\`, "`", "\\`", `*`, `\*`, `_`, `\_`, `<`, `\<`, `>`, `\>`)

// manDescription turns help text into markdown for md2man. Indented lines,
// the examples and tables of --help, become preformatted blocks so they keep
// their line breaks and alignment; other lines are escaped prose.
func manDescription(long string) string {
	var b strings.Builder
	inBlock := false
	for line := range strings.Lines(long) {
		line = strings.TrimSuffix(line, "\n")
		indented := strings.TrimSpace(line) != "" && (line[0] == ' ' || line[0] == '\t')
		switch {
		case indented && !inBlock:
			b.WriteString("\n```\n")
			inBlock = true
		case !indented && inBlock:
			b.WriteString("```\n\n")
			inBlock = false
		}
		if !inBlock {
			line = markdownEscaper.Replace(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if inBlock {
		b.WriteString("```\n")
	}
	return b.String()
}

func writeCompletions(root *cobra.Command, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := root.Name()
	scripts := []struct {
		file  string
		write func(io.Writer) error
	}{
		{name + ".bash", func(w io.Writer) error { return root.GenBashCompletionV2(w, true) }},
		{"_" + name, root.GenZshCompletion},
		{name + ".fish", func(w io.Writer) error { return root.GenFishCompletion(w, true) }},
		{name + ".ps1", root.GenPowerShellCompletionWithDesc},
	}
	for _, s := range scripts {
		if err := writeFile(filepath.Join(dir, s.file), s.write); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(path string, write func(io.Writer) error) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return write(f)
}
