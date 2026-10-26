package golang

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The tag editor of "Go: Add Tags To Struct Fields" and "Go: Remove Tags From Struct Fields" (SPEC §10.17 "As built (step
// 14.2)"). gopls does the same with its command gopls.modify_tags, which is gomodifytags inside the server; this is the same
// algorithm in Go for a server that does not have the command, working on the text of the document: it parses the file, changes
// the tags of the fields in the range in the syntax tree, prints the whole file the way gofmt does (so that the columns of the
// tags are aligned) and gives the difference as edits.
//
// Ported from gomodifytags (BSD-3-Clause, Fatih Arslan): modifytags/modifytags.go, with the tag parser of fatih/structtag
// (BSD-3-Clause) and the word splitter of fatih/camelcase (MIT); third_party/gomodifytags/ has their licenses.

// tagMod says what to do to the tags of the fields of a range (gomodifytags' Modification).
type tagMod struct {
	// Add are the tags to add ("json", or "json:name" to give the name), AddOptions the options to add per tag.
	Add        []string
	AddOptions map[string][]string
	// Remove are the tags to remove, RemoveOptions the options to remove per tag.
	Remove        []string
	RemoveOptions map[string][]string
	// Overwrite replaces the names of tags that exist when adding; SkipUnexported leaves the unexported fields alone.
	Overwrite, SkipUnexported bool
	// Transform is how a field name becomes the name in a tag: snakecase (the default), camelcase, lispcase, pascalcase,
	// titlecase or keep.
	Transform string
	// ValueFormat is the format of the value after the transformation, such as "column:{field}".
	ValueFormat string
	// Clear removes all tags, before any is added; ClearOptions removes all options.
	Clear, ClearOptions bool
}

// transforms are the names tagMod.Transform may have.
var transforms = []string{"", "snakecase", "camelcase", "lispcase", "pascalcase", "titlecase", "keep"}

// validate checks that the modification does something and that its transform is a known one.
func (m tagMod) validate() error {
	known := false
	for _, t := range transforms {
		known = known || t == m.Transform
	}
	switch {
	case !known:
		return errors.New("TagsRequestInvalid", "the transform is snakecase, camelcase, lispcase, pascalcase, titlecase or keep", "transform", m.Transform)
	case len(m.Add) == 0 && len(m.AddOptions) == 0 && !m.Clear && !m.ClearOptions && len(m.RemoveOptions) == 0 && len(m.Remove) == 0:
		return errors.New("TagsRequestInvalid", "one of the tags to add, the options to add, the tags to remove, the options to remove, clear tags or clear options is needed")
	}
	return nil
}

// modifyTags changes the tags of the fields of the structs that touch the byte range [start, end] of the Go source src and returns
// the new text. With start == end (a cursor) the range is the innermost struct that holds it. A source with syntax errors fails
// with TagsSourceInvalid, a cursor outside any struct with TagsNoStruct, a tag that cannot be read with TagInvalid, and a
// modification that asks for nothing or has an unknown transform with TagsRequestInvalid.
func modifyTags(src string, start, end int, mod tagMod) (string, error) {
	if err := mod.validate(); err != nil {
		return "", err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "document.go", src, parser.ParseComments)
	if err != nil {
		return "", errors.NewErr("TagsSourceInvalid", err).SetMessage("the file has a syntax error: " + err.Error())
	}
	tf := fset.File(file.Pos())
	if tf == nil || start < 0 || end < start || end > tf.Size() {
		return "", errors.New("TagsRequestInvalid", "the range is outside the text", "start", start, "end", end)
	}
	from, to := tf.Pos(start), tf.Pos(end)
	if from == to {
		var enclosing *ast.StructType
		ast.Inspect(file, func(n ast.Node) bool {
			if s, ok := n.(*ast.StructType); ok && s.Pos() <= from && from <= s.End() {
				enclosing = s
			}
			return true
		})
		if enclosing == nil {
			return "", errors.New("TagsNoStruct", "the cursor is not in a struct type")
		}
		from, to = enclosing.Pos(), enclosing.End()
	}
	var failures []string
	ast.Inspect(file, func(n ast.Node) bool {
		s, ok := n.(*ast.StructType)
		if !ok {
			return true
		}
		for _, f := range s.Fields.List {
			if from > f.End() || f.Pos() > to {
				continue
			}
			name := ""
			for _, id := range f.Names {
				if !mod.SkipUnexported || isExported(id.Name) {
					name = id.Name
					break
				}
			}
			if f.Names == nil { // an embedded field
				id, ok := f.Type.(*ast.Ident)
				if !ok {
					continue
				}
				if !mod.SkipUnexported {
					name = id.Name
				}
			}
			if name == "" {
				continue
			}
			current := ""
			if f.Tag != nil {
				current = f.Tag.Value
			}
			res, err := mod.processField(name, current)
			if err != nil {
				pos := fset.Position(f.Pos())
				failures = append(failures, strconv.Itoa(pos.Line)+":"+strconv.Itoa(pos.Column)+": "+err.Error())
				continue
			}
			switch {
			case res == "":
				f.Tag = nil
			case f.Tag == nil:
				f.Tag = &ast.BasicLit{Value: res}
			default:
				f.Tag.Value = res
			}
		}
		return true
	})
	if len(failures) > 0 {
		return "", errors.New("TagInvalid", "the tag of a field cannot be read: "+strings.Join(failures, "; "), "fields", len(failures))
	}
	var out bytes.Buffer
	if err := format.Node(&out, fset, file); err != nil {
		return "", errors.NewErr("TagsSourceInvalid", err)
	}
	return out.String(), nil
}

// isExported tells whether the name starts with an upper case letter.
func isExported(name string) bool {
	for _, c := range name {
		return unicode.IsUpper(c)
	}
	return false
}

// processField is the new tag literal of a field (a raw string literal with backquotes, or "" for no tag) given its current
// literal.
func (m tagMod) processField(field, literal string) (string, error) {
	text := ""
	if literal != "" {
		var err error
		if text, err = strconv.Unquote(literal); err != nil {
			return "", err
		}
	}
	tags, err := parseTags(text)
	if err != nil {
		return "", err
	}
	if len(m.Remove) > 0 {
		tags.delete(m.Remove...)
	}
	for key, opts := range m.RemoveOptions {
		for _, o := range opts {
			tags.deleteOptions(key, o)
		}
	}
	if m.Clear {
		tags.delete(tags.keys()...)
	}
	if m.ClearOptions {
		for _, t := range tags.list {
			t.Options = nil
		}
	}
	m.addTags(field, tags)
	for key, opts := range m.AddOptions {
		tags.addOptions(key, opts...)
	}
	res := tags.String()
	if res == "" {
		return "", nil
	}
	return "`" + res + "`", nil
}

// addTags adds the tags of the modification to a field.
func (m tagMod) addTags(field string, tags *structTags) {
	if len(m.Add) == 0 {
		return
	}
	words := splitWords(field)
	var name string
	switch m.Transform {
	case "lispcase":
		lower := make([]string, len(words))
		for i, w := range words {
			lower[i] = strings.ToLower(w)
		}
		name = strings.Join(lower, "-")
	case "camelcase":
		titled := make([]string, len(words))
		for i, w := range words {
			titled[i] = titleWord(w)
		}
		titled[0] = strings.ToLower(titled[0])
		name = strings.Join(titled, "")
	case "pascalcase":
		titled := make([]string, len(words))
		for i, w := range words {
			titled[i] = titleWord(w)
		}
		name = strings.Join(titled, "")
	case "titlecase":
		titled := make([]string, len(words))
		for i, w := range words {
			titled[i] = titleWord(w)
		}
		name = strings.Join(titled, " ")
	case "keep":
		name = field
	default: // snakecase
		var lower []string
		for _, w := range words {
			if w = strings.Trim(w, "_"); w != "" {
				lower = append(lower, strings.ToLower(w))
			}
		}
		name = strings.Join(lower, "_")
	}
	if m.ValueFormat != "" {
		previous := name
		name = strings.ReplaceAll(m.ValueFormat, "{field}", name)
		if name == m.ValueFormat { // the old style, for compatibility
			name = strings.ReplaceAll(m.ValueFormat, "$field", previous)
		}
	}
	for _, key := range m.Add {
		if k, v, ok := strings.Cut(key, ":"); ok {
			key, name = k, v
		}
		if t := tags.get(key); t == nil {
			tags.set(&structTag{Key: key, Name: name})
		} else if m.Overwrite {
			t.Name = name
		}
	}
}

// titleWord upper cases the first letter of a word (the words splitWords gives are of one kind of characters, so this is what
// strings.Title makes of them).
func titleWord(w string) string {
	r, n := utf8.DecodeRuneInString(w)
	if n == 0 {
		return w
	}
	return string(unicode.ToTitle(r)) + w[n:]
}

// splitWords splits a Go name into its words: "MyField" is My and Field, "HTTPServer" is HTTP and Server, "GL11Version" is GL, 11
// and Version. A text that is not valid UTF-8 is one word.
//
// Ported from fatih/camelcase: camelcase.go (Split)
func splitWords(src string) []string {
	if !utf8.ValidString(src) {
		return []string{src}
	}
	var runes [][]rune
	last := 0
	for _, r := range src {
		class := 4
		switch {
		case unicode.IsLower(r):
			class = 1
		case unicode.IsUpper(r):
			class = 2
		case unicode.IsDigit(r):
			class = 3
		}
		if class == last {
			runes[len(runes)-1] = append(runes[len(runes)-1], r)
		} else {
			runes = append(runes, []rune{r})
		}
		last = class
	}
	// an upper case run followed by a lower case run gives its last letter to it: "PDFL" "oader" is "PDF" "Loader"
	for i := 0; i < len(runes)-1; i++ {
		if unicode.IsUpper(runes[i][0]) && unicode.IsLower(runes[i+1][0]) {
			runes[i+1] = append([]rune{runes[i][len(runes[i])-1]}, runes[i+1]...)
			runes[i] = runes[i][:len(runes[i])-1]
		}
	}
	words := []string{}
	for _, r := range runes {
		if len(r) > 0 {
			words = append(words, string(r))
		}
	}
	if len(words) == 0 {
		return []string{""}
	}
	return words
}

// ---- the tags of one field ----

// structTag is one key:"name,option,..." of a field's tag.
type structTag struct {
	Key, Name string
	Options   []string
}

// value is the quoted part: name,option,...
func (t *structTag) value() string {
	if len(t.Options) == 0 {
		return t.Name
	}
	return t.Name + "," + strings.Join(t.Options, ",")
}

// String is the tag as it is written.
func (t *structTag) String() string { return t.Key + ":" + strconv.Quote(t.value()) }

func (t *structTag) hasOption(o string) bool {
	for _, x := range t.Options {
		if x == o {
			return true
		}
	}
	return false
}

// structTags are the tags of a field, in order.
type structTags struct{ list []*structTag }

// parseTags reads the text of a field tag (the content of the literal). It fails with TagInvalid for a tag that is not a list of
// key:"value" pairs.
//
// Ported from fatih/structtag: tags.go (Parse)
func parseTags(tag string) (*structTags, error) {
	tags := &structTags{}
	for tag != "" {
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}
		// scan to the colon: a space, a quote or a control character is a syntax error
		i = 0
		for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' && tag[i] != 0x7f {
			i++
		}
		switch {
		case i == 0:
			return nil, errors.New("TagInvalid", "bad syntax for struct tag key", "tag", tag)
		case i+1 >= len(tag) || tag[i] != ':':
			return nil, errors.New("TagInvalid", "bad syntax for struct tag pair", "tag", tag)
		case tag[i+1] != '"':
			return nil, errors.New("TagInvalid", "bad syntax for struct tag value", "tag", tag)
		}
		key := tag[:i]
		tag = tag[i+1:]
		// scan the quoted string
		i = 1
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			return nil, errors.New("TagInvalid", "bad syntax for struct tag value", "tag", tag)
		}
		quoted := tag[:i+1]
		tag = tag[i+1:]
		value, err := strconv.Unquote(quoted)
		if err != nil {
			return nil, errors.New("TagInvalid", "bad syntax for struct tag value", "tag", quoted)
		}
		parts := strings.Split(value, ",")
		t := &structTag{Key: key, Name: parts[0]}
		if len(parts) > 1 {
			t.Options = parts[1:]
		}
		tags.list = append(tags.list, t)
	}
	return tags, nil
}

