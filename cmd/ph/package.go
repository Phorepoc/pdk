// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Phorepoc/pdk/lib/cli"
	"github.com/Phorepoc/pdk/lib/pkgindex"
)

// indexFlags: which indexes a package command reads.
func indexFlags(fs *flag.FlagSet) {
	fs.Var(&multi{}, "index", "read this index (a `source`: URL or file) before the default ones; repeatable")
	fs.Bool("no-default", false, "do not read the Phorepoc index")
}

// indexes: -index ones first, then $PHOREPOC_INDEXES (separated by spaces), then the Phorepoc index -- an id in an
// earlier index wins.
func indexes(fs *flag.FlagSet) []string {
	srcs := append([]string{}, *fs.Lookup("index").Value.(*multi)...)
	srcs = append(srcs, strings.Fields(os.Getenv("PHOREPOC_INDEXES"))...)
	if !on(fs, "no-default") {
		srcs = append(srcs, pkgindex.DefaultIndex)
	}
	return srcs
}

func loadItems(fs *flag.FlagSet) (*pkgindex.Client, []pkgindex.Item, error) {
	srcs := indexes(fs)
	if len(srcs) == 0 {
		return nil, nil, cli.Usagef("no index to read (-index, $PHOREPOC_INDEXES, or drop -no-default)")
	}
	c := pkgindex.New()
	items, err := c.LoadAll(context.Background(), srcs)
	return c, items, err
}

