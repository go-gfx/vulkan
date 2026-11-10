// Package vkgen reads the Khronos registry (vk.xml) and generates the Go
// mirrors of the Vulkan structures, enumerations and handles that go-gfx/vulkan
// calls. Offsets and sizes are computed by the C rules for a 64-bit target, and
// the generator emits a test that pins every Go struct against that table, so
// that a layout has a witness other than the hand that typed it.
package vkgen

import (
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// MaxRegistryBytes bounds the registry the reader accepts: the real file is
// about 3.3 MB, and the reader is fuzzed with arbitrary bytes.
const MaxRegistryBytes = 64 << 20

// Member is one field of a structure, as the registry spells it.
type Member struct {
	Name    string
	Type    string
	Pointer bool     // a pointer of any depth: the generator mirrors it as unsafe.Pointer
	Dims    []string // array dimensions, each a number or the name of a constant
	Bits    string   // bit-field width, unsupported by the generator
	Values  string   // the fixed value of an sType member
}

// Struct is a `<type category="struct">`.
type Struct struct {
	Name    string
	Members []Member
}

// EnumValue is one resolved enumerator.
type EnumValue struct {
	Name  string
	Value int64
}

// Enum is one `<enums>` block, extended by the `<require>` elements.
type Enum struct {
	Name    string
	Bitmask bool
	Wide    bool // bitwidth 64
	Values  []EnumValue
}

// Constant is an entry of the API Constants block.
type Constant struct {
	Type  string
	Value string
}

// Registry is what the generator needs of vk.xml.
type Registry struct {
	Structs   map[string]*Struct
	Enums     map[string]*Enum
	EnumTypes map[string]bool // names declared with category="enum"
	Constants map[string]Constant
	Base      map[string]string // VkFlags -> uint32_t
	Flags     map[string]string // VkBufferCreateFlags -> VkFlags
	Handles   map[string]bool   // name -> dispatchable
	Aliases   map[string]string // alias -> target, for structs, enums, handles
	Version   string            // VK_HEADER_VERSION, when found
	pending   []pendingEnum
}

type pendingEnum struct {
	extends string
	name    string
	alias   string
	value   string
	bitpos  string
	offset  string
	ext     int
	neg     bool
}

type node struct {
	elem string // "" for text
	text string
}

// OpenRegistry opens a registry file; a name ending in .gz is decompressed. The
// repository keeps vk.xml.gz (3.3 MB of XML is 0.3 MB gzipped), and Parse bounds
// the decompressed size, so a gzip bomb is refused.
func OpenRegistry(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(path, ".gz") {
		return f, nil
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("vkgen: %s: %w", path, err)
	}
	return readCloser{zr, f}, nil
}

type readCloser struct {
	io.Reader
	f *os.File
}

func (r readCloser) Close() error { return r.f.Close() }

// Parse reads a registry. It never panics on arbitrary input.
func Parse(r io.Reader) (*Registry, error) {
	lr := &io.LimitedReader{R: r, N: MaxRegistryBytes + 1}
	d := xml.NewDecoder(lr)
	d.Strict = true
	reg := &Registry{
		Structs: map[string]*Struct{}, Enums: map[string]*Enum{}, EnumTypes: map[string]bool{},
		Constants: map[string]Constant{}, Base: map[string]string{}, Flags: map[string]string{},
		Handles: map[string]bool{}, Aliases: map[string]string{},
	}
	extNumber := 0
	scOnly := false // inside a <require> that is for Vulkan SC only
	sawRegistry := false
	for {
		tok, err := d.Token()
		if lr.N <= 0 {
			return nil, errors.New("vkgen: registry larger than the limit")
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("vkgen: %w", err)
		}
		if ee, ok := tok.(xml.EndElement); ok && ee.Name.Local == "require" {
			scOnly = false
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "require":
			scOnly = !forVulkan(se)
		case "registry":
			sawRegistry = true
		case "type":
			if err := reg.parseType(d, se); err != nil {
				return nil, err
			}
		case "enums":
			if err := reg.parseEnums(d, se); err != nil {
				return nil, err
			}
		case "feature":
			// A version feature has an api attribute; the <feature> that
			// names a device feature inside a <require> does not.
			if attr(se, "api") != "" {
				extNumber = 0
			}
		case "extension":
			extNumber, _ = strconv.Atoi(attr(se, "number"))
		case "enum":
			// Only reached inside <require> (the other blocks are consumed above).
			if attr(se, "extends") != "" && !scOnly && forVulkan(se) {
				p := pendingEnum{
					extends: attr(se, "extends"), name: attr(se, "name"), alias: attr(se, "alias"),
					value: attr(se, "value"), bitpos: attr(se, "bitpos"), offset: attr(se, "offset"),
					ext: extNumber, neg: attr(se, "dir") == "-",
				}
				if n, err := strconv.Atoi(attr(se, "extnumber")); err == nil {
					p.ext = n
				}
				reg.pending = append(reg.pending, p)
			}
		}
	}
	if !sawRegistry {
		return nil, errors.New("vkgen: no <registry> element")
	}
	if err := reg.resolve(); err != nil {
		return nil, err
	}
	return reg, nil
}

// forVulkan reports whether an element applies to the vulkan API: it has no
// api attribute, or the comma-separated list names vulkan. Vulkan SC variants
// of a member or an enumerator would otherwise duplicate the Vulkan one.
func forVulkan(se xml.StartElement) bool {
	a := attr(se, "api")
	if a == "" {
		return true
	}
	for _, x := range strings.Split(a, ",") {
		if strings.TrimSpace(x) == "vulkan" {
			return true
		}
	}
	return false
}

func attr(se xml.StartElement, name string) string {
	for _, a := range se.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// inner reads the content of the element just started, as text and child
// elements (with their text), up to its end tag.
func inner(d *xml.Decoder) ([]node, error) {
	var out []node
	depth := 1
	var cur *node
	for depth > 0 {
		tok, err := d.Token()
		if err != nil {
			return nil, fmt.Errorf("vkgen: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 1 {
				out = append(out, node{elem: t.Name.Local})
				cur = &out[len(out)-1]
			}
			depth++
		case xml.EndElement:
			depth--
			if depth == 1 {
				cur = nil
			}
		case xml.CharData:
			if depth == 1 {
				out = append(out, node{text: string(t)})
			} else if cur != nil {
				cur.text += string(t)
			}
		}
	}
	return out, nil
}

func (r *Registry) parseType(d *xml.Decoder, se xml.StartElement) error {
	cat := attr(se, "category")
	name := attr(se, "name")
	alias := attr(se, "alias")
	if cat == "" {
		return nil // a reference inside <require>, or the <type> inside a member
	}
	if !forVulkan(se) {
		return d.Skip()
	}
	if alias != "" && name != "" {
		r.Aliases[name] = alias
		return d.Skip()
	}
	switch cat {
	case "enum":
		if name != "" {
			r.EnumTypes[name] = true
		}
		return d.Skip()
	case "struct":
		return r.parseStruct(d, name)
	case "define":
		nodes, err := inner(d)
		if err != nil {
			return err
		}
		for i, n := range nodes {
			if n.elem == "name" && strings.TrimSpace(n.text) == "VK_HEADER_VERSION" && i+1 < len(nodes) {
				r.Version = strings.TrimSpace(nodes[i+1].text)
			}
		}
		return nil
	case "basetype", "bitmask", "handle":
		nodes, err := inner(d)
		if err != nil {
			return err
		}
		var typ, nm string
		for _, n := range nodes {
			switch n.elem {
			case "type":
				typ = strings.TrimSpace(n.text)
			case "name":
				nm = strings.TrimSpace(n.text)
			}
		}
		if nm == "" {
			nm = name
		}
		switch {
		case nm == "" || typ == "":
			// An opaque typedef (void) or a requirement line: nothing to record.
		case cat == "basetype":
			r.Base[nm] = typ
		case cat == "bitmask":
			r.Flags[nm] = typ
		default:
			r.Handles[nm] = typ == "VK_DEFINE_HANDLE"
		}
		return nil
	}
	return d.Skip()
}

func (r *Registry) parseStruct(d *xml.Decoder, name string) error {
	s := &Struct{Name: name}
	for {
		tok, err := d.Token()
		if err != nil {
			return fmt.Errorf("vkgen: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if name != "" {
				r.Structs[name] = s
			}
			return nil
		case xml.StartElement:
			if t.Name.Local != "member" || !forVulkan(t) {
				if err := d.Skip(); err != nil {
					return fmt.Errorf("vkgen: %w", err)
				}
				continue
			}
			nodes, err := inner(d)
			if err != nil {
				return err
			}
			m, err := member(nodes)
			if err != nil {
				return fmt.Errorf("vkgen: struct %s: %w", name, err)
			}
			m.Values = attr(t, "values")
			s.Members = append(s.Members, m)
		}
	}
}

func member(nodes []node) (Member, error) {
	var m Member
	var after strings.Builder
	seenName := false
	for _, n := range nodes {
		switch {
		case n.elem == "type" && m.Type == "":
			m.Type = strings.TrimSpace(n.text)
		case n.elem == "name" && !seenName:
			m.Name = strings.TrimSpace(n.text)
			seenName = true
		case n.elem == "comment":
		case seenName:
			after.WriteString(n.text)
		case n.elem == "" && m.Type != "":
			if strings.Contains(n.text, "*") {
				m.Pointer = true
			}
		}
	}
	if m.Type == "" || m.Name == "" {
		return m, errors.New("member without a type or a name")
	}
	rest := strings.TrimSpace(after.String())
	if strings.HasPrefix(rest, ":") {
		m.Bits = strings.TrimSpace(rest[1:])
		return m, nil
	}
	for strings.HasPrefix(rest, "[") {
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return m, fmt.Errorf("member %s: unterminated array dimension", m.Name)
		}
		m.Dims = append(m.Dims, strings.TrimSpace(rest[1:end]))
		rest = strings.TrimSpace(rest[end+1:])
	}
	return m, nil
}

func (r *Registry) parseEnums(d *xml.Decoder, se xml.StartElement) error {
	name := attr(se, "name")
	typ := attr(se, "type")
	if name == "API Constants" {
		return r.parseConstants(d)
	}
	e := &Enum{Name: name, Bitmask: typ == "bitmask", Wide: attr(se, "bitwidth") == "64"}
	for {
		tok, err := d.Token()
		if err != nil {
			return fmt.Errorf("vkgen: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if name != "" && (typ == "enum" || typ == "bitmask") {
				r.Enums[name] = e
			}
			return nil
		case xml.StartElement:
			if t.Name.Local == "enum" && forVulkan(t) {
				r.pending = append(r.pending, pendingEnum{
					extends: name, name: attr(t, "name"), alias: attr(t, "alias"),
					value: attr(t, "value"), bitpos: attr(t, "bitpos"),
				})
			}
			if err := d.Skip(); err != nil {
				return fmt.Errorf("vkgen: %w", err)
			}
		}
	}
}

func (r *Registry) parseConstants(d *xml.Decoder) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return fmt.Errorf("vkgen: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return nil
		case xml.StartElement:
			if t.Name.Local == "enum" && attr(t, "name") != "" {
				r.Constants[attr(t, "name")] = Constant{Type: attr(t, "type"), Value: attr(t, "value")}
				if a := attr(t, "alias"); a != "" {
					r.Aliases[attr(t, "name")] = a
				}
			}
			if err := d.Skip(); err != nil {
				return fmt.Errorf("vkgen: %w", err)
			}
		}
	}
}

