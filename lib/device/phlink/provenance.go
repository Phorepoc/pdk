// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package phlink

import (
	"bytes"
	"fmt"
	"strings"
)

// Provenance is the build record an image carries: tools/make_phorepoc_rom.sh puts it into the ROM as
// z:\resource\versions\build.txt (plain text, so it can be found in the image bytes).
type Provenance struct {
	Board string            `json:"board"`
	Date  string            `json:"date"`
	Repos map[string]string `json:"repos"` // repository -> commit ("+dirty" = uncommitted changes in its checkout)
	Dirty []string          `json:"dirty,omitempty"`
	// Changes: per repository with uncommitted changes, what they were (the changed files and a hash of the diff), so a
	// push of an experiment says exactly what was on the phone
	Changes map[string]*Change `json:"changes,omitempty"`
	// Base: "current" (every pinned repository at or past the board's public pins) or why it was not checked
	Base string `json:"base,omitempty"`
}

// Change is one repository's uncommitted changes at build time.
type Change struct {
	Diff  string   `json:"diff"` // sha256 (12 hex) of `git diff HEAD`
	Files []string `json:"files"`
}

var provMagic = []byte("PHOREPOC-BUILD v1\n")

// ReadProvenance finds the build record in an image. An error = the image has none (built by something other than the
// pipeline: the stale private tree's images of 2026-10-28 lacked the OS fixes and nothing noticed).
func ReadProvenance(img []byte) (*Provenance, error) {
	i := bytes.Index(img, provMagic)
	if i < 0 {
		return nil, fmt.Errorf("the image carries no build record (resource\\versions\\build.txt): not built by the Phorepoc pipeline")
	}
	rest := img[i+len(provMagic):]
	end := bytes.Index(rest, []byte("\nEND"))
	if end < 0 || end > 32768 {
		return nil, fmt.Errorf("the image's build record is truncated")
	}
	p := &Provenance{Repos: map[string]string{}}
	for _, line := range strings.Split(string(rest[:end]), "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 2 && f[0] == "board":
			p.Board = f[1]
		case len(f) == 2 && f[0] == "date":
			p.Date = f[1]
		case len(f) == 3 && f[0] == "repo":
			p.Repos[f[1]] = f[2]
			if strings.HasSuffix(f[2], "+dirty") {
				p.Dirty = append(p.Dirty, f[1])
			}
		case len(f) == 3 && f[0] == "diff":
			p.change(f[1]).Diff = f[2]
		case len(f) == 3 && f[0] == "changed":
			c := p.change(f[1])
			c.Files = append(c.Files, f[2])
		}
	}
	if len(p.Repos) == 0 {
		return nil, fmt.Errorf("the image's build record lists no repositories")
	}
	return p, nil
}

func (p *Provenance) change(repo string) *Change {
	if p.Changes == nil {
		p.Changes = map[string]*Change{}
	}
	if p.Changes[repo] == nil {
		p.Changes[repo] = &Change{}
	}
	return p.Changes[repo]
}

// checkProvenance is the push gate. Refused: an image without a build record, and an image whose base is stale (a
// pinned repository behind the board's public pin: see freshness.go). Uncommitted changes on a current base are
// recorded (Provenance.Changes), not refused. unverified=true is only for an image that carries no record at all
// (kernel bring-up builds made outside the pipeline).
func checkProvenance(img []byte, unverified bool, src baseSource) (*Provenance, error) {
	p, err := ReadProvenance(img)
	if err != nil {
		if unverified {
			return nil, nil
		}
		return nil, fmt.Errorf("push refused: %v (unverified=true only for a kernel experiment built outside the pipeline)", err)
	}
	if err := checkBase(p, src); err != nil {
		return nil, fmt.Errorf("push refused: %v", err)
	}
	p.Base = "current"
	return p, nil
}

// withProvenance adds the build record to a push result (the op history keeps it: which commits went on the phone).
func withProvenance(res any, p *Provenance) any {
	switch r := res.(type) {
	case PushResult:
		r.Provenance = p
		return r
	case DeltaResult:
		r.Provenance = p
		return r
	}
	return res
}
