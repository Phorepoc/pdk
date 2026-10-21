// Copyright (c) Nokia Corporation and/or its subsidiary(-ies) (ThemeInstaller); Copyright The Phorepoc Authors (the Go port)
// SPDX-License-Identifier: EPL-2.0 OR GPL-2.0-or-later

package odt

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The DOM ThemeInstaller builds with Xerces (XMLParser), reduced to what the converter reads: elements with
// namespace-resolved names, attributes kept sorted by name (as a Xerces NamedNodeMap does) and text nodes.

type nodeKind int

const (
	elementNode nodeKind = iota
	textNode
	cdataNode
)

type attribute struct {
	name  string
	value string
}

type node struct {
	kind     nodeKind
	name     string // qualified name (prefix:local)
	local    string
	ns       string
	hasNS    bool
	attrs    []*attribute // sorted by name
	children []*node
	parent   *node
	text     string
}

func (n *node) attr(name string) (string, bool) {
	for _, a := range n.attrs {
		if a.name == name {
			return a.value, true
		}
	}
	return "", false
}

// setAttr sets (or replaces) an attribute, keeping the list sorted by name like NamedNodeMapImpl.
func (n *node) setAttr(name, value string) {
	i := sort.Search(len(n.attrs), func(i int) bool { return n.attrs[i].name >= name })
	if i < len(n.attrs) && n.attrs[i].name == name {
		n.attrs[i].value = value
		return
	}
	n.attrs = append(n.attrs, nil)
	copy(n.attrs[i+1:], n.attrs[i:])
	n.attrs[i] = &attribute{name, value}
}

func (n *node) appendChild(c *node) {
	c.parent = n
	n.children = append(n.children, c)
}

// textContent is Node.getTextContent: the text of all descendants.
func (n *node) textContent() string {
	switch n.kind {
	case textNode, cdataNode:
		return n.text
	}
	var sb strings.Builder
	for _, c := range n.children {
		sb.WriteString(c.textContent())
	}
	return sb.String()
}

// elements lists the element descendants of n (n included) in document order.
func (n *node) elements(out *[]*node) {
	if n.kind != elementNode {
		return
	}
	*out = append(*out, n)
	for _, c := range n.children {
		c.elements(out)
	}
}

// findAll is Document.getElementsByTagName.
func (n *node) findAll(tag string) []*node {
	var all, res []*node
	n.elements(&all)
	for _, e := range all {
		if e.name == tag {
			res = append(res, e)
		}
	}
	return res
}

type xmlParser struct {
	s        string
	pos      int
	entities map[string]string
	root     *node
}

// parseXML parses an XML document. dtd, when non-empty, is the external DTD that declares general entities
// (ThemeInstaller substitutes the localisation DTD for whatever the DOCTYPE names).
func parseXML(data []byte, dtd []byte) (*node, error) {
	p := &xmlParser{entities: map[string]string{}}
	p.s = decodeXML(data)
	if len(dtd) > 0 {
		if err := p.loadDTD(decodeDTD(dtd)); err != nil {
			return nil, err
		}
	}
	doc := &node{kind: elementNode, name: "#document"}
	if err := p.parseDocument(doc); err != nil {
		return nil, err
	}
	if p.root == nil {
		return nil, fmt.Errorf("xml: no document element")
	}
	return p.root, nil
}

func decodeXML(b []byte) string {
	b = []byte(strings.TrimPrefix(string(b), "\xef\xbb\xbf"))
	s := string(b)
	if strings.HasPrefix(s, "<?xml") {
		if end := strings.Index(s, "?>"); end > 0 {
			decl := strings.ToLower(s[:end])
			if i := strings.Index(decl, "encoding"); i >= 0 && (strings.Contains(decl, "iso-8859-1") || strings.Contains(decl, "latin1")) {
				return latin1ToUTF8(b)
			}
		}
	}
	if !utf8.Valid(b) {
		return latin1ToUTF8(b)
	}
	return s
}

func decodeDTD(b []byte) string {
	b = []byte(strings.TrimPrefix(string(b), "\xef\xbb\xbf"))
	if utf8.Valid(b) {
		return string(b)
	}
	return latin1ToUTF8(b)
}

