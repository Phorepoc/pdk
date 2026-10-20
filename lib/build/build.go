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
			"PHOREPOC_SYMBIAN="+e.Symbian, "PHOREPOC_BUILD="+e.Build)
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

// Commands is the build command group, as `ph help` lists it.
const Commands = ` SOURCE
  fetch                   the Symbian source: pinned commits of the Phorepoc forks -> <core>/symbian (core's fetch.sh, on the host)
  export [pkg...]         the epoc32/include tree from the bld.inf exports (tools/epoc32_export.py; --all = every package)
  casefix                 case-insensitive include links for the case-sensitive file system (tools/casefix.py)
  mbg [--all|pkg...]      the icon-id headers (.mbg) components include (tools/mbg.py; before build)

 BUILD
  build <key> [key...]    build components for x32 (tools/emitter/build_mmp_x32.py; keys = core's registry/components*.py)
  closure <key>           ... and everything it links against (--closure)
  all                     rebuild EVERY component, providers first (--all)
  arm <key> [key...]      build ARM images (tools/emitter/build_mmp.py)
  rsc <key> [key...]      compile a component's resources (tools/emitter/build_mmp.py --rsc)
  rom [ssm|@|@app]        build the ROM section (default ssm = the phone's real System State Manager boot; @ = bootui)
  stale                   images linked before a provider of theirs existed (need a relink)
  sis <key> [key...]      package components as an installable SIS (tools/make_sis.py)
  skin                    the default skin package (series60skin .skn + .mif; tools/skin_gen.py)
  icons                   the icon files (.mif) of every registered component (tools/mif_all.py)
  zdata                   the Z: data the packages' bld.inf files export (tools/zdata_gen.py)

 TEST
  tests [args]            byte-compare the images built here with another build (tests/compare_images.py)
  shell                   a shell in the container, with the mounts above

 OPTIONS (environment)
  PHOREPOC_CORE      the core checkout: registry, overlay, source (default: the core folder next to the pdk)
  PHOREPOC_OS_ROOT   our overlay, components and board files (default: <core>/os)
  PHOREPOC_SYMBIAN   the Symbian source: symbian-2010/ and symbian-rcl3/ (default: <core>/symbian)
  PHOREPOC_BUILD     all generated files, a directory or a Docker volume name (default: <core>/build)
  PHOREPOC_ADAPTATIONS  adaptation checkouts built with core (path list): their os/ is merged over core's, their
                     registry/ adds their components
  PHOREPOC_IMAGE     the toolchain image (default: ` + DefaultImage + `)
  NET=1 (comms stack)  FONTS=ubuntu|dejavu|gdr  UI_RENDERER=classic|alfred|both  SHELLS=s3|s4|both  FAST=1 (keep core images)
`

// IsCommand reports whether name is a build command.
func IsCommand(name string) bool {
	switch name {
	case "fetch", "export", "casefix", "mbg", "build", "closure", "all", "arm", "rsc", "rom", "stale", "sis", "skin", "icons",
		"zdata", "tests", "shell":
		return true
	}
	return false
}

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

// Run executes one build command.
func Run(cmd string, args []string) error {
	e, err := FromEnv()
	if err != nil {
		return err
	}
	py := func(script string, pre ...string) error {
		return e.InContainer(append(append([]string{"python3", script}, pre...), args...)...)
	}
	each := func(script string, pre ...string) error {
		for _, k := range args {
			if err := e.InContainer(append(append([]string{"python3", script}, pre...), k)...); err != nil {
				return err
			}
		}
		return nil
	}
	switch cmd {
	case "fetch":
		c := exec.Command(bashPath(), append([]string{filepath.Join(e.Core, "fetch.sh")}, args...)...)
		c.Dir = e.Core
		return stdio(c).Run()
	case "export":
		return py("tools/epoc32_export.py")
	case "casefix":
		return py("tools/casefix.py")
	case "mbg":
		return py("tools/mbg.py")
	case "build":
		return py("tools/emitter/build_mmp_x32.py")
	case "closure":
		return py("tools/emitter/build_mmp_x32.py", "--closure")
	case "all":
		return e.InContainer("python3", "tools/emitter/build_mmp_x32.py", "--all")
	case "arm":
		return each("tools/emitter/build_mmp.py")
	case "rsc":
		return each("tools/emitter/build_mmp.py", "--rsc")
	case "rom":
		boot := "ssm"
		if len(args) > 0 {
			boot = args[0]
		}
		return e.InContainer("bash", "-c", romScript, "_", boot)
	case "stale":
		return e.InContainer("python3", "tools/stale_stubs.py")
	case "sis":
		return py("tools/make_sis.py")
	case "skin":
		return py("tools/skin_gen.py")
	case "icons":
		return py("tools/mif_all.py")
	case "zdata":
		return py("tools/zdata_gen.py")
	case "tests":
		return py("tests/compare_images.py")
	case "shell":
		if inContainer() {
			return stdio(exec.Command("bash")).Run()
		}
		return e.InContainer("bash")
	}
	return fmt.Errorf("unknown command %q", cmd)
}
