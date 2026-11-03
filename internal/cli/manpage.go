package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/redact"
)

// manpageIntro opens the help topics section; it is roff source.
const manpageIntro = `The same text is printed by \fBqsysmin help \fItopic\fR.`

// WriteManpage builds the command tree and writes the manpage qsysmin.1 into dir. The page is a single file
// that covers every command, so the release archive carries one page. date is the date shown in its header.
func WriteManpage(dir string, date time.Time) error {
	opts := &Options{Redactor: &redact.Redactor{}}
	if _, err := newRootCommand(opts); err != nil {
		return err
	}
	return opts.prog.Manpage(dir, manpageIntro, func(root *cobra.Command, dir string) error {
		page := renderManpage(root, date)
		return os.WriteFile(filepath.Join(dir, root.Name()+".1"), []byte(page), 0o644)
	})
}

func renderManpage(root *cobra.Command, date time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, ".TH \"QSYSMIN\" \"1\" \"%s\" \"qsysmin %s\" \"User Commands\"\n",
		date.UTC().Format("January 2006"), roffText(root.Version))
	b.WriteString(".SH NAME\n" + roffText(root.Name()+" - "+root.Short) + "\n")
	b.WriteString(".SH SYNOPSIS\n.B " + roffText(root.Name()) + "\n[\\fIcommand\\fR] [\\fIflags\\fR]\n")
	b.WriteString(".SH DESCRIPTION\n")
	writeParagraphs(&b, root.Long)
	if usage := root.PersistentFlags().FlagUsages(); usage != "" {
		b.WriteString(".SH OPTIONS\n")
		writeLiteral(&b, usage)
	}
	b.WriteString(".SH COMMANDS\n")
	writeCommands(&b, root)
	return b.String()
}

func writeCommands(b *strings.Builder, parent *cobra.Command) {
	for _, c := range parent.Commands() {
		if !c.IsAvailableCommand() || c.Name() == "help" {
			continue
		}
		b.WriteString(".SS " + quoteArg(c.CommandPath()) + "\n")
		b.WriteString(".B " + roffText(c.UseLine()) + "\n")
		text := c.Long
		if text == "" {
			text = c.Short
		}
		writeParagraphs(b, text)
		if usage := c.LocalFlags().FlagUsages(); usage != "" {
			b.WriteString(".PP\nFlags:\n")
			writeLiteral(b, usage)
		}
		writeCommands(b, c)
	}
}

// writeParagraphs writes text as paragraphs split at blank lines. A paragraph with an indented line is a
// list or an example and keeps its line breaks.
func writeParagraphs(b *strings.Builder, text string) {
	for _, para := range strings.Split(strings.TrimSpace(text), "\n\n") {
		lines := strings.Split(para, "\n")
		literal := false
		for _, l := range lines {
			if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
				literal = true
			}
		}
		b.WriteString(".PP\n")
		if literal {
			writeLiteral(b, para)
			continue
		}
		for _, l := range lines {
			b.WriteString(roffText(l) + "\n")
		}
	}
}

func writeLiteral(b *strings.Builder, text string) {
	b.WriteString(".nf\n")
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		b.WriteString(roffText(l) + "\n")
	}
	b.WriteString(".fi\n")
}

// roffText makes one line of plain text safe as roff source: a backslash, a hyphen and a leading request
// character mean nothing but themselves.
func roffText(line string) string {
	line = strings.ReplaceAll(line, `\`, `\e`)
	line = strings.ReplaceAll(line, "-", `\-`)
	if strings.HasPrefix(line, ".") || strings.HasPrefix(line, "'") {
		line = `\&` + line
	}
	return line
}

// quoteArg is roffText as one quoted macro argument.
func quoteArg(s string) string {
	return `"` + strings.ReplaceAll(roffText(s), `"`, `\(dq`) + `"`
}
