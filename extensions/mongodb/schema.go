package mongodb

import (
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Limits of the sampled schema: how deep documents are followed and how many fields one level shows.
const (
	schemaMaxDepth  = 6
	schemaMaxFields = 300
)

// SchemaField is a field found in the sample, with the types its values had and the fields of the documents inside it.
type SchemaField struct {
	// Name is the key; Path is the dotted path from the top of the document.
	Name, Path string
	// Count is how many documents of the sample had the field.
	Count int
	// Types counts the BSON types of the values ("string", "int", "null", "object", "array").
	Types map[string]int
	// ElementTypes counts the types of the items of the arrays, when the field was an array.
	ElementTypes map[string]int
	// Children are the fields of the documents inside this field (or inside the arrays of it), in the order they were first seen.
	Children []*SchemaField
	// MoreFields says that fields were left out at this level (SchemaMaxFields).
	MoreFields bool

	byName map[string]*SchemaField
}

// Schema is what a sample of documents shows about a collection.
type Schema struct {
	// Documents is the size of the sample.
	Documents int
	// Fields are the top-level fields, in the order they were first seen.
	Fields []*SchemaField
	// MoreFields says that fields were left out at the top level.
	MoreFields bool
}

// InferSchema reads the fields and their types out of a sample of documents.
func InferSchema(docs []bson.D) *Schema {
	s := &Schema{Documents: len(docs)}
	root := &SchemaField{}
	for _, d := range docs {
		addDocument(root, d, "", 0)
	}
	s.Fields, s.MoreFields = root.Children, root.MoreFields
	return s
}

// addDocument records the fields of one document under parent.
func addDocument(parent *SchemaField, doc bson.D, prefix string, depth int) {
	seen := map[string]bool{}
	for _, e := range doc {
		f := parent.child(e.Key, prefix)
		if f == nil {
			continue
		}
		if !seen[e.Key] {
			f.Count++ // a document counts once for a field, though an array of documents can name it many times
			seen[e.Key] = true
		}
		f.record(e.Value, depth)
	}
}

// child finds or adds the field with the name below the parent; nil when the level is full.
func (f *SchemaField) child(name, prefix string) *SchemaField {
	if c, ok := f.byName[name]; ok {
		return c
	}
	if len(f.Children) >= schemaMaxFields {
		f.MoreFields = true
		return nil
	}
	if f.byName == nil {
		f.byName = map[string]*SchemaField{}
	}
	path := name
	if prefix != "" {
		path = prefix + "." + name
	}
	c := &SchemaField{Name: name, Path: path, Types: map[string]int{}}
	f.byName[name] = c
	f.Children = append(f.Children, c)
	return c
}

// record notes the type of a value, and follows documents and arrays.
func (f *SchemaField) record(v any, depth int) {
	t := bsonTypeName(v)
	f.Types[t]++
	switch x := v.(type) {
	case bson.D:
		if depth < schemaMaxDepth {
			addDocument(f, x, f.Path, depth+1)
		}
	case bson.A:
		if f.ElementTypes == nil {
			f.ElementTypes = map[string]int{}
		}
		for _, item := range x {
			f.ElementTypes[bsonTypeName(item)]++
			if d, ok := item.(bson.D); ok && depth < schemaMaxDepth {
				addDocument(f, d, f.Path, depth+1)
			}
		}
	}
}

// bsonTypeName names the BSON type of a decoded value, as mongosh's typeof for documents does ("int", "long", "double").
func bsonTypeName(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case int32:
		return "int"
	case int64:
		return "long"
	case float64:
		return "double"
	case bool:
		return "bool"
	case bson.ObjectID:
		return "objectId"
	case bson.DateTime:
		return "date"
	case bson.Decimal128:
		return "decimal"
	case bson.Binary:
		if x.Subtype == bson.TypeBinaryUUID || x.Subtype == bson.TypeBinaryUUIDOld {
			return "uuid"
		}
		return "binData"
	case bson.Timestamp:
		return "timestamp"
	case bson.Regex:
		return "regex"
	case bson.D, bson.M:
		return "object"
	case bson.A:
		return "array"
	case bson.MinKey:
		return "minKey"
	case bson.MaxKey:
		return "maxKey"
	case bson.JavaScript, bson.CodeWithScope:
		return "javascript"
	case bson.Symbol:
		return "symbol"
	case bson.Undefined:
		return "undefined"
	case bson.DBPointer:
		return "dbPointer"
	}
	return "unknown"
}

// TypesText writes the types of the field in order of how often they were seen: "string", "string | null", "array<object>".
func (f *SchemaField) TypesText() string {
	names := sortedByCount(f.Types)
	for i, n := range names {
		if n == "array" && len(f.ElementTypes) > 0 {
			names[i] = "array<" + strings.Join(sortedByCount(f.ElementTypes), " | ") + ">"
		}
	}
	return strings.Join(names, " | ")
}

// sortedByCount lists the names of the counts, the most seen first and by name among equals.
func sortedByCount(counts map[string]int) []string {
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	return names
}

// Percent is how much of the sample had the field, of the given size of the sample: the whole numbers 1 to 100 (a field in one
// document of many is not shown as 0).
func (f *SchemaField) Percent(sample int) int {
	if sample <= 0 {
		return 0
	}
	p := (f.Count*100 + sample/2) / sample
	if p < 1 {
		p = 1
	}
	if p > 100 {
		p = 100
	}
	return p
}

// primaryType is the type most seen in the field, for its icon.
func (f *SchemaField) primaryType() string {
	names := sortedByCount(f.Types)
	if len(names) == 0 {
		return "unknown"
	}
	// null next to another type is the other type with missing values
	if names[0] == "null" && len(names) > 1 {
		return names[1]
	}
	return names[0]
}