func latin1ToUTF8(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// loadDTD reads the internal general entity declarations (<!ENTITY name "value">) of a DTD.
func (p *xmlParser) loadDTD(s string) error {
	i := 0
	for i < len(s) {
		switch {
		case strings.HasPrefix(s[i:], "<!--"):
			e := strings.Index(s[i:], "-->")
			if e < 0 {
				return fmt.Errorf("dtd: unterminated comment")
			}
			i += e + 3
		case strings.HasPrefix(s[i:], "<?"):
			e := strings.Index(s[i:], "?>")
			if e < 0 {
				return fmt.Errorf("dtd: unterminated PI")
			}
			i += e + 2
		case strings.HasPrefix(s[i:], "<!ENTITY"):
			j := i + len("<!ENTITY")
			for j < len(s) && isXMLSpace(s[j]) {
				j++
			}
			if j < len(s) && s[j] == '%' { // parameter entity: not needed
				e := strings.IndexByte(s[j:], '>')
				if e < 0 {
					return fmt.Errorf("dtd: unterminated declaration")
				}
				i = j + e + 1
				continue
			}
			k := j
			for k < len(s) && !isXMLSpace(s[k]) {
				k++
			}
			name := s[j:k]
			for k < len(s) && isXMLSpace(s[k]) {
				k++
			}
			if k >= len(s) || (s[k] != '"' && s[k] != '\'') {
				// external entity: skip
				e := strings.IndexByte(s[k:], '>')
				if e < 0 {
					return fmt.Errorf("dtd: unterminated declaration")
				}
				i = k + e + 1
				continue
			}
			q := s[k]
			e := strings.IndexByte(s[k+1:], q)
			if e < 0 {
				return fmt.Errorf("dtd: unterminated entity value")
			}
			val := s[k+1 : k+1+e]
			if _, dup := p.entities[name]; !dup { // the first declaration wins
				p.entities[name] = resolveCharRefs(val)
			}
			end := strings.IndexByte(s[k+1+e:], '>')
			if end < 0 {
				return fmt.Errorf("dtd: unterminated declaration")
			}
			i = k + 1 + e + end + 1
		default:
			i++
		}
	}
	return nil
}

func resolveCharRefs(v string) string {
	if !strings.Contains(v, "&#") {
		return v
	}
	var sb strings.Builder
	for i := 0; i < len(v); {
		if strings.HasPrefix(v[i:], "&#") {
			if e := strings.IndexByte(v[i:], ';'); e > 0 {
				if r, ok := parseCharRef(v[i+2 : i+e]); ok {
					sb.WriteRune(r)
					i += e + 1
					continue
				}
			}
		}
		sb.WriteByte(v[i])
		i++
	}
	return sb.String()
}

func parseCharRef(s string) (rune, bool) {
	var n uint64
	var err error
	if strings.HasPrefix(s, "x") {
		n, err = strconv.ParseUint(s[1:], 16, 32)
	} else {
		n, err = strconv.ParseUint(s, 10, 32)
	}
	if err != nil {
		return 0, false
	}
	return rune(n), true
}

func isXMLSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func (p *xmlParser) fail(format string, a ...any) error {
	line := 1 + strings.Count(p.s[:min(p.pos, len(p.s))], "\n")
	return fmt.Errorf("xml: line %d: %s", line, fmt.Sprintf(format, a...))
}

func (p *xmlParser) parseDocument(doc *node) error {
	p.s = strings.ReplaceAll(strings.ReplaceAll(p.s, "\r\n", "\n"), "\r", "\n")
	var cur = doc
	var text strings.Builder
	flush := func() {
		if text.Len() == 0 {
			return
		}
		if cur != doc {
			if n := len(cur.children); n > 0 && cur.children[n-1].kind == textNode {
				cur.children[n-1].text += text.String()
			} else {
				cur.appendChild(&node{kind: textNode, text: text.String()})
			}
		}
		text.Reset()
	}
	type nsScope map[string]string
	scopes := []nsScope{{"xml": "http://www.w3.org/XML/1998/namespace"}}
	lookup := func(prefix string) (string, bool) {
		for i := len(scopes) - 1; i >= 0; i-- {
			if v, ok := scopes[i][prefix]; ok {
				return v, v != "" || prefix != ""
			}
		}
		return "", false
	}
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		switch {
		case c != '<' && c != '&':
			e := strings.IndexAny(p.s[p.pos:], "<&")
			if e < 0 {
				e = len(p.s) - p.pos
			}
			text.WriteString(p.s[p.pos : p.pos+e])
			p.pos += e
		case c == '&':
			v, err := p.parseReference(false)
			if err != nil {
				return err
			}
			text.WriteString(v)
		case strings.HasPrefix(p.s[p.pos:], "<!--"):
			e := strings.Index(p.s[p.pos:], "-->")
			if e < 0 {
				return p.fail("unterminated comment")
			}
			flush()
			p.pos += e + 3
		case strings.HasPrefix(p.s[p.pos:], "<![CDATA["):
			e := strings.Index(p.s[p.pos:], "]]>")
			if e < 0 {
				return p.fail("unterminated CDATA")
			}
			flush()
			cur.appendChild(&node{kind: cdataNode, text: p.s[p.pos+9 : p.pos+e]})
			p.pos += e + 3
		case strings.HasPrefix(p.s[p.pos:], "<?"):
			e := strings.Index(p.s[p.pos:], "?>")
			if e < 0 {
				return p.fail("unterminated processing instruction")
			}
			flush()
			p.pos += e + 2
		case strings.HasPrefix(p.s[p.pos:], "<!DOCTYPE"):
			if err := p.skipDoctype(); err != nil {
				return err
			}
		case strings.HasPrefix(p.s[p.pos:], "</"):
			e := strings.IndexByte(p.s[p.pos:], '>')
			if e < 0 {
				return p.fail("unterminated end tag")
			}
			name := strings.TrimSpace(p.s[p.pos+2 : p.pos+e])
			if cur == doc || cur.name != name {
				return p.fail("mismatched end tag </%s>", name)
			}
			flush()
			if cur.parent == nil {
				cur = doc
			} else {
				cur = cur.parent
			}
			scopes = scopes[:len(scopes)-1]
			p.pos += e + 1
		default: // start tag
			flush()
			p.pos++
			name := p.readName()
			if name == "" {
				return p.fail("bad start tag")
			}
			var raw []*attribute
			selfClose := false
			for {
				p.skipSpace()
				if p.pos >= len(p.s) {
					return p.fail("unterminated start tag")
				}
				if p.s[p.pos] == '/' && strings.HasPrefix(p.s[p.pos:], "/>") {
					selfClose = true
					p.pos += 2
					break
				}
				if p.s[p.pos] == '>' {
					p.pos++
					break
				}
				an := p.readName()
				if an == "" {
					return p.fail("bad attribute in <%s>", name)
				}
				p.skipSpace()
				if p.pos >= len(p.s) || p.s[p.pos] != '=' {
					return p.fail("attribute %s has no value", an)
				}
				p.pos++
				p.skipSpace()
				v, err := p.readAttrValue()
				if err != nil {
					return err
				}
				raw = append(raw, &attribute{an, v})
			}
			scope := nsScope{}
			for _, a := range raw {
				if a.name == "xmlns" {
					scope[""] = a.value
				} else if strings.HasPrefix(a.name, "xmlns:") {
					scope[a.name[6:]] = a.value
				}
			}
			scopes = append(scopes, scope)
			el := &node{kind: elementNode, name: name, local: name}
			prefix := ""
			if i := strings.IndexByte(name, ':'); i >= 0 {
				prefix, el.local = name[:i], name[i+1:]
			}
			if uri, ok := lookup(prefix); ok {
				el.ns, el.hasNS = uri, uri != ""
			}
			for _, a := range raw {
				el.setAttr(a.name, a.value)
			}
			if cur == doc {
				if p.root != nil {
					return p.fail("more than one document element")
				}
				p.root = el
				el.parent = nil
			} else {
				cur.appendChild(el)
			}
			if selfClose {
				scopes = scopes[:len(scopes)-1]
			} else {
				cur = el
			}
		}
	}
	flush()
	if cur != doc {
		return p.fail("unexpected end of file inside <%s>", cur.name)
	}
	return nil
}

