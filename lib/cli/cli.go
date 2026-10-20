// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package cli is the command framework of ph: every command is defined once (group, name, operands, summary,
// description, options, examples), and the overview, `ph help <command>`, the Markdown manual pages (docs/ph/) and the
// option parsing all come from that one definition, so the documentation cannot drift from the code.
//
// The layout follows zfs(8) / zpool(8): a command is a verb on one kind of object (`ph image rom`), options are the
// same letters everywhere (-H script mode, -o fields, -n dry run, -v verbose, -b board, -a architecture), and every
// command has a manual page with NAME, SYNOPSIS, DESCRIPTION, OPTIONS and EXAMPLES.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Width is the column the help text is wrapped at.
const Width = 100

// Command is one ph command.
type Command struct {
	Group       string              // the object it acts on: "source", "component", "image", "board", "device", "" (top level)
	Name        string              // the verb: "build"
	Operands    string              // what follows the options in the synopsis: "<component>..."
	Summary     string              // one line, a sentence without the final full stop
	Description string              // paragraphs (blank line between them); wrapped on output
	Flags       func(*flag.FlagSet) // its options; the usage string of each is its documentation
	Examples    []Example
	// Aliases are older spellings that still work for one release, hidden from the overview: "rom", or
	// "<name>=<arguments>" for one that stands for options too ("all=-A" = ph component build -A)
	Aliases []string
	Run     func(fs *flag.FlagSet, args []string) error
}

// Example is one entry of a command's EXAMPLES section.
type Example struct {
	What string // a sentence: what it does
	Cmd  string // the command line, without the leading "ph "
}

// Path is the command as typed: "image rom", or "help" for a top-level command.
func (c *Command) Path() string {
	if c.Group == "" {
		return c.Name
	}
	return c.Group + " " + c.Name
}

// FlagSet builds the command's options.
func (c *Command) FlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("ph "+c.Path(), flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if c.Flags != nil {
		c.Flags(fs)
	}
	return fs
}

// Topic is a help page that is not a command ("environment").
type Topic struct {
	Name, Summary, Text string
}

// Group is a section of the overview.
type Group struct {
	Name, Title, Summary string
}

// App is the whole command line.
type App struct {
	Name     string
	Summary  string
	Groups   []Group
	Commands []*Command
	Topics   []Topic
	Out, Err io.Writer
}

// ErrUsage marks an error whose message is a usage problem: the command's synopsis is printed with it.
var ErrUsage = errors.New("usage")

// Usagef returns a usage error.
func Usagef(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrUsage}, a...)...)
}

// Find resolves argv to a command and the remaining arguments, following aliases.
func (a *App) Find(argv []string) (*Command, []string, string) {
	if len(argv) == 0 {
		return nil, nil, ""
	}
	if len(argv) >= 2 {
		for _, c := range a.Commands {
			if c.Group != "" && c.Group == argv[0] && c.Name == argv[1] {
				return c, argv[2:], ""
			}
		}
	}
	for _, c := range a.Commands {
		if c.Group == "" && c.Name == argv[0] {
			return c, argv[1:], ""
		}
	}
	for _, c := range a.Commands {
		for _, al := range c.Aliases {
			name, extra, _ := strings.Cut(al, "=")
			if name == argv[0] {
				return c, append(strings.Fields(extra), argv[1:]...), al
			}
		}
	}
	return nil, nil, ""
}

