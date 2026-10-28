// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

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
	if end < 0 || end > 8192 {
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
		}
	}
	if len(p.Repos) == 0 {
		return nil, fmt.Errorf("the image's build record lists no repositories")
	}
	return p, nil
}

// checkProvenance is the push gate: an image without a build record, or built from a checkout with uncommitted
// changes, is refused unless the caller passes unverified=true (kernel bring-up experiments).
func checkProvenance(img []byte, unverified bool) (*Provenance, error) {
	p, err := ReadProvenance(img)
	if unverified {
		return p, nil
	}
	if err != nil {
		return nil, fmt.Errorf("push refused: %v (pass unverified=true only for a deliberate experiment)", err)
	}
	if len(p.Dirty) > 0 {
		return nil, fmt.Errorf("push refused: built from uncommitted changes in %s (commit them, or pass unverified=true)",
			strings.Join(p.Dirty, ", "))
	}
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