func (p *xmlParser) skipSpace() {
	for p.pos < len(p.s) && isXMLSpace(p.s[p.pos]) {
		p.pos++
	}
}

func (p *xmlParser) readName() string {
	st := p.pos
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if isXMLSpace(c) || c == '=' || c == '>' || c == '/' || c == '<' || c == '"' || c == '\'' {
			break
		}
		p.pos++
	}
	return p.s[st:p.pos]
}

func (p *xmlParser) readAttrValue() (string, error) {
	if p.pos >= len(p.s) || (p.s[p.pos] != '"' && p.s[p.pos] != '\'') {
		return "", p.fail("attribute value must be quoted")
	}
	q := p.s[p.pos]
	p.pos++
	var sb strings.Builder
	for {
		if p.pos >= len(p.s) {
			return "", p.fail("unterminated attribute value")
		}
		c := p.s[p.pos]
		switch {
		case c == q:
			p.pos++
			return sb.String(), nil
		case c == '&':
			v, err := p.parseReference(true)
			if err != nil {
				return "", err
			}
			sb.WriteString(v)
		case c == '<':
			return "", p.fail("'<' in attribute value")
		case c == '\n' || c == '\t' || c == '\r':
			sb.WriteByte(' ')
			p.pos++
		default:
			sb.WriteByte(c)
			p.pos++
		}
	}
}

