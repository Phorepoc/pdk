// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/Phorepoc/pdk/lib/build"
	"github.com/Phorepoc/pdk/lib/cli"
)

func otherCommands(app *cli.App) []*cli.Command {
	return []*cli.Command{
		{
			Name:    "shell",
			Summary: "Open a shell in the toolchain container",
			Description: "An interactive shell in the toolchain image, with the same mounts and settings as every " +
				"build command: the pdk at /pdk, core at /core, the source at /symbian, the build output at /build, " +
				"the adaptations at /adapt/<n>.",
			Flags: boardFlag,
			Examples: []cli.Example{
				{What: "Look around a board build:", Cmd: "shell -b myboard"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				e, err := env(str(fs, "b"))
				if err != nil {
					return err
				}
				return e.Shell()
			},
		},
		{
			Name:    "version",
			Summary: "Show ph's version",
			Examples: []cli.Example{
				{What: "Which ph is this?", Cmd: "version"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				fmt.Println("ph", version)
				return nil
			},
		},
		{
			Name: "help", Operands: "[<command>|<topic>]",
			Summary: "Show the commands, a command's manual page, or a topic",
			Description: "Without operands, the list of commands. With a command (\"ph help image rom\"), its manual " +
				"page: synopsis, description, options and examples. With a topic (\"ph help environment\"), that " +
				"topic. -markdown writes every manual page as Markdown into a directory (the pdk's docs/ph/).",
			Flags: func(fs *flag.FlagSet) {
				fs.String("markdown", "", "write every manual page as Markdown into this `directory`")
			},
			Examples: []cli.Example{
				{What: "How to build the ROM:", Cmd: "help image rom"},
				{What: "Regenerate the documentation:", Cmd: "help -markdown docs/ph"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if dir := str(fs, "markdown"); dir != "" {
					return app.WriteMarkdown(dir)
				}
				return app.HelpArgs(args)
			},
		},
	}
}

func topics() []cli.Topic {
	var opts strings.Builder
	for _, o := range build.ImageOptions {
		fmt.Fprintf(&opts, "  %s=%s\n      %s.\n\n", o.Name, o.Values, upper(o.Help))
	}
	return []cli.Topic{
		{
			Name:    "environment",
			Summary: "The settings that say where everything is",
			Text: "ph finds its pieces through these variables (\"ph source status\" shows what they resolve to):\n\n" +
				"  PHOREPOC_CORE\n" +
				"      The core checkout. Default: the core folder next to the pdk.\n\n" +
				"  PHOREPOC_ADAPTATIONS\n" +
				"      The adaptation checkouts built with core, as a path list. Their os/ trees are laid over\n" +
				"      core's, their registries add their components. -b <board> sets it for one command.\n\n" +
				"  PHOREPOC_SYMBIAN\n" +
				"      The Symbian source: symbian-2010/ and symbian-rcl3/. Default: <core>/symbian.\n\n" +
				"  PHOREPOC_BUILD\n" +
				"      All generated files: a directory, or a Docker volume name. Default: <core>/build.\n\n" +
				"  PHOREPOC_OS_ROOT\n" +
				"      core's OS tree. Default: <core>/os.\n\n" +
				"  PHOREPOC_IMAGE\n" +
				"      The toolchain container image. Default: " + build.DefaultImage + ".\n\n" +
				"  PHONELAB_URL, PHONELAB_TOKEN\n" +
				"      The PhoneLab service the device commands talk to, and its bearer token.\n",
		},
		{
			Name:    "image-options",
			Summary: "The recipe settings of \"ph image rom -O name=value\"",
			Text: "Given as -O name=value, as many as needed (ph image rom -b myboard -O net=1 -O fonts=dejavu):\n\n" +
				strings.TrimRight(opts.String(), "\n") + "\n",
		},
	}
}

func upper(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
