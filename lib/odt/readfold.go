// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package odt

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// readFileFold reads path like os.ReadFile, but when it does not exist it looks for the same name in another case, as
// the phone's file system is case-insensitive (a manifest naming "view.xml" for View.xml works there, and on Windows).
func readFileFold(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return data, err
	}
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	ents, derr := os.ReadDir(dir)
	if derr != nil {
		return nil, err
	}
	for _, e := range ents {
		if strings.EqualFold(e.Name(), base) {
			return os.ReadFile(filepath.Join(dir, e.Name()))
		}
	}
	return nil, err
}
