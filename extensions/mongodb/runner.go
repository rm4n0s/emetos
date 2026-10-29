package mongodb

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/emetos/extensions/mongodb/playground"
)

// maxSetDocs is the most documents a result set loads; "More" stops there (SPEC §10.22 "Results": nothing beyond what is shown, and
// what is shown is bounded).
const maxSetDocs = 10000

// RunItem is a statement of a playground that is about to run, with the text it was read from.
type RunItem struct {
	// Text is the source of the statement, for the Messages tab.
	Text string
	// Span is where it is in the playground, for the place of an error.
	Span playground.Span
	// Statement is the statement the parser made.
	Statement playground.Statement
}

// RunError is why a statement failed.
type RunError struct {
	// Message is what the server or Emetos said.
	Message string
	// Code and CodeName are those of the server's error; zero and empty when the failure was not the server's.
	Code     int
	CodeName string
	// Tag is the tag of the error of Emetos that carried it (MongoRun*, MongoReadFailed...).
	Tag string
}

// Text writes the error for a person: "Error 11000 (DuplicateKey): E11000 duplicate key ...", or the message alone.
func (e *RunError) Text() string {
	if e.Code == 0 {
		return e.Message
	}
	head := "Error " + strconv.Itoa(e.Code)
	if e.CodeName != "" {
		head += " (" + e.CodeName + ")"
	}
	return head + ": " + e.Message
}

// DocSet is a result of documents: those read so far, and the cursor that reads more.
type DocSet struct {
	// Title is the name of the call that made it ("orders.find").
	Title string
	// Call is the call, for fetching more and for deciding whether its documents can be edited.
	Call Call
	// Docs are the documents read so far.
	Docs []bson.D
	// Cursor reads more documents while it is not nil.
	Cursor Cursor
	// PageSize is how many documents a read takes.
	PageSize int
	// Editable says that the documents are whole documents of Call.Database.Call.Collection, so that they can be edited and deleted.
	Editable bool
	// Truncated says that reading stopped at maxSetDocs.
	Truncated bool
	// OnDrop, when set, is called with the cursor when the set lets go of it (it ran dry, or the set was released), so that its
	// owner stops keeping track of it.
	OnDrop func(Cursor)
}

// More tells whether a read can bring more documents.
func (s *DocSet) More() bool { return s.Cursor != nil && !s.Truncated }

// Release closes the cursor of the set, in the background, and forgets it.
func (s *DocSet) Release() {
	c := s.Cursor
	s.Cursor = nil
	if c != nil {
		if s.OnDrop != nil {
			s.OnDrop(c)
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := c.Close(ctx); err != nil {
				slog.Debug("a cursor did not close", "err", err)
			}
		}()
	}
}

// Append adds the documents a read brought; done says the cursor has no more.
func (s *DocSet) Append(docs []bson.D, done bool) {
	room := maxSetDocs - len(s.Docs)
	if len(docs) > room {
		docs = docs[:max(room, 0)]
		s.Truncated = true
	}
	s.Docs = append(s.Docs, docs...)
	if len(s.Docs) >= maxSetDocs && !done {
		s.Truncated = true
	}
	if done && s.Cursor != nil {
		c := s.Cursor
		s.Cursor = nil
		if s.OnDrop != nil {
			s.OnDrop(c)
		}
	}
}

// StepResult is what one statement of a run gave.
type StepResult struct {
	Item RunItem
	// Database is the database the statement ran on.
	Database string
	// Use is the database a "use" statement selected.
	Use string
	// Call is the call that was made; nil for "use" and for a statement that could not be translated.
	Call *Call
	// Sets are the documents the statement returned: one set for a call that has documents, none otherwise.
	Sets []*DocSet
	// Write says what a call that writes did.
	Write *WriteSummary
	// Message is the line the Messages tab shows when the statement worked.
	Message string
	// Err is why the statement failed; Cancelled that the user stopped it.
	Err       *RunError
	Cancelled bool
	Elapsed   time.Duration
}

