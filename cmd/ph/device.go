package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Phorepoc/pdk/lib/cli"
	"github.com/Phorepoc/pdk/lib/client"
)

// connFlags: how a device command reaches PhoneLab (the service that owns the phone's link).
func connFlags(fs *flag.FlagSet) {
	fs.String("url", envOr("PHONELAB_URL", "http://localhost:8737"), "the PhoneLab service `url` (default: $PHONELAB_URL, else http://localhost:8737)")
	fs.String("token", os.Getenv("PHONELAB_TOKEN"), "the service's bearer `token` (default: $PHONELAB_TOKEN)")
	fs.String("d", "", "the `device` id (default: the only device the service has)")
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// device connects and resolves the device id.
func device(fs *flag.FlagSet) (*client.Client, string, error) {
	c := client.New(str(fs, "url"), str(fs, "token"))
	dev := str(fs, "d")
	if dev == "" {
		h, err := c.Health()
		if err != nil {
			return nil, "", err
		}
		if len(h.Devices) != 1 {
			return nil, "", fmt.Errorf("the service has %d devices: name one with -d", len(h.Devices))
		}
		dev = h.Devices[0].Device
	}
	return c, dev, nil
}

func with(extra func(*flag.FlagSet)) func(*flag.FlagSet) {
	return func(fs *flag.FlagSet) {
		if extra != nil {
			extra(fs)
		}
		connFlags(fs)
	}
}

func bootID(s, what string) (int64, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, cli.Usagef("%s: a boot id, not %q", what, s)
	}
	return n, nil
}