func packageCommands() []*cli.Command {
	return []*cli.Command{
		{
			Group: "package", Name: "list", Operands: "[<word>...]",
			Summary: "List the installable items of the indexes",
			Description: "One line per item of the indexes: id, category, the newest approved release, the " +
				"architectures and a summary. Words filter on the id, name and summary.\n\n" +
				"The indexes are read in order: those given with -index, then $PHOREPOC_INDEXES, then the Phorepoc " +
				"index (" + pkgindex.DefaultIndex + "); an id listed by an earlier index wins.",
			Flags: func(fs *flag.FlagSet) {
				fs.String("c", "", "only this `category`: app, game, driver, library, codec, theme, tool, system")
				fs.String("a", "", "only items for this `architecture`: arm or x32")
				fs.String("b", "", "only items for this `board`")
				indexFlags(fs)
			},
			Examples: []cli.Example{
				{What: "Everything for the PinePhone:", Cmd: "package list -a arm -b pinephone"},
				{What: "Games, from a second index too:", Cmd: "package list -c game -index https://example.org/index.json"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				_, items, err := loadItems(fs)
				if err != nil {
					return err
				}
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				fmt.Fprintln(w, "ID\tCATEGORY\tRELEASE\tARCH\tSUMMARY")
				for i := range items {
					it := &items[i]
					if c := str(fs, "c"); c != "" && it.Category != c {
						continue
					}
					if !it.Fits(str(fs, "a"), str(fs, "b")) || !matches(it, args) {
						continue
					}
					tag := "-"
					if r := it.Latest(); r != nil {
						tag = r.Tag
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", it.ID, it.Category, tag, strings.Join(it.Compatibility.Arch, ","), it.Summary)
				}
				return w.Flush()
			},
		},
		{
			Group: "package", Name: "info", Operands: "<id>",
			Summary: "Show an item: description, compatibility, approved releases",
			Description: "Prints everything the index says about the item: name, licence, homepage, which devices it " +
				"installs on, what it requires, its description, and every approved release with its file, size and " +
				"SHA-256.",
			Flags: indexFlags,
			Examples: []cli.Example{
				{What: "About the codec pack:", Cmd: "package info codecpack"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) != 1 {
					return cli.Usagef("which item?")
				}
				_, items, err := loadItems(fs)
				if err != nil {
					return err
				}
				it, err := pkgindex.Find(items, args[0])
				if err != nil {
					return err
				}
				fmt.Printf("%s -- %s\n\n", it.ID, it.Name)
				fmt.Printf("    %s\n\n", it.Summary)
				fmt.Printf("    category    %s\n    licence     %s\n", it.Category, it.License)
				if it.Homepage != "" {
					fmt.Printf("    homepage    %s\n", it.Homepage)
				}
				c := it.Compatibility
				fmt.Printf("    arch        %s\n", strings.Join(c.Arch, ", "))
				if len(c.Boards) > 0 {
					fmt.Printf("    boards      %s\n", strings.Join(c.Boards, ", "))
				}
				if c.MinCore != "" {
					fmt.Printf("    min core    %s\n", c.MinCore)
				}
				if c.Legacy {
					fmt.Printf("    legacy      also runs on stock Symbian OS 9.x phones\n")
				}
				if c.NeedsPlatsecHack {
					fmt.Printf("    note        installs only on a phone whose platform security is patched\n")
				}
				if len(it.Requires) > 0 {
					fmt.Printf("    requires    %s\n", strings.Join(it.Requires, ", "))
				}
				fmt.Printf("    index       %s\n", it.Index)
				if it.Description != "" {
					fmt.Printf("\n%s\n", indent(strings.TrimSpace(it.Description), "    "))
				}
				fmt.Printf("\nRELEASES\n")
				for _, r := range it.Resolved {
					fmt.Printf("    %-12s %s  %d bytes\n                 sha256 %s\n", r.Tag, r.Asset, r.Size, r.SHA256)
				}
				return nil
			},
		},
		{
			Group: "package", Name: "fetch", Operands: "<id>[@<tag>]...",
			Summary: "Download approved releases into the package cache",
			Description: "Downloads each item's newest approved release (or the tagged one) into the package cache " +
				"and checks its SHA-256 against the index; a file that does not match is not kept. A file already in " +
				"the cache with the right hash counts as downloaded, so a cache prefilled from an SD card or USB " +
				"stick installs offline.\n\n" +
				"The cache: -o, else $PHOREPOC_PACKAGE_CACHE, else the user's cache directory (phorepoc/packages). " +
				"Prints each file's path.",
			Flags: func(fs *flag.FlagSet) {
				fs.String("o", "", "the cache `directory`")
				indexFlags(fs)
			},
			Examples: []cli.Example{
				{What: "The newest codec pack:", Cmd: "package fetch codecpack"},
				{What: "A particular release, into a folder:", Cmd: "package fetch -o sd/packages tls@v1.0"},
			},
			Run: func(fs *flag.FlagSet, args []string) error {
				if len(args) == 0 {
					return cli.Usagef("which items?")
				}
				c, items, err := loadItems(fs)
				if err != nil {
					return err
				}
				dir := str(fs, "o")
				if dir == "" {
					dir = pkgindex.CacheDir()
				}
				for _, a := range args {
					id, tag, _ := strings.Cut(a, "@")
					it, err := pkgindex.Find(items, id)
					if err != nil {
						return err
					}
					rel, err := it.Release(tag)
					if err != nil {
						return err
					}
					p, err := c.Fetch(context.Background(), rel, dir)
					if err != nil {
						return err
					}
					abs, _ := filepath.Abs(p)
					fmt.Printf("%s %s -> %s\n", it.ID, rel.Tag, abs)
				}
				return nil
			},
		},
		installCommand(),
	}
}

// installCommand: fetch, upload through PhoneLab, run pheinstall in a device shell, wait for its log line.
func installCommand() *cli.Command {
	return &cli.Command{
		Group: "package", Name: "install", Operands: "<id>[@<tag>]",
		Summary: "Install an item on a device through PhoneLab",
		Description: "Fetches the release as \"package fetch\" does (SHA-256 checked), uploads it to the device's " +
			"C:\\Data\\Installs, and runs pheinstall in a device shell (the phetty daemon), which installs it " +
			"silently through the software installer. Waits for pheinstall's log line and reports the result.\n\n" +
			"Needs a device whose image carries pheinstall, phfs (files) and phettyd (shells).",
		Flags: with(func(fs *flag.FlagSet) {
			fs.String("drive", "c", "install to this `drive`")
			fs.String("o", "", "the cache `directory` (as for fetch)")
			fs.Int("wait", 180, "give up after this many `seconds`")
			indexFlags(fs)
		}),
		Examples: []cli.Example{
			{What: "Install the newest release:", Cmd: "package install hello"},
			{What: "A release, to the memory card:", Cmd: "package install -drive e hello@v1.0"},
		},
		Run: func(fs *flag.FlagSet, args []string) error {
			if len(args) != 1 {
				return cli.Usagef("which item?")
			}
			pc, items, err := loadItems(fs)
			if err != nil {
				return err
			}
			id, tag, _ := strings.Cut(args[0], "@")
			it, err := pkgindex.Find(items, id)
			if err != nil {
				return err
			}
			rel, err := it.Release(tag)
			if err != nil {
				return err
			}
			dir := str(fs, "o")
			if dir == "" {
				dir = pkgindex.CacheDir()
			}
			local, err := pc.Fetch(context.Background(), rel, dir)
			if err != nil {
				return err
			}
			c, dev, err := device(fs)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(local)
			if err != nil {
				return err
			}
			name := filepath.Base(local)
			remote := "/C:/Data/Installs/" + name
			const chunk = 3072 // what one fs_write takes
			for off := 0; off == 0 || off < len(data); off += chunk {
				end := off + chunk
				if end > len(data) {
					end = len(data)
				}
				if _, err := c.Op(dev, "fs_write", map[string]any{"path": remote, "offset": off,
					"data_b64": base64.StdEncoding.EncodeToString(data[off:end]), "truncate": off == 0}); err != nil {
					return fmt.Errorf("upload %s: %w", remote, err)
				}
				if end == len(data) {
					break
				}
			}
			fmt.Printf("uploaded %s (%d bytes)\n", remote, len(data))
			// pheinstall's log lines seen so far: the answer is a newer one naming this file
			last := int64(0)
			if lines, err := c.Lines(dev, "[pheinstall]", 0, 200); err == nil {
				for _, l := range lines {
					if l.ID > last {
						last = l.ID
					}
				}
			}
			if _, err := c.Op(dev, "tty_list", map[string]any{"start": true}); err != nil {
				return fmt.Errorf("device shell: %w", err)
			}
			raw, err := c.Op(dev, "tty_new", nil)
			if err != nil {
				return fmt.Errorf("device shell: %w", err)
			}
			var tty struct {
				ID int `json:"id"`
			}
			if err := json.Unmarshal(raw, &tty); err != nil {
				return fmt.Errorf("tty_new: %w", err)
			}
			defer c.Op(dev, "tty_kill", map[string]any{"id": tty.ID})
			cmd := fmt.Sprintf("pheinstall C:\\Data\\Installs\\%s %s\r", name, str(fs, "drive"))
			if _, err := c.Op(dev, "tty_write", map[string]any{"id": tty.ID, "text": cmd}); err != nil {
				return err
			}
			deadline := time.Now().Add(time.Duration(intFlag(fs, "wait")) * time.Second)
			for time.Now().Before(deadline) {
				time.Sleep(2 * time.Second)
				lines, err := c.Lines(dev, "[pheinstall]", 0, 50)
				if err != nil {
					continue
				}
				for _, l := range lines {
					if l.ID > last && strings.Contains(strings.ToLower(l.Text), strings.ToLower(name)) {
						fmt.Println(strings.TrimSpace(l.Text))
						if strings.Contains(l.Text, ": installed") {
							return nil
						}
						return fmt.Errorf("%s %s: not installed", it.ID, rel.Tag)
					}
				}
			}
			return fmt.Errorf("no answer from pheinstall within %ds (is it in the image?)", intFlag(fs, "wait"))
		},
	}
}

func intFlag(fs *flag.FlagSet, name string) int {
	n, _ := strconv.Atoi(str(fs, name))
	return n
}

func matches(it *pkgindex.Item, words []string) bool {
	hay := strings.ToLower(it.ID + " " + it.Name + " " + it.Summary)
	for _, w := range words {
		if !strings.Contains(hay, strings.ToLower(w)) {
			return false
		}
	}
	return true
}

func indent(s, pre string) string {
	return pre + strings.ReplaceAll(s, "\n", "\n"+pre)
}
