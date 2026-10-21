// Copyright (c) Nokia Corporation and/or its subsidiary(-ies) (ThemeInstaller); Copyright The Phorepoc Authors (the Go port)
// SPDX-License-Identifier: EPL-1.0

// Command phodt compiles a homescreen configuration (a manifest .dat naming the Xuikon XML, CSS and DTD) to the
// binary ODT form (.o0000), replacing the Java ThemeInstaller.
//
//	phodt manifest.dat -o out.o0000
//	phodt manifest.dat -d outdir        (writes outdir/<ThemeShortName>.o0000)
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Phorepoc/pdk/lib/odt"
)

func main() {
	out := flag.String("o", "", "output file")
	dir := flag.String("d", "", "output directory (the file is named <ThemeShortName>.o0000)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: phodt [-o out.o0000 | -d outdir] manifest.dat")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 || (*out == "") == (*dir == "") {
		flag.Usage()
		os.Exit(2)
	}
	m, err := odt.ReadManifest(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "phodt:", err)
		os.Exit(1)
	}
	data, err := odt.Compile(m, odt.DefaultOptions())
	if err != nil {
		fmt.Fprintln(os.Stderr, "phodt:", err)
		os.Exit(1)
	}
	path := *out
	if path == "" {
		path = filepath.Join(*dir, m.OutputName())
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "phodt:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "phodt:", err)
		os.Exit(1)
	}
	fmt.Println(path)
}
