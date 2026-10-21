// Copyright (c) Nokia Corporation and/or its subsidiary(-ies) (ThemeInstaller); Copyright The Phorepoc Authors (the Go port)
// SPDX-License-Identifier: EPL-1.0

package odt

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// odtWriter is ODTDataOutputStream: the little-endian primitives and the Symbian descriptor encodings.
type odtWriter struct{ bytes.Buffer }

func (w *odtWriter) int32(v int32)   { binary.Write(&w.Buffer, binary.LittleEndian, v) }
func (w *odtWriter) uint32(v uint32) { binary.Write(&w.Buffer, binary.LittleEndian, v) }
func (w *odtWriter) int16(v int)     { binary.Write(&w.Buffer, binary.LittleEndian, int16(v)) }
func (w *odtWriter) byte(v int)      { w.WriteByte(byte(v)) }
func (w *odtWriter) bool(v bool)     { w.WriteByte(byte(btoi(v))) }
func (w *odtWriter) real64(v float64) {
	binary.Write(&w.Buffer, binary.LittleEndian, math.Float64bits(v))
}

// cardinality is the compressed length prefix of a Symbian descriptor.
func (w *odtWriter) cardinality(length int, is8bit bool) {
	count := length * 2
	if is8bit {
		count++
	}
	switch {
	case count <= 127:
		w.byte(count << 1)
	case count <= 65535>>2:
		w.int16(count<<2 + 1)
	default:
		w.int32(int32(count<<3 + 3))
	}
}

func (w *odtWriter) string8(s string) {
	b := latin1Bytes(s)
	w.cardinality(len(b), true)
	w.Write(b)
}

func (w *odtWriter) string16(s string) error {
	b, err := scsuCompress(s)
	if err != nil {
		return err
	}
	w.cardinality(utf16Len(s), false)
	w.Write(b)
	return nil
}

// stringPool is StringPool: unique strings, referenced by index.
type stringPool struct {
	list []string
	idx  map[string]int
}

func (p *stringPool) add(s string) int {
	if p.idx == nil {
		p.idx = map[string]int{}
	}
	if i, ok := p.idx[s]; ok {
		return i
	}
	p.list = append(p.list, s)
	p.idx[s] = len(p.list) - 1
	return len(p.list) - 1
}

func (p *stringPool) bytes() []byte {
	w := &odtWriter{}
	w.int16(len(p.list))
	for _, s := range p.list {
		w.int16(utf16Len(s))
		w.string8(s)
	}
	return w.Bytes()
}

// Value types of a CSS property value in the ODT (ValueTypeResolver).
const (
	eUnknown   = 0
	eNumber    = 1
	eString    = 19
	eURI       = 20
	eIdent     = 21
	eAttr      = 22
	eRGBColor  = 25
	eUnitValue = 28
)

var sacToODT = map[int]int{
	sacInteger: eNumber, sacReal: eNumber, sacPercentage: 2, sacEm: 3, sacEx: 4, sacPixel: 5, sacCentimeter: 6,
	sacMillimeter: 7, sacInch: 8, sacPoint: 9, sacPica: 10, sacDegree: 11, sacRadian: 12, sacGradian: 13,
	sacMillisecond: 14, sacSecond: 15, sacHertz: 16, sacKilohertz: 17, sacDimension: eUnitValue,
	sacStringValue: eString, sacIdent: eIdent, sacURI: eURI, sacAttr: eAttr, sacRGBColor: eRGBColor,
}

type propertyValue struct {
	valueType int
	real      float64
	strRef    int
	identType int
	rgb       [3]int
}

type externalizer struct {
	pool   stringPool
	out    odtWriter
	nodeID int32
}

const (
	kindNode      = 0
	kindAttribute = 1
	kindProperty  = 2
)

func isPropertyElement(n *node) bool { return n.kind == elementNode && n.name == stringProperty }

func (e *externalizer) run(root *node) ([]byte, error) {
	if root != nil {
		e.out.bool(true)
		if err := e.node(root); err != nil {
			return nil, err
		}
	} else {
		e.out.bool(false)
	}
	return append(e.pool.bytes(), e.out.Bytes()...), nil
}

