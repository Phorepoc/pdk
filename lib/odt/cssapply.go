// Copyright (c) Nokia Corporation and/or its subsidiary(-ies) (ThemeInstaller); Copyright The Phorepoc Authors (the Go port)
// SPDX-License-Identifier: EPL-2.0 OR GPL-2.0-or-later

package odt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// CSSHandler / CSSDOMProcessor / CSSMatchMaker: the style sheet is applied to the XML DOM by adding
// <styleproperty> child elements to every element a rule matches.

const (
	stringProperty    = "styleproperty"
	stringPseudoclass = "pseudoclass"
	stringName        = "name"
	stringValue       = "value"
	stringInherit     = "inherit"
)

var inheritableProperties = []string{"visibility", "block-progression", "direction", "color", "font-family",
	"font-size", "font-weight", "font-style", "_s60-tab-style", "_s60-tab-color"}

var pseudoClassNames = map[string]int{"none": 0, "focus": 1, "passivefocus": 2, "active": 3, "hold": 4, "enabled": 5,
	"disabled": 6, "hover": 7, "link": 8, "visited": 9, "edit": 10}

type cssPropertyValue struct {
	typ int
	val string
}

func (v cssPropertyValue) typeAndValue() string { return strconv.Itoa(v.typ) + "|" + v.val }

type cssRule struct {
	sel    *selector
	keys   []string // style map keys in java.util.HashMap iteration order
	props  map[string][]cssPropertyValue
	pseudo bool
	spec   [3]int // id, attribute (class/pseudo), element
}

type cssApplier struct {
	rules   []*cssRule
	doc     *node
	baseDir string
}

func (a *cssApplier) importStyle(file string) error {
	// An @import is parsed by a fresh handler that applies its rules to the document straight away (the
	// imported sheet's endDocument), before the main sheet's rules are.
	path := file
	if !isAbsPath(file) {
		path = filepath.Join(a.baseDir, file)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("css: import %s: %w", file, err)
	}
	sub := &cssApplier{doc: a.doc, baseDir: a.baseDir}
	if err := parseStylesheet(latin1ToUTF8(data), sub); err != nil {
		return err
	}
	return sub.finish()
}

func isAbsPath(p string) bool {
	return strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") || (len(p) > 1 && p[1] == ':')
}

func (a *cssApplier) endSelector(sels []*selector, keys []string, props map[string]*styleProperty) {
	if len(props) == 0 {
		return
	}
	conv := map[string][]cssPropertyValue{}
	for k, p := range props {
		var vals []cssPropertyValue
		lexicalValue(p.values, &vals)
		conv[k] = vals
	}
	order := javaHashMapOrder(keys)
	for _, s := range sels {
		a.rules = append(a.rules, &cssRule{sel: s, keys: order, props: conv, pseudo: s.isPseudo()})
	}
}

// lexicalValue is CSSHandler.lexicalValue.
func lexicalValue(units []*lexUnit, out *[]cssPropertyValue) {
	for _, u := range units {
		switch u.typ {
		case sacOperatorComma:
		case sacInteger:
			*out = append(*out, cssPropertyValue{sacInteger, strconv.Itoa(u.intVal)})
		case sacReal, sacDimension, sacEm, sacEx, sacInch, sacPixel, sacCentimeter, sacMillimeter, sacPoint, sacPica,
			sacPercentage, sacDegree, sacGradian, sacRadian, sacMillisecond, sacSecond, sacHertz, sacKilohertz:
			*out = append(*out, cssPropertyValue{u.typ, javaFloatString(u.fVal)})
		case sacRGBColor:
			var sb strings.Builder
			colorValue(u.params, &sb)
			*out = append(*out, cssPropertyValue{sacRGBColor, sb.String()})
		case sacURI, sacIdent, sacStringValue, sacAttr:
			if c, ok := colorNames[u.str]; ok {
				*out = append(*out, cssPropertyValue{sacRGBColor, rgbString(c[0], c[1], c[2])})
			} else if strings.Contains(u.str, " ") {
				*out = append(*out, cssPropertyValue{u.typ, "\"" + u.str + "\""})
			} else {
				*out = append(*out, cssPropertyValue{u.typ, u.str})
			}
		case sacInherit:
			*out = append(*out, cssPropertyValue{sacIdent, stringInherit})
		case sacFunction:
			if u.str != "hsl" {
				panic(fmt.Errorf("css: unknown CSS function: %s", u.str))
			}
			r, g, b := colorFromHsl(u.params)
			*out = append(*out, cssPropertyValue{sacRGBColor, rgbString(r, g, b)})
		}
	}
}

func rgbString(r, g, b int) string {
	return fmt.Sprintf("%d|%d,%d|%d,%d|%d", sacInteger, r, sacInteger, g, sacInteger, b)
}

