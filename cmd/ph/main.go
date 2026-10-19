// ph -- the Phorepoc platform development kit's command line. Device work goes through a running PhoneLab service
// (it owns the phone's link); build commands join here as the build tools move into the pdk library.
//
//	ph health                          service + device health
//	ph ops                             the device's operations
//	ph op <name> [key=value ...]       run any operation (values: numbers, true/false, else strings)
//	ph push <image> [-wait <regexp>]   upload into slot B, reboot, optionally wait for a boot line
//	ph log [-grep s] [-boot n] [-n N]  stored log lines
//	ph shot [-which 0|1|2] [-o file]   a screenshot as PNG
//
// Flags before the command: -url (default $PHONELAB_URL or http://localhost:8737), -token ($PHONELAB_TOKEN),
// -device (default: the only device).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Phorepoc/pdk/lib/client"
)

func main() {
	base := flag.String("url", envOr("PHONELAB_URL", "http://localhost:8737"), "PhoneLab service URL")
	token := flag.String("token", os.Getenv("PHONELAB_TOKEN"), "PhoneLab bearer token")
	dev := flag.String("device", "", "device id (default: the only device)")
	flag.Usage = usage
	flag.Parse()
	if flag.NArg() == 0 {
		usage()
		os.Exit(2)
	}
	c := client.New(*base, *token)
	if err := run(c, *dev, flag.Arg(0), flag.Args()[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ph:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: ph [-url URL] [-token T] [-device ID] <command> [args]

  health                          service + device health
  ops                             the device's operations
  op <name> [key=value ...]       run any operation
  push <image> [-wait <regexp>]   upload into slot B, reboot, optionally wait for a boot line
  log [-grep s] [-boot n] [-n N]  stored log lines
  shot [-which 0|1|2] [-o file]   a screenshot as PNG (default shot.png)
`)
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func run(c *client.Client, dev, cmd string, args []string) error {
	if dev == "" && cmd != "health" {
		h, err := c.Health()
		if err != nil {
			return err
		}
		if len(h.Devices) != 1 {
			return fmt.Errorf("%d devices: choose one with -device", len(h.Devices))
		}
		dev = h.Devices[0].Device
	}
	switch cmd {
	case "health":
		h, err := c.Health()
		if err != nil {
			return err
		}
		return printJSON(h)
	case "ops":
		ops, err := c.Ops(dev)
		if err != nil {
			return err
		}
		for _, o := range ops {
			fmt.Printf("%-12s %-9s %s\n", o.Name, o.Capability, o.Description)
		}
		return nil
	case "op":
		if len(args) == 0 {
			return fmt.Errorf("op: which operation?")
		}
		a, err := kv(args[1:])
		if err != nil {
			return err
		}
		return result(c.Op(dev, args[0], a))
	case "push":
		fs := flag.NewFlagSet("push", flag.ExitOnError)
		wait := fs.String("wait", "", "after the reboot, wait for a log line matching this")
		timeout := fs.Int("timeout", 240, "seconds to wait")
		mode := fs.String("mode", "", "auto | full | delta")
		img, rest := splitFirst(args)
		_ = fs.Parse(rest)
		if img == "" {
			return fmt.Errorf("push: which image?")
		}
		a := map[string]any{"file": img}
		if *mode != "" {
			a["mode"] = *mode
		}
		if err := result(c.Op(dev, "push", a)); err != nil {
			return err
		}
		if *wait != "" {
			return result(c.Op(dev, "wait_for", map[string]any{"pattern": *wait, "timeout_s": *timeout}))
		}
		return nil
	case "log":
		fs := flag.NewFlagSet("log", flag.ExitOnError)
		grep := fs.String("grep", "", "substring (case-insensitive)")
		boot := fs.Int64("boot", 0, "boot id (0 = all)")
		n := fs.Int("n", 200, "lines")
		_ = fs.Parse(args)
		lines, err := c.Lines(dev, *grep, *boot, *n)
		if err != nil {
			return err
		}
		for _, l := range lines {
			fmt.Printf("%d b%d %s\n", l.ID, l.Boot, l.Text)
		}
		return nil
	case "shot":
		fs := flag.NewFlagSet("shot", flag.ExitOnError)
		which := fs.Int("which", 2, "0 = OS screen, 1 = panel layer, 2 = both")
		out := fs.String("o", "shot.png", "output file")
		_ = fs.Parse(args)
		r, err := c.Op(dev, "screenshot", map[string]any{"which": *which})
		if err != nil {
			return err
		}
		var s struct{ Blob string }
		if err := json.Unmarshal(r, &s); err != nil || s.Blob == "" {
			return fmt.Errorf("screenshot: no image in %s", r)
		}
		png, err := c.Blob(s.Blob)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*out, png, 0o644); err != nil {
			return err
		}
		fmt.Println(*out)
		return nil
	}
	return fmt.Errorf("unknown command %q (ph -h)", cmd)
}

// splitFirst: the first non-flag argument, and the rest (flags may come before or after it).
func splitFirst(args []string) (string, []string) {
	for i, a := range args {
		if !strings.HasPrefix(a, "-") && (i == 0 || !strings.HasPrefix(args[i-1], "-") || strings.Contains(args[i-1], "=")) {
			return a, append(append([]string{}, args[:i]...), args[i+1:]...)
		}
	}
	return "", args
}

// kv parses key=value pairs: numbers (hex 0x.. too) and true/false become typed values.
func kv(args []string) (map[string]any, error) {
	m := map[string]any{}
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			return nil, fmt.Errorf("%q: want key=value", a)
		}
		switch {
		case v == "true" || v == "false":
			m[k] = v == "true"
		case strings.HasPrefix(v, "0x"):
			m[k] = v // the service parses hex strings itself (addresses)
		default:
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				m[k] = n
			} else {
				m[k] = v
			}
		}
	}
	return m, nil
}

func result(r json.RawMessage, err error) error {
	if err != nil {
		if e, ok := err.(*client.Error); ok && len(e.Result) > 0 {
			fmt.Println(string(e.Result))
		}
		return err
	}
	var v any
	if json.Unmarshal(r, &v) == nil {
		return printJSON(v)
	}
	fmt.Println(string(r))
	return nil
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
