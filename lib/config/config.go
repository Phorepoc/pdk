// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package config reads phonelab.toml.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

type Device struct {
	ID     string         `toml:"id"`
	Driver string         `toml:"driver"`
	Port   string         `toml:"port"` // explicit port; else matched by vid/pid
	VID    string         `toml:"vid"`
	PID    string         `toml:"pid"`
	Mock   string         `toml:"mock"` // a transcript file: run against the fake phone instead
	Opts   map[string]any `toml:"opts"`
}

type Config struct {
	Listen        string   `toml:"listen"`
	DataDir       string   `toml:"data_dir"`
	Token         string   `toml:"token"` // when set, API/WS/MCP-over-HTTP need "Authorization: Bearer <token>"
	RetentionDays int      `toml:"retention_days"`
	Workspace     string   `toml:"workspace"` // relative "file" op params resolve against this (default: the working directory)
	Devices       []Device `toml:"device"`
}

func Default() Config {
	return Config{
		Listen:  "0.0.0.0:8737",
		DataDir: DefaultDataDir(),
		Devices: []Device{{ID: "phone", Driver: "phlink", VID: "0E22", PID: "0666"}},
	}
}

func DefaultDataDir() string {
	if d := os.Getenv("PHONELAB_DATA"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if l := os.Getenv("LOCALAPPDATA"); l != "" {
			return filepath.Join(l, "phonelab")
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".local", "share", "phonelab")
	}
	return "phonelab-data"
}

// Load reads path (missing file = defaults).
func Load(path string) (Config, error) {
	c := Default()
	if path == "" {
		return c, nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	devs := c.Devices
	c.Devices = nil
	if _, err := toml.Decode(string(b), &c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	if len(c.Devices) == 0 {
		c.Devices = devs
	}
	if c.DataDir == "" {
		c.DataDir = DefaultDataDir()
	}
	seen := map[string]bool{}
	for _, d := range c.Devices {
		if d.ID == "" || d.Driver == "" {
			return c, fmt.Errorf("%s: every [[device]] needs id and driver", path)
		}
		if seen[d.ID] {
			return c, fmt.Errorf("%s: duplicate device id %q", path, d.ID)
		}
		seen[d.ID] = true
	}
	return c, nil
}