// parseReference reads &name; or &#n; at p.pos and returns its replacement text.
func (p *xmlParser) parseReference(inAttr bool) (string, error) {
	e := strings.IndexByte(p.s[p.pos:], ';')
	if e < 0 {
		return "", p.fail("unterminated reference")
	}
	name := p.s[p.pos+1 : p.pos+e]
	p.pos += e + 1
	if strings.HasPrefix(name, "#") {
		r, ok := parseCharRef(name[1:])
		if !ok {
			return "", p.fail("bad character reference &%s;", name)
		}
		return string(r), nil
	}
	return p.expandEntity(name, 0)
}

func (p *xmlParser) expandEntity(name string, depth int) (string, error) {
	switch name {
	case "lt":
		return "<", nil
	case "gt":
		return ">", nil
	case "amp":
		return "&", nil
	case "apos":
		return "'", nil
	case "quot":
		return "\"", nil
	}
	v, ok := p.entities[name]
	if !ok {
		return "", p.fail("the entity %q was not declared", name)
	}
	if depth > 16 {
		return "", p.fail("entity %q is recursive", name)
	}
	// the replacement text may reference further entities
	var sb strings.Builder
	for i := 0; i < len(v); {
		if v[i] == '&' {
			if e := strings.IndexByte(v[i:], ';'); e > 0 {
				inner, err := p.expandEntity(v[i+1:i+e], depth+1)
				if err != nil {
					return "", err
				}
				sb.WriteString(inner)
				i += e + 1
				continue
			}
		}
		sb.WriteByte(v[i])
		i++
	}
	return sb.String(), nil
}

func (p *xmlParser) skipDoctype() error {
	// <!DOCTYPE name [SYSTEM "x" | PUBLIC "p" "x"] [ internal subset ]>
	i := p.pos + len("<!DOCTYPE")
	depth := 0
	var quote byte
	subsetStart := -1
	for ; i < len(p.s); i++ {
		c := p.s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '[':
			if depth == 0 {
				subsetStart = i + 1
			}
			depth++
		case ']':
			depth--
			if depth == 0 && subsetStart >= 0 {
				if err := p.loadDTD(p.s[subsetStart:i]); err != nil {
					return err
				}
			}
		case '>':
			if depth == 0 {
				p.pos = i + 1
				return nil
			}
		}
	}
	return p.fail("unterminated DOCTYPE")
}
