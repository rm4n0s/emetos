package mongodb

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/emetos/extensions/mongodb/playground"
)

// DefaultDatabase is the database a statement runs on when the playground, the editor and the connection name none (the shell's "test").
const DefaultDatabase = "test"

// Step is a statement of a playground as the runner does it: a change of the current database, or a call to the server.
type Step struct {
	// Use is the database that "use" selects; empty for a call.
	Use string
	// Call is the request to the server; nil for "use".
	Call *Call
}

// optionNames are the options each method of a collection accepts as its last argument (SPEC §10.22). The driver takes no other.
var optionNames = map[string][]string{
	"aggregate":      {"allowDiskUse", "collation", "hint", "comment", "let", "maxTimeMS", "bypassDocumentValidation"},
	"countDocuments": {"limit", "skip", "hint", "collation", "maxTimeMS", "comment"},
	"estimatedDocumentCount": {
		"maxTimeMS", "comment",
	},
	"distinct":   {"collation", "maxTimeMS", "comment"},
	"insertOne":  {"bypassDocumentValidation", "comment"},
	"insertMany": {"ordered", "bypassDocumentValidation", "comment"},
	"updateOne":  {"upsert", "arrayFilters", "collation", "hint", "bypassDocumentValidation", "comment", "let"},
	"updateMany": {"upsert", "arrayFilters", "collation", "hint", "bypassDocumentValidation", "comment", "let"},
	"replaceOne": {"upsert", "collation", "hint", "bypassDocumentValidation", "comment", "let"},
	"deleteOne":  {"collation", "hint", "comment", "let"},
	"deleteMany": {"collation", "hint", "comment", "let"},
	"bulkWrite":  {"ordered", "bypassDocumentValidation", "comment"},
}

// Translate turns a statement into the step the runner does. db is the database the statement runs on ("use" before it, else the
// editor's, else the connection's); empty means DefaultDatabase. The error has a tag MongoRun*.
func Translate(st playground.Statement, db string) (Step, error) {
	if db == "" {
		db = DefaultDatabase
	}
	switch s := st.(type) {
	case *playground.Use:
		return Step{Use: s.Database}, nil
	case *playground.Show:
		if s.What == playground.ShowDatabases {
			return Step{Call: &Call{Kind: CallShowDatabases, Database: "admin"}}, nil
		}
		return Step{Call: &Call{Kind: CallShowCollections, Database: db}}, nil
	case *playground.Command:
		d := db
		if s.Admin {
			d = "admin"
		}
		return Step{Call: &Call{Kind: CallCommand, Database: d, Command: s.Document}}, nil
	case *playground.CollectionCall:
		c, err := translateCollectionCall(s, db)
		if err != nil {
			return Step{}, err
		}
		return Step{Call: c}, nil
	}
	return Step{}, errors.New("MongoRunUnknownStatement", "this kind of statement cannot be run", "type", fmt.Sprintf("%T", st))
}

