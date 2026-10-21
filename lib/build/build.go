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
	"strings"
)

// DefaultImage is the toolchain image; PHOREPOC_IMAGE overrides it.
const DefaultImage = "ghcr.io/phorepoc/base:latest"

// passThrough lists the recipe options that are handed to the container when set.
var passThrough = []string{
	"NET", "NET_TEST", "NTP_SERVER", "FONTS", "UI_RENDERER", "SHELLS", "ALF", "NGA", "NGA_KEEP", "FAST",
	"LEAVE_TRACE", "SCHED_TRACE", "MIF_ALL", "IBY_DATA", "ROM_ARCH", "ROM_TAIL", "ROM_EXCLUDE", "SSM_TEST",
	"SECONDARY_IMAGE", "PHOREPOC_S4", "PHOREPOC_QT48", "PHETTY", "CENREP_EXTRA", "ROM_DATA_EXTRA", "UTC_OFFSET",
	"WEBFONTS", "THEMES", "X32_LD", "X32_DEFS", "X32_INCLUDE", "X32_FULLERR", "PRUNE_UNRESOLVED", "ROM_HEAP_LIMITS",
	"TRACE", "SCREEN", "SCALE", "DBG", "KCLEAN",
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

// Kernel builds the x86-64 kernel and the GRUB ISO that carries the ROM of "ph image rom" (<build>/x86/ekern).
func (e *Env) Kernel() error {
	return e.InContainer("bash", "tools/make_phorepoc_kernel.sh")
}

// Vbox packages the kernel ISO and a blank C: disk as a VirtualBox appliance; args are make_ova.py's options.
func (e *Env) Vbox(args ...string) error {
	return e.Python("tools/make_ova.py", args...)
}

// Shell opens an interactive shell in the toolchain container with the mounts.
func (e *Env) Shell() error {
	if inContainer() {
		return stdio(exec.Command("bash")).Run()
	}
	return e.InContainer("bash")
}

// Board is a device the pdk builds for: the adaptation checkouts that make it, and its architecture.
type Board struct {
	Name, Summary string
	Adaptations   []string // checkout folder names, next to core
	Arch          string   // "x32" or "arm"
}

// Boards are the boards ph knows by name (-b).
var Boards = []Board{
	{"pc", "QEMU, VirtualBox and real PCs: x86-64 with the x32 ABI", []string{"adaptation-x86-64", "adaptation-pc"}, "x32"},
	{"pinephone", "the PINE64 PinePhone: ARM, Allwinner A64", []string{"adaptation-a64", "adaptation-pinephone"}, "arm"},
}

// FindBoard returns the named board.
func FindBoard(name string) (*Board, error) {
	var names []string
	for i := range Boards {
		if Boards[i].Name == name {
			return &Boards[i], nil
		}
		names = append(names, Boards[i].Name)
	}
	return nil, fmt.Errorf("no board %q (boards: %s)", name, strings.Join(names, ", "))
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
	b, err := FindBoard(name)
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
	{"trace", "TRACE", "<hex>", "ph image kernel: the kernel's trace mask (default 0x80000000: panics only)"},
	{"screen", "SCREEN", "<w>x<h>", "ph image kernel: the screen the display HAL reports (default 360x640)"},
	{"scale", "SCALE", "n", "ph image kernel: show each screen pixel as an n x n block (default 2)"},
	{"dbg", "DBG", "0|1", "ph image kernel: a debug kernel (default 1)"},
	{"kclean", "KCLEAN", "0|1", "ph image kernel: rebuild the kernel objects even when nothing changed"},
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
