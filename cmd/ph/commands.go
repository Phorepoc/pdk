// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/Phorepoc/pdk/lib/build"
	"github.com/Phorepoc/pdk/lib/cli"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

// multi is a repeatable string option.
type multi []string

func (m *multi) String() string     { return strings.Join(*m, " ") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

// env is the build environment, for the board when one is named.
func env(board string) (*build.Env, error) {
	e, err := build.FromEnv()
	if err != nil {
		return nil, err
	}
	if board != "" {
		if err := e.UseBoard(board); err != nil {
			return nil, err
		}
	}
	return e, nil
}

func boardFlag(fs *flag.FlagSet) {
	fs.String("b", "", "build for this `board` (\"ph board list\"): core plus the board's adaptation repositories. "+
		"Without it, the adaptations in PHOREPOC_ADAPTATIONS")
}

func str(fs *flag.FlagSet, name string) string { return fs.Lookup(name).Value.String() }
func on(fs *flag.FlagSet, name string) bool    { return fs.Lookup(name).Value.String() == "true" }
func vals(fs *flag.FlagSet, name string) multi { return *fs.Lookup(name).Value.(*multi) }
func allFlag(fs *flag.FlagSet, what string)    { fs.Bool("a", false, what) }
func noArgs(args []string) error {
	if len(args) > 0 {
		return cli.Usagef("unexpected %q", strings.Join(args, " "))
	}
	return nil
}

// python runs a pdk tool for the board given by -b.
func python(fs *flag.FlagSet, script string, args ...string) error {
	e, err := env(str(fs, "b"))
	if err != nil {
		return err
	}
	return e.Python(script, args...)
}

// packages: -a = every package, else the named ones, else the tool's default set.
func packages(fs *flag.FlagSet, args []string) []string {
	if on(fs, "a") {
		if len(args) > 0 {
			return nil
		}
		return []string{"--all"}
	}
	return args
}

func commands(app *cli.App) []*cli.Command {
	cmds := []*cli.Command{
		// ---------------------------------------------------------------- source
		{
			Group: "source", Name: "fetch", Operands: "[<package>...]",
			Summary: "Fetch the pinned Symbian source",
			Description: "Clones or updates the Symbian packages core's sources.lock pins, at those commits, into " +
				"<core>/symbian (symbian-2010/ and symbian-rcl3/). Without operands, every package in the lock. " +
				"Runs core's fetch.sh on the host, not in the container.",
			Examples: []cli.Example{
				{What: "Fetch everything the build needs:", Cmd: "source fetch"},
			},
			Aliases: []string{"fetch"},
			Run: func(fs *flag.FlagSet, args []string) error {
				e, err := build.FromEnv()
				if err != nil {
					return err
				}
				return e.Fetch(args...)
			},
		},
		{
			Group: "source", Name: "export", Operands: "[<package>...]",
			Summary: "Build the include tree from the packages' exports",
			Description: "Does what abld export did: preprocesses each package's bld.inf files and copies their " +
				"PRJ_EXPORTS headers into <build>/epoc32/include, the tree every component compiles against.\n\n" +
				"Without operands, the operating-system and middleware packages the core build needs; -a exports " +
				"every package of both source trees, in two passes around casefix (a bld.inf that names a header " +
				"under another letter case only preprocesses once casefix has linked that spelling). Where two " +
				"packages export the same header, the Symbian^3 RCL_3 line wins.",
			Flags: func(fs *flag.FlagSet) {
				allFlag(fs, "every package of both source trees (runs casefix itself)")
				boardFlag(fs)
			},
			Examples: []cli.Example{
				{What: "The include tree a clean build needs:", Cmd: "source export -a"},
				{What: "Re-export one package after changing its headers:", Cmd: "source export ../symbian-rcl3/oss.FCL.sf.mw.classicui"},
			},
			Aliases: []string{"export"},
			Run: func(fs *flag.FlagSet, args []string) error {
				if on(fs, "a") && len(args) > 0 {
					return cli.Usagef("-a and package names together")
				}
				return python(fs, "tools/epoc32_export.py", packages(fs, args)...)
			},
		},
		{
			Group: "source", Name: "casefix", Operands: "[<package>...]",
			Summary: "Link the header spellings a case-sensitive file system misses",
			Description: "The 2010 source was written for Windows: #include <AknUtils.h> finds aknutils.h. For every " +
				"include spelling that names a header under another letter case, casefix puts a link with that " +
				"spelling into <build>/casefix, which the compilers search last. Where several headers share a " +
				"name, the exported one in the order the components' SYSTEMINCLUDE paths search wins. No source " +
				"file is changed.",
			Flags: func(fs *flag.FlagSet) {
				allFlag(fs, "scan every package of both source trees")
				boardFlag(fs)
			},
			Examples: []cli.Example{
				{What: "Refresh the links after a full export:", Cmd: "source casefix -a"},
			},
			Aliases: []string{"casefix"},
			Run: func(fs *flag.FlagSet, args []string) error {
				return python(fs, "tools/casefix.py", packages(fs, args)...)
			},
		},
		{
			Group: "source", Name: "mbg", Operands: "[<package>...]",
			Summary: "Generate the icon id headers (.mbg)",
			Description: "Components include <x.mbg>, the enumeration of the icons in x.mif, which the S60 build " +
				"generated from each bld.inf's mifconv block. This writes those headers into the include tree. " +
				"Run it after an export and before building components.",
			Flags: func(fs *flag.FlagSet) {
				allFlag(fs, "every package of both source trees")
				boardFlag(fs)
			},
			Examples: []cli.Example{
				{What: "All icon headers, for a clean build:", Cmd: "source mbg -a"},
			},
			Aliases: []string{"mbg"},
			Run: func(fs *flag.FlagSet, args []string) error {
				return python(fs, "tools/mbg.py", packages(fs, args)...)
			},
		},
		{
			Group: "source", Name: "status",
			Summary: "Show where the source, the OS tree, the adaptations and the build output are",
			Description: "Prints the locations ph uses (from the PHOREPOC_* settings, \"ph help environment\") and " +
				"whether each exists.",
			Flags: func(fs *flag.FlagSet) {
				boardFlag(fs)
				fs.Bool("H", false, "script mode: no header, fields separated by a tab")
			},
			Examples: []cli.Example{
				{What: "Where a PinePhone build would look:", Cmd: "source status -b pinephone"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				e, err := env(str(fs, "b"))
				if err != nil {
					return err
				}
				rows := [][2]string{{"pdk", e.PDK}, {"core", e.Core}, {"os", e.OSRoot}, {"symbian", e.Symbian},
					{"build", e.Build}, {"image", e.Image}}
				for _, a := range e.Adaptations {
					rows = append(rows, [2]string{"adaptation", a})
				}
				table(on(fs, "H"), []string{"WHAT", "WHERE", "STATE"}, func(add func(...string)) {
					for _, r := range rows {
						state := "missing"
						if r[0] == "image" {
							state = "-"
						} else if st, err := os.Stat(r[1]); err == nil && st.IsDir() {
							state = "present"
						} else if r[0] == "build" {
							state = "volume or new"
						}
						add(r[0], r[1], state)
					}
				})
				return nil
			},
		},
		// ------------------------------------------------------------- component
		{
			Group: "component", Name: "list", Operands: "[<component>...]",
			Summary: "List the registered components",
			Description: "One line per component of the registry (core's plus the adaptations'), or of the named " +
				"ones. The fields: name, the repository that registers it (core or an adaptation), its kind " +
				"(component, test, ssm = only in a System State Manager boot, nga = only with the NGA window " +
				"server) and its .mmp.",
			Flags: func(fs *flag.FlagSet) {
				fs.Bool("H", false, "script mode: no header, fields separated by a tab")
				fs.String("o", "name,repo,kind,mmp", "the `fields` to show, comma-separated: name, repo, kind, mmp")
				boardFlag(fs)
			},
			Examples: []cli.Example{
				{What: "Every component a PC build knows:", Cmd: "component list -b pc"},
				{What: "Only the names, for a script:", Cmd: "component list -H -o name"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				h := "0"
				if on(fs, "H") {
					h = "1"
				}
				return python(fs, "-c", listScript, h, str(fs, "o"), strings.Join(args, ","))
			},
		},
		{
			Group: "component", Name: "info", Operands: "<component>",
			Summary: "Show a component's registry entry",
			Description: "Prints the component's whole registry entry: its .mmp and every build setting " +
				"(dropped and added sources, defines, flags, ROM name ...).",
			Flags: boardFlag,
			Examples: []cli.Example{
				{What: "How Avkon is built:", Cmd: "component info avkon"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) != 1 {
					return cli.Usagef("one component")
				}
				return python(fs, "-c", infoScript, args[0])
			},
		},
		{
			Group: "component", Name: "build", Operands: "[<component>...]",
			Summary: "Build components into images",
			Description: "Compiles each component from its real .mmp with the modern toolchain and writes its " +
				"E32Image: x32 images into <build>/x32img, ARM images into <build>/arm.\n\n" +
				"-r also builds everything the named components link against, providers first. -A builds every " +
				"registered component, providers first (ARM: every component, in registry order); a component that fails is reported and the " +
				"build goes on. Run \"ph component resources\" first: components include the .rsg headers it " +
				"generates.",
			Flags: func(fs *flag.FlagSet) {
				fs.Bool("r", false, "recursive: also everything the components link against, providers first")
				fs.Bool("A", false, "every registered component")
				fs.String("arch", "x32", "the `architecture`: x32 (amd64 CPU, 32-bit pointers) or arm")
				fs.String("shard", "", "with -A: only every n-th component from the i-th (`i/n`, a CI matrix job's share)")
				fs.Bool("compile-only", false, "compile the objects, link nothing (the shards of a split build; a later -A links)")
				boardFlag(fs)
			},
			Examples: []cli.Example{
				{What: "Build two components for the PC:", Cmd: "component build -b pc estor bafl"},
				{What: "A component and everything below it:", Cmd: "component build -r avkon"},
				{What: "Everything (a clean build):", Cmd: "component build -A -b pc"},
				{What: "An ARM image for the PinePhone:", Cmd: "component build -b pinephone -arch arm phetty"},
			},
			Aliases: []string{"build", "closure=-r", "all=-A", "arm=-arch arm"},
			Run: func(fs *flag.FlagSet, args []string) error {
				all, rec, arch := on(fs, "A"), on(fs, "r"), str(fs, "arch")
				switch {
				case all && len(args) > 0:
					return cli.Usagef("-A and component names together")
				case !all && len(args) == 0:
					return cli.Usagef("which components? (or -A for all)")
				case arch != "x32" && arch != "arm":
					return cli.Usagef("-arch %s: x32 or arm", arch)
				case arch == "arm" && rec:
					return cli.Usagef("-r is x32 only for now")
				}
				var extra []string
				if sh := str(fs, "shard"); sh != "" {
					if !all {
						return cli.Usagef("-shard goes with -A")
					}
					extra = append(extra, "--shard", sh)
				}
				if on(fs, "compile-only") {
					extra = append(extra, "--compile-only")
				}
				if arch == "arm" {
					if all {
						return python(fs, "tools/emitter/build_mmp.py", append([]string{"--all"}, extra...)...)
					}
					return python(fs, "tools/emitter/build_mmp.py", append(args, extra...)...)
				}
				switch {
				case all:
					return python(fs, "tools/emitter/build_mmp_x32.py", append([]string{"--all"}, extra...)...)
				case rec:
					return python(fs, "tools/emitter/build_mmp_x32.py", append([]string{"--closure"}, args...)...)
				}
				return python(fs, "tools/emitter/build_mmp_x32.py", args...)
			},
		},
		{
			Group: "component", Name: "resources", Operands: "<component>...",
			Summary: "Compile components' resources (.rsc) and their headers (.rsg)",
			Description: "Runs the release's own resource compiler (rcomp) over each component's resource files, " +
				"preprocessed as abld did. The .rsg headers go into the include tree, where other components " +
				"include them: compile every component's resources (-A) before a full build.",
			Flags: func(fs *flag.FlagSet) {
				fs.Bool("A", false, "every registered component")
				boardFlag(fs)
			},
			Examples: []cli.Example{
				{What: "Every resource, before a clean build:", Cmd: "component resources -A"},
				{What: "One component's after a change:", Cmd: "component resources avkon"},
			},
			Aliases: []string{"rsc"},
			Run: func(fs *flag.FlagSet, args []string) error {
				if on(fs, "A") == (len(args) > 0) {
					return cli.Usagef("component names, or -A")
				}
				if on(fs, "A") {
					return python(fs, "tools/emitter/build_mmp.py", "--rsc", "--all")
				}
				return python(fs, "tools/emitter/build_mmp.py", append([]string{"--rsc"}, args...)...)
			},
		},
		{
			Group: "component", Name: "stale",
			Summary: "List images linked before a provider of theirs existed",
			Description: "An image whose imports went to stubs because a provider was not built yet has to be " +
				"linked again once it is. This lists those images, with the providers now present.",
			Flags:   boardFlag,
			Aliases: []string{"stale"},
			Examples: []cli.Example{
				{What: "What needs a relink after a partial build:", Cmd: "component stale"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				return python(fs, "tools/stale_stubs.py")
			},
		},
		{
			Group: "component", Name: "check",
			Summary: "Check that the registry's paths exist",
			Description: "Every .mmp, added source file and include path or forced include each registered component " +
				"names (core's registry and the adaptations') must exist in the merged OS tree or the Symbian " +
				"source. Lists the ones that do not; exits non-zero when there are any.",
			Flags: boardFlag,
			Examples: []cli.Example{
				{What: "Check the PC board's registry:", Cmd: "component check -b pc"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				return python(fs, "tools/registry_check.py")
			},
		},
		// ----------------------------------------------------------------- image
		{
			Group: "image", Name: "rom",
			Summary: "Build the ROM",
			Description: "Builds the core images (euser, efsrv, hal, efile, estart), stages every built component " +
				"with its resources, icons, central repository files and data, and lays them out with rombuild " +
				"into <build>/rom.\n\n" +
				"The boot (-s): ssm is the phone's real start-up through the System State Manager; bootui is the " +
				"older bring-up starter; any other name is an application bootui starts in the foreground.\n\n" +
				"Settings of the recipe are given with -O name=value (\"ph help image-options\" lists them).",
			Flags: func(fs *flag.FlagSet) {
				fs.String("s", "ssm", "the `boot`: ssm, bootui, or an application name")
				fs.Var(&multi{}, "O", "set an image `option`, name=value (repeatable)")
				boardFlag(fs)
			},
			Examples: []cli.Example{
				{What: "The PC image, as the phone boots:", Cmd: "image rom -b pc"},
				{What: "With networking, keeping the core images of the last build:", Cmd: "image rom -b pc -O net=1 -O fast=1"},
				{What: "Booting straight into Notes for a test:", Cmd: "image rom -b pc -s notes"},
			},
			Aliases: []string{"rom"},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				for _, o := range vals(fs, "O") {
					if err := build.SetImageOption(o); err != nil {
						return cli.Usagef("%v", err)
					}
				}
				e, err := env(str(fs, "b"))
				if err != nil {
					return err
				}
				boot := str(fs, "s")
				switch boot {
				case "ssm":
				case "bootui":
					boot = "@"
				default:
					boot = "@" + boot
				}
				return e.Rom(boot)
			},
		},
		{
			Group: "image", Name: "recipe", Operands: "<name> [<arg>...]",
			Summary: "Run an image recipe of the board's repositories",
			Description: "Runs recipes/<name>.sh from the board's repositories, in the toolchain container: the board " +
				"first, then its SoC and layers (docs/layering.md). Boards and SoC adaptations bring their own image " +
				"steps this way -- a kernel, a disk image, an appliance -- and \"ph board info\" lists them.\n\n" +
				"Settings of the recipes are given with -O name=value (\"ph help image-options\" lists them).",
			Flags: func(fs *flag.FlagSet) {
				fs.Var(&multi{}, "O", "set an image `option`, name=value (repeatable)")
				boardFlag(fs)
			},
			Examples: []cli.Example{
				{What: "The board's kernel and boot image, after \"ph image rom\":", Cmd: "image recipe kernel -b <board>"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) == 0 {
					return cli.Usagef("a recipe name")
				}
				for _, o := range vals(fs, "O") {
					if err := build.SetImageOption(o); err != nil {
						return cli.Usagef("%v", err)
					}
				}
				e, err := env(str(fs, "b"))
				if err != nil {
					return err
				}
				return e.Recipe(args[0], args[1:]...)
			},
		},
		{
			Group: "image", Name: "release",
			Summary: "Make the board's release files",
			Description: "Runs the board's RELEASE recipe (board.conf) on this machine, after every component is built: " +
				"it makes the images to publish into <out> (default <build>/release). The recipe may start " +
				"containers and call ph itself; CI runs the same command.",
			Flags: func(fs *flag.FlagSet) {
				fs.String("o", "", "the `folder` for the release files (default <build>/release)")
				boardFlag(fs)
			},
			Examples: []cli.Example{
				{What: "Everything a release of the board publishes:", Cmd: "image release -b <board>"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				e, err := env(str(fs, "b"))
				if err != nil {
					return err
				}
				return e.Release(str(fs, "b"), str(fs, "o"))
			},
		},
		{
			Group: "image", Name: "skin",
			Summary: "Build the skin packages (the default skin and the Haze and Jing themes)",
			Description: "Compiles the S60 default skin from its descriptor with the release's skin compiler: " +
				"series60skin.skn plus its icon file series60skin.mif (the SVG artwork encoded as the platform's " +
				"NVG and SVG-T binary formats), into <build>/skin-gen. Then the release's two complete themes, " +
				"Haze (the default theme) and Jing, into <build>/skin-gen/<name>.",
			Flags:   boardFlag,
			Aliases: []string{"skin"},
			Examples: []cli.Example{
				{What: "Rebuild the skin:", Cmd: "image skin"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				if err := python(fs, "tools/skin_gen.py"); err != nil {
					return err
				}
				return python(fs, "tools/theme_gen.py")
			},
		},
		{
			Group: "image", Name: "icons",
			Summary: "Build every component's icon files (.mif)",
			Description: "Runs mifconv for each icon file the registered components declare in their bld.inf " +
				"(and the extra folders core's registry/mif_dirs.txt lists), into <build>/icon-gen. An icon whose " +
				"artwork is not in the open release gets a transparent placeholder. Needs the include tree " +
				"(\"ph source export\").",
			Flags: func(fs *flag.FlagSet) {
				allFlag(fs, "rebuild every icon file, not only the missing ones")
				boardFlag(fs)
			},
			Aliases: []string{"icons"},
			Examples: []cli.Example{
				{What: "Only the icon files not built yet:", Cmd: "image icons"},
				{What: "All of them again:", Cmd: "image icons -a"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				if on(fs, "a") {
					return python(fs, "tools/mif_all.py", "--all")
				}
				return python(fs, "tools/mif_all.py")
			},
		},
		{
			Group: "image", Name: "zdata",
			Summary: "Collect the Z: data the packages export",
			Description: "Gathers the data files (configuration, databases, localisation) the packages' bld.inf " +
				"files export to the ROM drive Z:, into <build>/zdata-gen.",
			Flags:   boardFlag,
			Aliases: []string{"zdata"},
			Examples: []cli.Example{
				{What: "Refresh the Z: data:", Cmd: "image zdata"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				return python(fs, "tools/zdata_gen.py")
			},
		},
		{
			Group: "image", Name: "sis", Operands: "<component>...",
			Summary: "Package components as an installable SIS file",
			Description: "Builds a SIS package (the Symbian installer format) from the components' images and " +
				"resources, to install on a running device instead of rebuilding the ROM.",
			Flags: func(fs *flag.FlagSet) {
				fs.String("arch", "x32", "the `architecture` of the images to package: x32 or arm")
				boardFlag(fs)
			},
			Aliases: []string{"sis"},
			Examples: []cli.Example{
				{What: "Package the developer terminal:", Cmd: "image sis phettyd pheshell pheterm"},
				{What: "An ARM package (the PinePhone):", Cmd: "image sis -arch arm pheffmpeg"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) == 0 {
					return cli.Usagef("which components?")
				}
				if str(fs, "arch") == "arm" {
					args = append([]string{"--arm"}, args...)
				}
				return python(fs, "tools/make_sis.py", args...)
			},
		},
		{
			Group: "image", Name: "compare", Operands: "[<a> <b>]",
			Summary: "Byte-compare images with another build",
			Description: "Without operands, compares the images built here with a reference build row by row " +
				"(tests/compare_*.tsv): IDENTICAL, or DIFFERENT with both sizes and the first differing offsets. " +
				"With two operands, compares two files or two directories. Exits non-zero when anything differs. " +
				"This is the check every port of a build step to Go must pass.",
			Flags: func(fs *flag.FlagSet) {
				fs.String("ref", "", "the reference build's `directory` (default: the OS tree)")
				boardFlag(fs)
			},
			Aliases: []string{"tests"},
			Examples: []cli.Example{
				{What: "Two image folders:", Cmd: "image compare build/x32img ../ref/x32img"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				a := args
				if r := str(fs, "ref"); r != "" {
					a = append([]string{"--ref", r}, a...)
				}
				return python(fs, "tests/compare_images.py", a...)
			},
		},
		// ----------------------------------------------------------------- board
		{
			Group: "board", Name: "list",
			Summary: "List the boards ph builds for",
			Description: "A board is core plus its adaptation repositories, checked out next to core. The STATE " +
				"column says whether those checkouts are there.",
			Flags: func(fs *flag.FlagSet) {
				fs.Bool("H", false, "script mode: no header, fields separated by a tab")
			},
			Examples: []cli.Example{
				{What: "The boards and whether they can be built here:", Cmd: "board list"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				e, err := build.FromEnv()
				if err != nil {
					return err
				}
				table(on(fs, "H"), []string{"BOARD", "ARCH", "STATE", "DESCRIPTION"}, func(add func(...string)) {
					bs := e.Boards()
					for i := range bs {
						add(bs[i].Name, bs[i].Arch, boardState(e, &bs[i]), bs[i].Summary)
					}
				})
				return nil
			},
		},
		{
			Group: "board", Name: "info", Operands: "<board>",
			Summary: "Show a board's adaptation repositories",
			Description: "Lists the adaptation repositories the board is made of, where ph expects their " +
				"checkouts (next to core), and whether they are there.",
			Examples: []cli.Example{
				{What: "What a board is made of:", Cmd: "board info <board>"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) != 1 {
					return cli.Usagef("one board")
				}
				e, err := build.FromEnv()
				if err != nil {
					return err
				}
				b, err := e.FindBoard(args[0])
				if err != nil {
					return err
				}
				fmt.Printf("%s: %s (%s)\n\n", b.Name, b.Summary, b.Arch)
				table(false, []string{"ADAPTATION", "CHECKOUT", "STATE"}, func(add func(...string)) {
					for _, d := range e.AdaptationDirs(b) {
						state := "missing"
						if st, err := os.Stat(d); err == nil && st.IsDir() {
							state = "present"
						}
						add(d[strings.LastIndexAny(d, `/\`)+1:], d, state)
					}
				})
				fmt.Println()
				table(false, []string{"RECIPE", "FROM"}, func(add func(...string)) {
					for _, d := range e.AdaptationDirs(b) {
						rs, _ := filepath.Glob(filepath.Join(d, "recipes", "*.sh"))
						for _, r := range rs {
							add(strings.TrimSuffix(filepath.Base(r), ".sh"), filepath.Base(d))
						}
					}
				})
				if b.Release != "" {
					fmt.Printf("\nrelease: %s (ph image release -b %s)\n", b.Release, b.Name)
				}
				return nil
			},
		},
	}
	cmds = append(cmds, packageCommands()...)
	cmds = append(cmds, deviceCommands()...)
	return append(cmds, otherCommands(app)...)
}

func boardState(e *build.Env, b *build.Board) string {
	for _, d := range e.AdaptationDirs(b) {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			return "missing"
		}
	}
	return "ready"
}

// table prints rows aligned in columns (or tab-separated without a header in script mode).
func table(script bool, header []string, rows func(add func(...string))) {
	if script {
		rows(func(f ...string) { fmt.Println(strings.Join(f, "\t")) })
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, strings.Join(header, "\t"))
	rows(func(f ...string) { fmt.Fprintln(w, strings.Join(f, "\t")) })
	w.Flush()
}

// listScript / infoScript read the registry inside the container (it is Python until the registry moves to Go).
const listScript = `
import sys
sys.path[:0] = ["tools/emitter"]
import paths, components as C, os
script, fields, only = sys.argv[1] == "1", sys.argv[2].split(","), [k for k in sys.argv[3].split(",") if k]
origin = {}
for reg in paths.ADAPTATION_REGISTRIES:
    for f in os.listdir(reg):
        if f.startswith("components_") and f.endswith(".py"):
            for k in __import__(f[:-3]).COMPONENTS:
                origin[k] = os.path.basename(os.path.dirname(reg))
def kind(c):
    return ",".join(k for k in ("test", "ssm", "nga", "device") if c.get(k)) or "component"
def mmp(c):
    m = c.get("mmp", "")
    return m.replace(paths.OS_ROOT + "/", "") if isinstance(m, str) else ""
rows = []
for k in sorted(C.COMPONENTS):
    if only and k not in only:
        continue
    c = C.COMPONENTS[k] if isinstance(C.COMPONENTS[k], dict) else {}
    v = {"name": k, "repo": origin.get(k, "core"), "kind": kind(c), "mmp": mmp(c)}
    rows.append([v.get(f, "?") for f in fields])
missing = [k for k in only if k not in C.COMPONENTS]
if missing:
    sys.exit("no component " + ", ".join(missing))
if script:
    for r in rows: print("\t".join(r))
else:
    w = [max(len(r[i]) for r in rows + [[f.upper() for f in fields]]) for i in range(len(fields))]
    print("   ".join(f.upper().ljust(w[i]) for i, f in enumerate(fields)).rstrip())
    for r in rows: print("   ".join(x.ljust(w[i]) for i, x in enumerate(r)).rstrip())
`

const infoScript = `
import sys, pprint
sys.path[:0] = ["tools/emitter"]
import components as C
k = sys.argv[1]
if k not in C.COMPONENTS:
    sys.exit("no component " + k)
print(k)
print()
pprint.pprint(C.COMPONENTS[k], width=110, indent=4)
`