// translateCollectionCall turns db.<collection>.<method>(...) and its chain into a Call.
func translateCollectionCall(s *playground.CollectionCall, db string) (*Call, error) {
	c := &Call{Database: db, Collection: s.Collection}
	args := s.Args
	// doc returns the argument i as a document, empty when it is not there
	doc := func(i int) bson.D {
		if i < len(args) {
			d, _ := args[i].(bson.D)
			return d
		}
		return bson.D{}
	}
	// opts reads the options in the argument i
	opts := func(i int) error {
		if i >= len(args) {
			return nil
		}
		d, _ := args[i].(bson.D)
		o, err := parseOptions(s.Method, d)
		if err != nil {
			return err
		}
		c.Options = o
		return nil
	}
	switch s.Method {
	case "find", "findOne":
		c.Kind = CallFind
		if s.Method == "findOne" {
			c.Kind, c.Limit = CallFindOne, 1
		}
		c.Filter, c.Projection = doc(0), doc(1)
		if len(c.Projection) == 0 {
			c.Projection = nil
		}
		return c, applyChain(c, s)
	case "aggregate":
		c.Kind = CallAggregate
		c.Pipeline, _ = args[0].(bson.A)
		return c, opts(1)
	case "countDocuments":
		c.Kind, c.Filter = CallCountDocuments, doc(0)
		if err := opts(1); err != nil {
			return nil, err
		}
		return c, nil
	case "estimatedDocumentCount":
		c.Kind = CallEstimatedCount
		return c, opts(0)
	case "distinct":
		c.Kind = CallDistinct
		c.Field, _ = args[0].(string)
		c.Filter = doc(1)
		return c, opts(2)
	case "insertOne":
		c.Kind, c.Docs = CallInsertOne, []bson.D{doc(0)}
		return c, opts(1)
	case "insertMany":
		c.Kind = CallInsertMany
		arr, _ := args[0].(bson.A)
		if len(arr) == 0 {
			return nil, errors.New("MongoRunNothingToInsert", "insertMany needs at least one document", "method", s.Method)
		}
		for _, d := range arr {
			dd, _ := d.(bson.D)
			c.Docs = append(c.Docs, dd)
		}
		return c, opts(1)
	case "updateOne", "updateMany":
		c.Kind = CallUpdateOne
		if s.Method == "updateMany" {
			c.Kind = CallUpdateMany
		}
		c.Filter = doc(0)
		update, err := updateOf(s.Method, args[1])
		if err != nil {
			return nil, err
		}
		c.Update = update
		return c, opts(2)
	case "replaceOne":
		c.Kind, c.Filter, c.Replacement = CallReplaceOne, doc(0), doc(1)
		for _, e := range c.Replacement {
			if strings.HasPrefix(e.Key, "$") {
				return nil, errors.New("MongoRunReplacementHasOperators", "the replacement of replaceOne is a whole document; it has the operator "+e.Key+" (use updateOne for operators)", "method", s.Method, "key", e.Key)
			}
		}
		return c, opts(2)
	case "deleteOne", "deleteMany":
		c.Kind = CallDeleteOne
		if s.Method == "deleteMany" {
			c.Kind = CallDeleteMany
		}
		c.Filter = doc(0)
		return c, opts(1)
	case "bulkWrite":
		c.Kind = CallBulkWrite
		arr, _ := args[0].(bson.A)
		if len(arr) == 0 {
			return nil, errors.New("MongoRunBadBulkOperation", "bulkWrite needs at least one operation", "method", s.Method)
		}
		for i, o := range arr {
			op, err := bulkOpOf(i, o)
			if err != nil {
				return nil, err
			}
			c.Ops = append(c.Ops, op)
		}
		return c, opts(1)
	case "createIndex":
		keys := doc(0)
		if len(keys) == 0 {
			return nil, errors.New("MongoRunBadIndex", "createIndex needs the keys of the index, a document such as { field: 1 }", "method", s.Method)
		}
		spec := bson.D{{Key: "key", Value: keys}}
		named := false
		for _, e := range doc(1) {
			if e.Key == "key" {
				return nil, errors.New("MongoRunBadIndex", "the keys of the index are the first argument of createIndex, not an option", "method", s.Method)
			}
			if e.Key == "name" {
				named = true
			}
			spec = append(spec, e)
		}
		if !named {
			spec = append(spec, bson.E{Key: "name", Value: indexName(keys)})
		}
		c.Kind = CallCommand
		c.Command = bson.D{{Key: "createIndexes", Value: s.Collection}, {Key: "indexes", Value: bson.A{spec}}}
		c.Collection = ""
		return c, nil
	case "dropIndex":
		c.Kind = CallCommand
		c.Command = bson.D{{Key: "dropIndexes", Value: s.Collection}, {Key: "index", Value: args[0]}}
		c.Collection = ""
		return c, nil
	case "getIndexes":
		c.Kind = CallGetIndexes
		return c, nil
	case "drop":
		c.Kind = CallCommand
		c.Command = bson.D{{Key: "drop", Value: s.Collection}}
		c.Collection = ""
		return c, nil
	}
	return nil, errors.New("MongoRunUnknownMethod", "the method "+s.Method+" cannot be run", "method", s.Method)
}

