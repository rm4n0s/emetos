package playground

import (
	"sort"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Pos is a place in the source of a playground.
type Pos struct {
	// Offset is the byte offset from the start of the source.
	Offset int
	// Line and Column count from zero; Column counts the bytes of the UTF-8 line.
	Line, Column int
}

// Span is a stretch of the source: Start is the first byte and End the byte after the last.
type Span struct{ Start, End Pos }

// Contains tells whether the offset is inside the span (its end included, so that a cursor after the last character is in).
func (s Span) Contains(offset int) bool { return offset >= s.Start.Offset && offset <= s.End.Offset }

// lineIndex turns byte offsets into lines and columns.
type lineIndex struct {
	starts []int // the offset of the first byte of each line
}

// newLineIndex indexes the lines of src (a line ends at a line feed).
func newLineIndex(src string) *lineIndex {
	idx := &lineIndex{starts: []int{0}}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			idx.starts = append(idx.starts, i+1)
		}
	}
	return idx
}

// pos returns the Pos of a byte offset.
func (x *lineIndex) pos(offset int) Pos {
	line := sort.Search(len(x.starts), func(i int) bool { return x.starts[i] > offset }) - 1
	if line < 0 {
		line = 0
	}
	return Pos{Offset: offset, Line: line, Column: offset - x.starts[line]}
}

// span returns the Span of two byte offsets.
func (x *lineIndex) span(start, end int) Span { return Span{Start: x.pos(start), End: x.pos(end)} }

// Statement is one statement of a playground. The kinds are Use, Show, Command and CollectionCall.
type Statement interface {
	// Span is where the statement is in the source, from its first token to its last (a closing semicolon is not in it).
	Span() Span
	isStatement()
}

// Use is "use <database>": the database the next statements work on.
type Use struct {
	Place    Span
	Database string
}

// Span implements Statement.
func (s *Use) Span() Span { return s.Place }
func (*Use) isStatement() {}

// ShowKind is what a Show statement lists.
type ShowKind string

// The things "show" lists.
const (
	// ShowDatabases is "show dbs" (and "show databases").
	ShowDatabases ShowKind = "dbs"
	// ShowCollections is "show collections" (and "show tables").
	ShowCollections ShowKind = "collections"
)

// Show is "show dbs" or "show collections".
type Show struct {
	Place Span
	What  ShowKind
}

// Span implements Statement.
func (s *Show) Span() Span { return s.Place }
func (*Show) isStatement() {}

// Command is db.runCommand({...}) or db.adminCommand({...}).
type Command struct {
	Place Span
	// Admin says it was db.adminCommand, run on the admin database.
	Admin bool
	// Document is the command, with its name as the first key.
	Document bson.D
	// ArgSpan is where the document is.
	ArgSpan Span
}

// Span implements Statement.
func (s *Command) Span() Span { return s.Place }
func (*Command) isStatement() {}

// CollectionCall is db.<collection>.<method>(arguments) with the cursor methods chained after it.
type CollectionCall struct {
	Place Span
	// Collection is the name of the collection, written db.name, db.getCollection("name") or db["name"].
	Collection string
	// Method is the method of the collection (find, insertOne...). Method names are listed in Methods.
	Method string
	// MethodSpan is where the name of the method is.
	MethodSpan Span
	// Args are the arguments, checked against the method's signature: a document is a bson.D, an array a bson.A, a count an int64,
	// a name a string. ArgSpans says where each is.
	Args     []any
	ArgSpans []Span
	// Chain are the calls after the method (.sort(...).limit(10)), in order.
	Chain []ChainCall
}

// Span implements Statement.
func (s *CollectionCall) Span() Span { return s.Place }
func (*CollectionCall) isStatement() {}

// ChainCall is a call on the cursor a method returned: sort, skip, limit, project, count, toArray or pretty.
type ChainCall struct {
	Name     string
	Place    Span
	Args     []any
	ArgSpans []Span
}

// Item is one statement of a script, or the error of a statement that could not be read. Exactly one of Statement and Err is set.
type Item struct {
	// Span is where the statement (or the text that could not be read) is.
	Span      Span
	Statement Statement
	// Err is why the statement could not be read: an *errors.Error with a tag MongoParse*.
	Err error
}

// Script is a playground read to its end.
type Script struct {
	// Source is the text that was read.
	Source string
	// Items are the statements and the mistakes, in the order of the source.
	Items []Item

	idx *lineIndex
}

// Pos returns the place of a byte offset of the source.
func (s *Script) Pos(offset int) Pos {
	if offset < 0 {
		offset = 0
	}
	if offset > len(s.Source) {
		offset = len(s.Source)
	}
	return s.idx.pos(offset)
}

// At returns the item the cursor at the byte offset is in: the item that contains the offset; else, the item before it that ends
// on the same line; else, the item after it that starts on the same line; else none. This is the statement that Run runs.
func (s *Script) At(offset int) (Item, bool) {
	for _, it := range s.Items {
		if it.Span.Contains(offset) {
			return it, true
		}
	}
	line := s.Pos(offset).Line
	var before *Item
	for i := range s.Items {
		it := &s.Items[i]
		if it.Span.End.Offset <= offset && it.Span.End.Line == line {
			before = it
		}
	}
	if before != nil {
		return *before, true
	}
	for _, it := range s.Items {
		if it.Span.Start.Offset >= offset && it.Span.Start.Line == line {
			return it, true
		}
	}
	return Item{}, false
}

// Between returns the items that are inside the byte range [from, to) of the source: those that overlap it.
func (s *Script) Between(from, to int) []Item {
	var out []Item
	for _, it := range s.Items {
		if it.Span.Start.Offset < to && it.Span.End.Offset > from {
			out = append(out, it)
		}
	}
	return out
}