// RunOptions say how a run goes.
type RunOptions struct {
	// Database is the database the first statement runs on; DefaultDatabase when empty.
	Database string
	// PageSize is how many documents a read takes.
	PageSize int
	// Started is called before statement i begins; Finished with its result. Both are called on the goroutine of the run.
	Started  func(i int, item RunItem)
	Finished func(res *StepResult)
}

// Run does the statements one after the other on the server, stopping at the first one that fails or is cancelled. It returns the
// results of the statements that were done. ctx ends the run: the statement that is running is cancelled.
func Run(ctx context.Context, b Backend, items []RunItem, opts RunOptions) []*StepResult {
	db := opts.Database
	if db == "" {
		db = DefaultDatabase
	}
	var results []*StepResult
	for i, item := range items {
		if ctx.Err() != nil {
			break
		}
		if opts.Started != nil {
			opts.Started(i, item)
		}
		res := runOne(ctx, b, item, db, opts.PageSize)
		if res.Use != "" {
			db = res.Use
		}
		results = append(results, res)
		if opts.Finished != nil {
			opts.Finished(res)
		}
		if res.Err != nil || res.Cancelled {
			break
		}
	}
	return results
}

// runOne translates and does one statement.
func runOne(ctx context.Context, b Backend, item RunItem, db string, pageSize int) *StepResult {
	res := &StepResult{Item: item, Database: db}
	start := time.Now()
	defer func() { res.Elapsed = time.Since(start) }()
	step, err := Translate(item.Statement, db)
	if err != nil {
		res.Err = runErrorOf(err)
		return res
	}
	if step.Call == nil {
		res.Use = step.Use
		res.Database = step.Use
		res.Message = "switched to database " + step.Use
		return res
	}
	call := step.Call
	res.Call = call
	reply, err := b.Exec(ctx, *call, pageSize)
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			res.Cancelled = true
			res.Err = &RunError{Message: "The statement was cancelled."}
			return res
		}
		res.Err = runErrorOf(err)
		return res
	}
	res.Write = reply.Write
	switch {
	case reply.Write != nil:
		res.Message = writeText(*reply.Write)
		res.Sets = []*DocSet{newSet(*call, reply, pageSize)}
	case len(reply.Docs) > 0 || reply.Cursor != nil:
		set := newSet(*call, reply, pageSize)
		res.Sets = []*DocSet{set}
		res.Message = documentsText(len(set.Docs), set.More()) + " returned"
	default:
		res.Message = "No documents returned"
	}
	return res
}

// newSet makes the set of documents a reply holds.
func newSet(call Call, reply Reply, pageSize int) *DocSet {
	return &DocSet{Title: call.Title(), Call: call, Docs: reply.Docs, Cursor: reply.Cursor, PageSize: pageSize, Editable: call.Editable()}
}

// runErrorOf makes the error of a statement from the error of the call.
func runErrorOf(err error) *RunError {
	e := &RunError{Message: err.Error()}
	if appErr, ok := errors.FromError(err); ok {
		e.Tag = appErr.Tag
		e.Code, e.CodeName = ServerError(err)
	}
	return e
}

// documentsText writes a count of documents: "1 document", "20 documents", "20+ documents" when there are more.
func documentsText(n int, more bool) string {
	s := groupDigits(n)
	if more {
		s += "+"
	}
	if n == 1 && !more {
		return s + " document"
	}
	return s + " documents"
}

// writeText writes what a write did: "1 inserted", "2 matched, 1 modified", "3 deleted".
func writeText(w WriteSummary) string {
	var parts []string
	add := func(n int64, what string) {
		if n > 0 {
			parts = append(parts, groupDigits(int(n))+" "+what)
		}
	}
	add(w.Inserted, "inserted")
	add(w.Matched, "matched")
	add(w.Modified, "modified")
	add(w.Upserted, "upserted")
	add(w.Deleted, "deleted")
	if len(parts) == 0 {
		return "0 documents changed"
	}
	return strings.Join(parts, ", ")
}

// groupDigits writes a whole number with a comma between the thousands.
func groupDigits(n int) string {
	s := strconv.Itoa(n)
	if n < 1000 {
		return s
	}
	var b strings.Builder
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
