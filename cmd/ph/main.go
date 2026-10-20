// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// ph -- the Phorepoc platform development kit's command line.
//
// The commands are defined in commands.go (one definition each: options, help text, examples); lib/cli turns them into
// the overview, `ph help <command>` and the manual pages in docs/ph/. Run `ph help` for the list.
package main

import (
	"os"

	"github.com/Phorepoc/pdk/lib/cli"
)

func main() {
	app := newApp()
	app.Out, app.Err = os.Stdout, os.Stderr
	os.Exit(app.Run(os.Args[1:]))
}

// newApp is the whole command line (also used by the tests and the documentation generator).
func newApp() *cli.App {
	app := &cli.App{
		Name: "ph",
		Summary: "The Phorepoc platform development kit: build Phorepoc from the Symbian source, and drive a " +
			"test device through PhoneLab.",
		Groups: []cli.Group{
			{Name: "source", Title: "Source", Summary: "The Symbian source and the include tree every build compiles against."},
			{Name: "component", Title: "Components", Summary: "The registry's components: list them, build them."},
			{Name: "image", Title: "Images", Summary: "What goes onto a device: the ROM, its data, installable packages."},
			{Name: "board", Title: "Boards", Summary: "The devices ph builds for, made of core plus adaptation repositories."},
			{Name: "device", Title: "Device", Summary: "A device on the PhoneLab service: push images, read logs, take screenshots."},
			{Name: "", Title: "Other", Summary: "Everything else."},
		},
		Topics: topics(),
	}
	app.Commands = commands(app)
	return app
}
