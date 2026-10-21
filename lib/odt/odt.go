// Copyright (c) Nokia Corporation and/or its subsidiary(-ies) (ThemeInstaller); Copyright The Phorepoc Authors (the Go port)
// SPDX-License-Identifier: EPL-2.0 OR GPL-2.0-or-later

// Package odt compiles the Symbian^3 homescreen configuration sources (Xuikon XML, CSS and a localisation DTD)
// into the binary "ODT" form (.o0000) the homescreen reads at run time. It is a Go port of the ThemeInstaller
// XML2ODT compiler (Nokia, EPL-1.0; this port EPL-2.0), so no Java is needed to build the configuration binaries.
//
// Ported: the manifest (.dat) reading, the XML parse with the DTD's entities, the CSS parse and its application
// to the XML (rule matching by specificity, inheritance), and the ODT writer. Not ported: language specific
// manifests (<LanguageSpecific>), bitmap resources (<FileResource>, which ThemeInstaller converts with bmconv),
// the localisation-settings composer, XInclude, and a DOCTYPE whose DTD is not supplied.
package odt

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Options are ThemeInstaller.prop settings.
type Options struct {
	ProviderName string // theme_provider_name
	NameSpace    string // theme_name_space
}

// DefaultOptions are the values of ThemeInstaller.prop.
func DefaultOptions() Options {
	return Options{ProviderName: "Nokia", NameSpace: "http://www.series60.com/xml/xmluiml/1"}
}

// Theme status flags (ThemeStatusResolver).
const (
	statusNone              = 0x0000
	statusLicenceeDefault   = 0x0002
	statusOperatorDefault   = 0x0004
	statusUserDefault       = 0x0008
	statusMakeActive        = 0x0010
	statusLicenceeRestoable = 0x0040
	statusLocked            = 0x0080
)

var themeStatus = map[string]int{
	"ThemeStatusNone": statusNone, "ThemeStatusLicenceeDefault": statusLicenceeDefault,
	"ThemeStatusOperatorDefault": statusOperatorDefault, "ThemeStatusUserDefault": statusUserDefault,
	"ThemeStatusMakeActive": statusMakeActive, "ThemeStatusLicenceeRestorable": statusLicenceeRestoable,
	"ThemeStatusLocked": statusLocked,
}

// Manifest is a theme manifest (.dat file).
type Manifest struct {
	AppUID, ProviderUID, ThemeUID uint32
	haveUIDs                      [3]bool
	FullName, ShortName, Version  string
	haveFull, haveShort, haveVer  bool
	Flags                         int
	XMLFile, CSSFile, DTDFile     string
	Dir                           string // the directory the files are named relative to
}

// ReadManifest parses a manifest file.
func ReadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	root, err := parseXML(data, nil)
	if err != nil {
		return nil, fmt.Errorf("theme manifest parsing failed: %w", err)
	}
	m := &Manifest{Dir: filepath.Dir(path)}
	one := func(tag string) (*node, error) {
		l := root.findAll(tag)
		if len(l) > 1 {
			return nil, fmt.Errorf("syntax error in manifest file, more child nodes than expected (%s)", tag)
		}
		if len(l) == 0 {
			return nil, nil
		}
		return l[0], nil
	}
	uid := func(tag string, dst *uint32, have *bool) error {
		n, err := one(tag)
		if err != nil || n == nil {
			return err
		}
		v, err := strconv.ParseUint(n.textContent(), 16, 32)
		if err != nil {
			return fmt.Errorf("manifest %s: %w", tag, err)
		}
		*dst, *have = uint32(v), true
		return nil
	}
	if err := uid("AppUid", &m.AppUID, &m.haveUIDs[0]); err != nil {
		return nil, err
	}
	if err := uid("Provideruid", &m.ProviderUID, &m.haveUIDs[1]); err != nil {
		return nil, err
	}
	if err := uid("ThemeUid", &m.ThemeUID, &m.haveUIDs[2]); err != nil {
		return nil, err
	}
	if l := root.findAll("ThemeFullName"); len(l) > 0 {
		m.FullName, m.haveFull = l[0].textContent(), true
	}
	if n, err := one("ThemeShortName"); err != nil {
		return nil, err
	} else if n != nil {
		m.ShortName, m.haveShort = n.textContent(), true
	}
	if n, err := one("ThemeVersion"); err != nil {
		return nil, err
	} else if n != nil {
		m.Version, m.haveVer = n.textContent(), true
	}
	for _, n := range root.findAll("ThemeStatus") {
		v, ok := themeStatus[n.textContent()]
		if !ok {
			return nil, fmt.Errorf("invalid theme status %q", n.textContent())
		}
		m.Flags |= v
		if n.textContent() == "ThemeStatusLicenceeRestorable" {
			m.Flags |= statusLicenceeDefault
		}
	}
	if n, err := one("FileXML"); err != nil {
		return nil, err
	} else if n != nil {
		m.XMLFile = n.textContent()
	}
	// FileCSS and FileDTD: at most one language independent
	if n, err := independent(root, "FileCSS"); err != nil {
		return nil, err
	} else if n != nil {
		m.CSSFile = n.textContent()
	}
	if n, err := independent(root, "FileDTD"); err != nil {
		return nil, err
	} else if n != nil {
		m.DTDFile = n.textContent()
	}
	if len(root.findAll("LanguageSpecific")) > 0 {
		return nil, fmt.Errorf("odt: language specific manifests (<LanguageSpecific>) are not supported")
	}
	if len(root.findAll("FileResource")) > 0 {
		return nil, fmt.Errorf("odt: resource files (<FileResource>) are not supported")
	}
	if m.XMLFile == "" {
		return nil, fmt.Errorf("manifest %s has no <FileXML>", path)
	}
	return m, nil
}

