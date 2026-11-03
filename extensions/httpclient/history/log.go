package history

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// State is the part of extension.State the log uses.
type State interface {
	Get(key string) (json.RawMessage, bool)
	Set(key string, value any) error
}

// The keys of the log in the state of the workspace. A value of the state is at most 64 KiB, so the entries are kept in chunks of
// ChunkSize, each under the key StateKey + "." + the number of the chunk (the entry with the sequence number n is in chunk n/ChunkSize),
// and the key StateKey holds the numbers the chunks are counted from.
const (
	// StateKey is the key of the counters of the log.
	StateKey = "httpclient.history"
	// ChunkSize is how many entries one key of the state holds.
	ChunkSize = 10
	// MaxEntryBytes is the most JSON an entry takes; a larger one loses the text of its body, and then its request.
	MaxEntryBytes = 5000
	// DefaultLimit is the number of entries kept when the setting does not say.
	DefaultLimit = 200
)

// Entry is a request that was sent.
type Entry struct {
	// Seq numbers the entries of the workspace from 0 in the order they were sent.
	Seq  int       `json:"seq"`
	Time time.Time `json:"time"`
	// Name is the name of the request when it came from a collection.
	Name   string `json:"name,omitempty"`
	Method string `json:"method"`
	URL    string `json:"url"`
	// Status is the status code, 0 when no response came (Error says why).
	Status     int    `json:"status,omitempty"`
	StatusText string `json:"statusText,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	Size       int64  `json:"size,omitempty"`
	Error      string `json:"error,omitempty"`
	// Request is the request as it was written (its {{variables}} not replaced), with the secrets masked; Body and Variables are the
	// text of its body files, masked the same way. Truncated says that the entry was too large to keep all of it.
	Request   *format.Request `json:"request,omitempty"`
	Body      string          `json:"body,omitempty"`
	Variables string          `json:"variables,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
}

// Sent is what the log is told about a request that was sent.
type Sent struct {
	// Time is when it was sent; the log's clock when zero.
	Time    time.Time
	Request format.Request
	// Body and Variables are the text of the body files of the request.
	Body, Variables string
	Status          int
	StatusText      string
	Duration        time.Duration
	Size            int64
	// Err is why there was no response.
	Err string
}

// counters is what StateKey holds.
type counters struct {
	// Next is the sequence number of the next entry, Low the first chunk that may exist.
	Next int `json:"next"`
	Low  int `json:"low"`
}

// Log is the history of one workspace. It is safe for use from several goroutines.
type Log struct {
	state State
	limit func() int
	now   func() time.Time

	mu sync.Mutex
}

// Options are the optional parts of Open.
type Options struct {
	// Limit says how many entries to keep (the setting httpClient.historyLimit); DefaultLimit when nil. A limit of 0 keeps none.
	Limit func() int
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// Open makes the log over the state of the workspace. It reads nothing.
func Open(state State, opts Options) *Log {
	l := &Log{state: state, limit: opts.Limit, now: opts.Now}
	if l.limit == nil {
		l.limit = func() int { return DefaultLimit }
	}
	if l.now == nil {
		l.now = time.Now
	}
	return l
}

func chunkKey(n int) string { return StateKey + "." + strconv.Itoa(n) }

func (l *Log) counters() counters {
	var c counters
	if raw, ok := l.state.Get(StateKey); ok {
		if json.Unmarshal(raw, &c) != nil {
			c = counters{}
		}
	}
	return c
}

func (l *Log) chunk(n int) []Entry {
	raw, ok := l.state.Get(chunkKey(n))
	if !ok {
		return nil
	}
	var entries []Entry
	if json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	return entries
}

// Limit is the number of entries the log keeps now.
func (l *Log) Limit() int { return max(0, l.limit()) }

// Add masks the request, makes an entry of it and keeps it, dropping the entries past the limit. It returns the entry, or the zero
// Entry when the limit is 0. It fails with HttpHistoryEntryInvalid for a request without a method or a URL and returns the state's
// errors as they are.
func (l *Log) Add(s Sent) (Entry, error) {
	if strings.TrimSpace(s.Request.Method) == "" || strings.TrimSpace(s.Request.URL) == "" {
		return Entry{}, errors.New("HttpHistoryEntryInvalid", "a request in the history needs a method and a URL", "method", s.Request.Method)
	}
	limit := l.Limit()
	if limit == 0 {
		return Entry{}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.counters()
	req := MaskRequest(s.Request)
	e := Entry{
		Seq: c.Next, Time: s.Time, Name: req.Name, Method: strings.ToUpper(strings.TrimSpace(req.Method)), URL: req.URL,
		Status: s.Status, StatusText: s.StatusText, DurationMs: s.Duration.Milliseconds(), Size: s.Size, Error: s.Err,
		Request: &req, Body: MaskBody(bodyKind(req), s.Body), Variables: MaskBody(format.BodyJSON, s.Variables),
	}
	if e.Time.IsZero() {
		e.Time = l.now()
	}
	e.Time = e.Time.UTC()
	for _, step := range []func(*Entry){func(*Entry) {}, func(x *Entry) { x.Body, x.Variables, x.Truncated = "", "", true }, func(x *Entry) { x.Request = nil }} {
		step(&e)
		if data, err := json.Marshal(e); err == nil && len(data) <= MaxEntryBytes {
			break
		}
	}
	chunk := append(l.chunk(e.Seq/ChunkSize), e)
	if err := l.state.Set(chunkKey(e.Seq/ChunkSize), chunk); err != nil {
		return Entry{}, err
	}
	c.Next++
	keepFrom := max(0, c.Next-limit)
	for n := c.Low; n < keepFrom/ChunkSize; n++ {
		if err := l.state.Set(chunkKey(n), nil); err != nil {
			return e, err
		}
		c.Low = n + 1
	}
	return e, l.state.Set(StateKey, c)
}

// bodyKind is the kind of the body of a request.
func bodyKind(r format.Request) string {
	if r.Body == nil {
		return format.BodyNone
	}
	return r.Body.Type
}

// List returns the entries, the newest first, at most the limit of them.
func (l *Log) List() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.counters()
	limit := l.Limit()
	var out []Entry
	for n := c.Next - 1; n >= c.Low*ChunkSize && n >= c.Next-limit; {
		chunk := l.chunk(n / ChunkSize)
		for i := len(chunk) - 1; i >= 0 && len(out) < limit; i-- {
			if chunk[i].Seq <= n {
				out = append(out, chunk[i])
			}
		}
		n = (n/ChunkSize)*ChunkSize - 1
	}
	return out
}

// Get returns the entry with the sequence number.
func (l *Log) Get(seq int) (Entry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.chunk(seq / ChunkSize) {
		if e.Seq == seq {
			return e, true
		}
	}
	return Entry{}, false
}

// Clear forgets every entry. The numbers go on from where they were.
func (l *Log) Clear() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.counters()
	for n := c.Low; n <= c.Next/ChunkSize; n++ {
		if err := l.state.Set(chunkKey(n), nil); err != nil {
			return err
		}
	}
	return l.state.Set(StateKey, counters{Next: c.Next, Low: c.Next / ChunkSize})
}