func (ts *structTags) get(key string) *structTag {
	for _, t := range ts.list {
		if t.Key == key {
			return t
		}
	}
	return nil
}

// set puts the tag in place of the one with its key, or at the end.
func (ts *structTags) set(tag *structTag) {
	for i, t := range ts.list {
		if t.Key == tag.Key {
			ts.list[i] = tag
			return
		}
	}
	ts.list = append(ts.list, tag)
}

func (ts *structTags) keys() []string {
	keys := make([]string, len(ts.list))
	for i, t := range ts.list {
		keys[i] = t.Key
	}
	return keys
}

func (ts *structTags) delete(keys ...string) {
	var kept []*structTag
	for _, t := range ts.list {
		drop := false
		for _, k := range keys {
			drop = drop || k == t.Key
		}
		if !drop {
			kept = append(kept, t)
		}
	}
	ts.list = kept
}

func (ts *structTags) addOptions(key string, options ...string) {
	for _, t := range ts.list {
		if t.Key != key {
			continue
		}
		for _, o := range options {
			if !t.hasOption(o) {
				t.Options = append(t.Options, o)
			}
		}
	}
}

func (ts *structTags) deleteOptions(key string, options ...string) {
	for _, t := range ts.list {
		if t.Key != key {
			continue
		}
		var kept []string
		for _, o := range t.Options {
			drop := false
			for _, x := range options {
				drop = drop || x == o
			}
			if !drop {
				kept = append(kept, o)
			}
		}
		t.Options = kept
	}
}

