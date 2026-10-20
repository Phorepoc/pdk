// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package pkgindex reads Phorepoc software indexes (the index.json the index repository publishes) and fetches the
// approved release files they point to, checking each file's SHA-256. It is the download app's logic, used by
// `ph package`; the Symbian app will speak the same format.
package pkgindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultIndex is the curated Phorepoc index; users add their own next to it.
const DefaultIndex = "https://phorepoc.github.io/index/index.json"

// Release is one approved release of an item, resolved by the index builder.
type Release struct {
	Tag       string `json:"tag"`
	Published string `json:"published"`
	Asset     string `json:"asset"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
}

// Compatibility says which devices an item installs on.
type Compatibility struct {
	Arch             []string `json:"arch"`
	Boards           []string `json:"boards,omitempty"`
	MinCore          string   `json:"min_core,omitempty"`
	Legacy           bool     `json:"legacy,omitempty"`
	NeedsPlatsecHack bool     `json:"needs_platsec_hack,omitempty"`
}

// Item is one listing of an index, with its resolved releases.
type Item struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Summary        string        `json:"summary"`
	Description    string        `json:"description,omitempty"`
	Category       string        `json:"category"`
	License        string        `json:"license"`
	Homepage       string        `json:"homepage,omitempty"`
	Authors        []string      `json:"authors,omitempty"`
	Compatibility  Compatibility `json:"compatibility"`
	Requires       []string      `json:"requires,omitempty"`
	IconURL        string        `json:"icon_url,omitempty"`
	ScreenshotURLs []string      `json:"screenshot_urls,omitempty"`
	Resolved       []Release     `json:"resolved"`

	// Index is the index this item came from (set when loading; not part of the file).
	Index string `json:"-"`
}

// Latest is the item's newest resolved release (by publication time), or nil.
func (it *Item) Latest() *Release {
	if len(it.Resolved) == 0 {
		return nil
	}
	best := &it.Resolved[0]
	for i := range it.Resolved[1:] {
		if r := &it.Resolved[i+1]; r.Published > best.Published {
			best = r
		}
	}
	return best
}

// Release returns the resolved release with that tag ("" = the latest).
func (it *Item) Release(tag string) (*Release, error) {
	if tag == "" {
		if r := it.Latest(); r != nil {
			return r, nil
		}
		return nil, fmt.Errorf("%s: no resolved release", it.ID)
	}
	for i := range it.Resolved {
		if it.Resolved[i].Tag == tag {
			return &it.Resolved[i], nil
		}
	}
	return nil, fmt.Errorf("%s: release %q is not approved by its index", it.ID, tag)
}

// Fits reports whether the item installs on a device of that architecture and board ("" = any).
func (it *Item) Fits(arch, board string) bool {
	if arch != "" && !contains(it.Compatibility.Arch, arch) {
		return false
	}
	if board != "" && len(it.Compatibility.Boards) > 0 && !contains(it.Compatibility.Boards, board) {
		return false
	}
	return true
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

type document struct {
	Format    int    `json:"format"`
	Generated string `json:"generated"`
	Items     []Item `json:"items"`
}

// Client loads indexes and fetches files.
type Client struct {
	HTTP *http.Client
}

// New returns a client with a sane timeout.
func New() *Client { return &Client{HTTP: &http.Client{Timeout: 5 * time.Minute}} }

func (c *Client) open(ctx context.Context, src string) (io.ReadCloser, error) {
	if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
		return os.Open(strings.TrimPrefix(src, "file://"))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "phorepoc-ph")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", src, resp.Status)
	}
	return resp.Body, nil
}

// Load reads one index (a URL or a local file).
func (c *Client) Load(ctx context.Context, src string) ([]Item, error) {
	r, err := c.open(ctx, src)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var doc document
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s: not an index: %w", src, err)
	}
	if doc.Format != 1 {
		return nil, fmt.Errorf("%s: index format %d, this ph reads 1", src, doc.Format)
	}
	for i := range doc.Items {
		doc.Items[i].Index = src
	}
	return doc.Items, nil
}

// LoadAll reads several indexes in order; an id listed by an earlier index wins (the user's own list comes first,
// the default last). Items are sorted by id.
func (c *Client) LoadAll(ctx context.Context, srcs []string) ([]Item, error) {
	seen := map[string]bool{}
	var out []Item
	for _, s := range srcs {
		items, err := c.Load(ctx, s)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if !seen[it.ID] {
				seen[it.ID] = true
				out = append(out, it)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Find returns the item with that id.
func Find(items []Item, id string) (*Item, error) {
	for i := range items {
		if items[i].ID == id {
			return &items[i], nil
		}
	}
	return nil, fmt.Errorf("%s: no such item in the indexes", id)
}

// Fetch downloads a release into dir (the package cache) unless a file with the right SHA-256 is already there --
// a prefilled cache (copied from an SD card or USB stick) counts as downloaded. Returns the file's path.
func (c *Client) Fetch(ctx context.Context, rel *Release, dir string) (string, error) {
	if rel.SHA256 == "" {
		return "", fmt.Errorf("%s: the index gives no SHA-256", rel.Asset)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, filepath.Base(rel.Asset))
	if sum, err := fileSHA256(dst); err == nil && sum == rel.SHA256 {
		return dst, nil
	}
	r, err := c.open(ctx, rel.URL)
	if err != nil {
		return "", err
	}
	defer r.Close()
	tmp, err := os.CreateTemp(dir, ".part-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), r); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != rel.SHA256 {
		return "", fmt.Errorf("%s: SHA-256 %s, the index says %s -- not kept", rel.Asset, got, rel.SHA256)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return "", err
	}
	return dst, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CacheDir is the package cache: $PHOREPOC_PACKAGE_CACHE, else <user cache dir>/phorepoc/packages.
func CacheDir() string {
	if d := os.Getenv("PHOREPOC_PACKAGE_CACHE"); d != "" {
		return d
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "phorepoc", "packages")
}