// applyChain applies the calls after a find: sort, skip, limit, project and count.
func applyChain(c *Call, s *playground.CollectionCall) error {
	for _, ch := range s.Chain {
		switch ch.Name {
		case "sort":
			c.Sort, _ = ch.Args[0].(bson.D)
		case "project":
			c.Projection, _ = ch.Args[0].(bson.D)
			if len(c.Projection) == 0 {
				c.Projection = nil
			}
		case "skip":
			c.Skip = countOf(ch.Args[0])
		case "limit":
			c.Limit = countOf(ch.Args[0])
		case "count":
			// cursor.count() counts the documents that match, whatever skip and limit say
			*c = Call{Kind: CallCountDocuments, Database: c.Database, Collection: c.Collection, Filter: c.Filter}
		}
	}
	return nil
}

// countOf is the value of a whole number argument (int32 or float64 as the parser makes them).
func countOf(v any) int64 {
	n, _ := toInt64(v)
	return n
}

// toInt64 reads a number that is a whole number.
func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n == math.Trunc(n) && math.Abs(n) < 1<<53 {
			return int64(n), true
		}
	}
	return 0, false
}

// updateOf checks the update argument of updateOne and updateMany: a document of operators, or a pipeline.
func updateOf(method string, v any) (any, error) {
	switch u := v.(type) {
	case bson.D:
		if len(u) == 0 {
			return nil, errors.New("MongoRunUpdateNeedsOperators", method+" needs an update such as { $set: { a: 1 } }; the document is empty", "method", method)
		}
		for _, e := range u {
			if !strings.HasPrefix(e.Key, "$") {
				return nil, errors.New("MongoRunUpdateNeedsOperators", method+" changes a document with operators such as $set; the key "+e.Key+" is not one (replaceOne replaces a whole document)", "method", method, "key", e.Key)
			}
		}
		return u, nil
	case bson.A:
		if len(u) == 0 {
			return nil, errors.New("MongoRunUpdateNeedsOperators", method+" needs an update; the pipeline is empty", "method", method)
		}
		return u, nil
	}
	return nil, errors.New("MongoRunUpdateNeedsOperators", method+" needs an update document or a pipeline", "method", method)
}