// String is the tags as they are written in a literal.
func (ts *structTags) String() string {
	parts := make([]string, len(ts.list))
	for i, t := range ts.list {
		parts[i] = t.String()
	}
	return strings.Join(parts, " ")
}

// parseOptions reads "json=omitempty,xml=cdata" into the options per tag (gopls' optionsStringToMap). A part without "=" fails with
// TagsRequestInvalid.
func parseOptions(text string) (map[string][]string, error) {
	out := map[string][]string{}
	if strings.TrimSpace(text) == "" {
		return out, nil
	}
	for _, item := range strings.Split(text, ",") {
		key, option, found := strings.Cut(item, "=")
		if !found {
			return nil, errors.New("TagsRequestInvalid", "an option is written tag=option", "option", item)
		}
		out[key] = append(out[key], option)
	}
	return out, nil
}

// splitList splits a comma separated list, without the empty parts.
func splitList(text string) []string {
	var out []string
	for _, p := range strings.Split(text, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---- edits ----

// textEdits is the difference between two texts as edits of the first. When both have the same lines, each line that changed is
// an edit of the part of it that changed, so that the cursors and the selections in the lines are left where they are; else one
// edit covers what differs.
func textEdits(oldText, newText string) []extension.RangeEdit {
	if oldText == newText {
		return nil
	}
	a, b := strings.Split(oldText, "\n"), strings.Split(newText, "\n")
	if len(a) == len(b) {
		var edits []extension.RangeEdit
		for i := range a {
			if a[i] == b[i] {
				continue
			}
			p := commonPrefix(a[i], b[i])
			s := commonSuffix(a[i][p:], b[i][p:])
			edits = append(edits, extension.RangeEdit{
				Range: extension.Range{Start: extension.Position{Line: i, Col: p}, End: extension.Position{Line: i, Col: len(a[i]) - s}},
				Text:  b[i][p : len(b[i])-s],
			})
		}
		return edits
	}
	p := commonPrefix(oldText, newText)
	s := commonSuffix(oldText[p:], newText[p:])
	return []extension.RangeEdit{{
		Range: extension.Range{Start: positionAt(oldText, p), End: positionAt(oldText, len(oldText)-s)},
		Text:  newText[p : len(newText)-s],
	}}
}

// commonPrefix is how many bytes the two texts start with in common, not cutting a character.
func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	for n > 0 && n < len(a) && !utf8.RuneStart(a[n]) {
		n--
	}
	return n
}

// commonSuffix is how many bytes the two texts end with in common, not cutting a character.
func commonSuffix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[len(a)-1-n] == b[len(b)-1-n] {
		n++
	}
	for n > 0 && n < len(a) && !utf8.RuneStart(a[len(a)-n]) {
		n--
	}
	return n
}

// positionAt is the line and the column in bytes of a byte offset of the text.
func positionAt(text string, offset int) extension.Position {
	line := strings.Count(text[:offset], "\n")
	col := offset - (strings.LastIndex(text[:offset], "\n") + 1)
	return extension.Position{Line: line, Col: col}
}

// offsetOf is the byte offset of a position in the text; a position outside the text is moved into it.
func offsetOf(text string, p extension.Position) int {
	off := 0
	for line := 0; line < p.Line; line++ {
		i := strings.IndexByte(text[off:], '\n')
		if i < 0 {
			return len(text)
		}
		off += i + 1
	}
	rest := text[off:]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[:i]
	}
	return off + min(max(p.Col, 0), len(rest))
}
