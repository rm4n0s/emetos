package snippet

import (
	"slices"
	"strconv"
	"strings"
)

// Ported from VS Code: src/vs/editor/contrib/snippet/browser/snippetParser.ts

// Marker is a node of a parsed snippet: text, a tab stop (Placeholder), a Choice, a Variable, a
// Transform or the format strings in it, or the TextmateSnippet that is the root. Lengths and
// offsets of markers count bytes of the text they stand for.
type Marker interface {
	// Parent is the marker that holds this one, or nil for the root.
	Parent() Marker
	// Children are the markers inside this one.
	Children() []Marker
	// AppendChild adds a marker at the end; text added after text joins it.
	AppendChild(child Marker)
	// String is the text the marker inserts: placeholders and variables give their default text.
	String() string
	// ToTextmateString is the snippet syntax that parses back to this marker.
	ToTextmateString() string
	// Len is the length in bytes of the text of the marker itself, not counting its children.
	Len() int
	// Clone is a copy of the marker and of everything in it.
	Clone() Marker

	base() *markerBase
}

// markerBase is the part that every marker has: its parent and its children.
type markerBase struct {
	parent   Marker
	children []Marker
}

func (b *markerBase) base() *markerBase { return b }

// Parent implements Marker.
func (b *markerBase) Parent() Marker { return b.parent }

// Children implements Marker.
func (b *markerBase) Children() []Marker { return b.children }

// String implements Marker: the text of the children.
func (b *markerBase) String() string {
	var sb strings.Builder
	for _, c := range b.children {
		sb.WriteString(c.String())
	}
	return sb.String()
}

// Len implements Marker.
func (b *markerBase) Len() int { return 0 }

// appendChild adds a child to a parent; text that follows text is merged into it.
func appendChild(parent, child Marker) {
	b := parent.base()
	if t, ok := child.(*Text); ok && len(b.children) > 0 {
		if last, ok := b.children[len(b.children)-1].(*Text); ok {
			last.Value += t.Value
			return
		}
	}
	child.base().parent = parent
	b.children = append(b.children, child)
}

// adopt makes parent the parent of the markers, and of everything in them.
func adopt(parent Marker, markers []Marker) {
	for _, m := range markers {
		m.base().parent = parent
		adopt(m, m.Children())
	}
}

// cloneChildren copies the children of a marker into another one.
func cloneChildren(from, to Marker) {
	children := make([]Marker, len(from.Children()))
	for i, c := range from.Children() {
		children[i] = c.Clone()
	}
	to.base().children = children
	adopt(to, children)
}

// Replace puts markers in the place of a child of its parent.
func Replace(child Marker, others []Marker) {
	parent := child.Parent()
	if parent == nil {
		return
	}
	idx := slices.Index(parent.Children(), child)
	if idx < 0 {
		return
	}
	children := slices.Concat(parent.Children()[:idx:idx], others, parent.Children()[idx+1:])
	parent.base().children = children
	adopt(parent, others)
}

// SnippetOf is the snippet a marker is in, or nil when it is not in one.
func SnippetOf(m Marker) *TextmateSnippet {
	for m != nil {
		if s, ok := m.(*TextmateSnippet); ok {
			return s
		}
		m = m.Parent()
	}
	return nil
}

// RightMostDescendant is the last marker of the last marker of ... this one.
func RightMostDescendant(m Marker) Marker {
	for len(m.Children()) > 0 {
		m = m.Children()[len(m.Children())-1]
	}
	return m
}

// Text is plain text.
type Text struct {
	markerBase
	// Value is the text.
	Value string
}

// NewText makes a text marker.
func NewText(value string) *Text { return &Text{Value: value} }

// EscapeText puts a backslash before the characters that a snippet reads as syntax in plain
// text: $, } and \.
func EscapeText(value string) string {
	var sb strings.Builder
	for i := 0; i < len(value); i++ {
		if c := value[i]; c == '$' || c == '}' || c == '\\' {
			sb.WriteByte('\\')
		}
		sb.WriteByte(value[i])
	}
	return sb.String()
}

// AppendChild implements Marker.
func (t *Text) AppendChild(child Marker) { appendChild(t, child) }

// String implements Marker.
func (t *Text) String() string { return t.Value }

// ToTextmateString implements Marker.
func (t *Text) ToTextmateString() string { return EscapeText(t.Value) }

// Len implements Marker.
func (t *Text) Len() int { return len(t.Value) }

// Clone implements Marker.
func (t *Text) Clone() Marker { return NewText(t.Value) }