// resolve turns the enumerators collected in document order into values:
// plain values, bit positions, extension offsets (Vulkan spec, "Extending
// Enumerations": 1000000000 + (extnumber-1)*1000 + offset) and aliases.
func (r *Registry) resolve() error {
	have := map[string]int64{}
	var later []pendingEnum
	add := func(p pendingEnum, v int64) {
		e, ok := r.Enums[p.extends]
		if !ok {
			return // an enumeration the registry does not declare as enum or bitmask
		}
		if _, dup := have[p.name]; dup {
			return
		}
		have[p.name] = v
		e.Values = append(e.Values, EnumValue{Name: p.name, Value: v})
	}
	for _, p := range r.pending {
		if p.name == "" {
			continue
		}
		if p.alias != "" {
			later = append(later, p)
			continue
		}
		v, ok, err := p.number()
		if err != nil {
			return err
		}
		if ok {
			add(p, v)
		}
	}
	for range 4 { // aliases of aliases
		var rest []pendingEnum
		for _, p := range later {
			if v, ok := have[p.alias]; ok {
				add(p, v)
			} else {
				rest = append(rest, p)
			}
		}
		later = rest
	}
	r.pending = nil
	return nil
}

func (p pendingEnum) number() (int64, bool, error) {
	switch {
	case p.value != "":
		v, err := ParseInt(p.value)
		if err != nil {
			return 0, false, nil // a macro or an expression: not a value we mirror
		}
		return v, true, nil
	case p.bitpos != "":
		n, err := strconv.Atoi(p.bitpos)
		if err != nil || n < 0 || n > 63 {
			return 0, false, fmt.Errorf("vkgen: enumerator %s: bad bitpos %q", p.name, p.bitpos)
		}
		return int64(1) << uint(n), true, nil
	case p.offset != "":
		off, err := strconv.Atoi(p.offset)
		if err != nil || off < 0 || p.ext < 1 || off > 999 || p.ext > 1_000_000 {
			return 0, false, fmt.Errorf("vkgen: enumerator %s: bad offset %q (extension %d)", p.name, p.offset, p.ext)
		}
		v := int64(1_000_000_000) + int64(p.ext-1)*1000 + int64(off)
		if p.neg {
			v = -v
		}
		return v, true, nil
	}
	return 0, false, nil
}

// ParseInt reads a decimal or 0x-prefixed integer, optionally negative.
func ParseInt(s string) (int64, error) {
	s = strings.TrimSpace(s)
	v, err := strconv.ParseInt(s, 0, 64)
	if err != nil {
		return 0, err
	}
	return v, nil
}
