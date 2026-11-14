// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Phorepoc/pdk/lib/cli"
	"github.com/Phorepoc/pdk/lib/client"
	"github.com/Phorepoc/pdk/lib/store"
)

// deviceDataCommands: the PhoneLab service's stored record (boots, events, operation history, blobs) and the
// device's files. Every one is a call on lib/client, the same REST routes the web page and the MCP tools use.
func deviceDataCommands() []*cli.Command {
	return []*cli.Command{
		{
			Group: "device", Name: "list",
			Summary:     "List the service's devices",
			Description: "Every device the PhoneLab service has: id, driver, link state, current boot.",
			Flags:       with(nil),
			Examples:    []cli.Example{{What: "Which devices does the service have?", Cmd: "device list"}},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				ds, err := client.New(str(fs, "url"), str(fs, "token")).Devices()
				if err != nil {
					return err
				}
				table(false, []string{"DEVICE", "DRIVER", "LINK", "BOOT"}, func(add func(...string)) {
					for _, d := range ds {
						add(d.ID, d.Driver, d.Health.State, fmt.Sprint(d.Health.Boot))
					}
				})
				return nil
			},
		},
		{
			Group: "device", Name: "boots",
			Summary:     "List the device's recent boots",
			Description: "Recent boots, newest first: id, start time, slot, outcome, number of lines and faults.",
			Flags: with(func(fs *flag.FlagSet) {
				fs.Int("n", 20, "at most this many `boots`")
			}),
			Examples: []cli.Example{{What: "The last five boots:", Cmd: "device boots -n 5"}},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				c, dev, err := device(fs)
				if err != nil {
					return err
				}
				n, _ := strconv.Atoi(str(fs, "n"))
				bs, err := c.Boots(dev, 0, n)
				if err != nil {
					return err
				}
				table(false, []string{"BOOT", "STARTED", "SLOT", "OUTCOME", "LINES", "FAULTS", "PINNED"}, func(add func(...string)) {
					for _, b := range bs {
						add(fmt.Sprint(b.ID), time.UnixMilli(b.Started).Format("2006-01-02 15:04:05"), b.Slot, b.Outcome,
							fmt.Sprint(b.Lines), fmt.Sprint(b.Faults), fmt.Sprint(b.Pinned))
					}
				})
				return nil
			},
		},
		{
			Group: "device", Name: "events",
			Summary:     "Show the structured events parsed from the log",
			Description: "Boot starts, faults, panics, upload verdicts, link changes, monitor samples. -k picks kinds (comma list; a trailing dot is a prefix).",
			Flags: with(func(fs *flag.FlagSet) {
				fs.String("k", "", "only these event `kinds`, e.g. fault,panic or mon.")
				fs.Int64("b", 0, "only this `boot` (default: all)")
				fs.Int("n", 100, "at most this many `events`")
			}),
			Examples: []cli.Example{{What: "Faults and panics of boot 1095:", Cmd: "device events -k fault,panic -b 1095"}},
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
				es, err := c.Events(dev, str(fs, "k"), boot, n)
				if err != nil {
					return err
				}
				for _, e := range es {
					fmt.Printf("%d  b%d  %s  %s  %s\n", e.ID, e.Boot, time.UnixMilli(e.TS).Format("15:04:05.000"), e.Kind, e.Data)
				}
				return nil
			},
		},
		{
			Group: "device", Name: "history",
			Summary:     "Show the operations run on the device",
			Description: "The service's record of every operation (by the CLI, the API, MCP or the web page): when, who, which, how long, and its error.",
			Flags: with(func(fs *flag.FlagSet) {
				fs.Int("n", 20, "at most this many `operations`")
			}),
			Examples: []cli.Example{{What: "The last ten operations:", Cmd: "device history -n 10"}},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				c, dev, err := device(fs)
				if err != nil {
					return err
				}
				n, _ := strconv.Atoi(str(fs, "n"))
				ops, err := c.History(dev, 0, n)
				if err != nil {
					return err
				}
				table(false, []string{"TIME", "CALLER", "OPERATION", "MS", "ERROR"}, func(add func(...string)) {
					for _, o := range ops {
						add(time.UnixMilli(o.TS).Format("2006-01-02 15:04:05"), o.Caller, o.Op, fmt.Sprint(o.MS), o.Error)
					}
				})
				return nil
			},
		},
		{
			Group: "device", Name: "ls", Operands: "[<path>]",
			Summary:     "List a directory on the device",
			Description: "The device's file service (the nokia driver, or a phlink image that offers fs_list). Default path: /.",
			Flags:       with(nil),
			Examples:    []cli.Example{{What: "The phone's C: drive:", Cmd: "device ls C:/"}},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) > 1 {
					return cli.Usagef("at most one path")
				}
				p := "/"
				if len(args) == 1 {
					p = args[0]
				}
				c, dev, err := device(fs)
				if err != nil {
					return err
				}
				l, err := c.FsList(dev, p)
				if err != nil {
					return err
				}
				return printJSON(l)
			},
		},
		{
			Group: "device", Name: "pull", Operands: "<device-path> [<file>]",
			Summary:     "Copy a file from the device",
			Description: "Downloads a file through the service. Default local name: the file's own name in the current directory.",
			Flags:       with(nil),
			Examples:    []cli.Example{{What: "Fetch a log file:", Cmd: "device pull C:/data/boot.log"}},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) < 1 || len(args) > 2 {
					return cli.Usagef("a device path and optionally a local file")
				}
				out := path.Base(filepath.ToSlash(args[0]))
				if len(args) == 2 {
					out = args[1]
				}
				c, dev, err := device(fs)
				if err != nil {
					return err
				}
				b, err := c.FsRead(dev, args[0])
				if err != nil {
					return err
				}
				if err := os.WriteFile(out, b, 0o644); err != nil {
					return err
				}
				fmt.Printf("%s  (%d bytes)\n", out, len(b))
				return nil
			},
		},
		{
			Group: "device", Name: "put", Operands: "<file> <device-path>",
			Summary:     "Copy a file to the device",
			Description: "Uploads a local file through the service. The device's file service decides where writing is allowed.",
			Flags:       with(nil),
			Examples:    []cli.Example{{What: "Send a test file:", Cmd: "device put hello.txt E:/hello.txt"}},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) != 2 {
					return cli.Usagef("a local file and a device path")
				}
				f, err := os.Open(args[0])
				if err != nil {
					return err
				}
				defer f.Close()
				c, dev, err := device(fs)
				if err != nil {
					return err
				}
				n, err := c.FsWrite(dev, args[1], f)
				if err != nil {
					return err
				}
				fmt.Printf("%s  (%d bytes)\n", args[1], n)
				return nil
			},
		},
		{
			Group: "device", Name: "rm", Operands: "<device-path>",
			Summary:     "Delete a file on the device",
			Description: "Deletes one file through the service (only where the device's file service allows).",
			Flags:       with(nil),
			Examples:    []cli.Example{{What: "Remove a test file:", Cmd: "device rm E:/hello.txt"}},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) != 1 {
					return cli.Usagef("one device path")
				}
				c, dev, err := device(fs)
				if err != nil {
					return err
				}
				return c.FsDelete(dev, args[0])
			},
		},
		{
			Group: "device", Name: "blobs",
			Summary:     "List the service's stored blobs",
			Description: "Uploaded images and captured screenshots, by their sha256: what \"device push\" and \"device shot\" stored.",
			Flags: with(func(fs *flag.FlagSet) {
				fs.String("k", "", "only this `kind` (image, screenshot ...)")
				fs.Int("n", 50, "at most this many `blobs`")
			}),
			Examples: []cli.Example{{What: "The uploaded images:", Cmd: "device blobs -k image"}},
			Run: func(fs *flag.FlagSet, args []string) error {
				if err := noArgs(args); err != nil {
					return err
				}
				n, _ := strconv.Atoi(str(fs, "n"))
				bs, err := client.New(str(fs, "url"), str(fs, "token")).Blobs(str(fs, "k"), n)
				if err != nil {
					return err
				}
				table(false, []string{"SHA256", "KIND", "NAME", "SIZE", "PINNED"}, func(add func(...string)) {
					for _, b := range bs {
						add(b.SHA, b.Kind, b.Name, fmt.Sprint(b.Size), fmt.Sprint(b.Pinned))
					}
				})
				return nil
			},
		},
		{
			Group: "device", Name: "upload", Operands: "<file>",
			Summary:     "Store a file in the service's blob store",
			Description: "Uploads a local file and prints its blob record (sha256). \"device push -B <sha256>\" boots an uploaded image.",
			Flags: with(func(fs *flag.FlagSet) {
				fs.String("k", "image", "the blob `kind`")
			}),
			Examples: []cli.Example{{What: "Upload an image to a remote service:", Cmd: "device upload build/rom/phone.img"}},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) != 1 {
					return cli.Usagef("one file")
				}
				b, err := uploadFile(fs, args[0], str(fs, "k"))
				if err != nil {
					return err
				}
				return printJSON(b)
			},
		},
		{
			Group: "device", Name: "pin", Operands: "boot|blob <id>",
			Summary:     "Keep a boot or a blob past the retention period",
			Description: "Pinned boots and blobs are not pruned. -u releases the pin. A boot is named by its number, a blob by its sha256.",
			Flags: with(func(fs *flag.FlagSet) {
				fs.Bool("u", false, "release the pin instead")
			}),
			Examples: []cli.Example{{What: "Keep the good boot:", Cmd: "device pin boot 1095"}},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) != 2 || (args[0] != "boot" && args[0] != "blob") {
					return cli.Usagef("boot|blob and an id")
				}
				return client.New(str(fs, "url"), str(fs, "token")).Pin(args[0], args[1], !on(fs, "u"))
			},
		},
	}
}

// uploadFile stores a local file in the service's blob store.
func uploadFile(fs *flag.FlagSet, file, kind string) (store.Blob, error) {
	f, err := os.Open(file)
	if err != nil {
		return store.Blob{}, err
	}
	defer f.Close()
	return client.New(str(fs, "url"), str(fs, "token")).PutBlob(kind, filepath.Base(file), str(fs, "d"), f)
}