// Placeholder is a tab stop: $1, ${1:default}, ${1|a,b|} or ${1/regex/format/flags}. The tab stop
// 0 is the final one.
type Placeholder struct {
	markerBase
	// Index is the number of the tab stop.
	Index int
	// Transform changes the text the placeholder mirrors, or is nil.
	Transform *Transform
}

// NewPlaceholder makes a placeholder.
func NewPlaceholder(index int) *Placeholder { return &Placeholder{Index: index} }

// ComparePlaceholders orders placeholders by index, with the final tab stop last.
//
// Ported from VS Code: Placeholder.compareByIndex
func ComparePlaceholders(a, b *Placeholder) int {
	switch {
	case a.Index == b.Index:
		return 0
	case a.IsFinalTabstop():
		return 1
	case b.IsFinalTabstop():
		return -1
	case a.Index < b.Index:
		return -1
	}
	return 1
}

// IsFinalTabstop tells the tab stop $0.
func (p *Placeholder) IsFinalTabstop() bool { return p.Index == 0 }

// Choice is the choice of the placeholder, or nil when its content is not just one.
func (p *Placeholder) Choice() *Choice {
	if len(p.children) == 1 {
		c, _ := p.children[0].(*Choice)
		return c
	}
	return nil
}

// AppendChild implements Marker.
func (p *Placeholder) AppendChild(child Marker) { appendChild(p, child) }

// ToTextmateString implements Marker.
func (p *Placeholder) ToTextmateString() string {
	transform := ""
	if p.Transform != nil {
		transform = p.Transform.ToTextmateString()
	}
	switch {
	case len(p.children) == 0 && p.Transform == nil:
		return "$" + strconv.Itoa(p.Index)
	case len(p.children) == 0:
		return "${" + strconv.Itoa(p.Index) + transform + "}"
	case p.Choice() != nil:
		return "${" + strconv.Itoa(p.Index) + "|" + p.Choice().ToTextmateString() + "|" + transform + "}"
	}
	return "${" + strconv.Itoa(p.Index) + ":" + textmateOf(p.children) + transform + "}"
}

// Clone implements Marker.
func (p *Placeholder) Clone() Marker {
	ret := NewPlaceholder(p.Index)
	if p.Transform != nil {
		ret.Transform = p.Transform.Clone().(*Transform)
	}
	cloneChildren(p, ret)
	return ret
}

// textmateOf is the snippet syntax of markers, one after the other.
func textmateOf(markers []Marker) string {
	var sb strings.Builder
	for _, m := range markers {
		sb.WriteString(m.ToTextmateString())
	}
	return sb.String()
}

// Choice is the options of a placeholder, ${1|one,two|}. The first is what is inserted.
type Choice struct {
	markerBase
	// Options are the choices.
	Options []*Text
}

// AppendChild implements Marker: only text is a choice, and it is kept in Options.
func (c *Choice) AppendChild(child Marker) {
	if t, ok := child.(*Text); ok {
		t.parent = c
		c.Options = append(c.Options, t)
	}
}

// String implements Marker: the first option.
func (c *Choice) String() string { return c.Options[0].Value }

// ToTextmateString implements Marker.
func (c *Choice) ToTextmateString() string {
	parts := make([]string, len(c.Options))
	for i, o := range c.Options {
		var sb strings.Builder
		for j := 0; j < len(o.Value); j++ {
			if ch := o.Value[j]; ch == '|' || ch == ',' || ch == '\\' {
				sb.WriteByte('\\')
			}
			sb.WriteByte(o.Value[j])
		}
		parts[i] = sb.String()
	}
	return strings.Join(parts, ",")
}

// Len implements Marker.
func (c *Choice) Len() int { return c.Options[0].Len() }

// Clone implements Marker.
func (c *Choice) Clone() Marker {
	ret := &Choice{}
	for _, o := range c.Options {
		ret.AppendChild(NewText(o.Value))
	}
	return ret
}

// Variable is a name that a VariableResolver gives a value to: $name, ${name} or
// ${name:default}, with an optional transform.
type Variable struct {
	markerBase
	// Name is the name of the variable.
	Name string
	// Transform changes the value, or is nil.
	Transform *Transform
}

// NewVariable makes a variable.
func NewVariable(name string) *Variable { return &Variable{Name: name} }

// VariableResolver gives values to the variables of a snippet.
type VariableResolver interface {
	// Resolve is the value of a variable; ok is false when the resolver has none, which leaves the
	// default text of the variable.
	Resolve(v *Variable) (value string, ok bool)
}