func deviceCommands() []*cli.Command {
	return []*cli.Command{
		{
			Group: "device", Name: "status",
			Summary:     "Show the service's and the devices' health",
			Description: "The PhoneLab service's state and, per device: the link, the current boot, the slot, CPU and free RAM.",
			Flags:       with(nil),
			Aliases:     []string{"health"},
			Examples: []cli.Example{
				{What: "Is the phone connected, and which boot is it on?", Cmd: "device status"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				h, err := client.New(str(fs, "url"), str(fs, "token")).Health()
				if err != nil {
					return err
				}
				return printJSON(h)
			},
		},
		{
			Group: "device", Name: "ops",
			Summary:     "List the device's operations",
			Description: "Every operation the device's driver offers (\"ph device op\" runs one), with its capability and description.",
			Flags:       with(nil),
			Aliases:     []string{"ops"},
			Examples: []cli.Example{
				{What: "What can the phone do?", Cmd: "device ops"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				c, dev, err := device(fs)
				if err != nil {
					return err
				}
				ops, err := c.Ops(dev)
				if err != nil {
					return err
				}
				table(false, []string{"OPERATION", "CAPABILITY", "DESCRIPTION"}, func(add func(...string)) {
					for _, o := range ops {
						add(o.Name, o.Capability, o.Description)
					}
				})
				return nil
			},
		},
		{
			Group: "device", Name: "op", Operands: "<operation> [<key>=<value>...]",
			Summary: "Run any device operation",
			Description: "Runs one operation (\"ph device ops\" lists them) with its arguments. Values that look " +
				"like numbers or true/false are sent typed; 0x... stays a string (the service parses addresses).",
			Flags:   with(nil),
			Aliases: []string{"op"},
			Examples: []cli.Example{
				{What: "Read a register:", Cmd: "device op peek addr=0x01c20000 n=4"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) == 0 {
					return cli.Usagef("which operation?")
				}
				a, err := kv(args[1:])
				if err != nil {
					return cli.Usagef("%v", err)
				}
				c, dev, err := device(fs)
				if err != nil {
					return err
				}
				return result(c.Op(dev, args[0], a))
			},
		},
		{
			Group: "device", Name: "push", Operands: "<image>",
			Summary: "Install an image and boot it",
			Description: "Uploads the image into the device's test slot, reboots into it, waits for the new boot " +
				"(and for a log line, with -w), and prints the boot's report: faults, panics, test lines.",
			Flags: with(func(fs *flag.FlagSet) {
				fs.String("w", "", "after the reboot, wait for a log line matching this `regexp`")
				fs.Int("t", 240, "wait at most this many `seconds`")
				fs.String("m", "", "the upload `mode`: auto, full or delta (default: auto)")
			}),
			Aliases: []string{"push"},
			Examples: []cli.Example{
				{What: "Boot a new image and wait for the home screen:", Cmd: `device push build/rom/phone.img -w "Idle: ready"`},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) != 1 {
					return cli.Usagef("one image")
				}
				c, dev, err := device(fs)
				if err != nil {
					return err
				}
				wait := str(fs, "w")
				if wait == "" {
					wait = "." // the new boot's first line: still waits for it, and reports
				}
				a := map[string]any{"file": args[0], "wait": wait}
				if t, err := strconv.Atoi(str(fs, "t")); err == nil {
					a["wait_timeout_s"] = t
				}
				if m := str(fs, "m"); m != "" {
					a["mode"] = m
				}
				return result(c.Op(dev, "push", a))
			},
		},
		{
			Group: "device", Name: "input", Operands: "<script>",
			Summary: "Press keys, type, tap and swipe on the device",
			Description: "Runs a script of input steps, separated by ';' or new lines: key <name>, text <string>, " +
				"tap <x> <y>, long <x> <y>, swipe <x1> <y1> <x2> <y2>, wait <ms>, shot.",
			Flags:   with(nil),
			Aliases: []string{"input"},
			Examples: []cli.Example{
				{What: "Open the menu and the first application:", Cmd: `device input "key menu; wait 500; tap 60 120"`},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) == 0 {
					return cli.Usagef("which steps?")
				}
				c, dev, err := device(fs)
				if err != nil {
					return err
				}
				return result(c.Op(dev, "input", map[string]any{"script": strings.Join(args, " ")}))
			},
		},
		{
			Group: "device", Name: "shot",
			Summary: "Take a screenshot",
			Description: "Saves the device's screen as PNG. -w picks the layer: 0 = what the OS draws, 1 = the " +
				"panel layer, 2 = both as seen.",
			Flags: with(func(fs *flag.FlagSet) {
				fs.Int("w", 2, "the `layer`: 0, 1 or 2")
				fs.String("o", "shot.png", "the output `file`")
			}),
			Aliases: []string{"shot"},
			Examples: []cli.Example{
				{What: "The OS screen into home.png:", Cmd: "device shot -w 0 -o home.png"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				c, dev, err := device(fs)
				if err != nil {
					return err
				}
				which, _ := strconv.Atoi(str(fs, "w"))
				r, err := c.Op(dev, "screenshot", map[string]any{"which": which})
				if err != nil {
					return err
				}
				var s struct{ Blob string }
				if err := json.Unmarshal(r, &s); err != nil || s.Blob == "" {
					return fmt.Errorf("no image in the answer: %s", r)
				}
				png, err := c.Blob(s.Blob)
				if err != nil {
					return err
				}
				if err := os.WriteFile(str(fs, "o"), png, 0o644); err != nil {
					return err
				}
				fmt.Println(str(fs, "o"))
				return nil
			},
		},
		{
			Group: "device", Name: "log",
			Summary:     "Show stored log lines",
			Description: "The device's log as the service stored it: the newest lines, or those of one boot, optionally filtered.",
			Flags: with(func(fs *flag.FlagSet) {
				fs.String("g", "", "only lines containing this `text` (case-insensitive)")
				fs.Int64("b", 0, "only this `boot` (default: all)")
				fs.Int("n", 200, "at most this many `lines`")
			}),
			Aliases: []string{"log"},
			Examples: []cli.Example{
				{What: "The last panics of boot 1095:", Cmd: "device log -b 1095 -g panic"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				c, dev, err := device(fs)
				if err != nil {
					return err
				}
				boot, _ := strconv.ParseInt(str(fs, "b"), 10, 64)
				n, _ := strconv.Atoi(str(fs, "n"))
				lines, err := c.Lines(dev, str(fs, "g"), boot, n)
				if err != nil {
					return err
				}
				for _, l := range lines {
					fmt.Printf("%d  b%d  %s\n", l.ID, l.Boot, l.Text)
				}
				return nil
			},
		},
		{
			Group: "device", Name: "report", Operands: "[<boot>]",
			Summary:     "Show how a boot went",
			Description: "A boot's faults, panics and test lines, with symbols for fault addresses when -s names a symbol file. Default: the current boot.",
			Flags: with(func(fs *flag.FlagSet) {
				fs.String("s", "", "a symbol `file` for fault addresses")
			}),
			Aliases: []string{"report"},
			Examples: []cli.Example{
				{What: "The current boot:", Cmd: "device report"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) > 1 {
					return cli.Usagef("at most one boot")
				}
				a := map[string]any{}
				if len(args) == 1 {
					n, err := bootID(args[0], "report")
					if err != nil {
						return err
					}
					a["boot"] = n
				}
				if s := str(fs, "s"); s != "" {
					a["symbols"] = s
				}
				c, dev, err := device(fs)
				if err != nil {
					return err
				}
				return result(c.Op(dev, "boot_report", a))
			},
		},
		{
			Group: "device", Name: "diff", Operands: "<boot> [<boot>]",
			Summary:     "Show the log lines one boot has and another has not",
			Description: "Compares two boots' logs (the second defaults to the current boot): what changed after a push.",
			Flags:       with(nil),
			Aliases:     []string{"diff"},
			Examples: []cli.Example{
				{What: "What the current boot does differently from boot 1094:", Cmd: "device diff 1094"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) < 1 || len(args) > 2 {
					return cli.Usagef("one or two boots")
				}
				a := map[string]any{}
				for i, k := range []string{"a", "b"} {
					if i < len(args) {
						n, err := bootID(args[i], "diff")
						if err != nil {
							return err
						}
						a[k] = n
					}
				}
				c, dev, err := device(fs)
				if err != nil {
					return err
				}
				return result(c.Op(dev, "diff_boots", a))
			},
		},
	}
}

// kv parses key=value pairs: numbers and true/false become typed values; hex stays a string.
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
			m[k] = v
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