func (e *externalizer) node(n *node) error {
	e.out.int16(e.pool.add(n.name))
	e.out.int16(e.pool.add(n.ns)) // a missing namespace is added as ""
	isRef := false
	for _, a := range n.attrs {
		if a.name == "ref" {
			isRef = true
		}
	}
	e.out.bool(isRef)
	textFound := false
	var pc strings.Builder
	switch n.name {
	case "text", "desc", "marquee", "object", "newsticker":
		for _, c := range n.children {
			if c.kind == textNode {
				textFound = true
				pc.WriteString(c.text)
			}
		}
	}
	e.out.bool(textFound)
	if textFound {
		e.out.int16(utf16Len(pc.String()))
		e.out.string8(pc.String())
	}
	e.out.int32(e.nodeID)
	e.nodeID++

	// child nodes
	e.out.byte(kindNode)
	var kids, props []*node
	for _, c := range n.children {
		if c.kind != elementNode {
			continue
		}
		if isPropertyElement(c) {
			props = append(props, c)
		} else {
			kids = append(kids, c)
		}
	}
	e.out.int32(int32(len(kids)))
	for _, c := range kids {
		if err := e.node(c); err != nil {
			return err
		}
	}
	// attributes
	e.out.byte(kindAttribute)
	e.out.int32(int32(len(n.attrs)))
	for _, a := range n.attrs {
		e.out.int16(e.pool.add(a.name))
		e.out.int16(e.pool.add(a.value))
	}
	// properties
	e.out.byte(kindProperty)
	e.out.int32(int32(len(props)))
	for _, p := range props {
		if err := e.property(n, p); err != nil {
			return err
		}
	}
	return nil
}

func (e *externalizer) property(parent, p *node) error {
	var name, value string
	haveName, haveValue := false, false
	inherited := false
	var vals []*propertyValue
	for _, a := range p.attrs {
		if a.name == stringPseudoclass {
			continue
		}
		switch {
		case a.name == stringName && !haveName:
			name, haveName = a.value, true
		case strings.HasPrefix(a.name, stringValue):
			value, haveValue = a.value, true
			inherited = canInherit(parent, name, value)
			pv := &propertyValue{}
			if err := e.parseAttrValue(value, pv); err != nil {
				return err
			}
			vals = append(vals, pv)
		default:
			return fmt.Errorf("odt: error in DOM/style data externalization: property values")
		}
	}
	if !haveName || !haveValue {
		return fmt.Errorf("odt: error in DOM/style data externalization: property values")
	}
	e.out.int16(e.pool.add(name))
	e.out.byte(3) // property value list
	e.out.int32(int32(len(vals)))
	for _, v := range vals {
		if err := v.externalize(&e.out); err != nil {
			return err
		}
	}
	e.out.bool(inherited)
	pseudo := 0
	if pc, ok := p.attr(stringPseudoclass); ok {
		k, known := pseudoClassNames[pc]
		if !known {
			return fmt.Errorf("odt: error externalizing ODT/styles: unknown pseudo class")
		}
		pseudo = k
	}
	e.out.byte(pseudo)
	return nil
}

// parseAttrValue is DOMExternalizer.parseAttrValue: the "type|value" text back into a typed value.
func (e *externalizer) parseAttrValue(attr string, pv *propertyValue) error {
	i := strings.IndexByte(attr, '|')
	if i < 0 {
		return fmt.Errorf("odt: property value %q has no type", attr)
	}
	typ, err := strconv.Atoi(attr[:i])
	if err != nil {
		return fmt.Errorf("odt: property value %q: %v", attr, err)
	}
	value := attr[i+1:]
	vt, ok := sacToODT[typ]
	if !ok {
		return fmt.Errorf("odt: invalid value type while resolving CSS: %d", typ)
	}
	switch vt {
	case eIdent, eString, eURI, eAttr:
		pv.valueType = vt
		pv.strRef = e.pool.add(value)
		switch value {
		case "auto":
			pv.identType = 1
		case "inherit":
			pv.identType = 3
		case "none":
			pv.identType = 2
		}
	case eRGBColor:
		parts := strings.Split(value, ",")
		if len(parts) != 3 {
			return fmt.Errorf("odt: error in DOM/style data externalization: RGB Values: parameter amount")
		}
		for k, p := range parts {
			j := strings.IndexByte(p, '|')
			if j < 0 {
				return fmt.Errorf("odt: error in DOM/style data externalization: RGB Values: can't resolve value type")
			}
			t, _ := strconv.Atoi(p[:j])
			if sacToODT[t] != eNumber {
				return fmt.Errorf("odt: error in DOM/style data externalization: RGB Values: R, G or B not interger")
			}
			n, err := strconv.Atoi(p[j+1:])
			if err != nil || n < 0 || n > 255 {
				return fmt.Errorf("odt: bad RGB value %q", p)
			}
			pv.rgb[k] = n
		}
		pv.valueType = eRGBColor
	default: // numbers and measures
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("odt: property value %q: %v", attr, err)
		}
		pv.valueType = vt
		pv.real = f
	}
	return nil
}

func (v *propertyValue) externalize(w *odtWriter) error {
	w.byte(v.valueType)
	switch v.valueType {
	case eIdent, eString, eURI, eAttr, eUnknown:
		w.int16(v.strRef)
		w.byte(v.identType)
	case eRGBColor:
		w.byte(v.rgb[0])
		w.byte(v.rgb[1])
		w.byte(v.rgb[2])
	default:
		w.real64(v.real)
	}
	return nil
}
