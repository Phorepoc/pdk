// Copyright (c) Nokia Corporation and/or its subsidiary(-ies) (ThemeInstaller); Copyright The Phorepoc Authors (the Go port)
// SPDX-License-Identifier: EPL-2.0 OR GPL-2.0-or-later

package odt

import (
	"fmt"
	"strconv"
	"strings"
)

// A CSS parser for the subset ThemeInstaller accepts: it stands in for the Batik parser behind the SAC interfaces
// (org.w3c.css.sac). It produces the same lexical units and selector/condition trees.

// SAC lexical unit types (org.w3c.css.sac.LexicalUnit).
const (
	sacOperatorComma = 0
	sacOperatorSlash = 4
	sacInherit       = 12
	sacInteger       = 13
	sacReal          = 14
	sacEm            = 15
	sacEx            = 16
	sacPixel         = 17
	sacInch          = 18
	sacCentimeter    = 19
	sacMillimeter    = 20
	sacPoint         = 21
	sacPica          = 22
	sacPercentage    = 23
	sacURI           = 24
	sacRGBColor      = 27
	sacDegree        = 28
	sacGradian       = 29
	sacRadian        = 30
	sacMillisecond   = 31
	sacSecond        = 32
	sacHertz         = 33
	sacKilohertz     = 34
	sacIdent         = 35
	sacStringValue   = 36
	sacAttr          = 37
	sacFunction      = 41
	sacDimension     = 42
)

type lexUnit struct {
	typ    int
	intVal int
	fVal   float32
	str    string // string/ident/uri/attr value, function name
	params []*lexUnit
}

type selKind int

const (
	selElement selKind = iota
	selConditional
	selChild
	selDescendant
)

type condKind int

const (
	condID condKind = iota
	condClass
	condPseudo
	condAnd
)

type condition struct {
	kind        condKind
	value       string
	first, next *condition
}

type selector struct {
	kind     selKind
	local    string // element: "" with hasLocal=false is the universal selector
	hasLocal bool
	simple   *selector
	cond     *condition
	ancestor *selector
}

func (c *condition) String() string {
	switch c.kind {
	case condID:
		return "#" + c.value
	case condClass:
		return "." + c.value
	case condPseudo:
		return ":" + c.value
	}
	return c.first.String() + c.next.String()
}

func (s *selector) String() string {
	switch s.kind {
	case selElement:
		if !s.hasLocal {
			return "*"
		}
		return s.local
	case selConditional:
		return s.simple.String() + s.cond.String()
	case selChild:
		return s.ancestor.String() + " > " + s.simple.String()
	}
	return s.ancestor.String() + " " + s.simple.String()
}

// isPseudo is CSSSelectorParser.isPseudo.
func (s *selector) isPseudo() bool {
	switch s.kind {
	case selConditional:
		return s.cond.isPseudo()
	case selChild, selDescendant:
		return s.simple.isPseudo()
	}
	return false
}

func (c *condition) isPseudo() bool {
	switch c.kind {
	case condPseudo:
		return true
	case condAnd:
		return c.first.isPseudo() || c.next.isPseudo()
	}
	return false
}

type styleProperty struct {
	values    []*lexUnit
	important bool
}

// cssHandlerEvents receives the parse (the SAC DocumentHandler).
type cssHandlerEvents interface {
	endSelector(sels []*selector, keys []string, props map[string]*styleProperty)
	importStyle(file string) error
}

type cssScanner struct {
	s    string
	pos  int
	line int
}

func (c *cssScanner) fail(format string, a ...any) error {
	line := 1 + strings.Count(c.s[:min(c.pos, len(c.s))], "\n")
	return fmt.Errorf("css: line %d: %s", line, fmt.Sprintf(format, a...))
}

func (c *cssScanner) skipSpace() error {
	for c.pos < len(c.s) {
		switch {
		case isCSSSpace(c.s[c.pos]):
			c.pos++
		case strings.HasPrefix(c.s[c.pos:], "/*"):
			e := strings.Index(c.s[c.pos+2:], "*/")
			if e < 0 {
				return c.fail("unterminated comment")
			}
			c.pos += 2 + e + 2
		default:
			return nil
		}
	}
	return nil
}

func isCSSSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' }

func isIdentStart(b byte) bool {
	return b == '_' || b == '-' || b >= 0x80 || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '\\'
}

func isIdentChar(b byte) bool { return isIdentStart(b) || (b >= '0' && b <= '9') }

func (c *cssScanner) readIdent() string {
	st := c.pos
	for c.pos < len(c.s) && isIdentChar(c.s[c.pos]) {
		if c.s[c.pos] == '\\' && c.pos+1 < len(c.s) {
			c.pos++
		}
		c.pos++
	}
	return c.s[st:c.pos]
}

// parseStylesheet scans a style sheet, reporting each rule set to h.
func parseStylesheet(src string, h cssHandlerEvents) error {
	c := &cssScanner{s: src}
	for {
		if err := c.skipSpace(); err != nil {
			return err
		}
		for strings.HasPrefix(c.s[c.pos:], "<!--") || strings.HasPrefix(c.s[c.pos:], "-->") {
			c.pos += 3 + btoi(c.s[c.pos] == '<')
			if err := c.skipSpace(); err != nil {
				return err
			}
		}
		if c.pos >= len(c.s) {
			return nil
		}
		if c.s[c.pos] == '@' {
			if err := c.atRule(h); err != nil {
				return err
			}
			continue
		}
		if err := c.ruleSet(h); err != nil {
			return err
		}
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (c *cssScanner) atRule(h cssHandlerEvents) error {
	c.pos++
	name := strings.ToLower(c.readIdent())
	if name != "import" {
		// unknown at-rule: ignorable, skip it (the handler's ignorableAtRule does nothing)
		return c.skipAtRule()
	}
	if err := c.skipSpace(); err != nil {
		return err
	}
	var file string
	switch {
	case c.pos < len(c.s) && (c.s[c.pos] == '"' || c.s[c.pos] == '\''):
		s, err := c.readString()
		if err != nil {
			return err
		}
		file = s
	case strings.HasPrefix(strings.ToLower(c.s[c.pos:]), "url("):
		u, err := c.readURL()
		if err != nil {
			return err
		}
		file = u
	default:
		return c.fail("@import needs a file")
	}
	e := strings.IndexByte(c.s[c.pos:], ';')
	if e < 0 {
		return c.fail("unterminated @import")
	}
	c.pos += e + 1
	return h.importStyle(file)
}

func (c *cssScanner) skipAtRule() error {
	for c.pos < len(c.s) {
		switch c.s[c.pos] {
		case ';':
			c.pos++
			return nil
		case '{':
			depth := 0
			for c.pos < len(c.s) {
				if c.s[c.pos] == '{' {
					depth++
				} else if c.s[c.pos] == '}' {
					depth--
					if depth == 0 {
						c.pos++
						return nil
					}
				}
				c.pos++
			}
			return c.fail("unterminated at-rule block")
		}
		c.pos++
	}
	return nil
}

func (c *cssScanner) readString() (string, error) {
	q := c.s[c.pos]
	c.pos++
	var sb strings.Builder
	for c.pos < len(c.s) {
		b := c.s[c.pos]
		switch {
		case b == q:
			c.pos++
			return sb.String(), nil
		case b == '\\' && c.pos+1 < len(c.s):
			c.pos++
			sb.WriteByte(c.s[c.pos])
			c.pos++
		case b == '\n':
			return "", c.fail("newline in string")
		default:
			sb.WriteByte(b)
			c.pos++
		}
	}
	return "", c.fail("unterminated string")
}

func (c *cssScanner) readURL() (string, error) {
	c.pos += 4
	if err := c.skipSpace(); err != nil {
		return "", err
	}
	var u string
	if c.pos < len(c.s) && (c.s[c.pos] == '"' || c.s[c.pos] == '\'') {
		s, err := c.readString()
		if err != nil {
			return "", err
		}
		u = s
	} else {
		st := c.pos
		for c.pos < len(c.s) && c.s[c.pos] != ')' && !isCSSSpace(c.s[c.pos]) {
			c.pos++
		}
		u = c.s[st:c.pos]
	}
	if err := c.skipSpace(); err != nil {
		return "", err
	}
	if c.pos >= len(c.s) || c.s[c.pos] != ')' {
		return "", c.fail("unterminated url()")
	}
	c.pos++
	return u, nil
}

func (c *cssScanner) ruleSet(h cssHandlerEvents) error {
	st := c.pos
	for c.pos < len(c.s) && c.s[c.pos] != '{' {
		c.pos++
	}
	if c.pos >= len(c.s) {
		return c.fail("selector without a rule block")
	}
	sels, err := parseSelectorList(c.s[st:c.pos])
	if err != nil {
		return c.fail("%v", err)
	}
	c.pos++ // {
	props := map[string]*styleProperty{}
	var keys []string
	for {
		if err := c.skipSpace(); err != nil {
			return err
		}
		if c.pos >= len(c.s) {
			return c.fail("unterminated rule block")
		}
		if c.s[c.pos] == '}' {
			c.pos++
			break
		}
		if c.s[c.pos] == ';' {
			c.pos++
			continue
		}
		name := c.readIdent()
		if name == "" {
			return c.fail("property name expected")
		}
		if err := c.skipSpace(); err != nil {
			return err
		}
		if c.pos >= len(c.s) || c.s[c.pos] != ':' {
			return c.fail("':' expected after property %s", name)
		}
		c.pos++
		units, important, err := c.expression()
		if err != nil {
			return err
		}
		if _, ok := props[name]; !ok {
			keys = append(keys, name)
		}
		props[name] = &styleProperty{units, important}
	}
	h.endSelector(sels, keys, props)
	return nil
}

func parseSelectorList(s string) ([]*selector, error) {
	// strip comments
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			break
		}
		e := strings.Index(s[i+2:], "*/")
		if e < 0 {
			return nil, fmt.Errorf("unterminated comment in a selector")
		}
		s = s[:i] + " " + s[i+2+e+2:]
	}
	var out []*selector
	for _, part := range strings.Split(s, ",") {
		sel, err := parseSelector(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		out = append(out, sel)
	}
	return out, nil
}

func parseSelector(s string) (*selector, error) {
	if s == "" {
		return nil, fmt.Errorf("empty selector")
	}
	c := &cssScanner{s: s}
	var cur *selector
	comb := byte(0) // ' ' descendant, '>' child
	for {
		for c.pos < len(c.s) && isCSSSpace(c.s[c.pos]) {
			c.pos++
			if comb == 0 {
				comb = ' '
			}
		}
		if c.pos >= len(c.s) {
			break
		}
		if c.s[c.pos] == '>' {
			comb = '>'
			c.pos++
			continue
		}
		if c.s[c.pos] == '+' || c.s[c.pos] == '~' {
			return nil, fmt.Errorf("selector %q: combinator %c is not supported", s, c.s[c.pos])
		}
		simple, err := c.compound(s)
		if err != nil {
			return nil, err
		}
		if cur == nil {
			cur = simple
		} else {
			k := selDescendant
			if comb == '>' {
				k = selChild
			}
			cur = &selector{kind: k, ancestor: cur, simple: simple}
		}
		comb = 0
	}
	if cur == nil || comb == '>' {
		return nil, fmt.Errorf("selector %q is incomplete", s)
	}
	return cur, nil
}

func (c *cssScanner) compound(whole string) (*selector, error) {
	el := &selector{kind: selElement}
	if c.pos < len(c.s) && c.s[c.pos] == '*' {
		c.pos++
	} else if c.pos < len(c.s) && isIdentStart(c.s[c.pos]) {
		el.local, el.hasLocal = c.readIdent(), true
	}
	var cond *condition
	add := func(n *condition) {
		if cond == nil {
			cond = n
		} else {
			cond = &condition{kind: condAnd, first: cond, next: n}
		}
	}
	for c.pos < len(c.s) {
		var k condKind
		switch c.s[c.pos] {
		case '#':
			k = condID
		case '.':
			k = condClass
		case ':':
			k = condPseudo
			if strings.HasPrefix(c.s[c.pos:], "::") {
				return nil, fmt.Errorf("selector %q: pseudo-elements are not supported", whole)
			}
		default:
			goto done
		}
		c.pos++
		name := c.readIdent()
		if name == "" {
			return nil, fmt.Errorf("selector %q: name expected after %c", whole, c.s[c.pos-1])
		}
		if c.pos < len(c.s) && c.s[c.pos] == '(' {
			return nil, fmt.Errorf("selector %q: functional pseudo-classes are not supported", whole)
		}
		add(&condition{kind: k, value: name})
	}
done:
	if c.pos < len(c.s) && !isCSSSpace(c.s[c.pos]) && c.s[c.pos] != '>' {
		return nil, fmt.Errorf("selector %q: unexpected %q", whole, c.s[c.pos])
	}
	if !el.hasLocal && cond == nil && !(c.pos > 0 && c.s[c.pos-1] == '*') {
		return nil, fmt.Errorf("selector %q: unexpected %q", whole, c.s[c.pos:])
	}
	if cond == nil {
		return el, nil
	}
	return &selector{kind: selConditional, simple: el, cond: cond}, nil
}

// expression reads a property value up to the end of the declaration.
func (c *cssScanner) expression() ([]*lexUnit, bool, error) {
	var units []*lexUnit
	important := false
	for {
		if err := c.skipSpace(); err != nil {
			return nil, false, err
		}
		if c.pos >= len(c.s) {
			return nil, false, c.fail("unterminated declaration")
		}
		b := c.s[c.pos]
		switch {
		case b == ';' || b == '}':
			if b == ';' {
				c.pos++
			}
			if len(units) == 0 {
				return nil, false, c.fail("empty value")
			}
			return units, important, nil
		case b == '!':
			c.pos++
			if err := c.skipSpace(); err != nil {
				return nil, false, err
			}
			if !strings.EqualFold(c.readIdent(), "important") {
				return nil, false, c.fail("!important expected")
			}
			important = true
		case b == ',':
			c.pos++
			units = append(units, &lexUnit{typ: sacOperatorComma})
		case b == '/':
			c.pos++
			units = append(units, &lexUnit{typ: sacOperatorSlash})
		default:
			u, err := c.term()
			if err != nil {
				return nil, false, err
			}
			units = append(units, u)
		}
	}
}

func (c *cssScanner) term() (*lexUnit, error) {
	b := c.s[c.pos]
	switch {
	case b == '"' || b == '\'':
		s, err := c.readString()
		if err != nil {
			return nil, err
		}
		return &lexUnit{typ: sacStringValue, str: s}, nil
	case b == '#':
		c.pos++
		st := c.pos
		for c.pos < len(c.s) && isHex(c.s[c.pos]) {
			c.pos++
		}
		hex := c.s[st:c.pos]
		var r, g, bl int
		switch len(hex) {
		case 3:
			r, g, bl = hexv(hex[0])*17, hexv(hex[1])*17, hexv(hex[2])*17
		case 6:
			r, g, bl = hexv(hex[0])*16+hexv(hex[1]), hexv(hex[2])*16+hexv(hex[3]), hexv(hex[4])*16+hexv(hex[5])
		default:
			return nil, c.fail("bad colour #%s", hex)
		}
		return &lexUnit{typ: sacRGBColor, params: []*lexUnit{
			{typ: sacInteger, intVal: r}, {typ: sacOperatorComma},
			{typ: sacInteger, intVal: g}, {typ: sacOperatorComma},
			{typ: sacInteger, intVal: bl}}}, nil
	case b == '+' || b == '-' || b == '.' || (b >= '0' && b <= '9'):
		if (b == '-' || b == '+') && !(c.pos+1 < len(c.s) && (c.s[c.pos+1] == '.' || (c.s[c.pos+1] >= '0' && c.s[c.pos+1] <= '9'))) {
			break // an identifier such as -foo
		}
		return c.number()
	}
	if isIdentStart(b) {
		if strings.HasPrefix(strings.ToLower(c.s[c.pos:]), "url(") {
			u, err := c.readURL()
			if err != nil {
				return nil, err
			}
			return &lexUnit{typ: sacURI, str: u}, nil
		}
		id := c.readIdent()
		if c.pos < len(c.s) && c.s[c.pos] == '(' {
			c.pos++
			var params []*lexUnit
			for {
				if err := c.skipSpace(); err != nil {
					return nil, err
				}
				if c.pos >= len(c.s) {
					return nil, c.fail("unterminated function %s(", id)
				}
				if c.s[c.pos] == ')' {
					c.pos++
					break
				}
				if c.s[c.pos] == ',' {
					c.pos++
					params = append(params, &lexUnit{typ: sacOperatorComma})
					continue
				}
				u, err := c.term()
				if err != nil {
					return nil, err
				}
				params = append(params, u)
			}
			if strings.EqualFold(id, "rgb") {
				return &lexUnit{typ: sacRGBColor, params: params}, nil
			}
			if strings.EqualFold(id, "attr") && len(params) == 1 {
				return &lexUnit{typ: sacAttr, str: params[0].str}, nil
			}
			return &lexUnit{typ: sacFunction, str: id, params: params}, nil
		}
		if strings.EqualFold(id, "inherit") {
			return &lexUnit{typ: sacInherit, str: id}, nil
		}
		return &lexUnit{typ: sacIdent, str: id}, nil
	}
	return nil, c.fail("unexpected %q in a value", string(b))
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func hexv(b byte) int {
	v, _ := strconv.ParseUint(string(b), 16, 8)
	return int(v)
}

var cssUnits = map[string]int{
	"em": sacEm, "ex": sacEx, "px": sacPixel, "cm": sacCentimeter, "mm": sacMillimeter, "in": sacInch,
	"pt": sacPoint, "pc": sacPica, "deg": sacDegree, "grad": sacGradian, "rad": sacRadian,
	"ms": sacMillisecond, "s": sacSecond, "hz": sacHertz, "khz": sacKilohertz,
}

func (c *cssScanner) number() (*lexUnit, error) {
	st := c.pos
	if c.s[c.pos] == '+' || c.s[c.pos] == '-' {
		c.pos++
	}
	isReal := false
	for c.pos < len(c.s) && c.s[c.pos] >= '0' && c.s[c.pos] <= '9' {
		c.pos++
	}
	if c.pos < len(c.s) && c.s[c.pos] == '.' {
		isReal = true
		c.pos++
		for c.pos < len(c.s) && c.s[c.pos] >= '0' && c.s[c.pos] <= '9' {
			c.pos++
		}
	}
	text := c.s[st:c.pos]
	typ := sacInteger
	if isReal {
		typ = sacReal
	}
	if c.pos < len(c.s) && c.s[c.pos] == '%' {
		c.pos++
		typ = sacPercentage
	} else if c.pos < len(c.s) && isIdentStart(c.s[c.pos]) {
		unit := c.readIdent()
		if t, ok := cssUnits[strings.ToLower(unit)]; ok {
			typ = t
		} else {
			typ = sacDimension
		}
	}
	if typ == sacInteger {
		v, err := strconv.Atoi(strings.TrimPrefix(text, "+"))
		if err != nil {
			return nil, c.fail("bad integer %q", text)
		}
		return &lexUnit{typ: sacInteger, intVal: v, fVal: float32(v)}, nil
	}
	f, err := strconv.ParseFloat(strings.TrimPrefix(text, "+"), 32)
	if err != nil {
		return nil, c.fail("bad number %q", text)
	}
	return &lexUnit{typ: typ, fVal: float32(f)}, nil
}