// bulkOpOf reads one operation of a bulkWrite: a document with one key, the kind, as the shell writes them.
func bulkOpOf(i int, v any) (BulkOp, error) {
	d, _ := v.(bson.D)
	if len(d) != 1 {
		return BulkOp{}, errors.New("MongoRunBadBulkOperation", "operation "+strconv.Itoa(i+1)+" of bulkWrite is a document with one key, such as { insertOne: { document: {...} } }", "operation", i+1)
	}
	kind := d[0].Key
	body, _ := d[0].Value.(bson.D)
	op := BulkOp{Kind: kind}
	known := map[string][]string{
		"insertOne":  {"document"},
		"updateOne":  {"filter", "update", "upsert", "arrayFilters", "collation", "hint"},
		"updateMany": {"filter", "update", "upsert", "arrayFilters", "collation", "hint"},
		"replaceOne": {"filter", "replacement", "upsert", "collation", "hint"},
		"deleteOne":  {"filter", "collation", "hint"},
		"deleteMany": {"filter", "collation", "hint"},
	}
	keys, ok := known[kind]
	if !ok {
		return BulkOp{}, errors.New("MongoRunBadBulkOperation", "operation "+strconv.Itoa(i+1)+" of bulkWrite: "+kind+" is not one of insertOne, updateOne, updateMany, replaceOne, deleteOne, deleteMany", "operation", i+1, "kind", kind)
	}
	if _, isDoc := d[0].Value.(bson.D); !isDoc {
		return BulkOp{}, errors.New("MongoRunBadBulkOperation", "operation "+strconv.Itoa(i+1)+" of bulkWrite: "+kind+" takes a document", "operation", i+1, "kind", kind)
	}
	have := map[string]bool{}
	for _, e := range body {
		if !contains(keys, e.Key) {
			return BulkOp{}, errors.New("MongoRunBadBulkOperation", "operation "+strconv.Itoa(i+1)+" of bulkWrite: "+kind+" has no "+e.Key+" (it has "+strings.Join(keys, ", ")+")", "operation", i+1, "kind", kind, "key", e.Key)
		}
		have[e.Key] = true
		switch e.Key {
		case "document":
			dd, isDoc := e.Value.(bson.D)
			if !isDoc {
				return BulkOp{}, errors.New("MongoRunBadBulkOperation", "operation "+strconv.Itoa(i+1)+" of bulkWrite: "+e.Key+" of "+kind+" is a document", "operation", i+1, "kind", kind, "key", e.Key)
			}
			op.Doc = dd
		case "filter":
			dd, isDoc := e.Value.(bson.D)
			if !isDoc {
				return BulkOp{}, errors.New("MongoRunBadBulkOperation", "operation "+strconv.Itoa(i+1)+" of bulkWrite: "+e.Key+" of "+kind+" is a document", "operation", i+1, "kind", kind, "key", e.Key)
			}
			op.Filter = dd
		case "replacement":
			dd, isDoc := e.Value.(bson.D)
			if !isDoc {
				return BulkOp{}, errors.New("MongoRunBadBulkOperation", "operation "+strconv.Itoa(i+1)+" of bulkWrite: "+e.Key+" of "+kind+" is a document", "operation", i+1, "kind", kind, "key", e.Key)
			}
			for _, k := range dd {
				if strings.HasPrefix(k.Key, "$") {
					return BulkOp{}, errors.New("MongoRunBadBulkOperation", "operation "+strconv.Itoa(i+1)+" of bulkWrite: "+e.Key+" of "+kind+" is a whole document; it has the operator "+k.Key, "operation", i+1, "kind", kind, "key", e.Key)
				}
			}
			op.Doc = dd
		case "update":
			u, err := updateOf(kind, e.Value)
			if err != nil {
				return BulkOp{}, err
			}
			op.Update = u
		case "upsert":
			b, isBool := e.Value.(bool)
			if !isBool {
				return BulkOp{}, errors.New("MongoRunBadBulkOperation", "operation "+strconv.Itoa(i+1)+" of bulkWrite: "+e.Key+" of "+kind+" is true or false", "operation", i+1, "kind", kind, "key", e.Key)
			}
			op.Upsert = b
		case "arrayFilters":
			a, isArr := e.Value.(bson.A)
			if !isArr {
				return BulkOp{}, errors.New("MongoRunBadBulkOperation", "operation "+strconv.Itoa(i+1)+" of bulkWrite: "+e.Key+" of "+kind+" is an array", "operation", i+1, "kind", kind, "key", e.Key)
			}
			op.ArrayFilters = a
		case "collation":
			dd, isDoc := e.Value.(bson.D)
			if !isDoc {
				return BulkOp{}, errors.New("MongoRunBadBulkOperation", "operation "+strconv.Itoa(i+1)+" of bulkWrite: "+e.Key+" of "+kind+" is a document", "operation", i+1, "kind", kind, "key", e.Key)
			}
			op.Collation = dd
		case "hint":
			op.Hint = e.Value
		}
	}
	need := map[string][]string{"insertOne": {"document"}, "updateOne": {"filter", "update"}, "updateMany": {"filter", "update"},
		"replaceOne": {"filter", "replacement"}, "deleteOne": {"filter"}, "deleteMany": {"filter"}}[kind]
	for _, k := range need {
		if !have[k] {
			return BulkOp{}, errors.New("MongoRunBadBulkOperation", "operation "+strconv.Itoa(i+1)+" of bulkWrite: "+kind+" needs "+k, "operation", i+1, "kind", kind, "key", k)
		}
	}
	if op.Filter == nil && kind != "insertOne" {
		op.Filter = bson.D{}
	}
	return op, nil
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

// indexName is the name MongoDB gives an index from its keys: field_1_other_-1.
func indexName(keys bson.D) string {
	parts := make([]string, 0, len(keys)*2)
	for _, k := range keys {
		parts = append(parts, k.Key)
		switch v := k.Value.(type) {
		case string:
			parts = append(parts, v)
		default:
			if n, ok := toInt64(v); ok {
				parts = append(parts, strconv.FormatInt(n, 10))
			} else {
				parts = append(parts, playground.Format(v))
			}
		}
	}
	return strings.Join(parts, "_")
}

// parseOptions reads the options document of a method.
func parseOptions(method string, doc bson.D) (CallOptions, error) {
	var o CallOptions
	allowed := optionNames[method]
	for _, e := range doc {
		if !contains(allowed, e.Key) {
			return o, errors.New("MongoRunUnknownOption", "the option "+e.Key+" is not one of "+method+" in a playground ("+strings.Join(allowed, ", ")+")", "method", method, "option", e.Key)
		}
		switch e.Key {
		case "upsert", "allowDiskUse", "bypassDocumentValidation":
			b, ok := e.Value.(bool)
			if !ok {
				return o, errors.New("MongoRunBadOption", "the option "+e.Key+" of "+method+" is true or false", "method", method, "option", e.Key)
			}
			switch e.Key {
			case "upsert":
				o.Upsert = b
			case "allowDiskUse":
				o.AllowDiskUse = b
			default:
				o.BypassValidation = b
			}
		case "ordered":
			b, ok := e.Value.(bool)
			if !ok {
				return o, errors.New("MongoRunBadOption", "the option "+e.Key+" of "+method+" is true or false", "method", method, "option", e.Key)
			}
			o.Unordered = !b
		case "arrayFilters":
			a, ok := e.Value.(bson.A)
			if !ok {
				return o, errors.New("MongoRunBadOption", "the option "+e.Key+" of "+method+" is an array of documents", "method", method, "option", e.Key)
			}
			o.ArrayFilters = a
		case "collation", "let":
			d, ok := e.Value.(bson.D)
			if !ok {
				return o, errors.New("MongoRunBadOption", "the option "+e.Key+" of "+method+" is a document", "method", method, "option", e.Key)
			}
			if e.Key == "collation" {
				o.Collation = d
			} else {
				o.Let = d
			}
		case "hint":
			switch e.Value.(type) {
			case string, bson.D:
				o.Hint = e.Value
			default:
				return o, errors.New("MongoRunBadOption", "the option "+e.Key+" of "+method+" is the name of an index or its keys", "method", method, "option", e.Key)
			}
		case "comment":
			o.Comment = e.Value
		case "maxTimeMS":
			n, ok := toInt64(e.Value)
			if !ok || n < 0 {
				return o, errors.New("MongoRunBadOption", "the option "+e.Key+" of "+method+" is a number of milliseconds, zero or more", "method", method, "option", e.Key)
			}
			o.MaxTime = time.Duration(n) * time.Millisecond
		case "limit", "skip":
			n, ok := toInt64(e.Value)
			if !ok || n < 0 {
				return o, errors.New("MongoRunBadOption", "the option "+e.Key+" of "+method+" is a whole number, zero or more", "method", method, "option", e.Key)
			}
			if e.Key == "limit" {
				o.CountLimit = n
			} else {
				o.CountSkip = n
			}
		}
	}
	return o, nil
}

// Classify says what a call does to the data: it only reads, it writes, or it can lose a lot of data (SPEC §10.22 "Safety": drop,
// dropDatabase, deleteMany({}) and updateMany({}, ...)). A command is a read only when it is on the list of commands that read.
func Classify(c Call) Safety {
	switch c.Kind {
	case CallFind, CallFindOne, CallCountDocuments, CallEstimatedCount, CallDistinct, CallGetIndexes, CallShowDatabases, CallShowCollections:
		return Safety{Class: ClassRead, Verb: string(c.Kind)}
	case CallAggregate:
		if stage, writes := writingStage(c.Pipeline); writes {
			return Safety{Class: ClassWrite, Verb: "aggregate", Why: stage}
		}
		return Safety{Class: ClassRead, Verb: "aggregate"}
	case CallInsertOne, CallInsertMany, CallUpdateOne, CallReplaceOne, CallDeleteOne:
		return Safety{Class: ClassWrite, Verb: string(c.Kind)}
	case CallDeleteMany:
		if len(c.Filter) == 0 {
			return Safety{Class: ClassDestructive, Verb: "deleteMany", Why: "deleteMany({})"}
		}
		return Safety{Class: ClassWrite, Verb: "deleteMany"}
	case CallUpdateMany:
		if len(c.Filter) == 0 {
			return Safety{Class: ClassDestructive, Verb: "updateMany", Why: "updateMany({}, ...)"}
		}
		return Safety{Class: ClassWrite, Verb: "updateMany"}
	case CallBulkWrite:
		s := Safety{Class: ClassWrite, Verb: "bulkWrite"}
		for _, op := range c.Ops {
			if len(op.Filter) == 0 && (op.Kind == "deleteMany" || op.Kind == "updateMany") {
				s.Class, s.Why = ClassDestructive, op.Kind+"({})"
			}
		}
		return s
	case CallCommand:
		name := ""
		if len(c.Command) > 0 {
			name = c.Command[0].Key
		}
		switch strings.ToLower(name) {
		case "drop", "dropdatabase":
			return Safety{Class: ClassDestructive, Verb: name, Why: name}
		case "aggregate":
			for _, e := range c.Command {
				if p, ok := e.Value.(bson.A); ok && e.Key == "pipeline" {
					if stage, writes := writingStage(p); writes {
						return Safety{Class: ClassWrite, Verb: name, Why: stage}
					}
				}
			}
			return Safety{Class: ClassRead, Verb: name}
		}
		if readCommands[strings.ToLower(name)] {
			return Safety{Class: ClassRead, Verb: name}
		}
		return Safety{Class: ClassWrite, Verb: name}
	}
	return Safety{Class: ClassWrite, Verb: string(c.Kind)}
}

// writingStage finds a stage of a pipeline that writes ($out, $merge).
func writingStage(p bson.A) (string, bool) {
	for _, s := range p {
		if d, ok := s.(bson.D); ok && len(d) > 0 && (d[0].Key == "$out" || d[0].Key == "$merge") {
			return d[0].Key, true
		}
	}
	return "", false
}

// readCommands are the commands (in lower case) that only read, which a read-only connection may run with db.runCommand.
var readCommands = map[string]bool{
	"ping": true, "hello": true, "ismaster": true, "buildinfo": true, "listdatabases": true, "listcollections": true, "listindexes": true,
	"collstats": true, "dbstats": true, "serverstatus": true, "count": true, "distinct": true, "find": true, "explain": true,
	"connectionstatus": true, "getcmdlineopts": true, "hostinfo": true, "whatsmyuri": true, "listcommands": true, "replsetgetstatus": true,
	"top": true, "usersinfo": true, "rolesinfo": true, "getparameter": true, "getlog": true, "validate": true, "currentop": true,
	"getmore": true, "isdbgrid": true, "features": true,
}