func independent(root *node, tag string) (*node, error) {
	var res *node
	count := 0
	for _, n := range root.findAll(tag) {
		if n.parent == nil || n.parent.name != "LanguageSpecific" {
			res = n
			count++
		}
	}
	if count > 1 {
		return nil, fmt.Errorf("syntax error in manifest file, more language independent child nodes than expected (%s)", tag)
	}
	return res, nil
}

// Compile builds the ODT for a manifest.
func Compile(m *Manifest, opt Options) (data []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
				return
			}
			panic(r)
		}
	}()
	xmlData, err := os.ReadFile(filepath.Join(m.Dir, m.XMLFile))
	if err != nil {
		return nil, err
	}
	var dtd []byte
	if m.DTDFile != "" {
		if dtd, err = os.ReadFile(filepath.Join(m.Dir, m.DTDFile)); err != nil {
			return nil, err
		}
	}
	root, err := parseXML(xmlData, dtd)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", m.XMLFile, err)
	}
	if m.CSSFile != "" {
		cssData, err := os.ReadFile(filepath.Join(m.Dir, m.CSSFile))
		if err != nil {
			return nil, err
		}
		a := &cssApplier{doc: root, baseDir: m.Dir}
		if err := parseStylesheet(latin1ToUTF8(cssData), a); err != nil {
			return nil, fmt.Errorf("%s: %w", m.CSSFile, err)
		}
		if err := a.finish(); err != nil {
			return nil, fmt.Errorf("%s: %w", m.CSSFile, err)
		}
	}
	return writeODT(m, opt, root)
}

// symbianPath is the Symbian file system path the ODT is installed under (the resource that names the ODT itself).
func symbianPath(m *Manifest) string {
	drive := "c"
	if m.Flags&statusLicenceeDefault != 0 {
		drive = "z"
	}
	var sb strings.Builder
	sb.WriteString(drive + `:\private\10207254\themes\`)
	if m.haveUIDs[0] && m.haveUIDs[1] && m.haveUIDs[2] && m.haveVer {
		sb.WriteString(strconv.FormatUint(uint64(m.AppUID), 10) + `\`)
		sb.WriteString(strconv.FormatUint(uint64(m.ProviderUID), 10) + `\`)
		sb.WriteString(strconv.FormatUint(uint64(m.ThemeUID), 10) + `\`)
		sb.WriteString(m.Version + `\`)
	}
	sb.WriteString(m.ShortName + ".o0000")
	return sb.String()
}

// OutputName is the file name ThemeInstaller gives the ODT.
func (m *Manifest) OutputName() string { return m.ShortName + ".o0000" }

func writeODT(m *Manifest, opt Options, root *node) ([]byte, error) {
	w := &odtWriter{}
	w.int32(268435511) // KWriteOnceFileStoreUid
	w.int32(0)
	w.int32(0)
	w.int32(73066445) // the "unknown media" UID
	w.int32(20)

	// header
	if m.haveUIDs[0] {
		w.uint32(m.AppUID)
	}
	if m.haveUIDs[1] {
		w.uint32(m.ProviderUID)
	}
	if m.haveUIDs[2] {
		w.uint32(m.ThemeUID)
	}
	for _, s := range []struct {
		have bool
		v    string
	}{{opt.ProviderName != "", opt.ProviderName}, {m.haveFull, m.FullName}, {m.haveShort, m.ShortName}, {m.haveVer, m.Version}} {
		if s.have {
			if err := w.string16(s.v); err != nil {
				return nil, err
			}
		}
	}
	w.int32(0) // screen size x
	w.int32(0) // screen size y
	w.int32(0) // language
	w.int32(int32(m.Flags))

	w.int16('#')
	w.int32(1) // one resource: the ODT itself
	locking := 0
	if m.Flags&statusLicenceeDefault != 0 {
		locking = 1
	}
	w.int32(int32(locking))
	w.int32(1) // cache file
	w.int32(2) // resource type: ODT
	if err := w.string16(m.ShortName); err != nil {
		return nil, err
	}
	if err := w.string16(opt.NameSpace); err != nil {
		return nil, err
	}
	if err := w.string16(symbianPath(m)); err != nil {
		return nil, err
	}
	w.string8("unknown")
	w.int32(0) // size
	w.int32(0) // offset

	ex := &externalizer{}
	body, err := ex.run(root)
	if err != nil {
		return nil, err
	}
	w.Write(body)
	return w.Bytes(), nil
}