// Run executes argv. It returns the process exit status.
func (a *App) Run(argv []string) int {
	if len(argv) == 0 || argv[0] == "-h" || argv[0] == "--help" {
		a.Overview(a.Err)
		if len(argv) == 0 {
			return 2
		}
		return 0
	}
	c, args, alias := a.Find(argv)
	if c == nil {
		if a.isGroup(argv[0]) {
			fmt.Fprintf(a.Err, "ph: %s: which command?\n\n", argv[0])
			a.groupUsage(a.Err, argv[0])
			return 2
		}
		fmt.Fprintf(a.Err, "ph: unknown command %q\n\nRun \"ph help\" for the list of commands.\n", argv[0])
		return 2
	}
	if alias != "" {
		name, extra, _ := strings.Cut(alias, "=")
		fmt.Fprintf(a.Err, "ph: \"ph %s\" is now \"ph %s\" (the old name goes in the next release)\n\n",
			name, strings.TrimSpace(c.Path()+" "+extra))
	}
	fs := c.FlagSet()
	for _, x := range args {
		if x == "-h" || x == "--help" {
			a.Help(a.Out, c)
			return 0
		}
	}
	operands, err := parseInterspersed(fs, args)
	if err != nil {
		fmt.Fprintf(a.Err, "ph %s: %v\n\n", c.Path(), err)
		a.synopsisHint(a.Err, c)
		return 2
	}
	if err := c.Run(fs, operands); err != nil {
		fmt.Fprintf(a.Err, "ph %s: %v\n", c.Path(), strings.TrimPrefix(err.Error(), ErrUsage.Error()+": "))
		if errors.Is(err, ErrUsage) {
			fmt.Fprintln(a.Err)
			a.synopsisHint(a.Err, c)
			return 2
		}
		return 1
	}
	return 0
}

// parseInterspersed lets options come before or after the operands ("ph device push img.bin -w ready"); "--" ends
// the options.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var operands []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return operands, nil
		}
		if len(args) > 0 && len(rest) < len(args) && args[len(args)-len(rest)-1] == "--" {
			return append(operands, rest...), nil
		}
		operands = append(operands, rest[0])
		args = rest[1:]
	}
}

func (a *App) isGroup(name string) bool {
	for _, g := range a.Groups {
		if g.Name == name {
			return true
		}
	}
	return false
}

func (a *App) synopsisHint(w io.Writer, c *Command) {
	fmt.Fprintf(w, "usage:\n    %s\n\nRun \"ph help %s\" for the details.\n", Synopsis(c), c.Path())
}

// Synopsis is the command's one-line usage: "ph image rom [-b <board>] [-s <boot>] [-O <name>=<value>]...".
func Synopsis(c *Command) string {
	parts := []string{"ph", c.Path()}
	fs := c.FlagSet()
	var bools, vals []string
	fs.VisitAll(func(f *flag.Flag) {
		name, _ := flag.UnquoteUsage(f)
		if isBool(f) {
			bools = append(bools, f.Name)
		} else {
			v := "[-" + f.Name + " <" + name + ">]"
			if strings.HasSuffix(f.Usage, "(repeatable)") {
				v += "..."
			}
			vals = append(vals, v)
		}
	})
	// single-letter switches together, as zfs writes them: [-Hnv]
	var short, long []string
	for _, b := range bools {
		if len(b) == 1 {
			short = append(short, b)
		} else {
			long = append(long, "[-"+b+"]")
		}
	}
	sort.Strings(short)
	if len(short) > 0 {
		parts = append(parts, "[-"+strings.Join(short, "")+"]")
	}
	parts = append(parts, long...)
	parts = append(parts, vals...)
	if c.Operands != "" {
		parts = append(parts, c.Operands)
	}
	return strings.Join(parts, " ")
}

