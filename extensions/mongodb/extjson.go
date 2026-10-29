package mongodb

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/emetos/extensions/mongodb/playground"
)

// DocumentText writes a document as relaxed Extended JSON, indented with two spaces: the text of the JSON view and of the editor of
// a document (SPEC §10.22 "Results", "Editing a document").
func DocumentText(d bson.D) (string, error) {
	data, err := bson.MarshalExtJSONIndent(d, false, false, "", "  ")
	if err != nil {
		return "", errors.NewErr("MongoDocumentNotJSON", err)
	}
	return string(data), nil
}

// DocumentsText writes documents as one JSON array, one document after the other, for the JSON view.
func DocumentsText(docs []bson.D) (string, error) {
	var b strings.Builder
	b.WriteString("[")
	for i, d := range docs {
		t, err := DocumentText(d)
		if err != nil {
			return "", err
		}
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("\n")
		b.WriteString(indentLines(t, "  "))
	}
	if len(docs) > 0 {
		b.WriteString("\n")
	}
	b.WriteString("]")
	return b.String(), nil
}

// indentLines puts the prefix before every line.
func indentLines(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}

// ParseDocument reads the text of an edited document, Extended JSON (relaxed or canonical), into a document.
func ParseDocument(text string) (bson.D, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "{") {
		return nil, errors.New("MongoDocumentInvalid", "the text is not a document: it must start with {")
	}
	var d bson.D
	if err := bson.UnmarshalExtJSON([]byte(text), false, &d); err != nil {
		return nil, errors.NewErr("MongoDocumentInvalid", err)
	}
	return d, nil
}

// valueText is a value as one line of relaxed Extended JSON, for comparing two values and for the list of changes.
func valueText(v any) string {
	data, err := bson.MarshalExtJSON(bson.D{{Key: "v", Value: v}}, false, false)
	if err != nil {
		return "?"
	}
	data = bytes.TrimSpace(data)
	data = bytes.TrimPrefix(data, []byte(`{"v":`))
	data = bytes.TrimSuffix(data, []byte("}"))
	var out bytes.Buffer
	if err := json.Compact(&out, data); err != nil {
		return string(data)
	}
	return out.String()
}

// ChangeKind says how a field differs between the stored document and the edited one.
type ChangeKind string

// The kinds of change.
const (
	// Changed is a field whose value is different.
	Changed ChangeKind = "~"
	// Added is a field the edited document has and the stored one does not.
	Added ChangeKind = "+"
	// Removed is a field the stored document has and the edited one does not.
	Removed ChangeKind = "-"
)

// Change is a field that differs.
type Change struct {
	Kind ChangeKind
	// Path is the name of the field, a.b.c for a field of a document inside a document; an array is compared whole, as one field.
	Path string
	// Old and New are the values as a line of JSON, cut at 60 characters; empty for an added and a removed field.
	Old, New string
}

// String writes the change on one line: "~ qty: 2 -> 3", "+ note: \"x\"", "- tags".
func (c Change) String() string {
	switch c.Kind {
	case Added:
		return "+ " + c.Path + ": " + c.New
	case Removed:
		return "- " + c.Path
	}
	return "~ " + c.Path + ": " + c.Old + " -> " + c.New
}

// shorten cuts a text at n characters.
func shorten(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "..."
}

// Diff lists the fields that differ between the stored document and the edited one, comparing the values as relaxed Extended JSON, so
// that an int32 and an int64 of the same number are the same. Documents inside documents are compared field by field.
func Diff(original, edited bson.D) []Change {
	var out []Change
	diffInto(&out, "", original, edited)
	return out
}

func diffInto(out *[]Change, prefix string, original, edited bson.D) {
	find := func(d bson.D, key string) (any, bool) {
		for _, e := range d {
			if e.Key == key {
				return e.Value, true
			}
		}
		return nil, false
	}
	for _, o := range original {
		path := prefix + o.Key
		e, ok := find(edited, o.Key)
		if !ok {
			*out = append(*out, Change{Kind: Removed, Path: path})
			continue
		}
		od, oIsDoc := o.Value.(bson.D)
		ed, eIsDoc := e.(bson.D)
		if oIsDoc && eIsDoc {
			diffInto(out, path+".", od, ed)
			continue
		}
		if ot, et := valueText(o.Value), valueText(e); ot != et {
			*out = append(*out, Change{Kind: Changed, Path: path, Old: shorten(ot, 60), New: shorten(et, 60)})
		}
	}
	for _, e := range edited {
		if _, ok := find(original, e.Key); !ok {
			*out = append(*out, Change{Kind: Added, Path: prefix + e.Key, New: shorten(valueText(e.Value), 60)})
		}
	}
}

// Merge makes the document to store from the stored one and the edited text: the fields the edit left as they were keep their stored
// values (so a long, a date or a decimal that the relaxed text shows as a plain number keeps its type), the changed ones take the
// edited value, and the order of the edited document is kept.
func Merge(original, edited bson.D) bson.D {
	out := make(bson.D, 0, len(edited))
	for _, e := range edited {
		var o any
		found := false
		for _, x := range original {
			if x.Key == e.Key {
				o, found = x.Value, true
				break
			}
		}
		out = append(out, bson.E{Key: e.Key, Value: mergeValue(o, found, e.Value)})
	}
	return out
}

func mergeValue(orig any, found bool, edited any) any {
	if !found {
		return edited
	}
	switch ev := edited.(type) {
	case bson.D:
		if od, ok := orig.(bson.D); ok {
			return Merge(od, ev)
		}
	case bson.A:
		if oa, ok := orig.(bson.A); ok && len(oa) == len(ev) {
			out := make(bson.A, len(ev))
			for i := range ev {
				out[i] = mergeValue(oa[i], true, ev[i])
			}
			return out
		}
	}
	if valueText(orig) == valueText(edited) {
		return orig
	}
	return edited
}

// idOf returns the _id of a document.
func idOf(d bson.D) (any, bool) {
	for _, e := range d {
		if e.Key == "_id" {
			return e.Value, true
		}
	}
	return nil, false
}

// IDText writes an _id as the shell does: ObjectId("..."), "text", 12.
func IDText(id any) string {
	return playground.Format(id)
}
