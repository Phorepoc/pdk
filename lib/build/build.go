// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package build runs the Phorepoc build tools (tools/ in the pdk) on the OS tree (the core repository).
//
// Every command runs its tool inside the toolchain image (ghcr.io/phorepoc/base) with the pdk mounted at /pdk,
// core at /core, and the source, overlay and build directories at /symbian, /os and /build. Inside a container
// already (/.dockerenv) the tool runs directly, with the environment as it is.
package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// DefaultImage is the toolchain image; PHOREPOC_IMAGE overrides it.
const DefaultImage = "ghcr.io/phorepoc/base:latest"

// passThrough lists the recipe options that are handed to the container when set.
var passThrough = []string{
	"NET", "NET_TEST", "NTP_SERVER", "FONTS", "UI_RENDERER", "SHELLS", "ALF", "NGA", "NGA_KEEP", "FAST",
	"PHOREPOC_ARCH", "LEAVE_TRACE", "SCHED_TRACE", "MIF_ALL", "IBY_DATA", "ROM_ARCH", "ROM_TAIL", "ROM_EXCLUDE", "SSM_TEST",
	"SECONDARY_IMAGE", "PHOREPOC_S4", "PHOREPOC_QT48", "PHETTY", "CENREP_EXTRA", "ROM_DATA_EXTRA", "UTC_OFFSET",
	"WEBFONTS", "THEMES", "X32_LD", "X32_DEFS", "X32_LIBS", "X32_INCLUDE", "X32_FULLERR", "PRUNE_UNRESOLVED", "ROM_HEAP_LIMITS",
	"TRACE", "SCREEN", "SCALE", "DBG", "KCLEAN", "ARM_VFP", "ARM_CPU_FLAGS", "ALF_TRACE", "ALF_TIMING", "ALF_LOG", "ALF_RC",
}

// Env is where the pieces live.
type Env struct {
	PDK     string // the pdk checkout (tools/, tests/)
	Core    string // the core checkout (registry/, fetch.sh, default os/ symbian/ build/)
	OSRoot  string
	Symbian string
	Build   string // a directory or a Docker volume name
	Image   string
	// Adaptations: the adaptation checkouts built with core (PHOREPOC_ADAPTATIONS, the host's path-list separator):
	// each has an os/ tree merged over core's and a registry/ of its own components (tools/emitter/paths.py).
	Adaptations []string
	Arch        string // the board's architecture (UseBoard), "" without a board
}