func colorValue(units []*lexUnit, sb *strings.Builder) {
	for _, u := range units {
		switch u.typ {
		case sacOperatorComma:
			sb.WriteString(",")
		case sacInteger:
			fmt.Fprintf(sb, "%d|%d", sacInteger, u.intVal)
		case sacPercentage:
			fmt.Fprintf(sb, "%d|%d", sacInteger, colorFromPercentage(u.fVal))
		}
	}
}

func colorFromPercentage(p float32) int {
	if p < 0 || 100 < p {
		panic(fmt.Errorf("css: color percentage out of range"))
	}
	v := float32(255) * float32(p/float32(100))
	return int(float32(v + 0.5))
}

func unitFloat(u *lexUnit) float32 {
	if u.typ == sacInteger {
		return float32(u.intVal)
	}
	return u.fVal
}

func colorFromHsl(p []*lexUnit) (int, int, int) {
	// h , s , l
	if len(p) != 5 || p[0].typ != sacInteger {
		panic(fmt.Errorf("css: illegal HSL color values"))
	}
	return hslToRgb(float32(p[0].intVal), unitFloat(p[2]), unitFloat(p[4]))
}

func hslToRgb(hue, sat, lig float32) (int, int, int) {
	h := hue
	h /= 360
	if sat < 0 || 100 < sat || lig < 0 || 100 < lig {
		panic(fmt.Errorf("css: HSL value out of range"))
	}
	s := sat
	s /= 100
	l := lig
	l /= 100
	var color [3]float32
	if sat == 0 {
		e := int(float32(255*l) + 0.5)
		return e, e, e
	}
	var m2 float32
	if l < 0.5 {
		m2 = float32(l * float32(1.0+s))
	}
	if l >= 0.5 {
		m2 = float32(float32(l+s) - float32(l*s))
	}
	m1 := float32(float32(2.0*l) - m2)
	third := float32(float32(1.0) / float32(3.0))
	t := [3]float32{h + third, h, h - third}
	for i := 0; i < 3; i++ {
		if t[i] < 0 {
			t[i] = t[i] + 1.0
		}
		if t[i] > 1 {
			t[i] = t[i] - 1.0
		}
		switch {
		case float32(6.0*t[i]) < 1.0:
			color[i] = float32(m1 + float32(float32(float32(m2-m1)*6.0)*t[i]))
		case float32(2.0*t[i]) < 1.0:
			color[i] = m2
		case float32(3.0*t[i]) < 2.0:
			color[i] = float32(m1 + float32(float32(float32(m2-m1)*float32(float32(2.0)/float32(3.0)-t[i]))*6.0))
		default:
			color[i] = m1
		}
	}
	f := func(v float32) int { return int(float32(float32(255)*v) + 0.5) }
	return f(color[0]), f(color[1]), f(color[2])
}

// finish applies the collected rules (endDocument).
func (a *cssApplier) finish() (err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
				return
			}
			panic(r)
		}
	}()
	a.applyRulesToDom()
	return nil
}

func (a *cssApplier) applyRulesToDom() {
	var els []*node
	a.doc.elements(&els)
	for _, el := range els {
		if el.name == stringProperty {
			continue
		}
		var matched []*cssRule
		for _, r := range a.rules {
			if r.match(el) {
				matched = append(matched, r)
			}
		}
		// Collections.sort is stable; the specificity travels with the rule, which was last reset on this element
		specs := make([][3]int, len(matched))
		for i, r := range matched {
			specs[i] = r.spec
		}
		idx := make([]int, len(matched))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(i, j int) bool { return cmpSpec(specs[idx[i]], specs[idx[j]]) < 0 })
		for _, i := range idx {
			applyRuleToElement(el, matched[i])
		}
	}
	applyInheritance(a.doc)
}

func cmpSpec(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		if d := a[i] - b[i]; d != 0 {
			if d < 0 {
				return -1
			}
			return 1
		}
	}
	return 0
}

// --- CSSMatchMaker ---

func (r *cssRule) match(el *node) bool {
	r.spec = [3]int{}
	return matchSel(r.sel, &r.spec, el)
}

func matchSel(s *selector, spec *[3]int, el *node) bool {
	if el == nil {
		panic(fmt.Errorf("css: selector reached above the document element"))
	}
	switch s.kind {
	case selElement:
		if !s.hasLocal || s.local == el.local {
			spec[2]++
			return true
		}
		return false
	case selConditional:
		if !matchSel(s.simple, spec, el) {
			return false
		}
		return matchCondition(s.cond, spec, el)
	case selChild:
		if !matchSel(s.simple, spec, el) {
			return false
		}
		if el.parent == nil {
			panic(fmt.Errorf("css: child selector at the document element"))
		}
		return matchSel(s.ancestor, spec, el.parent)
	default: // descendant
		if !matchSel(s.simple, spec, el) {
			return false
		}
		for anc := el.parent; anc != nil; anc = anc.parent {
			if matchSel(s.ancestor, spec, anc) {
				return true
			}
		}
		return false
	}
}