func isBool(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// Overview writes `ph help`: every command by group, with its summary.
func (a *App) Overview(w io.Writer) {
	fmt.Fprintf(w, "usage:\n    ph <command> [<options>] [<operands>]\n\n")
	fmt.Fprintln(w, wrap(a.Summary, "", Width))
	col := 14 // one column for every group (and the topics), so the summaries line up
	for _, c := range a.Commands {
		if n := len(c.Path()); n > col {
			col = n
		}
	}
	for _, g := range a.Groups {
		cmds := a.inGroup(g.Name)
		if len(cmds) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s\n", g.Title)
		for _, c := range cmds {
			fmt.Fprintf(w, "    %-*s   %s\n", col, c.Path(), c.Summary)
		}
	}
	if len(a.Topics) > 0 {
		fmt.Fprintf(w, "\nTopics\n")
		for _, t := range a.Topics {
			fmt.Fprintf(w, "    %-*s   %s\n", col, t.Name, t.Summary)
		}
	}
	fmt.Fprintf(w, "\nRun \"ph help <command>\" for a command's options and examples (\"ph help image rom\"),\n"+
		"or \"ph help <topic>\" for a topic.\n")
}

func (a *App) groupUsage(w io.Writer, group string) {
	for _, g := range a.Groups {
		if g.Name == group {
			fmt.Fprintf(w, "%s\n", g.Title)
		}
	}
	for _, c := range a.inGroup(group) {
		fmt.Fprintf(w, "    %s\n", Synopsis(c))
	}
	fmt.Fprintf(w, "\nRun \"ph help %s <command>\" for the details.\n", group)
}

func (a *App) inGroup(group string) []*Command {
	var out []*Command
	for _, c := range a.Commands {
		if c.Group == group {
			out = append(out, c)
		}
	}
	return out
}

// HelpArgs serves `ph help [<group>] [<command>|<topic>]`.
func (a *App) HelpArgs(args []string) error {
	if len(args) == 0 {
		a.Overview(a.Out)
		return nil
	}
	for _, t := range a.Topics {
		if t.Name == args[0] {
			fmt.Fprintf(a.Out, "%s\n\n%s\n", strings.ToUpper(t.Name), indentBlock(t.Text, "    "))
			return nil
		}
	}
	if c, _, _ := a.Find(args); c != nil {
		a.Help(a.Out, c)
		return nil
	}
	if a.isGroup(args[0]) {
		a.groupUsage(a.Out, args[0])
		return nil
	}
	return Usagef("no command or topic %q", strings.Join(args, " "))
}

// Help writes a command's manual page as text.
func (a *App) Help(w io.Writer, c *Command) {
	fmt.Fprintf(w, "NAME\n    ph %s - %s\n\n", c.Path(), c.Summary)
	fmt.Fprintf(w, "SYNOPSIS\n%s\n\n", wrap(Synopsis(c), "    ", Width))
	if c.Description != "" {
		fmt.Fprintf(w, "DESCRIPTION\n%s\n\n", paragraphs(c.Description, "    "))
	}
	if opts := options(c); len(opts) > 0 {
		fmt.Fprintf(w, "OPTIONS\n")
		for i, o := range opts {
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "    %s\n%s\n", o.head, paragraphs(o.text, "        "))
		}
		fmt.Fprintln(w)
	}
	if len(c.Examples) > 0 {
		fmt.Fprintf(w, "EXAMPLES\n")
		for i, e := range c.Examples {
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "%s\n\n        ph %s\n", wrap(e.What, "    ", Width), e.Cmd)
		}
		fmt.Fprintln(w)
	}
	if len(c.Aliases) > 0 {
		fmt.Fprintf(w, "FORMERLY\n")
		for _, al := range c.Aliases {
			fmt.Fprintf(w, "    ph %s\n", formerly(c, al))
		}
		fmt.Fprintf(w, "\n    (still accepted for one release)\n\n")
	}
}

type option struct{ head, text string }

func options(c *Command) []option {
	var out []option
	c.FlagSet().VisitAll(func(f *flag.Flag) {
		name, usage := flag.UnquoteUsage(f)
		head := "-" + f.Name
		if !isBool(f) {
			head += " <" + name + ">"
		}
		if f.DefValue != "" && !isBool(f) && f.DefValue != "0" && !strings.Contains(usage, "default") {
			usage += " (default: " + f.DefValue + ")"
		}
		out = append(out, option{head, sentence(strings.TrimSuffix(usage, " (repeatable)")) + repeatNote(f)})
	})
	return out
}

// formerly: "rom", or "all  (= ph component build -A)" for an alias that stands for options too.
func formerly(c *Command, alias string) string {
	name, extra, ok := strings.Cut(alias, "=")
	if !ok {
		return name
	}
	return name + "  (= ph " + c.Path() + " " + extra + ")"
}

// sentence: a capital first letter and a full stop.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