// FromEnv reads the PHOREPOC_* variables. The pdk is PHOREPOC_PDK, else found from the executable or the current
// directory; core is PHOREPOC_CORE, else the "core" folder next to the pdk.
func FromEnv() (*Env, error) {
	e := &Env{Image: getenv("PHOREPOC_IMAGE", DefaultImage)}
	e.PDK = os.Getenv("PHOREPOC_PDK")
	if e.PDK == "" {
		e.PDK = findPDK()
	}
	if e.PDK == "" {
		return nil, fmt.Errorf("cannot find the pdk checkout (tools/emitter/paths.py): set PHOREPOC_PDK")
	}
	e.PDK, _ = filepath.Abs(e.PDK)
	e.Core = getenv("PHOREPOC_CORE", filepath.Join(filepath.Dir(e.PDK), "core"))
	e.Core, _ = filepath.Abs(e.Core)
	e.OSRoot = getenv("PHOREPOC_OS_ROOT", filepath.Join(e.Core, "os"))
	e.Symbian = getenv("PHOREPOC_SYMBIAN", filepath.Join(e.Core, "symbian"))
	e.Build = getenv("PHOREPOC_BUILD", filepath.Join(e.Core, "build"))
	for _, a := range filepath.SplitList(os.Getenv("PHOREPOC_ADAPTATIONS")) {
		if a != "" {
			abs, _ := filepath.Abs(a)
			e.Adaptations = append(e.Adaptations, abs)
		}
	}
	return e, nil
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func findPDK() string {
	var starts []string
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	for _, d := range starts {
		for ; ; d = filepath.Dir(d) {
			if _, err := os.Stat(filepath.Join(d, "tools", "emitter", "paths.py")); err == nil {
				return d
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	return ""
}

func inContainer() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// isPath: a build location that is a directory path (not a Docker volume name).
func isPath(s string) bool {
	return filepath.IsAbs(s) || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, `.\`) || strings.HasPrefix(s, `..\`)
}

// dockerArgs is the `docker run` command line (without the leading "docker") for a command in the image.
func (e *Env) dockerArgs(tty bool, cmd []string) ([]string, error) {
	args := []string{"run", "--rm"}
	if tty {
		args = append(args, "-it")
	}
	args = append(args, "-v", e.PDK+":/pdk", "-v", e.Core+":/core", "-w", "/pdk",
		"-e", "PHOREPOC_PDK=/pdk", "-e", "PHOREPOC_CORE=/core", "-e", "PHOREPOC_OS_ROOT=/os",
		"-e", "PHOREPOC_SYMBIAN=/symbian", "-e", "PHOREPOC_BUILD=/build")
	if isDir(e.OSRoot) {
		args = append(args, "-v", e.OSRoot+":/os:ro")
	}
	if isDir(e.Symbian) {
		args = append(args, "-v", e.Symbian+":/symbian:ro")
	}
	if isPath(e.Build) {
		if err := os.MkdirAll(e.Build, 0o755); err != nil {
			return nil, err
		}
	}
	args = append(args, "-v", e.Build+":/build") // a directory, or a Docker volume name
	var adapt []string
	for i, a := range e.Adaptations {
		m := fmt.Sprintf("/adapt/%d", i)
		args = append(args, "-v", a+":"+m+":ro")
		adapt = append(adapt, m)
	}
	if len(adapt) > 0 {
		args = append(args, "-e", "PHOREPOC_ADAPTATIONS="+strings.Join(adapt, ":"))
	}
	for _, v := range passThrough {
		if val, ok := os.LookupEnv(v); ok {
			args = append(args, "-e", v+"="+val)
		}
	}
	// ARM images use the VFP hardware (softfp ABI) for the registry's vfp=True components unless ARM_VFP=0 says otherwise (the x32 build ignores it)
	if _, ok := os.LookupEnv("ARM_VFP"); !ok {
		args = append(args, "-e", "ARM_VFP=1")
	}
	args = append(args, e.Image)
	return append(args, cmd...), nil
}

// InContainer runs a command in the toolchain image with the mounts, or directly when already inside a container.
func (e *Env) InContainer(cmd ...string) error {
	var c *exec.Cmd
	if inContainer() {
		c = exec.Command(cmd[0], cmd[1:]...)
		c.Dir = e.PDK
		c.Env = append(os.Environ(), "PHOREPOC_PDK="+e.PDK, "PHOREPOC_CORE="+e.Core, "PHOREPOC_OS_ROOT="+e.OSRoot,
			"PHOREPOC_SYMBIAN="+e.Symbian, "PHOREPOC_BUILD="+e.Build,
			"PHOREPOC_ADAPTATIONS="+strings.Join(e.Adaptations, string(os.PathListSeparator)))
	} else {
		if _, err := exec.LookPath("docker"); err != nil {
			return fmt.Errorf("docker not found")
		}
		st, _ := os.Stdin.Stat()
		so, _ := os.Stdout.Stat()
		tty := st != nil && so != nil && st.Mode()&os.ModeCharDevice != 0 && so.Mode()&os.ModeCharDevice != 0
		args, err := e.dockerArgs(tty, cmd)
		if err != nil {
			return err
		}
		c = exec.Command("docker", args...)
	}
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// romScript runs the ROM build quietly into a log. FAST=1 keeps the core images; LEAVE_TRACE=1 builds the traced
// core images (the [LEAVE]/SSM lines).
const romScript = `mkdir -p "$PHOREPOC_BUILD" && \
  FAST="${FAST:-1}" LEAVE_TRACE="${LEAVE_TRACE:-1}" BOOT="$1" SECONDARY_IMAGE="${SECONDARY_IMAGE:-efile.exe}" \
  bash tools/make_phorepoc_rom.sh > "$PHOREPOC_BUILD/rom-$1.log" 2>&1 \
  && echo "ROM ok ($(grep -a -m1 images "$PHOREPOC_BUILD/rom-$1.log" | cut -c1-100))" \
  || { echo "ROM FAILED:"; tail -5 "$PHOREPOC_BUILD/rom-$1.log"; exit 1; }`

func bashPath() string {
	if runtime.GOOS == "windows" {
		for _, p := range []string{`C:\Program Files\Git\bin\bash.exe`, `C:\Program Files (x86)\Git\bin\bash.exe`} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return "bash"
}

func stdio(c *exec.Cmd) *exec.Cmd {
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c
}

// Python runs one of the pdk's Python tools in the toolchain container (the build steps not yet ported to Go).
func (e *Env) Python(script string, args ...string) error {
	return e.InContainer(append([]string{"python3", script}, args...)...)
}

// Fetch runs core's fetch.sh on the host: the pinned Symbian source into <core>/symbian.
func (e *Env) Fetch(args ...string) error {
	c := exec.Command(bashPath(), append([]string{filepath.Join(e.Core, "fetch.sh")}, args...)...)
	c.Dir = e.Core
	return stdio(c).Run()
}

// Rom builds the ROM section with the given boot: "ssm" (the phone's real System State Manager boot), "@" (bootui)
// or "@<app>" (bootui starting that application).
func (e *Env) Rom(boot string) error {
	return e.InContainer("bash", "-c", romScript, "_", boot)
}

// recipeScript runs recipes/<name>.sh of the first of the build's repositories that has one, searched from the top
// (the board first: PHOREPOC_ADAPTATIONS lists the SoC first, the board after it, then the layers).
const recipeScript = `name="$1"; shift; IFS=: read -r -a repos <<< "$PHOREPOC_ADAPTATIONS"
for ((i=${#repos[@]}-1; i>=0; i--)); do r="${repos[i]}"; [ -f "$r/recipes/$name.sh" ] || continue
  cd "$r" && exec bash "recipes/$name.sh" "$@"; done
echo "no recipe $name: none of the board's repositories has recipes/$name.sh (${PHOREPOC_ADAPTATIONS:-no board: give -b})" >&2; exit 1`

// Recipe runs a board repository's recipes/<name>.sh in the container (docs/layering.md).
func (e *Env) Recipe(name string, args ...string) error {
	return e.InContainer(append([]string{"bash", "-c", recipeScript, "_", name}, args...)...)
}

// Shell opens an interactive shell in the toolchain container with the mounts.
func (e *Env) Shell() error {
	if inContainer() {
		return stdio(exec.Command("bash")).Run()
	}
	return e.InContainer("bash")
}

// Board is a device the pdk builds for, described by the board.conf at the root of its repository
// (docs/layering.md): the repositories it is made of and the architecture core is built for. The pdk keeps no list
// of boards: every checkout next to core with a board.conf is one.
type Board struct {
	Name, Summary string
	Adaptations   []string // checkout folder names, next to core (REPOS): the SoC, the board itself, the layers it ships
	Arch          string   // ARCH: "x32" or "arm"
	Release       string   // RELEASE: the recipe that makes the release files (relative to Dir)
	Dir           string   // the board repository's checkout
}

// ParseBoardConf reads a board.conf: KEY=value lines, values optionally in double quotes, # comments.
func ParseBoardConf(path string) (*Board, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	kv := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, " #"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s: not KEY=value: %q", path, line)
		}
		kv[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
	}
	b := &Board{Name: kv["NAME"], Summary: kv["SUMMARY"], Arch: kv["ARCH"], Release: kv["RELEASE"],
		Adaptations: strings.Fields(kv["REPOS"]), Dir: filepath.Dir(path)}
	if b.Name == "" || b.Arch == "" {
		return nil, fmt.Errorf("%s: NAME and ARCH are required", path)
	}
	return b, nil
}

// Boards finds the boards checked out next to core (each folder with a board.conf), sorted by name.
func (e *Env) Boards() []Board {
	var out []Board
	paths, _ := filepath.Glob(filepath.Join(filepath.Dir(e.Core), "*", "board.conf"))
	sort.Strings(paths)
	for _, p := range paths {
		if b, err := ParseBoardConf(p); err == nil {
			out = append(out, *b)
		} else {
			fmt.Fprintln(os.Stderr, "ph:", err)
		}
	}
	return out
}

// FindBoard returns the named board.
func (e *Env) FindBoard(name string) (*Board, error) {
	var names []string
	bs := e.Boards()
	for i := range bs {
		if bs[i].Name == name {
			return &bs[i], nil
		}
		names = append(names, bs[i].Name)
	}
	return nil, fmt.Errorf("no board %q next to core (boards found: %s; a board is a checkout with a board.conf)",
		name, strings.Join(names, ", "))
}

// AdaptationDirs: where the board's adaptation checkouts are expected (next to core).
func (e *Env) AdaptationDirs(b *Board) []string {
	var out []string
	for _, a := range b.Adaptations {
		out = append(out, filepath.Join(filepath.Dir(e.Core), a))
	}
	return out
}

// UseBoard builds for the board: its adaptations replace PHOREPOC_ADAPTATIONS, and an ARM board's ROM is ARM.
func (e *Env) UseBoard(name string) error {
	b, err := e.FindBoard(name)
	if err != nil {
		return err
	}
	dirs := e.AdaptationDirs(b)
	for _, d := range dirs {
		if !isDir(d) {
			return fmt.Errorf("board %s needs the %s checkout at %s", b.Name, filepath.Base(d), d)
		}
	}
	e.Adaptations = dirs
	e.Arch = b.Arch
	os.Setenv("PHOREPOC_ARCH", b.Arch)
	if b.Arch == "arm" {
		os.Setenv("ROM_ARCH", "arm")
	}
	return nil
}

// ImageOption is a setting of the build recipes (`ph image rom -O name=value`), handed to the container as an
// environment variable.
type ImageOption struct {
	Name, Env, Values, Help string
}

// ImageOptions are the settings the recipes read.
var ImageOptions = []ImageOption{
	{"net", "NET", "0|1", "the networking stack: ESOCK, TCP/IP, DNS, DHCP, the e1000 driver and SNTP"},
	{"net-test", "NET_TEST", "<host> <port>", "the host the nettest component reaches at boot (RConnection, DNS, a TCP GET)"},
	{"ntp-server", "NTP_SERVER", "<host>", "the SNTP server (default pool.ntp.org)"},
	{"fonts", "FONTS", "ubuntu|dejavu|gdr", "the UI font set (default ubuntu)"},
	{"webfonts", "WEBFONTS", "0|1", "the web fonts (CJK subset); 0 leaves them out"},
	{"themes", "THEMES", "0|1", "the extra 2010 themes (Haze, Jing); 0 leaves them out"},
	{"ui-renderer", "UI_RENDERER", "classic|alfred|both", "the UI render path(s) (default: both on NGA boards, classic elsewhere)"},
	{"shells", "SHELLS", "s3|s4|both", "the UI shell(s): Symbian^3, the Symbian^4 home screen (Qt 4.8 + Hb), or both"},
	{"nga", "NGA", "0|1", "the NGA window server (composition through OpenWF-C)"},
	{"nga-keep", "NGA_KEEP", "<component>,...", "keep these non-NGA twins in an NGA image (bisecting)"},
	{"alf", "ALF", "<stage>", "the Alfred (ALF) set at a bring-up stage"},
	{"phetty", "PHETTY", "on|off|exclude", "the developer terminal daemon (phettyd, pheshell, pheterm)"},
	{"utc-offset", "UTC_OFFSET", "<minutes>", "the home time zone, minutes from UTC (180 = UTC+3)"},
	{"ssm-test", "SSM_TEST", "<component>", "start this test component at boot (as ssmtest.exe)"},
	{"rom-tail", "ROM_TAIL", "<component>,...", "put these images last in the ROM (a delta push then sends only them)"},
	{"rom-exclude", "ROM_EXCLUDE", "<component>,...", "leave these out of the ROM (they ship as SIS packages)"},
	{"rom-data", "ROM_DATA_EXTRA", "<dest>=<src> ...", "extra Z: files, dest relative to Z:\\"},
	{"cenrep", "CENREP_EXTRA", "<path>/<uid>.txt ...", "central repository files that override the built ones"},
	{"rom-heap-limits", "ROM_HEAP_LIMITS", "0|1", "the Phorepoc heap and stack table; 0 = each image's own values"},
	{"secondary", "SECONDARY_IMAGE", "<image>", "the first user process the kernel starts (default efile.exe)"},
	{"s4", "PHOREPOC_S4", "0|1", "build the Symbian^4 components in their own tree"},
	{"qt48", "PHOREPOC_QT48", "0|1", "register Qt 4.8 as installed (its ROM stub)"},
	{"icons", "MIF_ALL", "0|1", "generate every component's icon files before the ROM (default 1)"},
	{"iby-data", "IBY_DATA", "0|1", "the data files the packages' .iby files list (default 1)"},
	{"fast", "FAST", "0|1", "keep the core images (euser, efsrv, hal, efile, estart) from the last build"},
	{"prune", "PRUNE_UNRESOLVED", "0|1", "drop images whose imports are not in the ROM instead of failing"},
	{"leave-trace", "LEAVE_TRACE", "0|1", "log each User::Leave with its caller (diagnostic)"},
	{"sched-trace", "SCHED_TRACE", "0|1", "the active scheduler with a dispatch log (diagnostic)"},
	{"trace", "TRACE", "<hex>", "kernel recipes: the kernel's trace mask (default 0x80000000: panics only)"},
	{"screen", "SCREEN", "<w>x<h>", "kernel recipes: the screen the display HAL reports (default 360x640)"},
	{"scale", "SCALE", "n", "kernel recipes: show each screen pixel as an n x n block (default 2)"},
	{"dbg", "DBG", "0|1", "kernel recipes: a debug kernel (default 1)"},
	{"kclean", "KCLEAN", "0|1", "kernel recipes: rebuild the kernel objects even when nothing changed"},
	{"x32-ld", "X32_LD", "bfd", "force GNU ld for the x32 links"},
	{"x32-defs", "X32_DEFS", "<component>:<MACRO>,...", "extra defines for some components (diagnostic)"},
	{"x32-include", "X32_INCLUDE", "<component>:<header>,...", "force-include a header in some components (diagnostic)"},
	{"x32-fullerr", "X32_FULLERR", "0|1", "the whole compiler output of the first failure"},
}

// SetImageOption applies `-O name=value`.
func SetImageOption(nv string) error {
	name, value, ok := strings.Cut(nv, "=")
	if !ok {
		return fmt.Errorf("-O %s: want <name>=<value> (\"ph help image-options\" lists them)", nv)
	}
	for _, o := range ImageOptions {
		if o.Name == name {
			return os.Setenv(o.Env, value)
		}
	}
	return fmt.Errorf("-O %s: no image option %q (\"ph help image-options\" lists them)", nv, name)
}

// Release runs the board's RELEASE recipe on this machine (board.conf, docs/layering.md) with the PHOREPOC_*
// variables of this build and OUT, the folder for the release files (default <build>/release).
func (e *Env) Release(board, out string) error {
	b, err := e.FindBoard(board)
	if err != nil {
		return err
	}
	if b.Release == "" {
		return fmt.Errorf("board %s has no RELEASE recipe in its board.conf", b.Name)
	}
	if out == "" {
		out = filepath.Join(e.Build, "release")
	}
	if abs, err := filepath.Abs(out); err == nil {
		out = abs
	}
	c := exec.Command(bashPath(), b.Release)
	c.Dir = b.Dir
	c.Env = append(os.Environ(), "PHOREPOC_PDK="+e.PDK, "PHOREPOC_CORE="+e.Core, "PHOREPOC_SYMBIAN="+e.Symbian,
		"PHOREPOC_BUILD="+e.Build, "PHOREPOC_ARCH="+b.Arch, "PHOREPOC_BOARD="+b.Name, "OUT="+out,
		"PHOREPOC_ADAPTATIONS="+strings.Join(e.Adaptations, string(os.PathListSeparator)))
	return stdio(c).Run()
}