// javaSplitSpace is String.split(" "): trailing empty strings are dropped (an empty input gives one empty string).
func javaSplitSpace(s string) []string {
	parts := strings.Split(s, " ")
	for len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func matchCondition(c *condition, spec *[3]int, el *node) bool {
	switch c.kind {
	case condID:
		v, _ := el.attr("id")
		for _, t := range javaSplitSpace(v) {
			if t == c.value {
				spec[0]++
				return true
			}
		}
		return false
	case condClass:
		v, _ := el.attr("class")
		for _, t := range javaSplitSpace(v) {
			if t == c.value {
				spec[1]++
				return true
			}
		}
		return false
	case condAnd:
		return matchCondition(c.first, spec, el) && matchCondition(c.next, spec, el)
	default: // pseudo
		if _, ok := pseudoClassNames[c.value]; ok {
			spec[1]++
			return true
		}
		return false
	}
}

// --- CSSDOMProcessor ---

var elementTypeInherits = map[string]bool{ // elements whose type is not "unspecified"
	"view": true, "panes": true, "statuspane": true, "mainpane": true, "controlpane": true, "dialogs": true,
	"dialog": true, "note": true, "button": true, "grid": true, "listitem": true, "datagrid": true,
	"gridcelltemplate": true, "list": true, "datalist": true, "listrowtemplate": true, "text": true, "image": true,
	"editor": true, "marquee": true, "newsticker": true, "tooltip": true, "box": true,
}

func canInheritElement(name string) bool { return elementTypeInherits[name] }

func isInheritableProperty(p string) bool {
	for _, q := range inheritableProperties {
		if p == q {
			return true
		}
	}
	return false
}

// canInherit is CSSDOMProcessor.canInherit(Element, name, value).
func canInherit(el *node, name, value string) bool {
	if value == stringInherit {
		return true
	}
	if !isInheritableProperty(name) {
		return false
	}
	return canInheritElement(el.name)
}

func hasAttrValue(el *node, name, value string) bool {
	if el == nil {
		return false
	}
	for _, a := range el.attrs {
		if name != "" {
			if a.value == value && a.name == name {
				return true
			}
		} else if a.value == value {
			return true
		}
	}
	return false
}

func childWithAttrValue(parent *node, elemName, value string) *node {
	for _, n := range parent.children {
		if n.kind == elementNode && hasAttrValue(n, "", value) && n.name == elemName {
			return n
		}
	}
	return nil
}

type nameValue struct{ name, value string }

func childWithAttributes(parent *node, elemName string, pairs []nameValue) *node {
	for _, n := range parent.children {
		if n.kind != elementNode || n.name != elemName {
			continue
		}
		ok := true
		if len(n.attrs) > 0 {
			for _, p := range pairs {
				if !hasAttrValue(n, p.name, p.value) {
					ok = false
					break
				}
			}
		}
		if ok {
			return n
		}
	}
	return nil
}

func addNewChild(parent *node, key, value, pseudo string, hasPseudo bool) *node {
	n := &node{kind: elementNode, name: stringProperty, local: stringProperty, ns: parent.ns, hasNS: parent.hasNS}
	if hasPseudo {
		n.setAttr(stringPseudoclass, pseudo)
	}
	n.setAttr(stringName, key)
	n.setAttr(stringValue, value)
	parent.appendChild(n)
	return n
}

func applyRuleToElement(el *node, r *cssRule) {
	for _, key := range r.keys {
		vals := r.props[key]
		if len(vals) == 0 {
			panic(fmt.Errorf("css: property %s has no value", key))
		}
		pairs := []nameValue{{stringName, key}}
		pseudoPart := ""
		if r.pseudo {
			s := r.sel.String()
			pseudoPart = s[strings.LastIndexByte(s, ':')+1:]
			pairs = append(pairs, nameValue{stringPseudoclass, pseudoPart})
		}
		e := childWithAttributes(el, stringProperty, pairs)
		if e != nil && hasAttrValue(e, stringName, key) {
			e.setAttr(stringValue, vals[0].typeAndValue())
			for i := 1; i < len(vals); i++ {
				e.setAttr(stringValue+strconv.Itoa(i), vals[i].typeAndValue())
			}
		} else {
			n := addNewChild(el, key, vals[0].typeAndValue(), pseudoPart, r.pseudo)
			for i := 1; i < len(vals); i++ {
				n.setAttr(stringValue+strconv.Itoa(i), vals[i].typeAndValue())
			}
		}
	}
}

func applyInheritance(doc *node) {
	var els []*node
	doc.elements(&els)
	for _, el := range els {
		if el.name == stringProperty || !canInheritElement(el.name) {
			continue
		}
		for _, prop := range inheritableProperties {
			if hasAttrValue(el, "", prop) {
				continue
			}
			found := false
			for n := el.parent; n != nil; n = n.parent {
				if n.kind == elementNode && childWithAttrValue(n, stringProperty, prop) != nil {
					found = true
					break
				}
			}
			if found && childWithAttrValue(el, stringProperty, prop) == nil {
				addNewChild(el, prop, strconv.Itoa(sacIdent)+"|"+stringInherit, "", false)
			}
		}
	}
}