// Resolve gives the variable the value the resolver has, after its transform, as its only
// content; it tells whether it got one. A variable with a transform always gets one: the
// transform sees an empty value when the resolver has none.
func (v *Variable) Resolve(r VariableResolver) bool {
	value, ok := r.Resolve(v)
	if v.Transform != nil {
		value, ok = v.Transform.Resolve(value), true
	}
	if !ok {
		return false
	}
	text := NewText(value)
	text.parent = v
	v.children = []Marker{text}
	return true
}

// AppendChild implements Marker.
func (v *Variable) AppendChild(child Marker) { appendChild(v, child) }

// ToTextmateString implements Marker.
func (v *Variable) ToTextmateString() string {
	transform := ""
	if v.Transform != nil {
		transform = v.Transform.ToTextmateString()
	}
	if len(v.children) == 0 {
		return "${" + v.Name + transform + "}"
	}
	return "${" + v.Name + ":" + textmateOf(v.children) + transform + "}"
}

// Clone implements Marker.
func (v *Variable) Clone() Marker {
	ret := NewVariable(v.Name)
	if v.Transform != nil {
		ret.Transform = v.Transform.Clone().(*Transform)
	}
	cloneChildren(v, ret)
	return ret
}

// walk visits markers depth first, each marker before what is in it; the walk stops when the
// visitor says false.
func walk(markers []Marker, visit func(Marker) bool) {
	stack := slices.Clone(markers)
	for len(stack) > 0 {
		m := stack[0]
		stack = stack[1:]
		if !visit(m) {
			return
		}
		stack = slices.Concat(m.Children(), stack)
	}
}

// TextmateSnippet is the root of a parsed snippet.
type TextmateSnippet struct {
	markerBase
	placeholders *placeholderInfo
}

type placeholderInfo struct {
	all  []*Placeholder
	last *Placeholder
}

// NewTextmateSnippet makes an empty snippet.
func NewTextmateSnippet() *TextmateSnippet { return &TextmateSnippet{} }

func (s *TextmateSnippet) info() *placeholderInfo {
	if s.placeholders == nil {
		info := &placeholderInfo{}
		s.Walk(func(m Marker) bool {
			if p, ok := m.(*Placeholder); ok {
				info.all = append(info.all, p)
				if info.last == nil || info.last.Index < p.Index {
					info.last = p
				}
			}
			return true
		})
		s.placeholders = info
	}
	return s.placeholders
}

// Placeholders are all the placeholders, in the order they are in the snippet.
func (s *TextmateSnippet) Placeholders() []*Placeholder { return s.info().all }

// Offset is the offset in the text of the snippet where a marker starts, or -1 for a marker that
// is not in the snippet.
func (s *TextmateSnippet) Offset(marker Marker) int {
	pos, found := 0, false
	s.Walk(func(m Marker) bool {
		if m == marker {
			found = true
			return false
		}
		pos += m.Len()
		return true
	})
	if !found {
		return -1
	}
	return pos
}

// FullLen is the length of the text of a marker and of everything in it.
func (s *TextmateSnippet) FullLen(marker Marker) int {
	n := 0
	walk([]Marker{marker}, func(m Marker) bool {
		n += m.Len()
		return true
	})
	return n
}

// EnclosingPlaceholders are the placeholders a placeholder is in, the nearest first.
func (s *TextmateSnippet) EnclosingPlaceholders(p *Placeholder) []*Placeholder {
	var ret []*Placeholder
	for parent := p.Parent(); parent != nil; parent = parent.Parent() {
		if enclosing, ok := parent.(*Placeholder); ok {
			ret = append(ret, enclosing)
		}
	}
	return ret
}

// ResolveVariables gives the variables their values.
func (s *TextmateSnippet) ResolveVariables(r VariableResolver) *TextmateSnippet {
	s.Walk(func(m Marker) bool {
		if v, ok := m.(*Variable); ok && v.Resolve(r) {
			s.placeholders = nil
		}
		return true
	})
	return s
}

// AppendChild implements Marker.
func (s *TextmateSnippet) AppendChild(child Marker) {
	s.placeholders = nil
	appendChild(s, child)
}

// Replace puts markers in the place of a marker of the snippet.
func (s *TextmateSnippet) Replace(child Marker, others []Marker) {
	s.placeholders = nil
	Replace(child, others)
}

// ToTextmateString implements Marker.
func (s *TextmateSnippet) ToTextmateString() string { return textmateOf(s.children) }

// Clone implements Marker.
func (s *TextmateSnippet) Clone() Marker {
	ret := NewTextmateSnippet()
	cloneChildren(s, ret)
	return ret
}

// Walk visits the markers of the snippet depth first, each before what is in it; it stops when
// the visitor says false.
func (s *TextmateSnippet) Walk(visit func(Marker) bool) { walk(s.children, visit) }
