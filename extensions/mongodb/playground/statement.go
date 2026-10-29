package playground

import (
	"math"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// argKind is what a method wants as an argument.
type argKind uint8

const (
	argDoc       argKind = iota // a document
	argDocs                     // an array of documents
	argDocOrDocs                // a document, or an array of documents (the update of updateOne may be a pipeline)
	argString                   // a string
	argCount                    // a whole number, zero or more
	argNameOrDoc                // a string, or a document (the index to drop)
)

// signature is the arguments of a method: the lowest number of them, and the kind of each possible one.
type signature struct {
	min  int
	args []argKind
}

// methods are the methods of a collection that a playground has, with their arguments (SPEC §10.22).
var methods = map[string]signature{
	"find":                   {0, []argKind{argDoc, argDoc}},
	"findOne":                {0, []argKind{argDoc, argDoc}},
	"aggregate":              {1, []argKind{argDocs, argDoc}},
	"countDocuments":         {0, []argKind{argDoc, argDoc}},
	"estimatedDocumentCount": {0, []argKind{argDoc}},
	"distinct":               {1, []argKind{argString, argDoc, argDoc}},
	"insertOne":              {1, []argKind{argDoc, argDoc}},
	"insertMany":             {1, []argKind{argDocs, argDoc}},
	"updateOne":              {2, []argKind{argDoc, argDocOrDocs, argDoc}},
	"updateMany":             {2, []argKind{argDoc, argDocOrDocs, argDoc}},
	"replaceOne":             {2, []argKind{argDoc, argDoc, argDoc}},
	"deleteOne":              {1, []argKind{argDoc, argDoc}},
	"deleteMany":             {1, []argKind{argDoc, argDoc}},
	"bulkWrite":              {1, []argKind{argDocs, argDoc}},
	"createIndex":            {1, []argKind{argDoc, argDoc}},
	"dropIndex":              {1, []argKind{argNameOrDoc}},
	"getIndexes":             {0, nil},
	"drop":                   {0, nil},
}

// Methods lists the names of the methods of a collection that a playground can call, in the order the documentation lists them.
func Methods() []string {
	return []string{"find", "findOne", "aggregate", "countDocuments", "estimatedDocumentCount", "distinct", "insertOne", "insertMany",
		"updateOne", "updateMany", "replaceOne", "deleteOne", "deleteMany", "bulkWrite", "createIndex", "dropIndex", "getIndexes", "drop"}
}

// chainMethods are the calls on a cursor, with the methods whose cursor each may follow. toArray and pretty change nothing:
// a playground shows every result as a list.
var chainMethods = map[string]struct {
	sig   signature
	after []string
}{
	"sort":    {signature{1, []argKind{argDoc}}, []string{"find"}},
	"skip":    {signature{1, []argKind{argCount}}, []string{"find"}},
	"limit":   {signature{1, []argKind{argCount}}, []string{"find"}},
	"project": {signature{1, []argKind{argDoc}}, []string{"find"}},
	"count":   {signature{0, nil}, []string{"find"}},
	"toArray": {signature{0, nil}, []string{"find", "aggregate", "getIndexes"}},
	"pretty":  {signature{0, nil}, []string{"find", "aggregate", "getIndexes"}},
}

// statement reads one statement.
func (p *parser) statement() (Statement, error) {
	t := p.peek()
	switch t.kind {
	case tkError:
		return nil, t.err
	case tkIdent:
		switch t.text {
		case "use":
			return p.use()
		case "show":
			return p.show()
		case "db":
			return p.dbStatement()
		}
		if reason, ok := unsupportedNames[t.text]; ok {
			return nil, errors.New("MongoParseUnsupported", reason, append(p.meta(t), "name", t.text)...)
		}
	}
	return nil, errors.New("MongoParseUnknownStatement", "a statement starts with use, show or db (db.collection.find(...))", append(p.meta(t), "found", describe(t))...)
}

// use reads "use name" and use("name").
func (p *parser) use() (Statement, error) {
	kw := p.next()
	// the name is the rest of the word, read from the text: it may have dashes
	i := p.lex.pos
	for i < len(p.src) && (p.src[i] == ' ' || p.src[i] == '\t') {
		i++
	}
	if i < len(p.src) && p.src[i] == '(' {
		_, err := p.expect("(", "(")
		if err != nil {
			return nil, err
		}
		n := p.next()
		if n.kind == tkError {
			return nil, n.err
		}
		if n.kind != tkString {
			return nil, errors.New("MongoParseExpectedToken", "use( takes the name of the database in quotes", append(p.meta(n), "found", describe(n))...)
		}
		if _, err := p.expect(")", ")"); err != nil {
			return nil, err
		}
		if msg := badDatabaseName(n.str); msg != "" {
			return nil, errors.New("MongoParseBadDatabaseName", msg, append(p.metaSpan(n.start, n.end), "name", n.str)...)
		}
		return &Use{Place: p.idx.span(kw.start, p.prevEnd), Database: n.str}, nil
	}
	if i < len(p.src) && (p.src[i] == '"' || p.src[i] == '\'') {
		n := p.next()
		if n.kind == tkError {
			return nil, n.err
		}
		if msg := badDatabaseName(n.str); msg != "" {
			return nil, errors.New("MongoParseBadDatabaseName", msg, append(p.metaSpan(n.start, n.end), "name", n.str)...)
		}
		return &Use{Place: p.idx.span(kw.start, p.prevEnd), Database: n.str}, nil
	}
	j := i
	for j < len(p.src) && !strings.ContainsRune(" \t\r\n;", rune(p.src[j])) {
		if strings.HasPrefix(p.src[j:], "//") || strings.HasPrefix(p.src[j:], "/*") {
			break
		}
		j++
	}
	name := p.src[i:j]
	if name == "" {
		return nil, errors.New("MongoParseBadDatabaseName", "use needs the name of a database on its line", p.metaSpan(kw.start, kw.end)...)
	}
	if msg := badDatabaseName(name); msg != "" {
		return nil, errors.New("MongoParseBadDatabaseName", msg, append(p.metaSpan(i, j), "name", name)...)
	}
	p.lex.pos, p.hasBuf = j, false
	p.prevEnd = j
	p.taken++
	return &Use{Place: p.idx.span(kw.start, j), Database: name}, nil
}

// badDatabaseName says what is wrong with the name of a database, or returns "" when it can be one.
func badDatabaseName(name string) string {
	switch {
	case name == "":
		return "the name of a database is not empty"
	case len(name) > 63:
		return "the name of a database has at most 63 bytes"
	case strings.ContainsAny(name, "/\\. \"$*<>:|?\x00"):
		return "the name of a database has none of / \\ . space \" $ * < > : | ?"
	}
	return ""
}

// show reads "show dbs" and "show collections".
func (p *parser) show() (Statement, error) {
	kw := p.next()
	t := p.peek()
	if t.kind == tkError && !t.nl {
		return nil, t.err
	}
	if t.kind != tkIdent || t.nl {
		return nil, errors.New("MongoParseUnsupportedShow", "show is followed by dbs or collections", append(p.metaSpan(kw.start, kw.end), "found", describe(t))...)
	}
	p.next()
	switch t.text {
	case "dbs", "databases":
		return &Show{Place: p.idx.span(kw.start, t.end), What: ShowDatabases}, nil
	case "collections", "tables":
		return &Show{Place: p.idx.span(kw.start, t.end), What: ShowCollections}, nil
	}
	return nil, errors.New("MongoParseUnsupportedShow", "show "+t.text+" is not supported; use show dbs or show collections", append(p.meta(t), "what", t.text)...)
}

// dbStatement reads a statement that starts with db.
func (p *parser) dbStatement() (Statement, error) {
	db := p.next()
	start := db.start
	t := p.peek()
	var collection string
	switch {
	case t.kind == tkPunct && t.text == "[":
		p.next()
		n := p.next()
		if n.kind == tkError {
			return nil, n.err
		}
		if n.kind != tkString {
			return nil, errors.New("MongoParseExpectedToken", "db[ takes the name of the collection in quotes", append(p.meta(n), "found", describe(n))...)
		}
		if _, err := p.expect("]", "]"); err != nil {
			return nil, err
		}
		collection = n.str
	case t.kind == tkPunct && t.text == ".":
		p.next()
		n := p.next()
		if n.kind == tkError {
			return nil, n.err
		}
		if n.kind != tkIdent {
			return nil, errors.New("MongoParseExpectedToken", "db. is followed by the name of a collection or of a method", append(p.meta(n), "found", describe(n))...)
		}
		after := p.peek()
		if after.kind == tkPunct && after.text == "(" {
			return p.dbMethod(start, n)
		}
		names := []string{n.text}
		for {
			dot := p.peek()
			if dot.kind != tkPunct || dot.text != "." {
				break
			}
			p.next()
			m := p.next()
			if m.kind == tkError {
				return nil, m.err
			}
			if m.kind != tkIdent {
				return nil, errors.New("MongoParseExpectedToken", "a . is followed by a name", append(p.meta(m), "found", describe(m))...)
			}
			if c := p.peek(); c.kind == tkPunct && c.text == "(" {
				// m is the method and names are the collection
				return p.collectionCall(start, strings.Join(names, "."), m)
			}
			names = append(names, m.text)
		}
		end := p.peek()
		if len(names) > 1 {
			return nil, errors.New("MongoParseExpectedToken", "expected ( after "+names[len(names)-1]+", found "+describe(end), append(p.metaFound(end), "found", describe(end))...)
		}
		return nil, errors.New("MongoParseExpectedToken", "db."+names[0]+" is followed by a method (db."+names[0]+".find(...)), found "+describe(end), append(p.metaFound(end), "found", describe(end))...)
	case t.kind == tkError:
		return nil, t.err
	default:
		return nil, errors.New("MongoParseExpectedToken", "db is followed by . or [", append(p.metaFound(t), "found", describe(t))...)
	}
	// db["name"] and db.getCollection("name") arrive here with the collection; a method follows
	if _, err := p.expect(".", "a . and the method"); err != nil {
		return nil, err
	}
	m := p.next()
	if m.kind == tkError {
		return nil, m.err
	}
	if m.kind != tkIdent {
		return nil, errors.New("MongoParseExpectedToken", "a . is followed by the name of a method", append(p.meta(m), "found", describe(m))...)
	}
	return p.collectionCall(start, collection, m)
}

// dbMethod reads db.method(...) where the name of the method was taken: getCollection, runCommand and adminCommand.
func (p *parser) dbMethod(start int, name token) (Statement, error) {
	switch name.text {
	case "getCollection":
		args, _, err := p.args()
		if err != nil {
			return nil, err
		}
		s, ok := "", len(args) == 1
		if ok {
			s, ok = args[0].(string)
		}
		if !ok || s == "" {
			return nil, errors.New("MongoParseBadArguments", "getCollection takes the name of the collection, a string", append(p.metaSpan(name.start, p.prevEnd), "method", name.text)...)
		}
		if _, err := p.expect(".", "a . and the method"); err != nil {
			return nil, err
		}
		m := p.next()
		if m.kind == tkError {
			return nil, m.err
		}
		if m.kind != tkIdent {
			return nil, errors.New("MongoParseExpectedToken", "a . is followed by the name of a method", append(p.meta(m), "found", describe(m))...)
		}
		return p.collectionCall(start, s, m)
	case "runCommand", "adminCommand":
		args, spans, err := p.args()
		if err != nil {
			return nil, err
		}
		doc, ok := bson.D(nil), len(args) == 1
		if ok {
			doc, ok = args[0].(bson.D)
		}
		if !ok || len(doc) == 0 {
			return nil, errors.New("MongoParseBadArguments", name.text+" takes one document, the command", append(p.metaSpan(name.start, p.prevEnd), "method", name.text)...)
		}
		return &Command{Place: p.idx.span(start, p.prevEnd), Admin: name.text == "adminCommand", Document: doc, ArgSpan: spans[0]}, nil
	}
	return nil, errors.New("MongoParseUnknownMethod", "db."+name.text+"(...) is not supported; a playground has db.<collection>.<method>, db.getCollection, db.runCommand and db.adminCommand", append(p.meta(name), "method", name.text)...)
}

// collectionCall reads the arguments of the method of a collection, whose name was taken, and the calls chained after it.
func (p *parser) collectionCall(start int, collection string, method token) (Statement, error) {
	sig, ok := methods[method.text]
	if !ok {
		return nil, errors.New("MongoParseUnknownMethod", method.text+" is not a method a playground has for a collection ("+strings.Join(Methods(), ", ")+")", append(p.meta(method), "method", method.text)...)
	}
	args, spans, err := p.args()
	if err != nil {
		return nil, err
	}
	if err := p.checkArgs(method, sig, args, spans); err != nil {
		return nil, err
	}
	call := &CollectionCall{Collection: collection, Method: method.text, MethodSpan: p.idx.span(method.start, method.end), Args: args, ArgSpans: spans}
	counted := false
	for {
		dot := p.peek()
		if dot.kind != tkPunct || dot.text != "." {
			break
		}
		p.next()
		name := p.next()
		if name.kind == tkError {
			return nil, name.err
		}
		if name.kind != tkIdent {
			return nil, errors.New("MongoParseExpectedToken", "a . is followed by the name of a method", append(p.meta(name), "found", describe(name))...)
		}
		spec, known := chainMethods[name.text]
		switch {
		case !known:
			return nil, errors.New("MongoParseBadChain", name.text+" cannot follow a call here; after find: sort, skip, limit, project, count; after find, aggregate and getIndexes: toArray, pretty", append(p.meta(name), "method", name.text)...)
		case counted:
			return nil, errors.New("MongoParseBadChain", "nothing can follow count()", append(p.meta(name), "method", name.text)...)
		case !contains(spec.after, method.text):
			return nil, errors.New("MongoParseBadChain", name.text+"() does not follow "+method.text+"() ("+strings.Join(spec.after, " or ")+" only)", append(p.meta(name), "method", name.text, "after", method.text)...)
		}
		cargs, cspans, err := p.args()
		if err != nil {
			return nil, err
		}
		if err := p.checkArgs(name, spec.sig, cargs, cspans); err != nil {
			return nil, err
		}
		counted = name.text == "count"
		call.Chain = append(call.Chain, ChainCall{Name: name.text, Place: p.idx.span(name.start, p.prevEnd), Args: cargs, ArgSpans: cspans})
	}
	call.Place = p.idx.span(start, p.prevEnd)
	return call, nil
}

// contains tells whether the list has the text.
func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// checkArgs checks the arguments of a call against the signature of its method.
func (p *parser) checkArgs(method token, sig signature, args []any, spans []Span) error {
	if len(args) < sig.min || len(args) > len(sig.args) {
		want := "no arguments"
		switch {
		case len(sig.args) == 1 && sig.min == 1:
			want = "one argument"
		case sig.min == len(sig.args) && len(sig.args) > 0:
			want = itoa(len(sig.args)) + " arguments"
		case len(sig.args) > 0:
			want = "from " + itoa(sig.min) + " to " + itoa(len(sig.args)) + " arguments"
		}
		return errors.New("MongoParseBadArguments", method.text+" takes "+want+", not "+itoa(len(args)), append(p.metaSpan(method.start, p.prevEnd), "method", method.text, "given", len(args))...)
	}
	for i, a := range args {
		kind := sig.args[i]
		if argOK(kind, a) {
			continue
		}
		return errors.New("MongoParseBadArgumentType", "argument "+itoa(i+1)+" of "+method.text+" is "+argText(kind), append(p.metaSpan(spans[i].Start.Offset, spans[i].End.Offset), "method", method.text, "argument", i+1)...)
	}
	return nil
}

// itoa writes a small number.
func itoa(n int) string { return strconv.Itoa(n) }

// argOK tells whether the value is what the kind wants. A count is turned into an int64 in place by the caller of Args; here
// a whole number of zero or more is enough.
func argOK(kind argKind, v any) bool {
	switch kind {
	case argDoc:
		_, ok := v.(bson.D)
		return ok
	case argDocs:
		arr, ok := v.(bson.A)
		if !ok {
			return false
		}
		for _, x := range arr {
			if _, isDoc := x.(bson.D); !isDoc {
				return false
			}
		}
		return true
	case argDocOrDocs:
		return argOK(argDoc, v) || argOK(argDocs, v)
	case argString:
		_, ok := v.(string)
		return ok
	case argCount:
		switch n := v.(type) {
		case int32:
			return n >= 0
		case float64:
			return n >= 0 && n == math.Trunc(n) && n < 1<<53
		}
		return false
	case argNameOrDoc:
		_, isString := v.(string)
		return isString || argOK(argDoc, v)
	}
	return false
}

// argText says what a kind of argument is, for a message.
func argText(kind argKind) string {
	return map[argKind]string{
		argDoc: "a document ({ ... })", argDocs: "an array of documents ([{ ... }, ...])", argDocOrDocs: "a document, or an array of documents",
		argString: "a string", argCount: "a whole number, zero or more", argNameOrDoc: "the name of the index, or its keys as a document",
	}[kind]
}