func repeatNote(f *flag.Flag) string {
	if strings.HasSuffix(f.Usage, "(repeatable)") {
		return " Can be given more than once."
	}
	return ""
}

// Markdown writes a command's manual page as Markdown (docs/ph/ph-<group>-<name>.md).
func Markdown(w io.Writer, c *Command) {
	fmt.Fprintf(w, "# ph %s\n\n%s.\n\n## Synopsis\n\n```\n%s\n```\n\n", c.Path(), c.Summary, Synopsis(c))
	if c.Description != "" {
		fmt.Fprintf(w, "## Description\n\n%s\n\n", strings.TrimSpace(c.Description))
	}
	if opts := options(c); len(opts) > 0 {
		fmt.Fprintf(w, "## Options\n\n")
		for _, o := range opts {
			fmt.Fprintf(w, "- `%s`: %s\n", o.head, strings.Join(strings.Fields(o.text), " "))
		}
		fmt.Fprintln(w)
	}
	if len(c.Examples) > 0 {
		fmt.Fprintf(w, "## Examples\n\n")
		for _, e := range c.Examples {
			fmt.Fprintf(w, "%s\n\n```sh\nph %s\n```\n\n", e.What, e.Cmd)
		}
	}
	if len(c.Aliases) > 0 {
		fmt.Fprintf(w, "## Formerly\n\nStill accepted for one release:\n\n")
		for _, al := range c.Aliases {
			fmt.Fprintf(w, "- `ph %s`\n", formerly(c, al))
		}
	}
}

// FileName is the command's manual page file: "ph-image-rom.md".
func FileName(c *Command) string {
	return "ph-" + strings.ReplaceAll(c.Path(), " ", "-") + ".md"
}

// Index writes docs/ph/README.md: the commands by group, linking each page.
func (a *App) Index(w io.Writer) {
	fmt.Fprintf(w, "# ph\n\n%s\n\nGenerated from the command definitions (`ph help -markdown <dir>`); do not edit.\n",
		strings.TrimSpace(a.Summary))
	for _, g := range a.Groups {
		cmds := a.inGroup(g.Name)
		if len(cmds) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n## %s\n\n%s\n\n| command | what it does |\n|---|---|\n", g.Title, g.Summary)
		for _, c := range cmds {
			fmt.Fprintf(w, "| [`ph %s`](%s) | %s |\n", c.Path(), FileName(c), c.Summary)
		}
	}
	for _, t := range a.Topics {
		fmt.Fprintf(w, "\n## %s\n\n%s\n", strings.ToUpper(t.Name[:1])+t.Name[1:], strings.TrimSpace(t.Text))
	}
}

// WriteMarkdown writes every manual page and the index into dir.
func (a *App) WriteMarkdown(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var b strings.Builder
	a.Index(&b)
	if err := os.WriteFile(dir+"/README.md", []byte(b.String()), 0o644); err != nil {
		return err
	}
	for _, c := range a.Commands {
		b.Reset()
		Markdown(&b, c)
		if err := os.WriteFile(dir+"/"+FileName(c), []byte(b.String()), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// paragraphs wraps each blank-line-separated paragraph at Width, indented. A paragraph whose lines start with
// spaces (a table, a list of settings) is kept as written, only indented.
func paragraphs(text, indent string) string {
	var out []string
	for _, p := range strings.Split(strings.TrimSpace(text), "\n\n") {
		if strings.HasPrefix(p, " ") || strings.Contains(p, "\n ") {
			out = append(out, indentBlock(p, indent))
		} else {
			out = append(out, wrap(p, indent, Width))
		}
	}
	return strings.Join(out, "\n\n")
}

func indentBlock(text, indent string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = indent + l
		}
	}
	return strings.Join(lines, "\n")
}

// wrap fills words into lines of at most width columns, each starting with indent.
func wrap(text, indent string, width int) string {
	var lines []string
	line := indent
	for _, word := range strings.Fields(text) {
		if len(line) > len(indent) && len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = indent
		}
		if len(line) > len(indent) {
			line += " "
		}
		line += word
	}
	return strings.Join(append(lines, line), "\n")
}
